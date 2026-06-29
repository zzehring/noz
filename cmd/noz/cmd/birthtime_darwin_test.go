//go:build darwin

package cmd

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestFileBirthtimeDarwin(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "f")
	if err := os.WriteFile(path, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	fi, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}

	// APFS/HFS+ record Birthtimespec, so a just-created file has a recent,
	// non-zero birth time.
	bt := fileBirthtime(path, fi)
	if bt.IsZero() {
		t.Fatal("birthtime is zero; expected a recorded creation time on macOS")
	}
	if d := time.Since(bt); d < -time.Minute || d > time.Hour {
		t.Errorf("birthtime %v is not close to now (delta %v)", bt, d)
	}
}
