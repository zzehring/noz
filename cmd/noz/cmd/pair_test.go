package cmd

import (
	"strings"
	"testing"
)

func TestValidSlug(t *testing.T) {
	// validSlug is safety-only — length is NOT its concern (so existing
	// over-length sessions stay removable). A long name must pass here.
	ok := []string{"feature-auth", "review-574366", "i-flaky-login-test", "x", strings.Repeat("a", maxSlugLen+10)}
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

func TestValidNewSlug(t *testing.T) {
	// validNewSlug adds the length cap on top of validSlug's safety checks.
	if err := validNewSlug(strings.Repeat("a", maxSlugLen)); err != nil {
		t.Errorf("length == max should pass: %v", err)
	}
	if err := validNewSlug(strings.Repeat("a", maxSlugLen+1)); err == nil {
		t.Errorf("length > max should fail")
	}
	if err := validNewSlug("a/b"); err == nil {
		t.Errorf("unsafe name should still fail")
	}
}

func TestIsShell(t *testing.T) {
	for _, s := range []string{"zsh", "bash", "sh", "fish"} {
		if !isShell(s) {
			t.Errorf("isShell(%q) = false, want true", s)
		}
	}
	for _, s := range []string{"claude", "node", "vim", "k9s", ""} {
		if isShell(s) {
			t.Errorf("isShell(%q) = true, want false", s)
		}
	}
}

func TestEncodeClaudeProject(t *testing.T) {
	cases := map[string]string{
		"/Users/user/worktrees/myrepo-feature-foo": "-Users-user-worktrees-myrepo-feature-foo",
		"/a/b/c":        "-a-b-c",
		"relative/path": "relative-path",
	}
	for in, want := range cases {
		if got := encodeClaudeProject(in); got != want {
			t.Errorf("encodeClaudeProject(%q) = %q, want %q", in, got, want)
		}
	}
}
