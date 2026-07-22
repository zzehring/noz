package cmd

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestIsNozLeftover(t *testing.T) {
	cases := []struct {
		name    string
		entries []string // files/dirs to create at top level
		want    bool
	}{
		{"empty dir", nil, true},
		{"only noz crumbs", []string{".noz", ".claude"}, true},
		{"noz crumbs plus DS_Store", []string{".claude", ".DS_Store"}, true},
		{"has a real file", []string{".claude", "main.go"}, false},
		{"has a source tree", []string{"README.md"}, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			dir := t.TempDir()
			for _, e := range c.entries {
				if err := os.WriteFile(filepath.Join(dir, e), []byte("x"), 0644); err != nil {
					t.Fatal(err)
				}
			}
			if got := isNozLeftover(dir); got != c.want {
				t.Errorf("isNozLeftover(%v) = %v, want %v", c.entries, got, c.want)
			}
		})
	}
}

// gitSandbox creates a throwaway repo with one commit and returns its path. It
// skips the test if git isn't available (matches the lifecycle-test contract).
func gitSandbox(t *testing.T) string {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	dir := t.TempDir()
	for _, args := range [][]string{
		{"init", "-q"},
		{"-c", "user.email=t@t", "-c", "user.name=t", "commit", "--allow-empty", "-qm", "init"},
	} {
		cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	return dir
}

func TestEnsureWorktreeSlot(t *testing.T) {
	t.Run("missing dir is ready to create", func(t *testing.T) {
		root := t.TempDir()
		wt := filepath.Join(root, "repo-slug")
		reuse, err := ensureWorktreeSlot(root, wt)
		if err != nil || reuse {
			t.Fatalf("got reuse=%v err=%v, want reuse=false err=nil", reuse, err)
		}
	})

	t.Run("real worktree is reused", func(t *testing.T) {
		repo := gitSandbox(t)
		root := t.TempDir()
		wt := filepath.Join(root, "repo-slug")
		out, err := exec.Command("git", "-C", repo, "worktree", "add", "--detach", wt).CombinedOutput()
		if err != nil {
			t.Fatalf("worktree add: %v\n%s", err, out)
		}
		reuse, err := ensureWorktreeSlot(root, wt)
		if err != nil || !reuse {
			t.Fatalf("got reuse=%v err=%v, want reuse=true err=nil", reuse, err)
		}
	})

	t.Run("leftover crumbs are cleared and recreated", func(t *testing.T) {
		root := t.TempDir()
		wt := filepath.Join(root, "repo-slug")
		if err := os.MkdirAll(filepath.Join(wt, ".claude"), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(root, filepath.Join(wt, ".noz")); err != nil {
			t.Fatal(err)
		}
		reuse, err := ensureWorktreeSlot(root, wt)
		if err != nil || reuse {
			t.Fatalf("got reuse=%v err=%v, want reuse=false err=nil", reuse, err)
		}
		if _, err := os.Stat(wt); !os.IsNotExist(err) {
			t.Fatalf("leftover dir should have been removed, stat err=%v", err)
		}
	})

	t.Run("unexpected files are refused, not deleted", func(t *testing.T) {
		root := t.TempDir()
		wt := filepath.Join(root, "repo-slug")
		if err := os.MkdirAll(wt, 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(wt, "important.txt"), []byte("keep me"), 0644); err != nil {
			t.Fatal(err)
		}
		reuse, err := ensureWorktreeSlot(root, wt)
		if err == nil {
			t.Fatal("expected an error for a dir with unexpected files")
		}
		if reuse {
			t.Fatal("reuse should be false on error")
		}
		if _, err := os.Stat(filepath.Join(wt, "important.txt")); err != nil {
			t.Fatalf("must not delete unexpected files, stat err=%v", err)
		}
	})

	t.Run("leftover outside root is refused", func(t *testing.T) {
		root := t.TempDir()
		outside := t.TempDir() // a sibling temp dir, not under root
		wt := filepath.Join(outside, "repo-slug")
		if err := os.MkdirAll(wt, 0755); err != nil {
			t.Fatal(err)
		}
		reuse, err := ensureWorktreeSlot(root, wt)
		if err == nil || reuse {
			t.Fatalf("got reuse=%v err=%v, want an error", reuse, err)
		}
		if _, err := os.Stat(wt); err != nil {
			t.Fatalf("must not delete dirs outside root, stat err=%v", err)
		}
	})
}
