package cmd

import "testing"

func TestEncodeClaudeProject(t *testing.T) {
	cases := map[string]string{
		"/Users/user/worktrees/webapp-feature-foo": "-Users-z-worktrees-webapp-feature-foo",
		"/a/b/c":        "-a-b-c",
		"relative/path": "relative-path",
	}
	for in, want := range cases {
		if got := encodeClaudeProject(in); got != want {
			t.Errorf("encodeClaudeProject(%q) = %q, want %q", in, got, want)
		}
	}
}
