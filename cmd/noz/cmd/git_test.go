package cmd

import "testing"

func TestWorktreeMainRepo(t *testing.T) {
	cases := []struct{ in, want string }{
		{"gitdir: /home/u/repo/.git/worktrees/feat", "/home/u/repo"},
		{"gitdir: /a/b/myrepo/.git/worktrees/x\n", "/a/b/myrepo"},
		{"  gitdir: /trim/me/.git/worktrees/y  ", "/trim/me"},
		{"/home/u/repo/.git", ""},            // normal repo, not a worktree pointer
		{"gitdir: /no/worktrees/marker", ""}, // missing /.git/worktrees/
		{"", ""},
	}
	for _, c := range cases {
		if got := worktreeMainRepo(c.in); got != c.want {
			t.Errorf("worktreeMainRepo(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}
