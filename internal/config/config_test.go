package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadDefaultsPort(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "config.json")
	if err := os.WriteFile(file, []byte(`{"host":"example.com","username":"admin","auth":"none","directory":".","remoteDirectory":"/remote"}`), 0600); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(file)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Port != 22 {
		t.Fatalf("port=%d; want 22", cfg.Port)
	}
	if cfg.Concurrency != DefaultConcurrency {
		t.Fatalf("concurrency=%d; want %d", cfg.Concurrency, DefaultConcurrency)
	}
	if cfg.MaxNewDirectoryFiles != DefaultMaxNewDirectoryFiles || cfg.UseGitignore {
		t.Fatalf("maxNewDirectoryFiles=%d useGitignore=%v; want %d, false", cfg.MaxNewDirectoryFiles, cfg.UseGitignore, DefaultMaxNewDirectoryFiles)
	}
}
func TestLoadPasswordFromEnvOverridesFile(t *testing.T) {
	file := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(file, []byte(`{"host":"h","username":"u","auth":"password","password":"from-file","directory":".","remoteDirectory":"/r"}`), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv(PasswordEnv, "")
	cfg, err := Load(file)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Password != "from-file" {
		t.Fatalf("password=%q; want value from file when env is empty", cfg.Password)
	}
	t.Setenv(PasswordEnv, "from-env")
	cfg, err = Load(file)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Password != "from-env" {
		t.Fatalf("password=%q; want value from env", cfg.Password)
	}
}
func TestExpandHomeOnlyExpandsLeadingTilde(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	got, err := ExpandHome("~/.ssh/id_rsa")
	if err != nil {
		t.Fatal(err)
	}
	if got != filepath.Join(home, ".ssh/id_rsa") {
		t.Fatalf("got %q", got)
	}
	unchanged, err := ExpandHome("folder/~/key")
	if err != nil {
		t.Fatal(err)
	}
	if unchanged != "folder/~/key" {
		t.Fatalf("got %q", unchanged)
	}
}
func TestValidateRejectsKeyWithoutPath(t *testing.T) {
	err := (Config{Host: "host", Username: "user", Auth: "key", Directory: ".", RemoteDirectory: "/remote", Port: 22}).Validate()
	if err == nil {
		t.Fatal("expected validation error")
	}
}
func TestValidateRejectsNegativeConcurrency(t *testing.T) {
	err := (Config{Host: "host", Username: "user", Auth: "none", Directory: ".", RemoteDirectory: "/remote", Port: 22, Concurrency: -1}).Validate()
	if err == nil {
		t.Fatal("expected validation error")
	}
}

func TestWriteExampleDoesNotOverwrite(t *testing.T) {
	file := filepath.Join(t.TempDir(), DefaultFile)
	if err := WriteExample(file); err != nil {
		t.Fatal(err)
	}
	if err := WriteExample(file); !os.IsExist(err) {
		t.Fatalf("error=%v; want exists", err)
	}
}
