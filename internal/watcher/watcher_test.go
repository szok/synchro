package watcher

import (
	"context"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"testing"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/szok/synchro/internal/paths"
)

type fakeOperations struct {
	mu                               sync.Mutex
	uploads, deletes, mkdirs, rmdirs []string
}

func (f *fakeOperations) Upload(p string) bool { f.add(&f.uploads, p); return true }
func (f *fakeOperations) DeleteFile(p string)  { f.add(&f.deletes, p) }
func (f *fakeOperations) CreateDir(p string)   { f.add(&f.mkdirs, p) }
func (f *fakeOperations) DeleteDir(p string)   { f.add(&f.rmdirs, p) }
func (f *fakeOperations) add(list *[]string, p string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	*list = append(*list, p)
}

// sortedUploads returns a sorted copy of the uploads so far.
func (f *fakeOperations) sortedUploads() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	uploads := append([]string(nil), f.uploads...)
	sort.Strings(uploads)
	return uploads
}

type fakeLogger struct {
	mu      sync.Mutex
	skipped []string
}

func (*fakeLogger) Change(string, string) {}
func (*fakeLogger) Error(string)          {}
func (l *fakeLogger) Skipped(path string, _ int) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.skipped = append(l.skipped, path)
}
func (l *fakeLogger) skippedPaths() []string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]string(nil), l.skipped...)
}

// testWatch returns a watch over root with no filter and the default options.
func testWatch(t *testing.T, root string, op *fakeOperations) *watch {
	t.Helper()
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { watcher.Close() })
	return newWatch(watcher, root, Options{}, op, &fakeLogger{})
}

func TestIsEditorBackup(t *testing.T) {
	for _, name := range []string{
		"/project/index.go~",
		"/project/.index.go.swp",
		"/project/.index.go.swo",
		"/project/.#index.go",
		"/project/#index.go#",
		"/project/OneSignalProvider.js.tmp.30851.36f8cb269703",
	} {
		if !isEditorBackup(name) {
			t.Fatalf("backup file %q was not recognized", name)
		}
	}
	if isEditorBackup("/project/index.go") || isEditorBackup("/project/tmp.notes.txt") {
		t.Fatal("regular file was recognized as a backup")
	}
}

func TestHandleMapsWriteToUpload(t *testing.T) {
	op := &fakeOperations{}
	testWatch(t, "/project", op).handle(fsnotify.Event{Name: "/project/index.go", Op: fsnotify.Write})
	if len(op.uploads) != 1 || op.uploads[0] != "/project/index.go" {
		t.Fatalf("uploads=%v", op.uploads)
	}
}
func TestHandleMapsRemoveToDelete(t *testing.T) {
	op := &fakeOperations{}
	testWatch(t, "/project", op).handle(fsnotify.Event{Name: "/project/old.go", Op: fsnotify.Remove})
	if len(op.deletes) != 1 || op.deletes[0] != "/project/old.go" {
		t.Fatalf("deletes=%v", op.deletes)
	}
}

func TestHandleMapsRemovedWatchedDirectoryToRemoveDirectory(t *testing.T) {
	dir := t.TempDir()
	child := filepath.Join(dir, "removed")
	if err := os.Mkdir(child, 0755); err != nil {
		t.Fatal(err)
	}
	op := &fakeOperations{}
	w := testWatch(t, dir, op)
	if err := w.addTree(child); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(child); err != nil {
		t.Fatal(err)
	}
	w.handle(fsnotify.Event{Name: child, Op: fsnotify.Remove})
	if len(op.rmdirs) != 1 || op.rmdirs[0] != child {
		t.Fatalf("rmdirs=%v", op.rmdirs)
	}
}
func TestHandleMapsCreatedFileToUpload(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "new.go")
	if err := os.WriteFile(file, []byte("x"), 0600); err != nil {
		t.Fatal(err)
	}
	op := &fakeOperations{}
	testWatch(t, dir, op).handle(fsnotify.Event{Name: file, Op: fsnotify.Create})
	if len(op.uploads) != 1 || op.uploads[0] != file {
		t.Fatalf("uploads=%v", op.uploads)
	}
}

// startWatch runs Watch over a fresh temp dir with a short settle delay.
func startWatch(t *testing.T, options Options) (string, *fakeOperations, *fakeLogger) {
	t.Helper()
	previous := settleDelay
	settleDelay = 100 * time.Millisecond
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if options.Filter == nil {
		options.Filter = paths.NewFilter(root, nil, nil)
	}
	op, log := &fakeOperations{}, &fakeLogger{}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		if err := Watch(ctx, root, options, op, log); err != nil {
			t.Error(err)
		}
	}()
	t.Cleanup(func() {
		cancel()
		<-done
		settleDelay = previous
	})
	time.Sleep(100 * time.Millisecond) // let Watch add its watches
	return root, op, log
}

// writeFiles creates dir/name for every name, with parent directories.
func writeFiles(t *testing.T, dir string, names ...string) {
	t.Helper()
	for _, name := range names {
		path := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

// eventually polls check until it returns true or a second passes.
func eventually(t *testing.T, what string, check func() bool) {
	t.Helper()
	for deadline := time.Now().Add(2 * time.Second); time.Now().Before(deadline); time.Sleep(20 * time.Millisecond) {
		if check() {
			return
		}
	}
	t.Fatal(what)
}

func TestWatchUploadsEverythingInANewDirectory(t *testing.T) {
	root, op, _ := startWatch(t, Options{NewDirectoryLimit: 10})
	// Written right after mkdir: some files exist before the watch is added.
	writeFiles(t, root, "pkg/a.js", "pkg/lib/b.js", "pkg/lib/deep/c.js")
	want := []string{
		filepath.Join(root, "pkg", "a.js"),
		filepath.Join(root, "pkg", "lib", "b.js"),
		filepath.Join(root, "pkg", "lib", "deep", "c.js"),
	}
	eventually(t, "new directory was not uploaded", func() bool { return len(op.sortedUploads()) >= len(want) })
	time.Sleep(200 * time.Millisecond) // no duplicate uploads afterwards
	if got := op.sortedUploads(); !equal(got, want) {
		t.Fatalf("uploads = %v, want %v", got, want)
	}
}

func TestWatchSkipsANewDirectoryOverTheLimit(t *testing.T) {
	root, op, log := startWatch(t, Options{NewDirectoryLimit: 3})
	writeFiles(t, root, "node_modules/a/1.js", "node_modules/a/2.js", "node_modules/b/3.js", "node_modules/b/4.js")
	eventually(t, "node_modules was not skipped", func() bool { return len(log.skippedPaths()) == 1 })
	if got := log.skippedPaths(); got[0] != "node_modules" {
		t.Fatalf("skipped = %v", got)
	}
	// Later changes inside the skipped directory stay ignored, others are synced.
	writeFiles(t, root, "node_modules/c/5.js", "index.js")
	eventually(t, "index.js was not uploaded", func() bool { return len(op.sortedUploads()) > 0 })
	time.Sleep(300 * time.Millisecond)
	if got := op.sortedUploads(); !equal(got, []string{filepath.Join(root, "index.js")}) {
		t.Fatalf("uploads = %v", got)
	}
	if got := log.skippedPaths(); len(got) != 1 {
		t.Fatalf("skipped = %v, want node_modules once", got)
	}
}

func TestWatchSkipsAMovedInDirectoryOverTheLimit(t *testing.T) {
	root, op, log := startWatch(t, Options{NewDirectoryLimit: 2})
	staging := t.TempDir()
	writeFiles(t, staging, "big/1", "big/2", "big/3")
	if err := os.Rename(filepath.Join(staging, "big"), filepath.Join(root, "big")); err != nil {
		t.Fatal(err)
	}
	eventually(t, "big was not skipped", func() bool { return len(log.skippedPaths()) == 1 })
	if got := op.sortedUploads(); len(got) != 0 {
		t.Fatalf("uploads = %v", got)
	}
}

func TestWatchLeavesOutGitignoredDirectories(t *testing.T) {
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	filter := paths.NewFilter(root, nil, paths.ParseGitignore("node_modules/\n"))
	w := newWatch(nil, root, Options{Filter: filter}, &fakeOperations{}, &fakeLogger{})
	writeFiles(t, root, "node_modules/a.js", "src/b.js")
	dirs, files, err := w.scan(root)
	if err != nil {
		t.Fatal(err)
	}
	if !equal(dirs, []string{root, filepath.Join(root, "src")}) || !equal(files, []string{filepath.Join(root, "src", "b.js")}) {
		t.Fatalf("dirs = %v, files = %v", dirs, files)
	}
}

func equal(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
