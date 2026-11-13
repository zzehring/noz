// Package config loads noz configuration and resolves policy file paths under
// the user's config dir.
package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// Config holds noz configuration.
type Config struct {
	configDir string

	// writeRoots are directories, beyond the session itself, that an agent may
	// write to. Config rather than state: the user writes it and noz only reads
	// it, so there is nothing to drift (PRINCIPLES #1). Normalized at load,
	// because policies compare these by prefix.
	writeRoots []string
}

// fileConfig is the on-disk shape of config.yaml.
type fileConfig struct {
	// WriteRoots widens write containment. The session is always writable; this
	// is for directories outside it, on purpose — most usefully a narrow
	// writable path inside a cross-repo brain (#31), which is readable anyway
	// but should not be writable wholesale.
	//
	//   write_roots:
	//     - ~/brain/zack/inbox
	WriteRoots []string `yaml:"write_roots"`
}

// Load reads configuration. If path is empty, looks for config.yaml in the
// default config dir. A missing file is not an error — noz does less rather
// than refusing to start (PRINCIPLES #5).
func Load(path string) (*Config, error) {
	var configDir, file string
	if path != "" {
		configDir = filepath.Dir(path)
		file = path
	} else {
		configDir = defaultConfigDir()
		file = filepath.Join(configDir, "config.yaml")
	}

	c := &Config{configDir: configDir}

	data, err := os.ReadFile(file)
	if err != nil {
		if os.IsNotExist(err) {
			return c, nil
		}
		return nil, fmt.Errorf("reading %s: %w", file, err)
	}

	var fc fileConfig
	if err := yaml.Unmarshal(data, &fc); err != nil {
		// A malformed config must never silently widen anything, so this fails
		// rather than falling back to defaults.
		return nil, fmt.Errorf("parsing %s: %w", file, err)
	}
	c.writeRoots = normalizeRoots(fc.WriteRoots)
	return c, nil
}

// WriteRoots returns the configured extra writable directories, normalized.
func (c *Config) WriteRoots() []string {
	return c.writeRoots
}

// normalizeRoots expands ~, makes each root absolute and clean, and drops any
// that would defeat the containment it exists to widen. Under a prefix
// comparison "" matches every path and "/" matches the filesystem, so a typo
// producing either would switch write containment off while still reading as a
// narrow grant — the same failure shape as an unpopulated workdir.
func normalizeRoots(in []string) []string {
	home, _ := os.UserHomeDir()
	var out []string
	for _, r := range in {
		r = strings.TrimSpace(r)
		if r == "" {
			continue
		}
		if r == "~" || strings.HasPrefix(r, "~/") {
			if home == "" {
				continue
			}
			r = filepath.Join(home, strings.TrimPrefix(strings.TrimPrefix(r, "~"), "/"))
		}
		abs, err := filepath.Abs(r)
		if err != nil {
			continue
		}
		if abs = filepath.Clean(abs); abs == "/" || (home != "" && abs == home) {
			// Handing back / or $HOME is not a narrow grant.
			continue
		}
		out = append(out, abs)
	}
	return out
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
