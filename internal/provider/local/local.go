package local

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"

	"github.com/zzehring/nozey/internal/gate"
	"github.com/zzehring/nozey/internal/provider"
)

// Local executes commands as local processes. No isolation — the CEL gate
// is the only safety layer. For dev/testing only.
type Local struct {
	workDir string
}

func New(workDir string) *Local {
	return &Local{workDir: workDir}
}

func (l *Local) Name() string { return "local" }

func (l *Local) Exec(ctx context.Context, req *gate.CommandRequest) (provider.ExecResult, error) {
	binary, err := exec.LookPath(req.Cmd)
	if err != nil {
		return provider.ExecResult{ExitCode: 127}, fmt.Errorf("command not found: %s", req.Cmd)
	}

	cmd := exec.CommandContext(ctx, binary, req.Args...)

	if req.WorkDir != "" {
		cmd.Dir = req.WorkDir
	} else if l.workDir != "" {
		cmd.Dir = l.workDir
	}

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err = cmd.Run()

	result := provider.ExecResult{
		Stdout: stdout.Bytes(),
		Stderr: stderr.Bytes(),
	}

	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			result.ExitCode = exitErr.ExitCode()
		} else {
			return result, err
		}
	}

	return result, nil
}
