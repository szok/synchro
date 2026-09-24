package watcher

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/fsnotify/fsnotify"
)

type fakeOperations struct{ uploads, deletes, mkdirs, rmdirs []string }

func (f *fakeOperations) Upload(p string) bool { f.uploads = append(f.uploads, p); return true }
func (f *fakeOperations) DeleteFile(p string)  { f.deletes = append(f.deletes, p) }
func (f *fakeOperations) CreateDir(p string)   { f.mkdirs = append(f.mkdirs, p) }
func (f *fakeOperations) DeleteDir(p string)   { f.rmdirs = append(f.rmdirs, p) }

type fakeLogger struct{}

func (fakeLogger) Change(string, string) {}
func (fakeLogger) Error(string)          {}

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
	handle(nil, "/project", fsnotify.Event{Name: "/project/index.go", Op: fsnotify.Write}, nil, nil, op, fakeLogger{})
	if len(op.uploads) != 1 || op.uploads[0] != "/project/index.go" {
		t.Fatalf("uploads=%v", op.uploads)
	}
}
func TestHandleMapsRemoveToDelete(t *testing.T) {
	op := &fakeOperations{}
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		t.Fatal(err)
	}
	defer watcher.Close()
	handle(watcher, "/project", fsnotify.Event{Name: "/project/old.go", Op: fsnotify.Remove}, nil, nil, op, fakeLogger{})
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
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		t.Fatal(err)
	}
	defer watcher.Close()
	if err := watcher.Add(child); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(child); err != nil {
		t.Fatal(err)
	}
	op := &fakeOperations{}
	handle(watcher, dir, fsnotify.Event{Name: child, Op: fsnotify.Remove}, nil, map[string]struct{}{child: {}}, op, fakeLogger{})
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
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		t.Fatal(err)
	}
	defer watcher.Close()
	handle(watcher, dir, fsnotify.Event{Name: file, Op: fsnotify.Create}, nil, map[string]struct{}{}, op, fakeLogger{})
	if len(op.uploads) != 1 || op.uploads[0] != file {
		t.Fatalf("uploads=%v", op.uploads)
	}
}
