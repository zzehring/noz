package local

import (
	"context"
	"strings"
	"testing"

	"github.com/zzehring/nozey/internal/gate"
)

func TestExecEcho(t *testing.T) {
	prov := New("")
	result, err := prov.Exec(context.Background(), &gate.CommandRequest{
		Cmd:  "echo",
		Args: []string{"hello", "world"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.ExitCode != 0 {
		t.Errorf("exit code = %d, want 0", result.ExitCode)
	}
	got := strings.TrimSpace(string(result.Stdout))
	if got != "hello world" {
		t.Errorf("stdout = %q, want %q", got, "hello world")
	}
}

func TestExecCommandNotFound(t *testing.T) {
	prov := New("")
	_, err := prov.Exec(context.Background(), &gate.CommandRequest{
		Cmd:  "this-command-does-not-exist-xyz",
		Args: []string{},
	})
	if err == nil {
		t.Error("expected error for missing command")
	}
}

func TestExecNonZeroExit(t *testing.T) {
	prov := New("")
	result, err := prov.Exec(context.Background(), &gate.CommandRequest{
		Cmd:  "false",
		Args: []string{},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.ExitCode == 0 {
		t.Error("expected non-zero exit code")
	}
}

func TestExecWorkDir(t *testing.T) {
	prov := New("")
	result, err := prov.Exec(context.Background(), &gate.CommandRequest{
		Cmd:     "pwd",
		Args:    []string{},
		WorkDir: "/tmp",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// On macOS /tmp is a symlink to /private/tmp
	got := strings.TrimSpace(string(result.Stdout))
	if !strings.HasSuffix(got, "tmp") {
		t.Errorf("pwd = %q, expected to end with 'tmp'", got)
	}
}

func TestName(t *testing.T) {
	prov := New("")
	if prov.Name() != "local" {
		t.Errorf("name = %q, want %q", prov.Name(), "local")
	}
}
