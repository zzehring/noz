package cmd

import (
	"fmt"
	"os"
	"os/exec"

	"github.com/spf13/cobra"
)

func newBackCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "back",
		Short: "Hop to your previous session (the last switch)",
		Long: `Switches the tmux client back to the session you were in before this one.
The history is tmux's own (client_last_session) — noz stores nothing.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if os.Getenv("TMUX") == "" {
				return fmt.Errorf("not inside tmux")
			}
			last := lastTmuxSession()
			if last == "" {
				return fmt.Errorf("no previous session to hop back to")
			}
			tmuxBin, err := exec.LookPath("tmux")
			if err != nil {
				return fmt.Errorf("tmux not found")
			}
			fmt.Fprintf(os.Stderr, "noz: hopping back to %s\n", last)
			return exec.Command(tmuxBin, "switch-client", "-l").Run()
		},
	}
}
