package keyring

import (
	"bytes"
	"strings"
	"testing"
)

// Secret is []byte precisely so it can be wiped; a string copy of a credential
// cannot be, and lingers until the GC happens to collect it.
func TestSecretZero(t *testing.T) {
	s := Secret("ghp_AbCdEfGhIjKlMnOp")
	s.Zero()
	if !bytes.Equal(s, make([]byte, len(s))) {
		t.Errorf("Zero() left %q", s)
	}
}

// The stored-credential instruction must scope the item to the noz binary, not
// emit -T "" (prompt on every read, which hangs an unattended run) or -A
// (trust everything, which hands the agent the secret).
func TestInstructionsScopesToTheBinary(t *testing.T) {
	got := Instructions("noz:forgejo:example.com", "token", "/Users/x/go/bin/noz")
	if runtimeIsDarwin() {
		if !strings.Contains(got, `-T "/Users/x/go/bin/noz"`) {
			t.Errorf("instruction does not scope -T to the binary: %q", got)
		}
		if strings.Contains(got, `-T ""`) || strings.Contains(got, " -A") {
			t.Errorf("instruction uses an unsafe -T/-A: %q", got)
		}
	}
}
