package cmd

import (
	"testing"
	"time"
)

func TestParseAge(t *testing.T) {
	cases := []struct {
		in      string
		want    time.Duration
		wantErr bool
	}{
		{"7d", 7 * 24 * time.Hour, false},
		{"2w", 2 * 7 * 24 * time.Hour, false},
		{"3h", 3 * time.Hour, false},  // Go duration fallback
		{"1D", 24 * time.Hour, false}, // case-insensitive
		{"", 0, true},
		{"abc", 0, true},
	}
	for _, c := range cases {
		got, err := parseAge(c.in)
		if (err != nil) != c.wantErr {
			t.Errorf("parseAge(%q) err=%v, wantErr=%v", c.in, err, c.wantErr)
			continue
		}
		if err == nil && got != c.want {
			t.Errorf("parseAge(%q) = %v, want %v", c.in, got, c.want)
		}
	}
}

// TestIsWithinRoot guards the destructive prune/rm paths: nothing outside the
// noz root, and never the root itself or a degenerate root.
func TestIsWithinRoot(t *testing.T) {
	cases := []struct {
		root, path string
		want       bool
	}{
		{"/home/u/worktrees", "/home/u/worktrees/repo-x", true},
		{"/home/u/worktrees", "/home/u/worktrees/a/b", true},
		{"/home/u/worktrees", "/home/u/worktrees", false},        // root itself
		{"/home/u/worktrees", "/home/u/other", false},            // sibling
		{"/home/u/worktrees", "/home/u/worktrees/../etc", false}, // escapes
		{"", "/anything", false},
		{"/", "/etc", false},
		{".", "x", false},
	}
	for _, c := range cases {
		if got := isWithinRoot(c.root, c.path); got != c.want {
			t.Errorf("isWithinRoot(%q, %q) = %v, want %v", c.root, c.path, got, c.want)
		}
	}
}
