package paths

import "testing"

func TestMatchGlob(t *testing.T) {
	tests := []struct {
		pattern, value string
		want           bool
	}{
		{"README.md", "README.md", true}, {"*.log", "server.log", true}, {"*.log", "server.txt", false},
		{"node_modules", "/project/node_modules/pkg/index.js", true}, {"node_modules", "/project/src/index.js", false},
		{"git", ".git", false}, {"*.json", "tsconfig.json.bak", false},
	}
	for _, test := range tests {
		if got := MatchGlob(test.pattern, test.value); got != test.want {
			t.Errorf("MatchGlob(%q,%q)=%v; want %v", test.pattern, test.value, got, test.want)
		}
	}
}
func TestIsExcluded(t *testing.T) {
	if !IsExcluded("/project/node_modules/pkg/index.js", []string{"node_modules", "*.log"}) {
		t.Fatal("expected excluded path")
	}
	if IsExcluded("/project/src/index.go", []string{"node_modules", "*.log"}) {
		t.Fatal("unexpected exclusion")
	}
}
func TestRemotePath(t *testing.T) {
	got := RemotePath("/local/project", "/var/www/html/", "/local/project/assets/main.css")
	if got != "/var/www/html/assets/main.css" {
		t.Fatalf("got %q", got)
	}
}
