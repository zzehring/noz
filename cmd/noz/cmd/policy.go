package cmd

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"github.com/zzehring/nozey/internal/config"
	"github.com/zzehring/nozey/internal/gate"
	"github.com/zzehring/nozey/internal/toolcall"
)

func newPolicyCmd() *cobra.Command {
	policyCmd := &cobra.Command{
		Use:   "policy",
		Short: "Policy introspection and validation",
	}

	policyCmd.AddCommand(newPolicyCheckCmd())
	policyCmd.AddCommand(newPolicyValidateCmd())
	policyCmd.AddCommand(newPolicyListCmd())

	return policyCmd
}

func newPolicyCheckCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "check <json>",
		Short: "Dry-run a command against the active policy",
		Args:  cobra.ExactArgs(1),
		RunE:  runPolicyCheck,
	}
}

func runPolicyCheck(cmd *cobra.Command, args []string) error {
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
		fmt.Printf("ALLOW  %s %v\n", req.Cmd, req.Args)
		fmt.Printf("  rule: %s\n", result.Rule)
	case gate.Deny:
		fmt.Printf("DENY   %s %v\n", req.Cmd, req.Args)
		fmt.Printf("  rule: %s\n", result.Rule)
		fmt.Printf("  reason: %s\n", result.Reason)
	case gate.Pause:
		fmt.Printf("PAUSE  %s %v\n", req.Cmd, req.Args)
		fmt.Printf("  rule: %s\n", result.Rule)
		fmt.Printf("  reason: %s\n", result.Reason)
	}

	return nil
}

func newPolicyValidateCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "validate <file>",
		Short: "Validate a CEL policy file",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			_, err := gate.New(args[0])
			if err != nil {
				return fmt.Errorf("invalid policy: %w", err)
			}
			fmt.Println("policy is valid")
			return nil
		},
	}
}

func newPolicyListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list [dir]",
		Short: "List available policies",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			var dir string
			if len(args) > 0 {
				dir = args[0]
			} else {
				cfg, err := config.Load(cfgFile)
				if err != nil {
					return fmt.Errorf("loading config: %w", err)
				}
				dir = cfg.PoliciesDir()
			}
			policies, err := gate.ListPolicies(dir)
			if err != nil {
				return err
			}
			for _, p := range policies {
				fmt.Println(p)
			}
			return nil
		},
	}
}
