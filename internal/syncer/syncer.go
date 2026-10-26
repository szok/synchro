// Package syncer implements file operations shared by full synchronization and watching.
package syncer

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	pathpkg "path"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/pkg/sftp"
	"github.com/szok/synchro/internal/config"
	"github.com/szok/synchro/internal/paths"
	"github.com/szok/synchro/internal/sftpclient"
)

// Logger receives operation results for terminal presentation.
type Logger interface {
	Upload(string)
	Download(string)
	Unchanged(string)
	Delete(string)
	Mkdir(string)
	Rmdir(string)
	Error(string)
	SyncAllStart(total, workers int)
	SyncAllDone(uploaded, total int)
}

// Syncer schedules SFTP operations for one configuration.
type Syncer struct {
	cfg       config.Config
	run       func(sftpclient.Operation)
	log       Logger
	localRoot string
	filter    *paths.Filter
}

// New creates a Syncer; filter decides which files SyncAll leaves out.
func New(cfg config.Config, filter *paths.Filter, run func(sftpclient.Operation), log Logger) (*Syncer, error) {
	root, err := cfg.LocalDirectory()
	if err != nil {
		return nil, err
	}
	return &Syncer{cfg: cfg, run: run, log: log, localRoot: root, filter: filter}, nil
}

func (s *Syncer) remote(local string) string {
	return paths.RemotePath(s.localRoot, s.cfg.RemoteDirectory, local)
}
func (s *Syncer) relative(local string) string {
	rel, err := filepath.Rel(s.localRoot, local)
	if err != nil {
		return local
	}
	return filepath.ToSlash(rel)
}

// downloadedNanos marks a file written by Download: its mtime is the remote
// file's (SFTP carries whole seconds) plus this fraction of a second. Any later
// local write replaces the mtime, so a file still carrying the mark is exactly
// what was downloaded and Upload does not send it back. Filesystems with
// coarser timestamps drop the mark; such files are simply uploaded again.
const downloadedNanos = 500_000_000

// Upload uploads a local file after creating its remote parent directory.
// It reports whether the file was uploaded successfully. A file left as
// Download wrote it, and still the same on the server, is not sent again.
func (s *Syncer) Upload(local string) bool {
	remote := s.remote(local)
	success := false
	s.run(func(client *sftp.Client) error {
		if info, err := os.Stat(local); err == nil && info.ModTime().Nanosecond() == downloadedNanos {
			if remoteInfo, err := client.Stat(remote); err == nil && matchesDownload(info, remoteInfo) {
				s.log.Unchanged(s.relative(local))
				success = true
				return nil
			}
		}
		if err := MkdirAll(client, pathpkg.Dir(remote)); err != nil {
			s.log.Error(fmt.Sprintf("mkdir error: %v", err))
			return nil
		}
		if err := upload(client, local, remote); err != nil {
			if isNotExist(err) {
				return nil
			}
			s.log.Error(fmt.Sprintf("Upload failed %s: %v", s.relative(local), err))
			return nil
		}
		s.log.Upload(s.relative(local))
		success = true
		return nil
	})
	return success
}

// matchesDownload reports whether local, carrying the download mark, still
// matches remote: same size and the same mtime to the second.
func matchesDownload(local, remote os.FileInfo) bool {
	return local.ModTime().Nanosecond() == downloadedNanos && remote.Mode().IsRegular() &&
		local.Size() == remote.Size() && local.ModTime().Unix() == remote.ModTime().Unix()
}

// Download replaces a local file with its remote copy and reports whether it
// succeeded. See download for how the file is written.
func (s *Syncer) Download(local string) bool {
	remote := s.remote(local)
	success := false
	s.run(func(client *sftp.Client) error {
		if err := download(client, remote, local); err != nil {
			if isNotExist(err) {
				s.log.Error(fmt.Sprintf("Download failed %s: %s does not exist on the server", s.relative(local), remote))
				return nil
			}
			s.log.Error(fmt.Sprintf("Download failed %s: %v", s.relative(local), err))
			return nil
		}
		s.log.Download(s.relative(local))
		success = true
		return nil
	})
	return success
}

func (s *Syncer) DeleteFile(local string) {
	remote := s.remote(local)
	s.run(func(client *sftp.Client) error {
		if err := client.Remove(remote); err != nil {
			if isNotExist(err) {
				return nil
			}
			s.log.Error(fmt.Sprintf("Delete failed %s: %v", remote, err))
			return nil
		}
		s.log.Delete(s.relative(local))
		return nil
	})
}
func (s *Syncer) CreateDir(local string) {
	remote := s.remote(local)
	s.run(func(client *sftp.Client) error {
		if err := MkdirAll(client, remote); err != nil {
			s.log.Error(fmt.Sprintf("Mkdir failed %s: %v", remote, err))
			return nil
		}
		s.log.Mkdir(s.relative(local))
		return nil
	})
}
func (s *Syncer) DeleteDir(local string) {
	remote := s.remote(local)
	s.run(func(client *sftp.Client) error {
		if err := client.RemoveDirectory(remote); err != nil {
			if isNotExist(err) {
				return nil
			}
			// Servers answer a bare SSH_FX_FAILURE for a directory that is not empty.
			if entries, readErr := client.ReadDir(remote); readErr == nil && len(entries) > 0 {
				s.log.Error(fmt.Sprintf("Rmdir skipped %s: remote directory still holds %d entries that were not synced from here", remote, len(entries)))
				return nil
			}
			s.log.Error(fmt.Sprintf("Rmdir failed %s: %v", remote, err))
			return nil
		}
		s.log.Rmdir(s.relative(local))
		return nil
	})
}

// SyncAll uploads every included regular file with bounded parallelism.
// Cancelling ctx stops scheduling new uploads; uploads already running finish.
func (s *Syncer) SyncAll(ctx context.Context) {
	files, err := CollectFiles(s.localRoot, s.filter)
	if err != nil {
		s.log.Error(fmt.Sprintf("Read local directory: %v", err))
		return
	}
	workers := s.workers(len(files))
	s.log.SyncAllStart(len(files), workers)

	uploaded := parallelUpload(ctx, files, workers, s.Upload)
	s.log.SyncAllDone(uploaded, len(files))
}

// UploadFiles uploads files with the configured parallelism and returns how
// many succeeded. Cancelling ctx stops scheduling new uploads.
func (s *Syncer) UploadFiles(ctx context.Context, files []string) int {
	return parallelUpload(ctx, files, s.workers(len(files)), s.Upload)
}

// workers returns the configured upload parallelism, capped at files.
func (s *Syncer) workers(files int) int {
	workers := s.cfg.Concurrency
	if workers == 0 {
		workers = config.DefaultConcurrency
	}
	return min(workers, files)
}

// ResolveTargets expands the files and directories named on the command line
// into the regular files to upload, without duplicates. Every target must lie
// inside root and must not be excluded; excluded entries inside a target
// directory are skipped, as in a full sync.
func ResolveTargets(root string, filter *paths.Filter, targets []string) ([]string, error) {
	var files []string
	seen := map[string]bool{}
	for _, target := range targets {
		absolute, err := filepath.Abs(target)
		if err != nil {
			return nil, err
		}
		rel, ok := within(root, absolute)
		if !ok {
			return nil, fmt.Errorf("%s is outside the synced directory %s", target, root)
		}
		info, err := os.Stat(absolute)
		if err != nil {
			return nil, err
		}
		var found []string
		switch {
		case rel != "." && filter.Excluded(absolute, info.IsDir()):
			return nil, fmt.Errorf("%s is excluded from syncing (\"exclude\" or .gitignore)", target)
		case info.IsDir():
			if found, err = CollectFiles(absolute, filter); err != nil {
				return nil, err
			}
		case info.Mode().IsRegular():
			found = []string{absolute}
		default:
			return nil, fmt.Errorf("%s is not a regular file or directory", target)
		}
		for _, file := range found {
			if !seen[file] {
				seen[file] = true
				files = append(files, file)
			}
		}
	}
	return files, nil
}

// ResolveDownloads checks the files named on the command line for download
// and returns their absolute paths, without duplicates. Every target must lie
// inside root and must not be excluded; it need not exist locally yet, but
// must not be a directory.
func ResolveDownloads(root string, filter *paths.Filter, targets []string) ([]string, error) {
	var files []string
	seen := map[string]bool{}
	for _, target := range targets {
		absolute, err := filepath.Abs(target)
		if err != nil {
			return nil, err
		}
		if rel, ok := within(root, absolute); !ok || rel == "." {
			return nil, fmt.Errorf("%s is not a file inside the synced directory %s", target, root)
		}
		info, err := os.Stat(absolute)
		switch {
		case err == nil && info.IsDir():
			return nil, fmt.Errorf("%s is a directory; only files can be downloaded", target)
		case err == nil && !info.Mode().IsRegular():
			return nil, fmt.Errorf("%s is not a regular file", target)
		case err != nil && !isNotExist(err):
			return nil, err
		case filter.Excluded(absolute, false):
			return nil, fmt.Errorf("%s is excluded from syncing (\"exclude\" or .gitignore)", target)
		}
		if !seen[absolute] {
			seen[absolute] = true
			files = append(files, absolute)
		}
	}
	return files, nil
}

// within returns path relative to root and whether path is root or lies below it.
func within(root, path string) (string, bool) {
	rel, err := filepath.Rel(root, path)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return rel, false
	}
	return rel, true
}

// parallelUpload runs upload for each file with bounded parallelism and
// returns how many uploads reported success. It stops handing out files once
// ctx is cancelled and waits for the uploads already in progress.
func parallelUpload(ctx context.Context, files []string, workers int, upload func(string) bool) int {
	jobs := make(chan string)
	var group sync.WaitGroup
	var succeeded atomic.Int64
	for range workers {
		group.Add(1)
		go func() {
			defer group.Done()
			for file := range jobs {
				if upload(file) {
					succeeded.Add(1)
				}
			}
		}()
	}
dispatch:
	for _, file := range files {
		if ctx.Err() != nil {
			break
		}
		select {
		case jobs <- file:
		case <-ctx.Done():
			break dispatch
		}
	}
	close(jobs)
	group.Wait()
	return int(succeeded.Load())
}

// CollectFiles recursively returns regular files that are not excluded.
func CollectFiles(root string, filter *paths.Filter) ([]string, error) {
	var files []string
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if path == root {
			return nil
		}
		if filter.Excluded(path, entry.IsDir()) {
			if entry.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if entry.Type().IsRegular() {
			files = append(files, path)
		}
		return nil
	})
	return files, err
}

// mkdirClient is deliberately small to make remote directory creation unit-testable.
type mkdirClient interface {
	Mkdir(string) error
	Stat(string) (os.FileInfo, error)
}

// MkdirAll creates each absolute remote path segment, ignoring existing directories.
func MkdirAll(client mkdirClient, remoteDirectory string) error {
	parts := strings.FieldsFunc(pathpkg.Clean(remoteDirectory), func(r rune) bool { return r == '/' })
	current := ""
	for _, part := range parts {
		current += "/" + part
		info, err := client.Stat(current)
		if err == nil {
			if !info.IsDir() {
				return fmt.Errorf("remote path is not a directory: %s", current)
			}
			continue
		}

		if err := client.Mkdir(current); err == nil {
			continue
		} else {
			mkdirErr := err
			// Some SFTP servers return SSH_FX_FAILURE, rather than an
			// already-exists status, when another client has created the directory.
			// Confirm its existence before treating that response as a failure.
			info, statErr := client.Stat(current)
			if statErr == nil && info.IsDir() {
				continue
			}
			return mkdirErr
		}
	}
	return nil
}

func isNotExist(err error) bool { return errors.Is(err, os.ErrNotExist) }

func upload(client *sftp.Client, local, remote string) error {
	source, err := os.Open(local)
	if err != nil {
		return err
	}
	defer source.Close()
	destination, err := client.Create(remote)
	if err != nil {
		return err
	}
	_, copyErr := destination.ReadFrom(source)
	closeErr := destination.Close()
	if copyErr != nil {
		return copyErr
	}
	return closeErr
}

// download writes remote to local through a temporary file next to it, so an
// interrupted transfer never leaves a truncated file. The temporary name
// contains ".tmp." and is therefore ignored by the watcher. The result keeps
// local's permissions (or takes remote's for a new file) and gets the remote
// mtime with the download mark (see downloadedNanos).
func download(client *sftp.Client, remote, local string) error {
	source, err := client.Open(remote)
	if err != nil {
		return err
	}
	defer source.Close()
	info, err := source.Stat()
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("remote path is not a regular file: %s", remote)
	}
	mode := info.Mode().Perm()
	if existing, err := os.Stat(local); err == nil {
		mode = existing.Mode().Perm()
	}
	if err := os.MkdirAll(filepath.Dir(local), 0o755); err != nil {
		return err
	}
	temp, err := os.CreateTemp(filepath.Dir(local), "."+filepath.Base(local)+".synchro.tmp.*")
	if err != nil {
		return err
	}
	// Removing fails harmlessly once the file has been renamed into place.
	defer os.Remove(temp.Name())
	_, copyErr := source.WriteTo(temp)
	closeErr := temp.Close()
	if copyErr != nil {
		return copyErr
	}
	if closeErr != nil {
		return closeErr
	}
	if err := os.Chmod(temp.Name(), mode); err != nil {
		return err
	}
	modified := info.ModTime().Truncate(time.Second).Add(downloadedNanos)
	if err := os.Chtimes(temp.Name(), time.Now(), modified); err != nil {
		return err
	}
	return os.Rename(temp.Name(), local)
}
