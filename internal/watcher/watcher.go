// Package watcher observes a directory tree and maps filesystem events to Syncer operations.
package watcher

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/szok/synchro/internal/paths"
)

// settleDelay is how long a directory created while watching must go without
// events before its contents are scanned and synced.
var settleDelay = time.Second

// Operations are the remote actions needed for a local file system event.
type Operations interface {
	Upload(string) bool
	DeleteFile(string)
	CreateDir(string)
	DeleteDir(string)
}
type Logger interface {
	Change(string, string)
	Error(string)
	Skipped(path string, limit int)
}

// Options configure Watch.
type Options struct {
	// Filter leaves paths out of syncing.
	Filter *paths.Filter
	// NewDirectoryLimit is the most files a directory created while watching
	// may hold; a bigger one is skipped. Negative disables the limit (callers
	// pass config.MaxNewDirectoryFiles, already defaulted).
	NewDirectoryLimit int
}

// watch is the state of one Watch call.
type watch struct {
	watcher    *fsnotify.Watcher
	root       string
	options    Options
	operations Operations
	log        Logger
	// watched holds every directory with an fsnotify watch.
	watched map[string]struct{}
	// fresh holds directories created while watching that have not settled
	// yet; events inside them are held back until they do.
	fresh map[string]*freshDirectory
	// skipped holds new directories left out for exceeding the limit.
	skipped map[string]struct{}
}

type freshDirectory struct {
	// created holds the distinct paths created inside, up to limit+1.
	created  map[string]struct{}
	deadline time.Time
}

// Watch recursively watches root until ctx is cancelled.
//
// A directory created while watching is not synced right away: its events are
// held back until it goes settleDelay without any, then its contents are
// scanned and uploaded (so files written before its watch was added are not
// missed). If it holds more than options.NewDirectoryLimit files, as
// node_modules after npm install does, it is skipped and reported instead.
func Watch(ctx context.Context, root string, options Options, operations Operations, log Logger) error {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return err
	}
	defer watcher.Close()
	root, err = filepath.Abs(root)
	if err != nil {
		return fmt.Errorf("resolve watch root: %w", err)
	}
	w := newWatch(watcher, root, options, operations, log)
	if err := w.addTree(root); err != nil {
		return err
	}
	ticker := time.NewTicker(settleDelay / 4)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case err, ok := <-watcher.Errors:
			if !ok {
				return nil
			}
			log.Error(fmt.Sprintf("Watcher error: %v", err))
		case event, ok := <-watcher.Events:
			if !ok {
				return nil
			}
			w.event(event)
		case now := <-ticker.C:
			w.settle(now)
		}
	}
}

func newWatch(watcher *fsnotify.Watcher, root string, options Options, operations Operations, log Logger) *watch {
	if options.Filter == nil {
		options.Filter = paths.NewFilter(root, nil, nil)
	}
	return &watch{
		watcher: watcher, root: root, options: options, operations: operations, log: log,
		watched: map[string]struct{}{}, fresh: map[string]*freshDirectory{}, skipped: map[string]struct{}{},
	}
}

// event routes one fsnotify event: dropped when excluded or inside a skipped
// directory, held back inside a fresh directory, handled otherwise.
func (w *watch) event(event fsnotify.Event) {
	name := event.Name
	if isEditorBackup(name) {
		return
	}
	if w.options.Filter.Excluded(name, w.isDir(name)) {
		return
	}
	if top := ancestorIn(w.skipped, w.root, name); top != "" {
		if name != top || !(event.Has(fsnotify.Create) || removed(event)) {
			return
		}
		// The skipped directory itself was removed or replaced.
		delete(w.skipped, top)
		if removed(event) {
			w.unwatch(top)
			return
		}
	}
	if top := ancestorIn(w.fresh, w.root, name); top != "" {
		w.hold(top, name, event)
		return
	}
	w.handle(event)
}

// isDir reports whether name is (or, when already removed, was) a directory.
func (w *watch) isDir(name string) bool {
	if _, ok := w.watched[name]; ok {
		return true
	}
	info, err := os.Stat(name)
	return err == nil && info.IsDir()
}

// hold records an event inside the fresh directory top and pushes back its deadline.
func (w *watch) hold(top, name string, event fsnotify.Event) {
	dir := w.fresh[top]
	if name == top && removed(event) {
		// Gone before it settled: nothing was sent, so there is nothing to delete.
		delete(w.fresh, top)
		w.unwatch(top)
		return
	}
	dir.deadline = time.Now().Add(settleDelay)
	if !event.Has(fsnotify.Create) {
		return
	}
	if w.isDir(name) {
		if err := w.addTree(name); err != nil {
			w.log.Error(fmt.Sprintf("Watch directory %s: %v", relative(w.root, name), err))
		}
	}
	if w.options.NewDirectoryLimit >= 0 {
		dir.created[name] = struct{}{}
		if len(dir.created) > w.options.NewDirectoryLimit {
			w.skip(top)
		}
	}
}

// settle syncs, or skips, every fresh directory whose deadline has passed.
func (w *watch) settle(now time.Time) {
	for top, dir := range w.fresh {
		if now.Before(dir.deadline) {
			continue
		}
		delete(w.fresh, top)
		dirs, files, err := w.scan(top)
		if err != nil {
			if !errors.Is(err, fs.ErrNotExist) {
				w.log.Error(fmt.Sprintf("Read directory %s: %v", relative(w.root, top), err))
			}
			w.unwatch(top)
			continue
		}
		if w.options.NewDirectoryLimit >= 0 && len(files) > w.options.NewDirectoryLimit {
			w.skip(top)
			continue
		}
		for _, dir := range dirs {
			w.log.Change("addDir", relative(w.root, dir))
			w.operations.CreateDir(dir)
		}
		for _, file := range files {
			w.log.Change("add", relative(w.root, file))
			w.operations.Upload(file)
		}
	}
}

// scan lists the directories (top first) and regular files under top that are
// not excluded.
func (w *watch) scan(top string) (dirs, files []string, err error) {
	err = filepath.WalkDir(top, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if path != top && (isEditorBackup(path) || w.options.Filter.Excluded(path, entry.IsDir())) {
			if entry.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if entry.IsDir() {
			dirs = append(dirs, path)
		} else if entry.Type().IsRegular() {
			files = append(files, path)
		}
		return nil
	})
	return dirs, files, err
}

// skip stops watching below the fresh directory top and reports it. top
// itself stays watched: its removal must be seen, and on macOS removing a
// directory's watch makes the parent report it as created again.
func (w *watch) skip(top string) {
	delete(w.fresh, top)
	w.unwatchBelow(top)
	w.skipped[top] = struct{}{}
	w.log.Skipped(relative(w.root, top), w.options.NewDirectoryLimit)
}

// unwatch removes the watches on dir and every directory below it.
func (w *watch) unwatch(dir string) {
	w.unwatchBelow(dir)
	if _, ok := w.watched[dir]; ok {
		_ = w.watcher.Remove(dir)
		delete(w.watched, dir)
	}
}

// unwatchBelow removes the watches on every directory below dir.
func (w *watch) unwatchBelow(dir string) {
	for watched := range w.watched {
		if watched != dir && isWithin(dir, watched) {
			_ = w.watcher.Remove(watched)
			delete(w.watched, watched)
		}
	}
}

// forget drops fresh and skipped directories at or below a removed directory.
func (w *watch) forget(dir string) {
	for top := range w.fresh {
		if isWithin(dir, top) {
			delete(w.fresh, top)
		}
	}
	for top := range w.skipped {
		if isWithin(dir, top) {
			delete(w.skipped, top)
		}
	}
}

// relative renders path relative to root for display, falling back to path
// itself if it cannot be made relative.
func relative(root, path string) string {
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return path
	}
	return filepath.ToSlash(rel)
}

// isWithin reports whether path is dir or lies below it.
func isWithin(dir, path string) bool {
	return path == dir || strings.HasPrefix(path, dir+string(filepath.Separator))
}

// ancestorIn returns the entry of set that is name or its closest ancestor
// below root, or "" when there is none.
func ancestorIn[V any](set map[string]V, root, name string) string {
	if len(set) == 0 {
		return ""
	}
	for path := name; path != root; {
		if _, ok := set[path]; ok {
			return path
		}
		parent := filepath.Dir(path)
		if parent == path {
			break
		}
		path = parent
	}
	return ""
}

func removed(event fsnotify.Event) bool {
	return event.Has(fsnotify.Remove) || event.Has(fsnotify.Rename)
}

// isEditorBackup reports editor-generated files that should not be synchronized.
func isEditorBackup(name string) bool {
	base := filepath.Base(name)
	if strings.HasSuffix(base, "~") || strings.HasPrefix(base, ".#") || (strings.HasPrefix(base, "#") && strings.HasSuffix(base, "#")) || strings.Contains(base, ".tmp.") {
		return true
	}
	switch filepath.Ext(base) {
	case ".swp", ".swo", ".swn", ".swm", ".swt":
		return true
	}
	return false
}

func (w *watch) addTree(root string) error {
	return filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !entry.IsDir() {
			return nil
		}
		if path != w.root && w.options.Filter.Excluded(path, true) {
			return filepath.SkipDir
		}
		if err := w.watcher.Add(path); err != nil {
			return err
		}
		w.watched[path] = struct{}{}
		return nil
	})
}

// handle maps an event outside fresh and skipped directories to operations.
func (w *watch) handle(event fsnotify.Event) {
	name := event.Name
	if event.Has(fsnotify.Create) {
		info, err := os.Stat(name)
		if err != nil {
			return
		}
		if info.IsDir() {
			// Watch first so nothing written from now on is missed; the
			// contents are synced once the directory settles.
			if err := w.addTree(name); err != nil {
				w.log.Error(fmt.Sprintf("Watch directory %s: %v", relative(w.root, name), err))
				return
			}
			w.fresh[name] = &freshDirectory{created: map[string]struct{}{}, deadline: time.Now().Add(settleDelay)}
		} else {
			w.log.Change("add", relative(w.root, name))
			w.operations.Upload(name)
		}
		return
	}
	if event.Has(fsnotify.Write) {
		w.log.Change("change", relative(w.root, name))
		w.operations.Upload(name)
		return
	}
	if removed(event) {
		if _, ok := w.watched[name]; ok {
			delete(w.watched, name)
			w.forget(name)
			w.log.Change("unlinkDir", relative(w.root, name))
			w.operations.DeleteDir(name)
			return
		}
		w.log.Change("unlink", relative(w.root, name))
		w.operations.DeleteFile(name)
	}
}
