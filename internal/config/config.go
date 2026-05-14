package config

import (
	"os"
	"path/filepath"
)

// Config holds noz configuration.
type Config struct {
	configDir string
}

// Load reads configuration. If path is empty, uses the default location.
func Load(path string) (*Config, error) {
	if path != "" {
		dir := filepath.Dir(path)
		return &Config{configDir: dir}, nil
	}

	configDir := defaultConfigDir()
	return &Config{configDir: configDir}, nil
}

// DefaultPolicy returns the path to the default policy file.
func (c *Config) DefaultPolicy() string {
	return filepath.Join(c.PoliciesDir(), "readonly.cel")
}

// PoliciesDir returns the path to the policies directory.
func (c *Config) PoliciesDir() string {
	return filepath.Join(c.configDir, "policies")
}

// WorkDir returns the default working directory for sandboxes.
// Falls back to current directory if the default doesn't exist.
func (c *Config) WorkDir() string {
	home, _ := os.UserHomeDir()
	dir := filepath.Join(home, "noz")
	if _, err := os.Stat(dir); err == nil {
		return dir
	}
	cwd, _ := os.Getwd()
	return cwd
}

func defaultConfigDir() string {
	if xdg := os.Getenv("XDG_CONFIG_HOME"); xdg != "" {
		return filepath.Join(xdg, "noz")
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config", "noz")
}
