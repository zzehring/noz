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
	var keepWorktree bool
	var deleteBranch bool

	cmd := &cobra.Command{
		Use:               "rm <slug>",
		Short:             "Remove a pairing session (worktree + tmux)",
		Args:              cobra.ExactArgs(1),
		ValidArgsFunction: completeTmuxSessions,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runRm(args[0], force, keepWorktree, deleteBranch)
		},
	}

	cmd.Flags().BoolVarP(&force, "force", "f", false, "skip confirmation for dirty worktrees")
	cmd.Flags().BoolVar(&keepWorktree, "keep-worktree", false, "only kill tmux session, keep worktree")
	cmd.Flags().BoolVar(&deleteBranch, "delete-branch", false, "also delete the local branch")

	return cmd
}

func runRm(slug string, force, keepWorktree, deleteBranch bool) error {
	if err := validSlug(slug); err != nil {
		return err
	}
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

	if !keepWorktree {
		if wtDir != "" && dirExists(wtDir) {
			if err := removeWorktree(wtDir, slug, force); err != nil {
				return err
			}
			fmt.Fprintf(os.Stderr, "noz: removed worktree at %s\n", wtDir)
			removed = true
		} else if dirExists(scratchDir) && isWithinRoot(root, scratchDir) {
			if err := os.RemoveAll(scratchDir); err != nil {
				return fmt.Errorf("removing scratch dir: %w", err)
			}
			fmt.Fprintf(os.Stderr, "noz: removed scratch workspace at %s\n", scratchDir)
			removed = true
		}
	}

	// Kill tmux session if it exists — but only if it's noz-managed, so we
	// never nuke an unrelated tmux session that happens to share the name.
	if tmuxHasSession(slug) {
		if isNozSession(slug) {
			exec.Command("tmux", "kill-session", "-t", slug).Run()
			fmt.Fprintf(os.Stderr, "noz: killed tmux session %s\n", slug)
			removed = true
		} else {
			fmt.Fprintf(os.Stderr, "noz: tmux session %q isn't noz-managed — leaving it (use `tmux kill-session -t %s` if you mean to)\n", slug, slug)
		}
	}

	// Delete branch if requested. Use a safe delete (-d) that refuses to drop
	// unmerged commits; never silently force-delete.
	if deleteBranch {
		if err := runGit("branch", "-d", slug); err != nil {
			fmt.Fprintf(os.Stderr, "noz: did not delete branch %s — it may have unmerged commits.\n", slug)
			fmt.Fprintf(os.Stderr, "noz: run `git branch -D %s` yourself if you're sure.\n", slug)
		} else {
			fmt.Fprintf(os.Stderr, "noz: deleted branch %s\n", slug)
			removed = true
		}
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
