package cmd

import (
	"github.com/spf13/cobra"
)

var (
	cfgFile    string
	policyName string
	jsonOutput bool
)

func NewRootCmd() *cobra.Command {
	rootCmd := &cobra.Command{
		Use:   "noz",
		Short: "Manage AI-agent pairing sessions (git worktrees + tmux)",
		Long: `noz manages AI-agent pairing sessions as git worktrees + tmux, derived
live from the filesystem, git, and tmux — no state files to drift.

List and switch sessions (ls, sw), spin up per-task workspaces (pair),
shape them with profiles, and clean up (rm, prune, mv).

Optional command-gating against CEL policies is available for agents that
support pre-tool hooks — see 'noz setup' and 'noz gate'.`,
	}

	rootCmd.PersistentFlags().StringVarP(&cfgFile, "config", "c", "", "config file (default ~/.config/noz/config.yaml)")
	rootCmd.PersistentFlags().BoolVar(&jsonOutput, "json", false, "JSON output for scripting")

	// Session management — the core of noz.
	rootCmd.AddCommand(newPairCmd())
	rootCmd.AddCommand(newLsCmd())
	rootCmd.AddCommand(newStatusCmd())
	rootCmd.AddCommand(newSwCmd())
	rootCmd.AddCommand(newMvCmd())
	rootCmd.AddCommand(newRmCmd())
	rootCmd.AddCommand(newRestoreCmd())
	rootCmd.AddCommand(newReapCmd())
	rootCmd.AddCommand(newPruneCmd())
	rootCmd.AddCommand(newProfileCmd())
	rootCmd.AddCommand(newSetupCmd())

	// Optional command-gating.
	rootCmd.AddCommand(newGateCmd())
	rootCmd.AddCommand(newPolicyCmd())

	return rootCmd
}

func Execute() error {
	return NewRootCmd().Execute()
}
