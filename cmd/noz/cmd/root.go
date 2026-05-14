package cmd

import (
	"github.com/spf13/cobra"
)

var (
	cfgFile    string
	policyName string
	provider   string
	agent      string
	verbose    int
	jsonOutput bool
)

func NewRootCmd() *cobra.Command {
	rootCmd := &cobra.Command{
		Use:   "noz",
		Short: "Sovereign & lightweight agent supervisor harness",
		Long:  `noz is a policy-enforced, provider-agnostic harness for autonomous AI agents. It provides CEL-based command vetting, pluggable isolation providers, and a gitops-native workflow.`,
	}

	rootCmd.PersistentFlags().StringVarP(&cfgFile, "config", "c", "", "config file (default ~/.config/noz/config.yaml)")
	rootCmd.PersistentFlags().StringVar(&policyName, "policy", "", "CEL policy name or path")
	rootCmd.RegisterFlagCompletionFunc("policy", completePolicyNames)
	rootCmd.PersistentFlags().StringVarP(&provider, "provider", "p", "", "isolation provider (local, smolvm, shuru)")
	rootCmd.PersistentFlags().StringVarP(&agent, "agent", "a", "", "coding agent (claude-code, opencode)")
	rootCmd.PersistentFlags().CountVarP(&verbose, "verbose", "v", "increase verbosity (up to -vvv)")
	rootCmd.PersistentFlags().BoolVar(&jsonOutput, "json", false, "JSON output for scripting")

	rootCmd.AddCommand(newGateCmd())
	rootCmd.AddCommand(newPolicyCmd())
	rootCmd.AddCommand(newPairCmd())
	rootCmd.AddCommand(newLsCmd())
	rootCmd.AddCommand(newRmCmd())
	rootCmd.AddCommand(newSetupCmd())

	return rootCmd
}

func Execute() error {
	return NewRootCmd().Execute()
}
