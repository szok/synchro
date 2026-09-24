// Package config loads and validates Synchro configuration files.
package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const (
	DefaultFile        = ".synchro.json"
	DefaultConcurrency = 8
	// PasswordEnv overrides the config file's "password" when set and non-empty.
	PasswordEnv = "SYNCHRO_PASSWORD"
)

// Config is the JSON configuration accepted by Synchro.
type Config struct {
	Host            string   `json:"host"`
	Port            int      `json:"port,omitempty"`
	Username        string   `json:"username"`
	Auth            string   `json:"auth"`
	PrivateKeyPath  string   `json:"privateKeyPath,omitempty"`
	Password        string   `json:"password,omitempty"`
	Directory       string   `json:"directory"`
	RemoteDirectory string   `json:"remoteDirectory"`
	Exclude         []string `json:"exclude,omitempty"`
	Concurrency     int      `json:"concurrency,omitempty"`
}

// Example returns the configuration written by --init.
func Example() Config {
	return Config{
		Host: "192.168.1.1", Port: 22, Username: "synchro-user", Auth: "key",
		PrivateKeyPath: "~/.ssh/id_rsa", Password: "", Directory: "./",
		RemoteDirectory: "/home/synchro-user/your-app/",
		Exclude:         []string{"node_modules", ".git", ".idea", "*.log", ".synchro.json"},
		Concurrency:     DefaultConcurrency,
	}
}

// Load reads, decodes, normalizes and validates a configuration file.
func Load(filename string) (Config, error) {
	absolute, err := filepath.Abs(filename)
	if err != nil {
		return Config{}, fmt.Errorf("resolve config path: %w", err)
	}
	contents, err := os.ReadFile(absolute)
	if err != nil {
		return Config{}, fmt.Errorf("read config %q: %w", absolute, err)
	}
	var cfg Config
	if err := json.Unmarshal(contents, &cfg); err != nil {
		return Config{}, fmt.Errorf("parse config %q: %w", absolute, err)
	}
	if password := os.Getenv(PasswordEnv); password != "" {
		cfg.Password = password
	}
	if cfg.Port == 0 {
		cfg.Port = 22
	}
	if cfg.Concurrency == 0 {
		cfg.Concurrency = DefaultConcurrency
	}
	if err := cfg.Validate(); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

// Validate reports a human-readable configuration error before a connection starts.
func (c Config) Validate() error {
	for name, value := range map[string]string{"host": c.Host, "username": c.Username, "auth": c.Auth, "directory": c.Directory, "remoteDirectory": c.RemoteDirectory} {
		if strings.TrimSpace(value) == "" {
			return fmt.Errorf("config field %q is required", name)
		}
	}
	if c.Port < 1 || c.Port > 65535 {
		return errors.New("config field \"port\" must be between 1 and 65535")
	}
	if c.Concurrency < 0 {
		return errors.New("config field \"concurrency\" must not be negative")
	}
	switch c.Auth {
	case "key":
		if strings.TrimSpace(c.PrivateKeyPath) == "" {
			return errors.New("config field \"privateKeyPath\" is required when auth is \"key\"")
		}
	case "password", "none":
	default:
		return fmt.Errorf("unsupported auth method %q; use key, password, or none", c.Auth)
	}
	return nil
}

// LocalDirectory returns the absolute local directory that will be synchronized.
func (c Config) LocalDirectory() (string, error) { return filepath.Abs(c.Directory) }

// ExpandHome expands a leading ~/ into the current user's home directory.
func ExpandHome(value string) (string, error) {
	if value != "~" && !strings.HasPrefix(value, "~/") {
		return value, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("find home directory: %w", err)
	}
	if value == "~" {
		return home, nil
	}
	return filepath.Join(home, value[2:]), nil
}

// WriteExample creates a new configuration file without replacing an existing one.
func WriteExample(filename string) error {
	contents, err := json.MarshalIndent(Example(), "", "  ")
	if err != nil {
		return fmt.Errorf("encode example config: %w", err)
	}
	contents = append(contents, '\n')
	file, err := os.OpenFile(filename, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return err
	}
	defer file.Close()
	if _, err := file.Write(contents); err != nil {
		return err
	}
	return nil
}
