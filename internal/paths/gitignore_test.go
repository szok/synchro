package paths

import (
	"os"
	"path/filepath"
	"testing"
)

func TestGitignoreIgnored(t *testing.T) {
	g := ParseGitignore(`
# dependencies
node_modules/
/dist
*.log
!keep.log
build/**/*.map
docs/*.tmp
\#hash
src/**/generated
[Tt]emp?
`)
	for _, tc := range []struct {
		path  string
		isDir bool
		want  bool
	}{
		{"node_modules", true, true},
		{"node_modules", false, false}, // dir-only pattern
		{"node_modules/react/index.js", false, true},
		{"packages/app/node_modules/x.js", false, true},
		{"dist", true, true},
		{"dist/app.js", false, true},
		{"src/dist/app.js", false, false}, // anchored to the root
		{"error.log", false, true},
		{"logs/deep/error.log", false, true},
		{"keep.log", false, false}, // negated
		{"build/a/b/app.js.map", false, true},
		{"build/app.js.map", false, true},
		{"build/app.js", false, false},
		{"docs/a.tmp", false, true},
		{"docs/sub/a.tmp", false, false},
		{"#hash", false, true},
		{"src/generated", true, true},
		{"src/a/b/generated/x.go", false, true},
		{"Temp1", false, true},
		{"temp1", false, true},
		{"xtemp1", false, false},
		{"src/main.go", false, false},
	} {
		if got := g.Ignored(tc.path, tc.isDir); got != tc.want {
			t.Errorf("Ignored(%q, %v) = %v, want %v", tc.path, tc.isDir, got, tc.want)
		}
	}
}

func TestGitignoreCannotReincludeInsideIgnoredDirectory(t *testing.T) {
	g := ParseGitignore("vendor/\n!vendor/keep.php\n")
	if !g.Ignored("vendor/keep.php", false) {
		t.Fatal("a file inside an ignored directory was re-included")
	}
}

func TestLoadGitignoreMissingFile(t *testing.T) {
	g, err := LoadGitignore(filepath.Join(t.TempDir(), ".gitignore"))
	if g != nil || err != nil {
		t.Fatalf("got %v, %v", g, err)
	}
}

func TestFilterCombinesExcludeAndGitignore(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, ".gitignore"), []byte("node_modules/\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	g, err := LoadGitignore(filepath.Join(root, ".gitignore"))
	if err != nil {
		t.Fatal(err)
	}
	f := NewFilter(root, []string{"*.log"}, g)
	for path, want := range map[string]bool{
		filepath.Join(root, "node_modules"):          true,
		filepath.Join(root, "node_modules", "a.js"):  true,
		filepath.Join(root, "app.log"):               true,
		filepath.Join(root, "src", "a.js"):           false,
		filepath.Join(filepath.Dir(root), "outside"): false,
	} {
		if got := f.Excluded(path, filepath.Base(path) == "node_modules"); got != want {
			t.Errorf("Excluded(%s) = %v, want %v", path, got, want)
		}
	}
}
