package config

import (
	"os"
	"path/filepath"
	"testing"
)

// A write root is compared by prefix, so a root that normalizes to "" or "/"
// would turn write containment off while still reading as a narrow grant.
func TestNormalizeRootsDropsUselessGrants(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip("no home dir")
	}

	got := normalizeRoots([]string{"", "   ", "/", home, "~"})
	if len(got) != 0 {
		t.Errorf("normalizeRoots kept a grant that defeats containment: %#v", got)
	}
}

func TestNormalizeRootsExpandsAndCleans(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip("no home dir")
	}

	got := normalizeRoots([]string{
		"~/brain/inbox",
		"~/brain/decisions/../inbox",
		"/var/data/brain/",
	})
	want := []string{
		filepath.Join(home, "brain", "inbox"),
		filepath.Join(home, "brain", "inbox"),
		"/var/data/brain",
	}
	if len(got) != len(want) {
		t.Fatalf("got %d roots, want %d: %#v", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("root %d = %q, want %q", i, got[i], want[i])
		}
	}
}

// A missing config is the common case and must not be an error — noz does less
// rather than refusing to start.
func TestLoadMissingFileIsNotAnError(t *testing.T) {
	c, err := Load(filepath.Join(t.TempDir(), "absent.yaml"))
	if err != nil {
		t.Fatalf("Load() on a missing file: %v", err)
	}
	if len(c.WriteRoots()) != 0 {
		t.Errorf("WriteRoots() = %#v, want empty", c.WriteRoots())
	}
}

// A malformed config must fail rather than fall back to defaults: falling back
// silently would be indistinguishable from a grant the user thought they made.
func TestLoadMalformedFileFails(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte("write_roots: [unclosed\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(path); err == nil {
		t.Error("Load() accepted a malformed config")
	}
}

func TestLoadReadsWriteRoots(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	body := "write_roots:\n  - " + dir + "/brain/inbox\n"
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}

	c, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(dir, "brain", "inbox")
	if roots := c.WriteRoots(); len(roots) != 1 || roots[0] != want {
		t.Errorf("WriteRoots() = %#v, want [%q]", roots, want)
	}
}
