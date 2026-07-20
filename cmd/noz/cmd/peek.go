package cmd

import (
	"fmt"
	"os/exec"
	"strconv"
	"strings"

	"github.com/spf13/cobra"
)

func newPeekCmd() *cobra.Command {
	var lines int
	cmd := &cobra.Command{
		Use:   "peek <slug>",
		Short: "Peek at another session's agent output without switching to it",
		Long: `Prints the recent terminal output of a live session's active pane, so you can
see what its agent is doing — or whether it's stuck waiting on a prompt —
without switching your tmux client to it. Read-only; captured live from tmux,
nothing stored.

  noz peek research-umm-integration       # last 40 lines
  noz peek review-1234 -n 100             # last 100 lines`,
		Args:              cobra.ExactArgs(1),
		ValidArgsFunction: completeTmuxSessions,
		RunE: func(cmd *cobra.Command, args []string) error {
			out, err := peekSession(args[0], lines)
			if err != nil {
				return err
			}
			fmt.Fprint(cmd.OutOrStdout(), out)
			return nil
		},
	}
	cmd.Flags().IntVarP(&lines, "lines", "n", 40, "how many lines of recent output to capture")
	return cmd
}

// peekSession captures the recent output of a live session's active pane.
// Read-only. Errors when the session isn't live (an idle worktree has no pane).
func peekSession(slug string, lines int) (string, error) {
	if err := validSlug(slug); err != nil {
		return "", err
	}
	if !tmuxHasSession(slug) {
		return "", fmt.Errorf("no live session %q — list with `noz ls` (an idle worktree has no pane to peek)", slug)
	}
	tmuxBin, err := exec.LookPath("tmux")
	if err != nil {
		return "", fmt.Errorf("tmux not found")
	}
	pane := activePaneID(tmuxBin, slug)
	if pane == "" {
		return "", fmt.Errorf("could not resolve a pane for %q", slug)
	}
	if lines < 1 {
		lines = 40
	}
	out, err := exec.Command(tmuxBin, "capture-pane", "-p", "-S", "-"+strconv.Itoa(lines), "-t", pane).Output()
	if err != nil {
		return "", fmt.Errorf("capturing pane: %w", err)
	}
	return string(out), nil
}

// activePaneID resolves a session's active pane id (e.g. "%246"). capture-pane
// needs a pane target and doesn't accept the `=name` exact-session form, so we
// go via list-panes (which does) and pick the active pane.
func activePaneID(tmuxBin, slug string) string {
	out, err := exec.Command(tmuxBin, "list-panes", "-t", sessionTarget(slug), "-F", "#{pane_active} #{pane_id}").Output()
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if id, ok := strings.CutPrefix(line, "1 "); ok {
			return strings.TrimSpace(id)
		}
	}
	return ""
}
