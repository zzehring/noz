package agent

import "testing"

func TestDetect(t *testing.T) {
	cases := map[string]string{
		"claude":   "claude",
		"2.1.78":   "claude", // Claude Code shows its version as the pane command
		"2.1.177":  "claude",
		"opencode": "opencode",
		"codex":    "codex",
		"gemini":   "gemini",
		"pi":       "pi",
		"zsh":      "",
		"nvim":     "",
		"node":     "",
		"":         "",
	}
	for cmd, want := range cases {
		if got := Detect(cmd); got != want {
			t.Errorf("Detect(%q) = %q, want %q", cmd, got, want)
		}
	}
}

func TestLookup(t *testing.T) {
	if a, ok := Lookup("claude"); !ok || a.Launch[0] != "claude" {
		t.Errorf("Lookup(claude) = %+v, %v", a, ok)
	}
	if _, ok := Lookup("nope"); ok {
		t.Error("Lookup(nope) should not be found")
	}
}

func TestNames(t *testing.T) {
	if len(Names()) == 0 || Names()[0] != "claude" {
		t.Errorf("Names() = %v", Names())
	}
}
