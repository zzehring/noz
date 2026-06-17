package cmd

import "testing"

func TestValidSlug(t *testing.T) {
	ok := []string{"feature-auth", "review-574366", "i-flaky-login-test", "x"}
	for _, s := range ok {
		if err := validSlug(s); err != nil {
			t.Errorf("validSlug(%q) = %v, want nil", s, err)
		}
	}
	bad := []string{"", "../../etc", "a/b", `a\b`, "-rf", ".hidden", "has space", "x\ty"}
	for _, s := range bad {
		if err := validSlug(s); err == nil {
			t.Errorf("validSlug(%q) = nil, want error", s)
		}
	}
}

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
