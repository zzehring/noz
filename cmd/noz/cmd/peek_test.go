package cmd

import (
	"strings"
	"testing"
)

func TestPeekSessionErrors(t *testing.T) {
	// Unsafe slug is rejected before touching tmux.
	if _, err := peekSession("bad/slug", 40); err == nil {
		t.Error("expected an error for an unsafe slug")
	}
	// A slug with no live session errors clearly rather than panicking.
	_, err := peekSession("definitely-not-a-live-session-xyz", 40)
	if err == nil || !strings.Contains(err.Error(), "no live session") {
		t.Errorf("expected a 'no live session' error, got %v", err)
	}
}
