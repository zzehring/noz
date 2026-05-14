package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

func newGoCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "go <prompt>",
		Short: "Dispatch an autonomous agent with a task",
		Long:  `Point an agent at a GH issue, alert, or task description. The agent runs in a sandboxed environment with CEL policy enforcement.`,
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			fmt.Println("noz go: not yet implemented (Phase 2)")
			return nil
		},
	}
}

func newPairCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "pair <slug>",
		Short: "Interactive pairing session (tmux + nvim + guard tower)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			fmt.Println("noz pair: not yet implemented (Phase 3)")
			return nil
		},
	}
}

func newUpCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "up",
		Short: "Start a sandbox instance",
		RunE: func(cmd *cobra.Command, args []string) error {
			fmt.Println("noz up: not yet implemented (Phase 2)")
			return nil
		},
	}
}

func newDownCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "down [id]",
		Short: "Stop and remove a sandbox instance",
		RunE: func(cmd *cobra.Command, args []string) error {
			fmt.Println("noz down: not yet implemented (Phase 2)")
			return nil
		},
	}
}

func newPsCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "ps",
		Short: "List running instances",
		RunE: func(cmd *cobra.Command, args []string) error {
			fmt.Println("noz ps: not yet implemented (Phase 2)")
			return nil
		},
	}
}
