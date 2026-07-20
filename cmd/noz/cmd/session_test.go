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
