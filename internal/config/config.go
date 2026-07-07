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
	var configDir string
	if path != "" {
		configDir = filepath.Dir(path)
	} else {
		configDir = defaultConfigDir()
	}
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

func defaultConfigDir() string {
	if xdg := os.Getenv("XDG_CONFIG_HOME"); xdg != "" {
		return filepath.Join(xdg, "noz")
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config", "noz")
}
