package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
)

func newPairCmd() *cobra.Command {
	var prNumber string
	var baseBranch string
	var noRepo bool

	cmd := &cobra.Command{
		Use:   "pair <slug>",
		Short: "Start a pairing session (worktree + tmux + CEL gate)",
		Long: `Creates a workspace and drops you into a tmux session with the noz
CEL gate active. If you're in a git repo, creates a worktree. Otherwise
creates a scratch directory.

Reuses existing sessions — if the tmux session already exists, attaches to it.

Examples:
  noz pair feature-auth        # worktree + tmux (like cw)
  noz pair --pr 456            # PR review (like cw-pr)
  noz pair investigate         # scratch dir (no repo)
  noz pair feature-auth main   # worktree from specific base branch`,
		Args: cobra.RangeArgs(0, 2),
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 && prNumber == "" {
				return fmt.Errorf("usage: noz pair <slug> or noz pair --pr <number>")
			}

			var slug string
			var base string

			if prNumber != "" {
				return runPairPR(prNumber)
			}

			slug = args[0]
			if len(args) > 1 {
				base = args[1]
			}
			if baseBranch != "" {
				base = baseBranch
			}

			if noRepo || !inGitRepo() {
				return runPairScratch(slug)
			}
			return runPairWorktree(slug, base)
		},
	}

	cmd.Flags().StringVar(&prNumber, "pr", "", "PR number to review (creates detached worktree)")
	cmd.Flags().StringVar(&baseBranch, "base", "", "base branch for worktree")
	cmd.Flags().BoolVar(&noRepo, "no-repo", false, "force scratch directory (skip git worktree)")

	return cmd
}

func runPairWorktree(slug, baseBranch string) error {
	repo, err := repoName()
	if err != nil {
		return err
	}

	root := nozRoot()
	wtDir := filepath.Join(root, repo+"-"+slug)

	if err := os.MkdirAll(root, 0755); err != nil {
		return fmt.Errorf("creating root dir: %w", err)
	}

	if dirExists(wtDir) {
		fmt.Fprintf(os.Stderr, "noz: worktree exists at %s, reusing\n", wtDir)
	} else {
		args := []string{"worktree", "add", wtDir, "-b", slug}
		if baseBranch != "" {
			args = append(args, baseBranch)
		}
		if err := runGit(args...); err != nil {
			return fmt.Errorf("creating worktree: %w", err)
		}
		fmt.Fprintf(os.Stderr, "noz: created worktree at %s on branch %s\n", wtDir, slug)
	}

	return tmuxSession(slug, wtDir)
}

func runPairPR(prNumber string) error {
	if _, err := exec.LookPath("gh"); err != nil {
		return fmt.Errorf("gh CLI not found (needed for --pr)")
	}

	repo, err := repoName()
	if err != nil {
		return err
	}

	// Get PR branch name
	out, err := exec.Command("gh", "pr", "view", prNumber, "--json", "headRefName", "-q", ".headRefName").Output()
	if err != nil {
		return fmt.Errorf("could not look up PR #%s: %w", prNumber, err)
	}
	branch := strings.TrimSpace(string(out))

	slug := "review-" + prNumber
	root := nozRoot()
	wtDir := filepath.Join(root, repo+"-"+slug)

	if err := os.MkdirAll(root, 0755); err != nil {
		return fmt.Errorf("creating root dir: %w", err)
	}

	if dirExists(wtDir) {
		fmt.Fprintf(os.Stderr, "noz: worktree exists at %s, reusing\n", wtDir)
	} else {
		if err := runGit("fetch", "origin", branch); err != nil {
			return fmt.Errorf("fetching branch: %w", err)
		}
		if err := runGit("worktree", "add", "--detach", wtDir, "origin/"+branch); err != nil {
			return fmt.Errorf("creating worktree: %w", err)
		}
		fmt.Fprintf(os.Stderr, "noz: created worktree at %s (detached at origin/%s)\n", wtDir, branch)
	}

	return tmuxSession(slug, wtDir)
}

func runPairScratch(slug string) error {
	root := nozRoot()
	dir := filepath.Join(root, "scratch-"+slug)

	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("creating scratch dir: %w", err)
	}

	if !dirExists(filepath.Join(dir, ".git")) {
		fmt.Fprintf(os.Stderr, "noz: created scratch workspace at %s\n", dir)
	} else {
		fmt.Fprintf(os.Stderr, "noz: reusing scratch workspace at %s\n", dir)
	}

	return tmuxSession(slug, dir)
}

// tmuxSession attaches to an existing session or creates a new one.
// Uses tmux new -A (attach-or-create), same as cw.
func tmuxSession(name, dir string) error {
	tmuxBin, err := exec.LookPath("tmux")
	if err != nil {
		return fmt.Errorf("tmux not found")
	}

	cmd := exec.Command(tmuxBin, "new", "-A", "-s", name, "-c", dir)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	return cmd.Run()
}

func inGitRepo() bool {
	err := exec.Command("git", "rev-parse", "--show-toplevel").Run()
	return err == nil
}

func repoName() (string, error) {
	out, err := exec.Command("git", "rev-parse", "--show-toplevel").Output()
	if err != nil {
		return "", fmt.Errorf("not in a git repo")
	}
	return filepath.Base(strings.TrimSpace(string(out))), nil
}

func runGit(args ...string) error {
	cmd := exec.Command("git", args...)
	cmd.Stdout = os.Stderr
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func nozRoot() string {
	if r := os.Getenv("NOZ_ROOT"); r != "" {
		return r
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, "worktrees")
}

func dirExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}
