package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

var (
	cfgFile    string
	policyName string
	jsonOutput bool
)

func NewRootCmd(version string) *cobra.Command {
	rootCmd := &cobra.Command{
		Use:     "noz",
		Version: version,
		Short:   "Manage AI-agent sessions (git worktrees + tmux)",
		Long: `noz manages AI-agent sessions as git worktrees + tmux, derived
live from the filesystem, git, and tmux — no state files to drift.

List sessions (ls), spin up per-task workspaces (open),
shape them with profiles, and clean up (rm, prune, mv).

Optional command-gating against CEL policies is available for agents that
support pre-tool hooks — see 'noz setup' and 'noz gate'.

Run with no arguments to show the session dashboard (same as 'noz ls').`,
		// Bare `noz` shows the dashboard; an unknown word is a bad command.
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) > 0 {
				return fmt.Errorf("unknown command %q for %q", args[0], cmd.CommandPath())
			}
			return runLs(cmd, "", false, false, false, defaultMinCategorySize)
		},
		SilenceUsage: true,
	}

	rootCmd.PersistentFlags().StringVarP(&cfgFile, "config", "c", "", "config file (default ~/.config/noz/config.yaml)")
	rootCmd.PersistentFlags().BoolVar(&jsonOutput, "json", false, "JSON output for scripting")

	// Session management — the core of noz.
	rootCmd.AddCommand(newOpenCmd())
	rootCmd.AddCommand(newLsCmd())
	rootCmd.AddCommand(newStatusCmd())
	rootCmd.AddCommand(newBackCmd())
	rootCmd.AddCommand(newPathCmd())
	rootCmd.AddCommand(newMcpCmd())
	rootCmd.AddCommand(newMvCmd())
	rootCmd.AddCommand(newRmCmd())
	rootCmd.AddCommand(newCloseCmd())
	rootCmd.AddCommand(newSpawnCmd())
	rootCmd.AddCommand(newRestoreCmd())
	rootCmd.AddCommand(newReapCmd())
	rootCmd.AddCommand(newPruneCmd())
	rootCmd.AddCommand(newMetricsCmd())
	rootCmd.AddCommand(newProfileCmd())
	rootCmd.AddCommand(newSetupCmd())

	// Optional command-gating.
	rootCmd.AddCommand(newGateCmd())
	rootCmd.AddCommand(newPolicyCmd())

	return rootCmd
}

func Execute(version string) error {
	return NewRootCmd(version).Execute()
}
