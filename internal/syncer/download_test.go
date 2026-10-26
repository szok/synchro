package syncer

import (
	"context"
	"net"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/pkg/sftp"
	"github.com/szok/synchro/internal/config"
	"github.com/szok/synchro/internal/paths"
	"github.com/szok/synchro/internal/sftpclient"
	"github.com/szok/synchro/internal/watcher"
)

type recordingLogger struct {
	mu     sync.Mutex
	events []string
}

func (l *recordingLogger) add(event, s string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.events = append(l.events, event+" "+s)
}
func (l *recordingLogger) Upload(s string)       { l.add("upload", s) }
func (l *recordingLogger) Download(s string)     { l.add("download", s) }
func (l *recordingLogger) Unchanged(s string)    { l.add("unchanged", s) }
func (l *recordingLogger) Delete(s string)       { l.add("delete", s) }
func (l *recordingLogger) Mkdir(s string)        { l.add("mkdir", s) }
func (l *recordingLogger) Rmdir(s string)        { l.add("rmdir", s) }
func (l *recordingLogger) Error(s string)        { l.add("error", s) }
func (l *recordingLogger) SyncAllStart(int, int) {}
func (l *recordingLogger) SyncAllDone(int, int)  {}
func (l *recordingLogger) Change(string, string) {}
func (l *recordingLogger) Skipped(string, int)   {}

// all returns every event so far.
func (l *recordingLogger) all() []string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]string(nil), l.events...)
}

// last returns the most recent event and forgets all of them.
func (l *recordingLogger) last() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	if len(l.events) == 0 {
		return ""
	}
	last := l.events[len(l.events)-1]
	l.events = nil
	return last
}

// newPipeSyncer returns a Syncer from a local to a "remote" temporary
// directory, served by an in-process SFTP server on the real filesystem.
func newPipeSyncer(t *testing.T) (s *Syncer, local, remote string, log *recordingLogger) {
	t.Helper()
	local, remote = t.TempDir(), t.TempDir()
	serverConn, clientConn := net.Pipe()
	server, err := sftp.NewServer(serverConn)
	if err != nil {
		t.Fatal(err)
	}
	go server.Serve()
	client, err := sftp.NewClientPipe(clientConn, clientConn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		client.Close()
		server.Close()
	})
	log = &recordingLogger{}
	cfg := config.Config{Directory: local, RemoteDirectory: filepath.ToSlash(remote)}
	s, err = New(cfg, paths.NewFilter(local, nil, nil), func(operation sftpclient.Operation) {
		if err := operation(client); err != nil {
			t.Errorf("operation: %v", err)
		}
	}, log)
	if err != nil {
		t.Fatal(err)
	}
	return s, local, remote, log
}

func writeFile(t *testing.T, path, content string, mode os.FileMode) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), mode); err != nil {
		t.Fatal(err)
	}
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(content)
}

func TestDownloadReplacesLocalFileAndMarksIt(t *testing.T) {
	s, local, remote, log := newPipeSyncer(t)
	writeFile(t, filepath.Join(remote, "src", "app.go"), "remote", 0o644)
	remoteTime := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	if err := os.Chtimes(filepath.Join(remote, "src", "app.go"), remoteTime, remoteTime); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(local, "src", "app.go")
	writeFile(t, file, "local, longer", 0o600)

	if !s.Download(file) {
		t.Fatalf("Download failed: %s", log.last())
	}
	if got := log.last(); got != "download src/app.go" {
		t.Fatalf("log = %q", got)
	}
	if got := readFile(t, file); got != "remote" {
		t.Fatalf("content = %q", got)
	}
	info, err := os.Stat(file)
	if err != nil {
		t.Fatal(err)
	}
	if want := remoteTime.Add(downloadedNanos); !info.ModTime().Equal(want) {
		t.Fatalf("mtime = %v, want %v", info.ModTime(), want)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("mode = %v, want the local file's 0600", info.Mode().Perm())
	}
	entries, err := os.ReadDir(filepath.Dir(file))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Fatalf("directory holds %d entries, want only app.go (no temporary file left)", len(entries))
	}
}

func TestDownloadCreatesMissingLocalFile(t *testing.T) {
	s, local, remote, log := newPipeSyncer(t)
	writeFile(t, filepath.Join(remote, "new", "file.txt"), "hello", 0o640)
	file := filepath.Join(local, "new", "file.txt")
	if !s.Download(file) {
		t.Fatalf("Download failed: %s", log.last())
	}
	if got := readFile(t, file); got != "hello" {
		t.Fatalf("content = %q", got)
	}
}

func TestDownloadOfMissingRemoteFileKeepsLocalFile(t *testing.T) {
	s, local, _, log := newPipeSyncer(t)
	file := filepath.Join(local, "app.go")
	writeFile(t, file, "local", 0o644)
	if s.Download(file) {
		t.Fatal("Download reported success for a file missing on the server")
	}
	if got := log.last(); !strings.HasPrefix(got, "error Download failed app.go:") || !strings.Contains(got, "does not exist on the server") {
		t.Fatalf("log = %q", got)
	}
	if got := readFile(t, file); got != "local" {
		t.Fatalf("content = %q", got)
	}
}

func TestUploadSkipsFileLeftAsDownloaded(t *testing.T) {
	s, local, remote, log := newPipeSyncer(t)
	remoteFile := filepath.Join(remote, "app.go")
	writeFile(t, remoteFile, "remote", 0o644)
	file := filepath.Join(local, "app.go")
	if !s.Download(file) {
		t.Fatalf("Download failed: %s", log.last())
	}
	log.last()

	if !s.Upload(file) {
		t.Fatalf("Upload failed: %s", log.last())
	}
	if got := log.last(); got != "unchanged app.go" {
		t.Fatalf("log = %q, want the upload skipped", got)
	}

	// Once the server's copy changes, the local file no longer matches it.
	writeFile(t, remoteFile, "changed on the server", 0o644)
	if !s.Upload(file) || log.last() != "upload app.go" {
		t.Fatal("a file that no longer matches the server was not uploaded")
	}
	if got := readFile(t, remoteFile); got != "remote" {
		t.Fatalf("remote content = %q", got)
	}
}

func TestUploadSendsDownloadedFileOnceEdited(t *testing.T) {
	s, local, remote, log := newPipeSyncer(t)
	writeFile(t, filepath.Join(remote, "app.go"), "remote", 0o644)
	file := filepath.Join(local, "app.go")
	if !s.Download(file) {
		t.Fatalf("Download failed: %s", log.last())
	}
	// Same size, so only the mtime tells the edit apart.
	writeFile(t, file, "edited", 0o644)
	if !s.Upload(file) || log.last() != "upload app.go" {
		t.Fatal("an edited file was not uploaded")
	}
	if got := readFile(t, filepath.Join(remote, "app.go")); got != "edited" {
		t.Fatalf("remote content = %q", got)
	}
}

func TestResolveDownloadsChecksTargets(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	writeFile(t, filepath.Join(root, "a.txt"), "", 0o644)
	writeFile(t, filepath.Join(root, "dir", "b.txt"), "", 0o644)
	filter := paths.NewFilter(root, []string{"*.log"}, nil)

	files, err := ResolveDownloads(root, filter, []string{
		filepath.Join(root, "a.txt"),
		filepath.Join(root, "missing.txt"),
		filepath.Join(root, "a.txt"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{filepath.Join(root, "a.txt"), filepath.Join(root, "missing.txt")}; strings.Join(files, "|") != strings.Join(want, "|") {
		t.Fatalf("files = %v, want %v", files, want)
	}
	for name, target := range map[string]string{
		"root":      root,
		"directory": filepath.Join(root, "dir"),
		"outside":   filepath.Join(outside, "x.txt"),
		"excluded":  filepath.Join(root, "app.log"),
	} {
		if _, err := ResolveDownloads(root, filter, []string{target}); err == nil {
			t.Errorf("%s: expected an error for %s", name, target)
		}
	}
}

func TestWatcherDoesNotSendDownloadedFileBack(t *testing.T) {
	s, local, remote, log := newPipeSyncer(t)
	writeFile(t, filepath.Join(remote, "app.go"), "remote", 0o644)
	file := filepath.Join(local, "app.go")
	writeFile(t, file, "local", 0o644)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error)
	go func() { done <- watcher.Watch(ctx, local, watcher.Options{NewDirectoryLimit: -1}, s, log) }()
	t.Cleanup(func() {
		cancel()
		if err := <-done; err != nil {
			t.Error(err)
		}
	})
	time.Sleep(200 * time.Millisecond) // let the watch start

	if !s.Download(file) {
		t.Fatalf("Download failed: %v", log.all())
	}
	deadline := time.Now().Add(3 * time.Second)
	for !slices.Contains(log.all(), "unchanged app.go") && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	time.Sleep(500 * time.Millisecond) // removals are held back briefly
	events := log.all()
	if !slices.Contains(events, "unchanged app.go") {
		t.Fatalf("the watcher did not see the download: %v", events)
	}
	for _, event := range events {
		if event != "download app.go" && event != "unchanged app.go" {
			t.Fatalf("unexpected event %q in %v", event, events)
		}
	}
}
