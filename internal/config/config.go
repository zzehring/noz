package config

import (
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// Auto-return modes: how an offshoot returns the user to its parent when its
// task is done (see `noz done`). notify is the unobtrusive default.
const (
	AutoReturnNotify = "notify" // announce done + suggest noz back/close; don't switch
	AutoReturnAuto   = "auto"   // switch back to the parent immediately
	AutoReturnOff    = "off"    // do nothing
)

// Config holds noz configuration.
type Config struct {
	configDir string

	// AutoReturn is the offshoot self-return mode (notify|auto|off). Defaults
	// to notify when unset or unrecognized in config.yaml.
	AutoReturn string
}

// fileConfig mirrors the on-disk config.yaml. Kept separate from Config so the
// YAML surface stays explicit and unexported fields don't leak into it.
type fileConfig struct {
	AutoReturn string `yaml:"auto_return"`
}

// Load reads configuration. If path is empty, uses the default location.
func Load(path string) (*Config, error) {
	var configDir, file string
	if path != "" {
		configDir = filepath.Dir(path)
		file = path
	} else {
		configDir = defaultConfigDir()
		file = filepath.Join(configDir, "config.yaml")
	}

	c := &Config{configDir: configDir, AutoReturn: AutoReturnNotify}

	// A missing config file is fine — defaults apply.
	if data, err := os.ReadFile(file); err == nil {
		var fc fileConfig
		if err := yaml.Unmarshal(data, &fc); err != nil {
			return nil, err
		}
		if m := NormalizeAutoReturn(fc.AutoReturn); m != "" {
			c.AutoReturn = m
		}
	}

	return c, nil
}

// NormalizeAutoReturn canonicalizes an auto-return mode string, returning "" if
// it isn't one of the known modes (so callers can fall through to a default).
func NormalizeAutoReturn(s string) string {
	switch s {
	case AutoReturnNotify, AutoReturnAuto, AutoReturnOff:
		return s
	default:
		return ""
	}
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
