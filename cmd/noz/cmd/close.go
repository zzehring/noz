package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
)

func newCloseCmd() *cobra.Command {
	var force, yes, keepWorktree, deleteBranch bool

	cmd := &cobra.Command{
		Use:   "close",
		Short: "Close the session you're in (hop away, then tear it down)",
		Long: `Closes the current noz session: hops your tmux client to your last session
(or another live one), then removes this session's worktree and tmux session.

This is the in-session counterpart to ` + "`noz rm`" + `, which refuses to remove the
session you're sitting in. close gets you out safely first, so killing the
session never yanks you into the void.

  noz close                  # tear down the current session, hop away
  noz close --keep-worktree  # just end the tmux session, keep the worktree
  noz close --delete-branch  # also delete the local branch
  noz close -f               # discard a dirty worktree`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runClose(force || yes, keepWorktree, deleteBranch)
		},
	}

	cmd.Flags().BoolVarP(&force, "force", "f", false, "discard a dirty worktree without confirming")
	cmd.Flags().BoolVarP(&yes, "yes", "y", false, "assume yes to prompts (non-interactive)")
	cmd.Flags().BoolVar(&keepWorktree, "keep-worktree", false, "only end the tmux session, keep the worktree")
	cmd.Flags().BoolVar(&deleteBranch, "delete-branch", false, "also delete the local branch")

	return cmd
}

func runClose(force, keepWorktree, deleteBranch bool) error {
	slug := currentTmuxSession()
	if slug == "" {
		return fmt.Errorf("not in a tmux session — `noz close` ends the session you're in (use `noz rm <slug>` otherwise)")
	}
	if !isNozSession(slug) {
		return fmt.Errorf("%q isn't a noz-managed session — nothing for noz to close", slug)
	}

	tmuxBin, err := exec.LookPath("tmux")
	if err != nil {
		return fmt.Errorf("tmux not found")
	}

	root := nozRoot()
	wtDir, scratchDir := sessionDirs(slug, root)

	// Refuse a dirty teardown up front — *before* we hop away — so we never
	// block on a confirmation prompt in a pane the user can no longer see.
	if !keepWorktree && !force && wtDir != "" && dirExists(wtDir) && worktreeIsDirty(wtDir) {
		return fmt.Errorf("%s has uncommitted changes — commit them, or `noz close -f` to discard", wtDir)
	}

	// Resolve the main repo dir while we're still inside the worktree, so the
	// git ops below (and our CWD) stay valid after we delete this worktree.
	mainRepo := mainRepoDir()

	// Land where you'd actually want to be: the parent you spawned this from
	// (lineage), then your last session. If neither is known, drop to the shell
	// you came from — don't guess a session for you (and don't let tmux fling
	// the client to an arbitrary one when we kill this session).
	target := sessionParent(slug)
	if target == "" || target == slug || !tmuxHasSession(target) {
		target = lastTmuxSession()
	}
	if target != "" && target != slug && tmuxHasSession(target) {
		exec.Command(tmuxBin, "switch-client", "-t", target).Run()
		exec.Command(tmuxBin, "display-message", fmt.Sprintf("noz: closed %s → %s", slug, target)).Run()
	} else {
		exec.Command(tmuxBin, "detach-client", "-s", slug).Run()
	}

	// Step out of the doomed worktree so git/filesystem ops stay valid.
	if mainRepo != "" {
		os.Chdir(mainRepo)
	} else {
		os.Chdir(root)
	}

	return teardownSession(slug, wtDir, scratchDir, root, force, keepWorktree, deleteBranch)
}

// mainRepoDir returns the absolute path of the main repository for the current
// worktree (the parent of the common git dir), or "" if not in a git repo.
func mainRepoDir() string {
	out, err := exec.Command("git", "rev-parse", "--git-common-dir").Output()
	if err != nil {
		return ""
	}
	gd := strings.TrimSpace(string(out))
	if gd == "" {
		return ""
	}
	if !filepath.IsAbs(gd) {
		if abs, err := filepath.Abs(gd); err == nil {
			gd = abs
		}
	}
	return filepath.Dir(gd)
}

// sessionParent returns the NOZ_PARENT lineage tag of a session (the session it
// was spawned from), or "" if unset.
func sessionParent(slug string) string {
	return tmuxSessionEnv(slug, "NOZ_PARENT")
}
