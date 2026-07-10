package cmd

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestStateDisplay(t *testing.T) {
	cases := map[string]string{
		"working":   "working",
		"waiting":   "waiting",
		"needs-you": "needs you",
		"needs you": "needs you",
		"blocked":   "needs you",
		"":          "",
		"bogus":     "",
	}
	for state, wantLabel := range cases {
		if label, _ := stateDisplay(state); label != wantLabel {
			t.Errorf("stateDisplay(%q) label = %q, want %q", state, label, wantLabel)
		}
	}
}

func TestClaudeState(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("NOZ_STATE_DIR", dir)

	// No tmux session → no state.
	if got := claudeState(sessionInfo{slug: "x"}); got != "" {
		t.Errorf("no-tmux state = %q, want \"\"", got)
	}

	// Cached state file wins over the heuristic.
	if err := os.WriteFile(filepath.Join(dir, "x"), []byte("needs-you\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if got := claudeState(sessionInfo{slug: "x", hasTmux: true, lastActive: time.Now()}); got != "needs-you" {
		t.Errorf("cached state = %q, want needs-you", got)
	}

	// No cache, agent running, recent activity → working.
	if got := claudeState(sessionInfo{slug: "y", hasTmux: true, agent: "claude", lastActive: time.Now()}); got != "working" {
		t.Errorf("recent state = %q, want working", got)
	}

	// No cache, agent running, stale activity → waiting.
	old := time.Now().Add(-time.Hour)
	if got := claudeState(sessionInfo{slug: "z", hasTmux: true, agent: "claude", lastActive: old}); got != "waiting" {
		t.Errorf("stale state = %q, want waiting", got)
	}

	// No agent running (plain shell) with recent output → no state, not "working".
	if got := claudeState(sessionInfo{slug: "w", hasTmux: true, lastActive: time.Now()}); got != "" {
		t.Errorf("agentless state = %q, want \"\"", got)
	}
}
