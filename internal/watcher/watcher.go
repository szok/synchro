// Package watcher observes a directory tree and maps filesystem events to Syncer operations.
package watcher

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/fsnotify/fsnotify"
	"github.com/szok/synchro/internal/paths"
)

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
}

// Watch recursively watches root until ctx is cancelled.
func Watch(ctx context.Context, root string, excluded []string, operations Operations, log Logger) error {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return err
	}
	defer watcher.Close()
	root, err = filepath.Abs(root)
	if err != nil {
		return fmt.Errorf("resolve watch root: %w", err)
	}
	watchedDirectories := make(map[string]struct{})
	if err := addTree(watcher, root, excluded, watchedDirectories); err != nil {
		return err
	}
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
			if isEditorBackup(event.Name) || paths.IsExcluded(event.Name, excluded) {
				continue
			}
			handle(watcher, root, event, excluded, watchedDirectories, operations, log)
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

func addTree(watcher *fsnotify.Watcher, root string, excluded []string, watchedDirectories map[string]struct{}) error {
	return filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !entry.IsDir() {
			return nil
		}
		if paths.IsExcluded(path, excluded) && path != root {
			return filepath.SkipDir
		}
		if err := watcher.Add(path); err != nil {
			return err
		}
		watchedDirectories[path] = struct{}{}
		return nil
	})
}
func handle(watcher *fsnotify.Watcher, root string, event fsnotify.Event, excluded []string, watchedDirectories map[string]struct{}, operations Operations, log Logger) {
	name := event.Name
	if event.Has(fsnotify.Create) {
		info, err := os.Stat(name)
		if err != nil {
			return
		}
		if info.IsDir() {
			log.Change("addDir", relative(root, name))
			if err := addTree(watcher, name, excluded, watchedDirectories); err != nil {
				log.Error(fmt.Sprintf("Watch directory %s: %v", relative(root, name), err))
				return
			}
			operations.CreateDir(name)
		} else {
			log.Change("add", relative(root, name))
			operations.Upload(name)
		}
		return
	}
	if event.Has(fsnotify.Write) {
		log.Change("change", relative(root, name))
		operations.Upload(name)
		return
	}
	if event.Has(fsnotify.Remove) || event.Has(fsnotify.Rename) {
		if _, ok := watchedDirectories[name]; ok {
			delete(watchedDirectories, name)
			log.Change("unlinkDir", relative(root, name))
			operations.DeleteDir(name)
			return
		}
		log.Change("unlink", relative(root, name))
		operations.DeleteFile(name)
	}
}
