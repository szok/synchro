package syncer

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"sync"
	"testing"

	"github.com/szok/synchro/internal/paths"
)

type statResult struct {
	info os.FileInfo
	err  error
}

type fakeMkdir struct {
	paths       []string
	errors      map[string]error
	statResults map[string][]statResult
}

func (f *fakeMkdir) Mkdir(path string) error {
	f.paths = append(f.paths, path)
	return f.errors[path]
}

func (f *fakeMkdir) Stat(path string) (os.FileInfo, error) {
	results := f.statResults[path]
	if len(results) == 0 {
		return nil, os.ErrNotExist
	}
	result := results[0]
	f.statResults[path] = results[1:]
	return result.info, result.err
}

func TestMkdirAllCreatesSegments(t *testing.T) {
	fake := &fakeMkdir{errors: map[string]error{}, statResults: map[string][]statResult{}}
	if err := MkdirAll(fake, "/a/b/c"); err != nil {
		t.Fatal(err)
	}
	want := []string{"/a", "/a/b", "/a/b/c"}
	if !reflect.DeepEqual(fake.paths, want) {
		t.Fatalf("paths=%v; want %v", fake.paths, want)
	}
}

func TestMkdirAllSkipsExistingDirectory(t *testing.T) {
	info, err := os.Stat(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	fake := &fakeMkdir{statResults: map[string][]statResult{"/a": {{info: info}}}}
	if err := MkdirAll(fake, "/a"); err != nil {
		t.Fatal(err)
	}
	if len(fake.paths) != 0 {
		t.Fatalf("paths=%v; want no Mkdir calls", fake.paths)
	}
}

func TestMkdirAllIgnoresGenericMkdirFailureForExistingDirectory(t *testing.T) {
	info, err := os.Stat(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	fake := &fakeMkdir{
		errors: map[string]error{"/a": errors.New("sftp: Failure")},
		statResults: map[string][]statResult{
			"/a": {{err: os.ErrNotExist}, {info: info}},
		},
	}
	if err := MkdirAll(fake, "/a"); err != nil {
		t.Fatal(err)
	}
}

func TestMkdirAllStopsOnFailure(t *testing.T) {
	fatal := errors.New("denied")
	fake := &fakeMkdir{errors: map[string]error{"/a": fatal}, statResults: map[string][]statResult{}}
	if err := MkdirAll(fake, "/a/b"); !errors.Is(err, fatal) {
		t.Fatalf("error=%v", err)
	}
	if len(fake.paths) != 1 {
		t.Fatalf("paths=%v", fake.paths)
	}
}

func TestIsNotExist(t *testing.T) {
	if !isNotExist(os.ErrNotExist) {
		t.Fatal("os.ErrNotExist was not recognized")
	}
	if isNotExist(errors.New("denied")) {
		t.Fatal("unrelated error was recognized as missing")
	}
}
func TestParallelUploadLimitsWorkersAndWaits(t *testing.T) {
	files := []string{"one", "two", "three", "four", "five"}
	started := make(chan struct{}, len(files))
	release := make(chan struct{}, len(files))
	finished := make(chan struct{})
	var mu sync.Mutex
	active, maximum, completed := 0, 0, 0
	var uploaded int

	go func() {
		uploaded = parallelUpload(context.Background(), files, 2, func(string) bool {
			mu.Lock()
			active++
			if active > maximum {
				maximum = active
			}
			mu.Unlock()
			started <- struct{}{}
			<-release
			mu.Lock()
			active--
			completed++
			mu.Unlock()
			return true
		})
		close(finished)
	}()

	<-started
	<-started
	select {
	case <-finished:
		t.Fatal("parallelUpload returned before uploads completed")
	default:
	}
	for range files {
		release <- struct{}{}
	}
	<-finished
	mu.Lock()
	defer mu.Unlock()
	if maximum != 2 {
		t.Fatalf("maximum=%d; want 2", maximum)
	}
	if completed != len(files) {
		t.Fatalf("completed=%d; want %d", completed, len(files))
	}
	if uploaded != len(files) {
		t.Fatalf("uploaded=%d; want %d", uploaded, len(files))
	}
}

func TestCollectFilesFiltersExcludedDirectoriesAndFiles(t *testing.T) {
	root := t.TempDir()
	must := func(name string) {
		path := filepath.Join(root, name)
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("x"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	must("index.go")
	must("node_modules/pkg/index.js")
	must("server.log")
	files, err := CollectFiles(root, paths.NewFilter(root, []string{"node_modules", "*.log"}, nil))
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(files, []string{filepath.Join(root, "index.go")}) {
		t.Fatalf("files=%v", files)
	}
}

func TestParallelUploadStopsDispatchingWhenCancelled(t *testing.T) {
	files := []string{"one", "two", "three", "four", "five"}
	ctx, cancel := context.WithCancel(context.Background())
	var mu sync.Mutex
	var calls []string
	uploaded := parallelUpload(ctx, files, 1, func(file string) bool {
		mu.Lock()
		calls = append(calls, file)
		mu.Unlock()
		cancel()
		return true
	})
	// The single worker finishes the file it was handed when cancel ran; at
	// most one more may already be queued in the unbuffered hand-off.
	if len(calls) == 0 || len(calls) > 2 {
		t.Fatalf("calls=%v; want 1 or 2 uploads after cancel", calls)
	}
	if uploaded != len(calls) {
		t.Fatalf("uploaded=%d; want %d", uploaded, len(calls))
	}
}

func TestResolveTargetsExpandsFilesAndDirectories(t *testing.T) {
	root := t.TempDir()
	for _, name := range []string{"a.txt", "dir/b.txt", "dir/c.log", "dir/sub/d.txt"} {
		path := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, nil, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	targets := []string{filepath.Join(root, "a.txt"), filepath.Join(root, "dir"), filepath.Join(root, "dir", "b.txt")}
	files, err := ResolveTargets(root, paths.NewFilter(root, []string{"*.log"}, nil), targets)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		filepath.Join(root, "a.txt"),
		filepath.Join(root, "dir", "b.txt"),
		filepath.Join(root, "dir", "sub", "d.txt"),
	}
	if !reflect.DeepEqual(files, want) {
		t.Fatalf("files = %v, want %v", files, want)
	}
}

func TestResolveTargetsRejectsInvalidTargets(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	for _, path := range []string{filepath.Join(root, "app.log"), filepath.Join(outside, "x.txt")} {
		if err := os.WriteFile(path, nil, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	for name, target := range map[string]string{
		"outside":  filepath.Join(outside, "x.txt"),
		"excluded": filepath.Join(root, "app.log"),
		"missing":  filepath.Join(root, "missing.txt"),
	} {
		if _, err := ResolveTargets(root, paths.NewFilter(root, []string{"*.log"}, nil), []string{target}); err == nil {
			t.Errorf("%s: expected an error for %s", name, target)
		}
	}
}
