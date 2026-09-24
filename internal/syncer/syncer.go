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

	"github.com/pkg/sftp"
	"github.com/szok/synchro/internal/config"
	"github.com/szok/synchro/internal/paths"
	"github.com/szok/synchro/internal/sftpclient"
)

// Logger receives operation results for terminal presentation.
type Logger interface {
	Upload(string)
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
}

func New(cfg config.Config, run func(sftpclient.Operation), log Logger) (*Syncer, error) {
	root, err := cfg.LocalDirectory()
	if err != nil {
		return nil, err
	}
	return &Syncer{cfg: cfg, run: run, log: log, localRoot: root}, nil
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

// Upload uploads a local file after creating its remote parent directory.
// It reports whether the file was uploaded successfully.
func (s *Syncer) Upload(local string) bool {
	remote := s.remote(local)
	success := false
	s.run(func(client *sftp.Client) error {
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
	files, err := CollectFiles(s.localRoot, s.cfg.Exclude)
	if err != nil {
		s.log.Error(fmt.Sprintf("Read local directory: %v", err))
		return
	}
	workers := s.cfg.Concurrency
	if workers == 0 {
		workers = config.DefaultConcurrency
	}
	if workers > len(files) {
		workers = len(files)
	}
	s.log.SyncAllStart(len(files), workers)

	uploaded := parallelUpload(ctx, files, workers, s.Upload)
	s.log.SyncAllDone(uploaded, len(files))
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
func CollectFiles(root string, exclude []string) ([]string, error) {
	var files []string
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if path == root {
			return nil
		}
		if paths.IsExcluded(path, exclude) {
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
