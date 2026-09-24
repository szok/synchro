// Package paths maps local paths to remote POSIX paths and filters exclusions.
package paths

import (
	"path/filepath"
	"regexp"
	"strings"
	"sync"
)

// RemotePath maps a local path under localRoot to an equivalent remote POSIX path.
func RemotePath(localRoot, remoteRoot, localPath string) string {
	rel, err := filepath.Rel(localRoot, localPath)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		// localPath is outside localRoot; fall back to its base name so the
		// upload cannot escape the remote root.
		rel = filepath.Base(localPath)
	}
	remoteRoot = strings.TrimRight(remoteRoot, "/")
	if remoteRoot == "" {
		remoteRoot = "/"
	}
	return strings.TrimRight(remoteRoot, "/") + "/" + filepath.ToSlash(rel)
}

// IsExcluded reports whether a path matches an exclusion by basename or full path.
func IsExcluded(filePath string, patterns []string) bool {
	base := filepath.Base(filePath)
	for _, pattern := range patterns {
		if MatchGlob(pattern, base) || MatchGlob(pattern, filepath.ToSlash(filePath)) {
			return true
		}
	}
	return false
}

// MatchGlob supports the legacy Synchro wildcard syntax: * and complete path segments.
func MatchGlob(pattern, value string) bool {
	m := compileGlob(pattern)
	return m.full.MatchString(value) || m.segment.MatchString(value)
}

type globMatcher struct{ full, segment *regexp.Regexp }

var (
	globCacheMu sync.Mutex
	globCache   = map[string]globMatcher{}
)

// compileGlob compiles (and caches) the regexps backing a wildcard pattern so
// that IsExcluded stays cheap when called for every file during a walk.
func compileGlob(pattern string) globMatcher {
	globCacheMu.Lock()
	defer globCacheMu.Unlock()
	if m, ok := globCache[pattern]; ok {
		return m
	}
	escaped := strings.ReplaceAll(regexp.QuoteMeta(pattern), `\*`, `.*`)
	m := globMatcher{
		full:    regexp.MustCompile("^" + escaped + "$"),
		segment: regexp.MustCompile(`(^|/)` + escaped + `(/|$)`),
	}
	globCache[pattern] = m
	return m
}
