package gate

import (
	"testing"
)

func TestAllowReadOnlyKubectl(t *testing.T) {
	policy := `
// kubectl-readonly
request.cmd == "kubectl" &&
  request.args.exists(a, a == "get" || a == "describe" || a == "logs")
  ? "ALLOW"
  : ""
---
// default-deny
"DENY"
`
	g, err := NewFromSource(policy)
	if err != nil {
		t.Fatalf("failed to create gate: %v", err)
	}

	tests := []struct {
		name    string
		req     CommandRequest
		verdict Verdict
	}{
		{
			name:    "kubectl get pods allowed",
			req:     CommandRequest{Cmd: "kubectl", Args: []string{"get", "pods", "-n", "prod"}},
			verdict: Allow,
		},
		{
			name:    "kubectl describe allowed",
			req:     CommandRequest{Cmd: "kubectl", Args: []string{"describe", "pod", "web-1"}},
			verdict: Allow,
		},
		{
			name:    "kubectl logs allowed",
			req:     CommandRequest{Cmd: "kubectl", Args: []string{"logs", "web-1"}},
			verdict: Allow,
		},
		{
			name:    "kubectl delete denied",
			req:     CommandRequest{Cmd: "kubectl", Args: []string{"delete", "pod", "web-1"}},
			verdict: Deny,
		},
		{
			name:    "unknown command denied",
			req:     CommandRequest{Cmd: "rm", Args: []string{"-rf", "/"}},
			verdict: Deny,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := g.Evaluate(&tt.req)
			if result.Verdict != tt.verdict {
				t.Errorf("got %s, want %s (rule: %s, reason: %s)", result.Verdict, tt.verdict, result.Rule, result.Reason)
			}
		})
	}
}

func TestPauseOnGitPush(t *testing.T) {
	policy := `
// git-local
request.cmd == "git" && !request.args.exists(a, a == "push")
  ? "ALLOW"
  : ""
---
// git-push-pause
request.cmd == "git" && request.args.exists(a, a == "push")
  ? "PAUSE"
  : ""
---
// default-deny
"DENY"
`
	g, err := NewFromSource(policy)
	if err != nil {
		t.Fatalf("failed to create gate: %v", err)
	}

	tests := []struct {
		name    string
		req     CommandRequest
		verdict Verdict
	}{
		{
			name:    "git status allowed",
			req:     CommandRequest{Cmd: "git", Args: []string{"status"}},
			verdict: Allow,
		},
		{
			name:    "git commit allowed",
			req:     CommandRequest{Cmd: "git", Args: []string{"commit", "-m", "fix"}},
			verdict: Allow,
		},
		{
			name:    "git push pauses",
			req:     CommandRequest{Cmd: "git", Args: []string{"push", "origin", "main"}},
			verdict: Pause,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := g.Evaluate(&tt.req)
			if result.Verdict != tt.verdict {
				t.Errorf("got %s, want %s (rule: %s)", result.Verdict, tt.verdict, result.Rule)
			}
		})
	}
}

func TestModeAwareness(t *testing.T) {
	policy := `
// interactive-allow-all-kubectl
request.mode == "interactive" && request.cmd == "kubectl"
  ? "ALLOW"
  : ""
---
// autonomous-readonly-kubectl
request.mode == "autonomous" && request.cmd == "kubectl" &&
  request.args.exists(a, a == "get" || a == "describe" || a == "logs")
  ? "ALLOW"
  : ""
---
// default-deny
"DENY"
`
	g, err := NewFromSource(policy)
	if err != nil {
		t.Fatalf("failed to create gate: %v", err)
	}

	tests := []struct {
		name    string
		req     CommandRequest
		verdict Verdict
	}{
		{
			name:    "interactive kubectl delete allowed",
			req:     CommandRequest{Cmd: "kubectl", Args: []string{"delete", "pod", "x"}, Mode: "interactive"},
			verdict: Allow,
		},
		{
			name:    "autonomous kubectl get allowed",
			req:     CommandRequest{Cmd: "kubectl", Args: []string{"get", "pods"}, Mode: "autonomous"},
			verdict: Allow,
		},
		{
			name:    "autonomous kubectl delete denied",
			req:     CommandRequest{Cmd: "kubectl", Args: []string{"delete", "pod", "x"}, Mode: "autonomous"},
			verdict: Deny,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := g.Evaluate(&tt.req)
			if result.Verdict != tt.verdict {
				t.Errorf("got %s, want %s (rule: %s)", result.Verdict, tt.verdict, result.Rule)
			}
		})
	}
}

func TestSafeToolsAllowed(t *testing.T) {
	policy := `
// safe-tools
request.cmd in ["cat", "ls", "find", "grep", "rg", "jq", "head", "tail", "wc"]
  ? "ALLOW"
  : ""
---
// default-deny
"DENY"
`
	g, err := NewFromSource(policy)
	if err != nil {
		t.Fatalf("failed to create gate: %v", err)
	}

	for _, cmd := range []string{"cat", "ls", "find", "grep", "rg", "jq", "head", "tail", "wc"} {
		t.Run(cmd+" allowed", func(t *testing.T) {
			result := g.Evaluate(&CommandRequest{Cmd: cmd, Args: []string{"some-file"}})
			if result.Verdict != Allow {
				t.Errorf("%s: got %s, want ALLOW", cmd, result.Verdict)
			}
		})
	}

	t.Run("rm denied", func(t *testing.T) {
		result := g.Evaluate(&CommandRequest{Cmd: "rm", Args: []string{"-rf", "/"}})
		if result.Verdict != Deny {
			t.Errorf("rm: got %s, want DENY", result.Verdict)
		}
	})
}

func TestDefaultDenyWhenNoRules(t *testing.T) {
	g, err := NewFromSource(`""`)
	if err != nil {
		t.Fatalf("failed to create gate: %v", err)
	}

	result := g.Evaluate(&CommandRequest{Cmd: "anything", Args: []string{}})
	if result.Verdict != Deny {
		t.Errorf("got %s, want DENY", result.Verdict)
	}
}

func TestRuleNaming(t *testing.T) {
	policy := `
// my-custom-rule
"ALLOW"
`
	g, err := NewFromSource(policy)
	if err != nil {
		t.Fatalf("failed to create gate: %v", err)
	}

	result := g.Evaluate(&CommandRequest{Cmd: "anything", Args: []string{}})
	if result.Rule != "my-custom-rule" {
		t.Errorf("got rule %q, want %q", result.Rule, "my-custom-rule")
	}
}

func TestInvalidCELReturnsError(t *testing.T) {
	_, err := NewFromSource(`this is not valid CEL`)
	if err == nil {
		t.Error("expected error for invalid CEL, got nil")
	}
}

func TestEmptyArgsHandled(t *testing.T) {
	policy := `
// allow-git
request.cmd == "git" ? "ALLOW" : ""
---
"DENY"
`
	g, err := NewFromSource(policy)
	if err != nil {
		t.Fatalf("failed to create gate: %v", err)
	}

	result := g.Evaluate(&CommandRequest{Cmd: "git", Args: []string{}})
	if result.Verdict != Allow {
		t.Errorf("got %s, want ALLOW", result.Verdict)
	}
}
