//go:build linux

package cmd

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestFileBirthtimeLinux(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "f")
	if err := os.WriteFile(path, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	fi, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}

	bt := fileBirthtime(path, fi)
	if bt.IsZero() {
		// tmpfs and some older filesystems don't record btime; statx then
		// clears STATX_BTIME and we correctly degrade to zero rather than guess.
		t.Skip("filesystem does not record btime (statx returned no STATX_BTIME)")
	}
	if d := time.Since(bt); d < -time.Minute || d > time.Hour {
		t.Errorf("birthtime %v is not close to now (delta %v)", bt, d)
	}
}

func TestFileBirthtimeLinuxMissing(t *testing.T) {
	// A nonexistent path yields the zero time, never an error or panic.
	missing := filepath.Join(t.TempDir(), "does-not-exist")
	if got := fileBirthtime(missing, nil); !got.IsZero() {
		t.Errorf("fileBirthtime(missing) = %v, want zero", got)
	}
}
