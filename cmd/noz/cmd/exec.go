package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"github.com/zzehring/nozey/internal/config"
	"github.com/zzehring/nozey/internal/gate"
	"github.com/zzehring/nozey/internal/provider/local"
	"github.com/zzehring/nozey/internal/toolcall"
)

func newExecCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "exec <json>",
		Short: "Execute a command through the CEL gate",
		Long:  `Parses a JSON tool call, evaluates it against the active CEL policy, and executes if allowed.`,
		Args:  cobra.ExactArgs(1),
		RunE:  runExec,
	}
}

func runExec(cmd *cobra.Command, args []string) error {
	ctx := context.Background()

	req, err := toolcall.Parse(args[0])
	if err != nil {
		return fmt.Errorf("invalid tool call: %w", err)
	}

	cfg, err := config.Load(cfgFile)
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}

	policyPath := policyName
	if policyPath == "" {
		policyPath = cfg.DefaultPolicy()
	}

	g, err := gate.New(policyPath)
	if err != nil {
		return fmt.Errorf("loading policy: %w", err)
	}

	result := g.Evaluate(req)

	if jsonOutput {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(result)
	}

	switch result.Verdict {
	case gate.Allow:
		fmt.Fprintf(os.Stderr, "ALLOW  %s %s (rule: %s)\n", req.Cmd, req.Args, result.Rule)
	case gate.Deny:
		fmt.Fprintf(os.Stderr, "DENY   %s %s (rule: %s, reason: %s)\n", req.Cmd, req.Args, result.Rule, result.Reason)
		os.Exit(2)
	case gate.Pause:
		fmt.Fprintf(os.Stderr, "PAUSE  %s %s (rule: %s, reason: %s)\n", req.Cmd, req.Args, result.Rule, result.Reason)
		os.Exit(3)
	}

	if result.Verdict != gate.Allow {
		return nil
	}

	prov := local.New(cfg.WorkDir())
	execResult, err := prov.Exec(ctx, req)
	if err != nil {
		return fmt.Errorf("exec: %w", err)
	}

	if len(execResult.Stdout) > 0 {
		os.Stdout.Write(execResult.Stdout)
	}
	if len(execResult.Stderr) > 0 {
		os.Stderr.Write(execResult.Stderr)
	}

	if execResult.ExitCode != 0 {
		os.Exit(execResult.ExitCode)
	}

	return nil
}
