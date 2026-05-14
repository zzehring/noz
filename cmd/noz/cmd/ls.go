package cmd

import (
	"fmt"
	"os/exec"
	"strings"

	"github.com/spf13/cobra"
)

func newLsCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "ls",
		Short: "List pairing sessions (worktrees + tmux status)",
		RunE:  runLs,
	}
}

func runLs(cmd *cobra.Command, args []string) error {
	// Get active tmux sessions
	sessions := tmuxSessions()

	if inGitRepo() {
		repo, err := repoName()
		if err != nil {
			return err
		}
		fmt.Fprintf(cmd.OutOrStdout(), "Sessions for %s:\n", repo)

		out, err := exec.Command("git", "worktree", "list").Output()
		if err != nil {
			return fmt.Errorf("listing worktrees: %w", err)
		}

		for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
			if line == "" {
				continue
			}
			fields := strings.Fields(line)
			if len(fields) == 0 {
				continue
			}
			wtBase := strings.TrimPrefix(fields[0], nozRoot()+"/")
			slug := strings.TrimPrefix(wtBase, repo+"-")

			marker := ""
			if sessions[slug] {
				marker = " [tmux]"
			}
			fmt.Fprintf(cmd.OutOrStdout(), "  %s%s\n", line, marker)
		}
	}

	// Also show scratch sessions
	scratchSessions := []string{}
	for name := range sessions {
		if !inGitRepo() || strings.HasPrefix(name, "scratch-") {
			scratchSessions = append(scratchSessions, name)
		}
	}

	if len(scratchSessions) > 0 {
		fmt.Fprintln(cmd.OutOrStdout(), "\nScratch sessions:")
		for _, s := range scratchSessions {
			fmt.Fprintf(cmd.OutOrStdout(), "  %s [tmux]\n", s)
		}
	}

	return nil
}

func tmuxSessions() map[string]bool {
	sessions := make(map[string]bool)
	out, err := exec.Command("tmux", "ls", "-F", "#S").Output()
	if err != nil {
		return sessions
	}
	for _, name := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if name != "" {
			sessions[name] = true
		}
	}
	return sessions
}
