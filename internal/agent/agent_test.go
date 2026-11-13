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

// oh-my-pi installs as `omp`, so a descriptor keyed on "pi" never matches it
// and `noz ls` shows the session with no agent.
func TestOmpIsItsOwnAgent(t *testing.T) {
	a, ok := Lookup("omp")
	if !ok {
		t.Fatal("omp is not in the registry")
	}
	if got := Detect("omp"); got != "omp" {
		t.Errorf("Detect(%q) = %q, want %q", "omp", got, "omp")
	}
	// pi must not claim omp, nor omp claim pi.
	if got := Detect("pi"); got != "pi" {
		t.Errorf("Detect(\"pi\") = %q, want \"pi\"", got)
	}

	// omp takes its prompt positionally, like claude, so --task reaches it.
	argv := a.LaunchWith("read .noz/brief/x.md and start")
	if len(argv) != 2 || argv[0] != "omp" || argv[1] != "read .noz/brief/x.md and start" {
		t.Errorf("LaunchWith() = %#v, want [omp <prompt>]", argv)
	}
	if got := a.LaunchWith(""); len(got) != 1 || got[0] != "omp" {
		t.Errorf("LaunchWith(\"\") = %#v, want [omp]", got)
	}
}
