package shellparse

import (
	"testing"
)

func TestSimpleCommand(t *testing.T) {
	cmds, err := Parse("kubectl get pods -n prod")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(cmds) != 1 {
		t.Fatalf("got %d commands, want 1", len(cmds))
	}
	if cmds[0].Name != "kubectl" {
		t.Errorf("name = %q, want %q", cmds[0].Name, "kubectl")
	}
	if len(cmds[0].Args) != 4 {
		t.Errorf("args len = %d, want 4: %v", len(cmds[0].Args), cmds[0].Args)
	}
}

func TestQuotedArgs(t *testing.T) {
	cmds, err := Parse(`grep -r "hello world" src/`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(cmds) != 1 {
		t.Fatalf("got %d commands, want 1", len(cmds))
	}
	if cmds[0].Args[1] != "hello world" {
		t.Errorf("args[1] = %q, want %q", cmds[0].Args[1], "hello world")
	}
}

func TestSingleQuotedArgs(t *testing.T) {
	cmds, err := Parse("echo 'hello world'")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cmds[0].Args[0] != "hello world" {
		t.Errorf("args[0] = %q, want %q", cmds[0].Args[0], "hello world")
	}
}

func TestPipeline(t *testing.T) {
	cmds, err := Parse("kubectl get pods | grep web | wc -l")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(cmds) != 3 {
		t.Fatalf("got %d commands, want 3", len(cmds))
	}
	if cmds[0].Name != "kubectl" {
		t.Errorf("cmd[0] = %q, want kubectl", cmds[0].Name)
	}
	if cmds[1].Name != "grep" {
		t.Errorf("cmd[1] = %q, want grep", cmds[1].Name)
	}
	if cmds[2].Name != "wc" {
		t.Errorf("cmd[2] = %q, want wc", cmds[2].Name)
	}
}

func TestChainAnd(t *testing.T) {
	cmds, err := Parse("cd /tmp && ls -la")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(cmds) != 2 {
		t.Fatalf("got %d commands, want 2", len(cmds))
	}
	if cmds[0].Name != "cd" {
		t.Errorf("cmd[0] = %q, want cd", cmds[0].Name)
	}
	if cmds[1].Name != "ls" {
		t.Errorf("cmd[1] = %q, want ls", cmds[1].Name)
	}
}

func TestChainOr(t *testing.T) {
	cmds, err := Parse("test -f file || echo missing")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(cmds) != 2 {
		t.Fatalf("got %d commands, want 2", len(cmds))
	}
	if cmds[0].Name != "test" {
		t.Errorf("cmd[0] = %q, want test", cmds[0].Name)
	}
	if cmds[1].Name != "echo" {
		t.Errorf("cmd[1] = %q, want echo", cmds[1].Name)
	}
}

func TestSemicolon(t *testing.T) {
	cmds, err := Parse("echo foo; echo bar")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(cmds) != 2 {
		t.Fatalf("got %d commands, want 2", len(cmds))
	}
}

func TestEnvPrefix(t *testing.T) {
	cmds, err := Parse("KUBECONFIG=/tmp/config kubectl get pods")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(cmds) != 1 {
		t.Fatalf("got %d commands, want 1", len(cmds))
	}
	if cmds[0].Name != "kubectl" {
		t.Errorf("name = %q, want kubectl", cmds[0].Name)
	}
}

func TestMultipleEnvPrefixes(t *testing.T) {
	cmds, err := Parse("FOO=bar BAZ=qux echo hello")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(cmds) != 1 {
		t.Fatalf("got %d commands, want 1", len(cmds))
	}
	if cmds[0].Name != "echo" {
		t.Errorf("name = %q, want echo", cmds[0].Name)
	}
}

func TestEmptyInput(t *testing.T) {
	cmds, err := Parse("")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(cmds) != 0 {
		t.Errorf("got %d commands, want 0", len(cmds))
	}
}

func TestCommandSubstitutionRejected(t *testing.T) {
	_, err := Parse("$(whoami)")
	if err == nil {
		t.Error("expected error for command substitution")
	}
}

func TestBacktickSubstitutionRejected(t *testing.T) {
	_, err := Parse("`whoami`")
	if err == nil {
		t.Error("expected error for backtick substitution")
	}
}

func TestEscapedQuotes(t *testing.T) {
	cmds, err := Parse(`echo "hello \"world\""`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cmds[0].Args[0] != `hello "world"` {
		t.Errorf("args[0] = %q, want %q", cmds[0].Args[0], `hello "world"`)
	}
}

func TestPipeInQuotesNotSplit(t *testing.T) {
	cmds, err := Parse(`echo "hello | world"`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(cmds) != 1 {
		t.Fatalf("got %d commands, want 1 (pipe is inside quotes)", len(cmds))
	}
	if cmds[0].Args[0] != "hello | world" {
		t.Errorf("args[0] = %q, want %q", cmds[0].Args[0], "hello | world")
	}
}

func TestRedirectionPassedAsArgs(t *testing.T) {
	// Redirections are kept as args — the real shell handles them.
	// The gate sees the command name for policy evaluation.
	cmds, err := Parse("echo hello > /tmp/out")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cmds[0].Name != "echo" {
		t.Errorf("name = %q, want echo", cmds[0].Name)
	}
}

func TestComplexRealWorldCommand(t *testing.T) {
	// Typical agent command
	cmds, err := Parse("git diff HEAD~1 --stat && go test ./... 2>&1 | head -20")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(cmds) != 3 {
		t.Fatalf("got %d commands, want 3: %v", len(cmds), cmds)
	}
	if cmds[0].Name != "git" {
		t.Errorf("cmd[0] = %q, want git", cmds[0].Name)
	}
	if cmds[1].Name != "go" {
		t.Errorf("cmd[1] = %q, want go", cmds[1].Name)
	}
	if cmds[2].Name != "head" {
		t.Errorf("cmd[2] = %q, want head", cmds[2].Name)
	}
}
