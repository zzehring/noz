package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

func newGoCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "run <prompt>",
		Short: "Dispatch an autonomous agent with a task",
		Long:  `Point an agent at a GH issue, alert, or task description. The agent runs in a sandboxed environment with CEL policy enforcement.`,
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			fmt.Println("noz run: not yet implemented (Phase 2)")
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
