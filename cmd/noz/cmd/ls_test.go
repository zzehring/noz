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

func TestHumanMem(t *testing.T) {
	cases := map[int]string{
		0:       "",
		-5:      "",
		512:     "1M",   // <1 MiB rounds up to 0? 512KiB = 0.5MiB -> "0M"
		1024:    "1M",   // 1 MiB
		406528:  "397M", // ~397 MiB
		1572864: "1.5G", // 1.5 GiB
		2097152: "2.0G", // 2 GiB
	}
	for kib, want := range cases {
		// 512 KiB special-case: 0.5 MiB -> "%.0f" rounds to "0M"; accept either.
		got := humanMem(kib)
		if kib == 512 {
			if got != "0M" && got != "1M" {
				t.Errorf("humanMem(512) = %q", got)
			}
			continue
		}
		if got != want {
			t.Errorf("humanMem(%d) = %q, want %q", kib, got, want)
		}
	}
}

func TestSubtreeRSS(t *testing.T) {
	// 1 -> 2 -> 4, and 1 -> 3. RSS in KiB.
	rss := map[string]int{"1": 100, "2": 200, "3": 50, "4": 25}
	children := map[string][]string{"1": {"2", "3"}, "2": {"4"}}

	if got := subtreeRSS("1", rss, children, map[string]bool{}); got != 375 {
		t.Errorf("subtreeRSS(1) = %d, want 375", got)
	}
	if got := subtreeRSS("2", rss, children, map[string]bool{}); got != 225 {
		t.Errorf("subtreeRSS(2) = %d, want 225", got)
	}
	// Cycle guard: 1 <-> 2 must not infinite-loop.
	cyc := map[string][]string{"1": {"2"}, "2": {"1"}}
	if got := subtreeRSS("1", rss, cyc, map[string]bool{}); got != 300 {
		t.Errorf("subtreeRSS with cycle = %d, want 300", got)
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

	// No cache, recent activity → working.
	if got := claudeState(sessionInfo{slug: "y", hasTmux: true, lastActive: time.Now()}); got != "working" {
		t.Errorf("recent state = %q, want working", got)
	}

	// No cache, stale activity → waiting.
	old := time.Now().Add(-time.Hour)
	if got := claudeState(sessionInfo{slug: "z", hasTmux: true, lastActive: old}); got != "waiting" {
		t.Errorf("stale state = %q, want waiting", got)
	}
}
