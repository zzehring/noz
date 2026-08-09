package cmd

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSessionFromDir(t *testing.T) {
	root := "/home/u/worktrees"

	// A directory that isn't a direct child of the noz root must not resolve to
	// a session — we never guess from an unrelated cwd.
	for _, dir := range []string{"", "/somewhere/else", "/home/u/worktrees/a/b", "/home/u"} {
		if got := sessionFromDir(dir, root); got != "" {
			t.Errorf("sessionFromDir(%q, %q) = %q, want \"\"", dir, root, got)
		}
	}

	// A direct child of the root does resolve (detectRepo with no .git falls
	// back to the whole basename as the slug — enough to confirm the guard lets
	// a real worktree through).
	if got := sessionFromDir(root+"/myrepo-feature", root); got == "" {
		t.Error("a direct child of the root should resolve to a slug, got \"\"")
	}
}

// A scratch workspace lives at "scratch-<slug>" but its canonical slug (and
// tmux session name) is the bare <slug>. detectRepo must strip the prefix so
// the slug round-trips through open/rm/close — otherwise `noz ls` shows
// "scratch-foo" and `noz rm scratch-foo` looks for a nonexistent
// "scratch-scratch-foo" dir and "scratch-foo" tmux session (the original bug).
func TestDetectRepoStripsScratchPrefix(t *testing.T) {
	cases := map[string]string{
		"scratch-fix-flaky-retries": "fix-flaky-retries",
		"scratch-foo":               "foo",
		"scratch-scratch-nested":    "scratch-nested", // strip exactly one
		"plain-dir":                 "plain-dir",      // no prefix, untouched
	}
	for name, want := range cases {
		// Use a path with no real .git, so detectRepo takes the no-repo path.
		repo, _, slug := detectRepo("/nonexistent/"+name, name)
		if repo != "" {
			t.Errorf("detectRepo(%q): repo = %q, want \"\"", name, repo)
		}
		if slug != want {
			t.Errorf("detectRepo(%q): slug = %q, want %q", name, slug, want)
		}
	}
}

// canonicalSlug must recover the bare slug when a user types the on-disk
// "scratch-" directory name, but only when the literal name resolves to
// nothing — a real session named "scratch-*" still takes precedence.
func TestCanonicalSlug(t *testing.T) {
	root := t.TempDir()
	// A scratch workspace on disk is "scratch-<slug>".
	if err := os.MkdirAll(filepath.Join(root, "scratch-foo"), 0755); err != nil {
		t.Fatal(err)
	}

	// Typed with the stray prefix -> resolves to the bare slug.
	if got := canonicalSlug("scratch-foo", root); got != "foo" {
		t.Errorf("canonicalSlug(\"scratch-foo\") = %q, want \"foo\"", got)
	}
	// Typed canonically -> unchanged.
	if got := canonicalSlug("foo", root); got != "foo" {
		t.Errorf("canonicalSlug(\"foo\") = %q, want \"foo\"", got)
	}
	// Nothing on disk for this name and no tmux match -> left as typed, so
	// teardown reports an honest "no session found" rather than guessing.
	if got := canonicalSlug("scratch-bar", root); got != "scratch-bar" {
		t.Errorf("canonicalSlug(\"scratch-bar\") = %q, want \"scratch-bar\" (unresolved, untouched)", got)
	}
}
