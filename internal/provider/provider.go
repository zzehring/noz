package provider

import (
	"context"

	"github.com/zzehring/nozey/internal/gate"
)

// ExecResult holds the output of a command execution.
type ExecResult struct {
	Stdout   []byte
	Stderr   []byte
	ExitCode int
}

// Provider is the interface that all isolation backends implement.
type Provider interface {
	// Name returns the provider identifier.
	Name() string

	// Exec runs a command that has already been vetted by the gate.
	Exec(ctx context.Context, req *gate.CommandRequest) (ExecResult, error)
}
