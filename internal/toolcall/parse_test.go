package toolcall

import (
	"testing"
)

func TestParseValid(t *testing.T) {
	input := `{"cmd": "kubectl", "args": ["get", "pods", "-n", "prod"]}`
	req, err := Parse(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if req.Cmd != "kubectl" {
		t.Errorf("cmd = %q, want %q", req.Cmd, "kubectl")
	}
	if len(req.Args) != 4 {
		t.Errorf("args len = %d, want 4", len(req.Args))
	}
	if req.Mode != "autonomous" {
		t.Errorf("mode = %q, want %q", req.Mode, "autonomous")
	}
}

func TestParseWithMode(t *testing.T) {
	input := `{"cmd": "git", "args": ["status"], "mode": "interactive"}`
	req, err := Parse(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if req.Mode != "interactive" {
		t.Errorf("mode = %q, want %q", req.Mode, "interactive")
	}
}

func TestParseEmptyCmd(t *testing.T) {
	_, err := Parse(`{"cmd": "", "args": []}`)
	if err == nil {
		t.Error("expected error for empty cmd")
	}
}

func TestParseInvalidJSON(t *testing.T) {
	_, err := Parse(`not json`)
	if err == nil {
		t.Error("expected error for invalid JSON")
	}
}

func TestParseRejectsShellMetaInCmd(t *testing.T) {
	cases := []string{
		`{"cmd": "kubectl; rm -rf /", "args": []}`,
		`{"cmd": "cat && echo pwned", "args": []}`,
		`{"cmd": "$(whoami)", "args": []}`,
	}
	for _, c := range cases {
		_, err := Parse(c)
		if err == nil {
			t.Errorf("expected error for %s", c)
		}
	}
}

func TestParseRejectsShellMetaInArgs(t *testing.T) {
	cases := []string{
		`{"cmd": "cat", "args": ["file; rm -rf /"]}`,
		`{"cmd": "echo", "args": ["$(whoami)"]}`,
		`{"cmd": "cat", "args": ["file | grep secret"]}`,
		`{"cmd": "cat", "args": ["file > /etc/passwd"]}`,
	}
	for _, c := range cases {
		_, err := Parse(c)
		if err == nil {
			t.Errorf("expected error for %s", c)
		}
	}
}

func TestParseAllowsNormalArgs(t *testing.T) {
	input := `{"cmd": "grep", "args": ["-r", "TODO", "src/", "--include=*.go"]}`
	req, err := Parse(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(req.Args) != 4 {
		t.Errorf("args len = %d, want 4", len(req.Args))
	}
}
