package cmd

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
)

func newRmCmd() *cobra.Command {
	var force bool

	cmd := &cobra.Command{
		Use:   "rm <slug>",
		Short: "Remove a pairing session (worktree + tmux)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runRm(args[0], force)
		},
	}

	cmd.Flags().BoolVarP(&force, "force", "f", false, "skip confirmation for dirty worktrees")

	return cmd
}

func runRm(slug string, force bool) error {
	root := nozRoot()

	// Try to find the worktree directory
	var wtDir string
	if inGitRepo() {
		repo, err := repoName()
		if err == nil {
			wtDir = filepath.Join(root, repo+"-"+slug)
		}
	}
	// Also check scratch
	scratchDir := filepath.Join(root, "scratch-"+slug)

	removed := false

	if wtDir != "" && dirExists(wtDir) {
		if err := removeWorktree(wtDir, slug, force); err != nil {
			return err
		}
		removed = true
	} else if dirExists(scratchDir) {
		if err := os.RemoveAll(scratchDir); err != nil {
			return fmt.Errorf("removing scratch dir: %w", err)
		}
		fmt.Fprintf(os.Stderr, "noz: removed scratch workspace at %s\n", scratchDir)
		removed = true
	}

	// Kill tmux session if it exists
	if tmuxHasSession(slug) {
		exec.Command("tmux", "kill-session", "-t", slug).Run()
		fmt.Fprintf(os.Stderr, "noz: killed tmux session %s\n", slug)
		removed = true
	}

	if !removed {
		return fmt.Errorf("no session found for %q", slug)
	}

	return nil
}

func removeWorktree(wtDir, slug string, force bool) error {
	if !force && worktreeIsDirty(wtDir) {
		fmt.Fprintf(os.Stderr, "noz: %s has uncommitted changes.\n", wtDir)
		fmt.Fprint(os.Stderr, "Force-remove anyway? [y/N] ")
		reader := bufio.NewReader(os.Stdin)
		reply, _ := reader.ReadString('\n')
		reply = strings.TrimSpace(strings.ToLower(reply))
		if reply != "y" && reply != "yes" {
			fmt.Fprintln(os.Stderr, "noz: aborted")
			return nil
		}
		return runGit("worktree", "remove", "--force", wtDir)
	}
	return runGit("worktree", "remove", wtDir)
}

func worktreeIsDirty(dir string) bool {
	cmd := exec.Command("git", "-C", dir, "diff", "--quiet")
	if cmd.Run() != nil {
		return true
	}
	cmd = exec.Command("git", "-C", dir, "diff", "--cached", "--quiet")
	return cmd.Run() != nil
}

func tmuxHasSession(name string) bool {
	return exec.Command("tmux", "has-session", "-t", name).Run() == nil
}
