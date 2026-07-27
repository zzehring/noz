package cmd

import "testing"

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
		repo, slug := detectRepo("/nonexistent/"+name, name)
		if repo != "" {
			t.Errorf("detectRepo(%q): repo = %q, want \"\"", name, repo)
		}
		if slug != want {
			t.Errorf("detectRepo(%q): slug = %q, want %q", name, slug, want)
		}
	}
}
