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
	var yes bool
	var keepWorktree bool
	var deleteBranch bool

	cmd := &cobra.Command{
		Use:               "rm <slug>...",
		Short:             "Remove one or more pairing sessions (worktree + tmux)",
		Args:              cobra.MinimumNArgs(1),
		ValidArgsFunction: completeTmuxSessions,
		RunE: func(cmd *cobra.Command, args []string) error {
			// -y/--yes is a synonym for --force here: the only prompt is the
			// dirty-worktree discard, so assuming yes means proceeding with it.
			return runRmMulti(args, force || yes, keepWorktree, deleteBranch)
		},
	}

	cmd.Flags().BoolVarP(&force, "force", "f", false, "discard a dirty worktree without confirming")
	cmd.Flags().BoolVarP(&yes, "yes", "y", false, "assume yes to prompts (non-interactive; e.g. discarding a dirty worktree)")
	cmd.Flags().BoolVar(&keepWorktree, "keep-worktree", false, "only kill tmux session, keep worktree")
	cmd.Flags().BoolVar(&deleteBranch, "delete-branch", false, "also delete the local branch")

	return cmd
}

// runRmMulti removes each named session independently. A failure on one (bad
// name, nothing found) is reported and skipped, never aborting the rest — so
// `noz rm a b c` cleans up everything it can in one pass.
func runRmMulti(slugs []string, force, keepWorktree, deleteBranch bool) error {
	var failed []string
	for _, slug := range slugs {
		if err := runRm(slug, force, keepWorktree, deleteBranch); err != nil {
			fmt.Fprintf(os.Stderr, "noz: %s: %v\n", slug, err)
			failed = append(failed, slug)
		}
	}
	if len(slugs) > 1 {
		fmt.Fprintf(os.Stderr, "noz: removed %d of %d session(s)\n", len(slugs)-len(failed), len(slugs))
	}
	if len(failed) > 0 {
		return fmt.Errorf("could not remove: %s", strings.Join(failed, ", "))
	}
	return nil
}

func runRm(slug string, force, keepWorktree, deleteBranch bool) error {
	if err := validSlug(slug); err != nil {
		return err
	}

	// Don't saw off the branch you're sitting on: refuse to remove the session
	// you're currently inside (killing it would yank you out, and its worktree
	// may be your CWD). Use `noz close` (it hops you away first), or run this
	// from another session.
	if slug == currentTmuxSession() {
		return fmt.Errorf("you're in %q — use `noz close` to end it, or run `noz rm %s` from another session", slug, slug)
	}

	root := nozRoot()
	wtDir, scratchDir := sessionDirs(slug, root)
	return teardownSession(slug, wtDir, scratchDir, root, force, keepWorktree, deleteBranch)
}

// sessionDirs resolves a session's worktree and scratch directories from the
// current repo context. wtDir is empty when not in a git repo.
func sessionDirs(slug, root string) (wtDir, scratchDir string) {
	if inGitRepo() {
		if repo, err := repoName(); err == nil {
			wtDir = filepath.Join(root, repo+"-"+slug)
		}
	}
	scratchDir = filepath.Join(root, "scratch-"+slug)
	return wtDir, scratchDir
}

// teardownSession removes a session's resources: worktree (unless keepWorktree),
// the local branch (if deleteBranch), and finally the tmux session. The tmux
// kill is LAST so a caller closing its own session (`noz close`) finishes all
// its work before this process is reaped along with the session. Shared by
// `noz rm` and `noz close`.
func teardownSession(slug, wtDir, scratchDir, root string, force, keepWorktree, deleteBranch bool) error {
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

		// Drop the session's noz context file too — it's keyed by repo+slug and
		// orphaned once the session is gone (no cruft left in the brain).
		if wtDir != "" {
			if repo := strings.TrimSuffix(filepath.Base(wtDir), "-"+slug); repo != "" {
				os.Remove(contextFilePath(repo, slug))
			}
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

	if !removed {
		return fmt.Errorf("no session found for %q", slug)
	}

	return nil
}

func removeWorktree(wtDir, slug string, force bool) error {
	if worktreeIsDirty(wtDir) {
		if !force {
			// Non-interactive (agent session, pipe): don't silently abort or
			// block on stdin — fail with the exact flag to re-run with.
			if !stdinIsTerminal() {
				return fmt.Errorf("%s has uncommitted changes; re-run with -y/--force to discard and remove", wtDir)
			}
			fmt.Fprintf(os.Stderr, "noz: %s has uncommitted changes.\n", wtDir)
			fmt.Fprint(os.Stderr, "Force-remove anyway? [y/N] ")
			reader := bufio.NewReader(os.Stdin)
			reply, _ := reader.ReadString('\n')
			reply = strings.TrimSpace(strings.ToLower(reply))
			if reply != "y" && reply != "yes" {
				return fmt.Errorf("aborted: %s has uncommitted changes (not removed)", slug)
			}
		}
		// Dirty + (forced or confirmed): git needs --force to drop the worktree.
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
