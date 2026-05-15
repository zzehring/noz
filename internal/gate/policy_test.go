package gate

import (
	"testing"
)

func TestYAMLBasicPolicy(t *testing.T) {
	yaml := []byte(`
name: test
rules:
  - name: allow-echo
    cel: request.cmd == "echo"
    verdict: ALLOW
  - name: default-deny
    verdict: DENY
`)
	g, err := NewFromYAML(yaml)
	if err != nil {
		t.Fatalf("failed to load YAML policy: %v", err)
	}

	tests := []struct {
		name    string
		req     CommandRequest
		verdict Verdict
		rule    string
	}{
		{"echo allowed", CommandRequest{Cmd: "echo", Args: []string{"hi"}}, Allow, "allow-echo"},
		{"rm denied", CommandRequest{Cmd: "rm", Args: []string{"file"}}, Deny, "default-deny"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := g.Evaluate(&tt.req)
			if result.Verdict != tt.verdict {
				t.Errorf("got %s, want %s", result.Verdict, tt.verdict)
			}
			if result.Rule != tt.rule {
				t.Errorf("rule = %q, want %q", result.Rule, tt.rule)
			}
		})
	}
}

func TestYAMLFileAccess(t *testing.T) {
	yaml := []byte(`
name: test-file-access
rules:
  - name: block-env-files
    cel: |
      request.tool in ["read", "write", "edit"] &&
        request.path.matches(".*\\.env$")
    verdict: DENY
  - name: block-ssh
    cel: |
      request.tool == "read" && request.path.contains("/.ssh/")
    verdict: DENY
  - name: allow-reads
    cel: request.tool == "read"
    verdict: ALLOW
  - name: block-writes
    cel: request.tool in ["write", "edit"]
    verdict: DENY
  - name: default-deny
    verdict: DENY
`)
	g, err := NewFromYAML(yaml)
	if err != nil {
		t.Fatalf("failed to load YAML policy: %v", err)
	}

	tests := []struct {
		name    string
		req     CommandRequest
		verdict Verdict
		rule    string
	}{
		{
			"read normal file",
			CommandRequest{Tool: "read", Path: "src/main.go"},
			Allow, "allow-reads",
		},
		{
			"read .env blocked",
			CommandRequest{Tool: "read", Path: "/app/.env"},
			Deny, "block-env-files",
		},
		{
			"read ssh key blocked",
			CommandRequest{Tool: "read", Path: "/home/user/.ssh/id_rsa"},
			Deny, "block-ssh",
		},
		{
			"write blocked",
			CommandRequest{Tool: "write", Path: "src/main.go"},
			Deny, "block-writes",
		},
		{
			"edit blocked",
			CommandRequest{Tool: "edit", Path: "src/main.go"},
			Deny, "block-writes",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := g.Evaluate(&tt.req)
			if result.Verdict != tt.verdict {
				t.Errorf("got %s, want %s (rule: %s)", result.Verdict, tt.verdict, result.Rule)
			}
			if result.Rule != tt.rule {
				t.Errorf("rule = %q, want %q", result.Rule, tt.rule)
			}
		})
	}
}

func TestYAMLPauseVerdict(t *testing.T) {
	yaml := []byte(`
name: test-pause
rules:
  - name: git-push-pause
    cel: |
      request.cmd == "git" && request.args.exists(a, a == "push")
    verdict: PAUSE
  - name: git-allow
    cel: request.cmd == "git"
    verdict: ALLOW
  - name: default-deny
    verdict: DENY
`)
	g, err := NewFromYAML(yaml)
	if err != nil {
		t.Fatalf("failed to load YAML policy: %v", err)
	}

	result := g.Evaluate(&CommandRequest{Cmd: "git", Args: []string{"push", "origin", "main"}})
	if result.Verdict != Pause {
		t.Errorf("got %s, want PAUSE", result.Verdict)
	}

	result = g.Evaluate(&CommandRequest{Cmd: "git", Args: []string{"status"}})
	if result.Verdict != Allow {
		t.Errorf("got %s, want ALLOW", result.Verdict)
	}
}

func TestYAMLInvalidVerdict(t *testing.T) {
	yaml := []byte(`
name: bad
rules:
  - name: bad-rule
    verdict: YOLO
`)
	_, err := NewFromYAML(yaml)
	if err == nil {
		t.Error("expected error for invalid verdict")
	}
}

func TestYAMLEmptyRules(t *testing.T) {
	yaml := []byte(`
name: empty
rules: []
`)
	_, err := NewFromYAML(yaml)
	if err == nil {
		t.Error("expected error for empty rules")
	}
}

func TestYAMLInvalidCEL(t *testing.T) {
	yaml := []byte(`
name: bad-cel
rules:
  - name: broken
    cel: this is not valid CEL at all
    verdict: ALLOW
`)
	_, err := NewFromYAML(yaml)
	if err == nil {
		t.Error("expected error for invalid CEL")
	}
}

func TestYAMLPolicyFiles(t *testing.T) {
	// Test that shipped YAML policies load without error
	for _, name := range []string{"readonly", "dev", "sre"} {
		t.Run(name, func(t *testing.T) {
			_, err := New("../../policies/" + name + ".yaml")
			if err != nil {
				t.Fatalf("failed to load %s.yaml: %v", name, err)
			}
		})
	}
}
