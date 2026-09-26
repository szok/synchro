package paths

import (
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// Gitignore matches slash-separated relative paths against the patterns of a
// single .gitignore file: comments, negation (!), directory-only patterns
// (trailing /), anchoring (a / anywhere but the end), *, ?, [...] and **.
type Gitignore struct{ rules []gitignoreRule }

type gitignoreRule struct {
	pattern  *regexp.Regexp
	negate   bool
	dirOnly  bool
	anchored bool
}

// LoadGitignore reads a .gitignore file; it returns nil without an error when
// the file does not exist.
func LoadGitignore(file string) (*Gitignore, error) {
	contents, err := os.ReadFile(file)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return ParseGitignore(string(contents)), nil
}

// ParseGitignore compiles .gitignore contents; invalid patterns are skipped.
func ParseGitignore(contents string) *Gitignore {
	g := &Gitignore{}
	for _, line := range strings.Split(contents, "\n") {
		line = strings.TrimSuffix(line, "\r")
		if !strings.HasSuffix(line, `\ `) {
			line = strings.TrimRight(line, " \t")
		}
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		var rule gitignoreRule
		if strings.HasPrefix(line, "!") {
			rule.negate = true
			line = line[1:]
		} else if strings.HasPrefix(line, `\#`) || strings.HasPrefix(line, `\!`) {
			line = line[1:]
		}
		if strings.HasSuffix(line, "/") {
			rule.dirOnly = true
			line = strings.TrimRight(line, "/")
		}
		if line == "" {
			continue
		}
		rule.anchored = strings.Contains(line, "/")
		pattern, err := regexp.Compile(gitignoreRegexp(strings.TrimPrefix(line, "/")))
		if err != nil {
			continue
		}
		rule.pattern = pattern
		g.rules = append(g.rules, rule)
	}
	return g
}

// Ignored reports whether rel (slash-separated, relative to the .gitignore's
// directory) is ignored. As in git, nothing inside an ignored directory can be
// re-included.
func (g *Gitignore) Ignored(rel string, isDir bool) bool {
	segments := strings.Split(rel, "/")
	for i := range segments {
		last := i == len(segments)-1
		if g.match(strings.Join(segments[:i+1], "/"), segments[i], !last || isDir) {
			return true
		}
	}
	return false
}

// match applies the rules to one path; the last matching rule wins.
func (g *Gitignore) match(path, base string, isDir bool) bool {
	ignored := false
	for _, rule := range g.rules {
		if rule.dirOnly && !isDir {
			continue
		}
		target := base
		if rule.anchored {
			target = path
		}
		if rule.pattern.MatchString(target) {
			ignored = !rule.negate
		}
	}
	return ignored
}

// gitignoreRegexp translates one gitignore glob into an anchored regexp.
func gitignoreRegexp(glob string) string {
	var b strings.Builder
	b.WriteString("^")
	for i := 0; i < len(glob); i++ {
		rest := glob[i:]
		switch {
		case strings.HasPrefix(rest, "**/"):
			b.WriteString("(?:.*/)?")
			i += 2
		case rest == "/**":
			b.WriteString("/.*")
			i += 2
		case strings.HasPrefix(rest, "**"):
			b.WriteString(".*")
			i++
		case glob[i] == '*':
			b.WriteString("[^/]*")
		case glob[i] == '?':
			b.WriteString("[^/]")
		case glob[i] == '[':
			// A ']' right after '[' belongs to the class, so search from i+2.
			end := -1
			if i+2 <= len(glob) {
				end = strings.IndexByte(glob[i+2:], ']')
			}
			if end < 0 {
				b.WriteString(`\[`)
				continue
			}
			class := glob[i+1 : i+2+end]
			if strings.HasPrefix(class, "!") {
				class = "^" + class[1:]
			}
			b.WriteString("[" + strings.ReplaceAll(class, `\`, `\\`) + "]")
			i += 2 + end
		case glob[i] == '\\' && i+1 < len(glob):
			b.WriteString(regexp.QuoteMeta(glob[i+1 : i+2]))
			i++
		default:
			b.WriteString(regexp.QuoteMeta(glob[i : i+1]))
		}
	}
	b.WriteString("$")
	return b.String()
}

// Filter decides which local paths are left out of syncing: the config's
// exclude patterns and, when set, the .gitignore at the synced root.
type Filter struct {
	root      string
	exclude   []string
	gitignore *Gitignore
}

// NewFilter combines exclude patterns with an optional root .gitignore.
func NewFilter(root string, exclude []string, gitignore *Gitignore) *Filter {
	return &Filter{root: root, exclude: exclude, gitignore: gitignore}
}

// Excluded reports whether path (absolute) is left out of syncing.
func (f *Filter) Excluded(path string, isDir bool) bool {
	if IsExcluded(path, f.exclude) {
		return true
	}
	if f.gitignore == nil {
		return false
	}
	rel, err := filepath.Rel(f.root, path)
	if err != nil || rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return false
	}
	return f.gitignore.Ignored(filepath.ToSlash(rel), isDir)
}
