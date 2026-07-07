package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
)

func newMvCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "mv <old-slug> <new-slug>",
		Short: "Rename a session (worktree dir, tmux session, git branch)",
		Long: `Renames a session across all layers:
  1. Renames the worktree directory
  2. Renames the tmux session (if active)
  3. Renames the git branch (if it matches the old slug)

Stateless — just renames the underlying resources.

Examples:
  noz mv investigate-thing feature-investigate   # recategorize
  noz mv typo-name correct-name                   # fix a name`,
		Args:              cobra.ExactArgs(2),
		ValidArgsFunction: completeTmuxSessions,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runMv(args[0], args[1])
		},
	}

	return cmd
}

func runMv(oldSlug, newSlug string) error {
	if err := validSlug(oldSlug); err != nil {
		return err
	}
	if err := validSlug(newSlug); err != nil {
		return err
	}
	if oldSlug == newSlug {
		return fmt.Errorf("old and new slug are the same")
	}

	root := nozRoot()
	renamed := false

	// Detect repo from old worktree dir
	repo := ""
	oldDir := ""

	// Try repo-prefixed dir first (worktree)
	if inGitRepo() {
		if r, err := repoName(); err == nil {
			candidate := filepath.Join(root, r+"-"+oldSlug)
			if dirExists(candidate) {
				repo = r
				oldDir = candidate
			}
		}
	}

	// Fallback: scan for the dir
	if oldDir == "" {
		entries, err := os.ReadDir(root)
		if err == nil {
			for _, e := range entries {
				if !e.IsDir() {
					continue
				}
				_, slug := detectRepo(filepath.Join(root, e.Name()), e.Name())
				if slug == oldSlug {
					detectedRepo, _ := detectRepo(filepath.Join(root, e.Name()), e.Name())
					repo = detectedRepo
					oldDir = filepath.Join(root, e.Name())
					break
				}
			}
		}
	}

	// Rename worktree directory
	if oldDir != "" {
		newDirName := newSlug
		if repo != "" {
			newDirName = repo + "-" + newSlug
		}
		newDir := filepath.Join(root, newDirName)

		if dirExists(newDir) {
			return fmt.Errorf("target directory already exists: %s", newDir)
		}

		// git worktree move (handles git internals)
		err := exec.Command("git", "worktree", "move", oldDir, newDir).Run()
		if err != nil {
			// Fallback to plain rename for scratch dirs
			if err := os.Rename(oldDir, newDir); err != nil {
				return fmt.Errorf("renaming directory: %w", err)
			}
		}
		fmt.Fprintf(os.Stderr, "noz: renamed worktree %s -> %s\n", filepath.Base(oldDir), filepath.Base(newDir))
		renamed = true

		if repo != "" {
			if err := os.Rename(contextFilePath(repo, oldSlug), contextFilePath(repo, newSlug)); err != nil && !os.IsNotExist(err) {
				fmt.Fprintf(os.Stderr, "noz: warning: could not rename context file: %v\n", err)
			}
		}
	}

	// Rename tmux session
	if tmuxHasSession(oldSlug) {
		if err := exec.Command("tmux", "rename-session", "-t", oldSlug, newSlug).Run(); err != nil {
			fmt.Fprintf(os.Stderr, "noz: warning: could not rename tmux session: %v\n", err)
		} else {
			fmt.Fprintf(os.Stderr, "noz: renamed tmux session %s -> %s\n", oldSlug, newSlug)
			renamed = true
		}
	}

	// Rename git branch (only if branch name matches old slug)
	if repo != "" {
		newDir := filepath.Join(root, repo+"-"+newSlug)
		out, err := exec.Command("git", "-C", newDir, "branch", "--show-current").Output()
		if err == nil {
			currentBranch := strings.TrimSpace(string(out))
			if currentBranch == oldSlug {
				if err := exec.Command("git", "-C", newDir, "branch", "-m", oldSlug, newSlug).Run(); err != nil {
					fmt.Fprintf(os.Stderr, "noz: warning: could not rename branch: %v\n", err)
				} else {
					fmt.Fprintf(os.Stderr, "noz: renamed branch %s -> %s\n", oldSlug, newSlug)
				}
			}
		}
	}

	if !renamed {
		return fmt.Errorf("no session found for %q", oldSlug)
	}

	return nil
}

