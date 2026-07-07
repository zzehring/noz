package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/spf13/cobra"
)

func newPruneCmd() *cobra.Command {
	var force bool
	var maxAge string
	var all bool

	cmd := &cobra.Command{
		Use:   "prune",
		Short: "Remove stale sessions (no tmux, older than threshold)",
		Long: `Finds worktree directories with no active tmux session and older than
the age threshold, then removes them.

Preview-only by default — shows what would be removed without deleting.
Use --force to actually remove.

Examples:
  noz prune                    # preview: show stale sessions (7d default)
  noz prune --force            # actually remove them
  noz prune --age 3d --force   # remove sessions older than 3 days
  noz prune --all --force      # remove ALL sessions without active tmux`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runPrune(cmd, force, maxAge, all)
		},
	}

	cmd.Flags().BoolVar(&force, "force", false, "actually remove (default is preview only)")
	cmd.Flags().BoolVar(&all, "all", false, "remove all sessions without active tmux (ignore age)")
	cmd.Flags().StringVar(&maxAge, "age", "7d", "remove sessions older than this (e.g., 1d, 3d, 7d, 2w)")

	return cmd
}

type staleSession struct {
	name string
	path string
	age  time.Duration
	size string
}

func runPrune(cmd *cobra.Command, force bool, maxAge string, all bool) error {
	root := nozRoot()
	sessions := tmuxSessions()

	threshold, err := parseAge(maxAge)
	if err != nil {
		return fmt.Errorf("invalid age %q: %w", maxAge, err)
	}

	entries, err := os.ReadDir(root)
	if err != nil {
		if os.IsNotExist(err) {
			fmt.Fprintln(cmd.OutOrStdout(), "no sessions to prune")
			return nil
		}
		return fmt.Errorf("reading %s: %w", root, err)
	}

	var stale []staleSession
	var kept int

	for _, e := range entries {
		if !e.IsDir() {
			continue
		}

		name := e.Name()
		path := filepath.Join(root, name)
		_, slug := detectRepo(path, name)
		if slug == "" {
			kept++
			continue
		}

		info, err := e.Info()
		if err != nil {
			continue
		}
		age := time.Since(info.ModTime())
		hasTmux := sessions[slug]

		// Skip if tmux session is active
		if hasTmux {
			kept++
			continue
		}

		// Skip if not old enough (unless --all)
		if !all && age < threshold {
			kept++
			continue
		}

		size := dirSize(path)
		stale = append(stale, staleSession{
			name: name, path: path, age: age, size: size,
		})
	}

	// Sort by age descending
	sort.Slice(stale, func(i, j int) bool {
		return stale[i].age > stale[j].age
	})

	w := cmd.OutOrStdout()

	if len(stale) == 0 {
		fmt.Fprintf(w, "nothing to prune (%d active sessions)\n", kept)
		return nil
	}

	// Print what would be / will be removed
	action := "would remove"
	if force {
		action = "removing"
	}

	var totalRemoved, skipped int
	for _, s := range stale {
		dirty := ""
		if worktreeHasChanges(s.path) {
			dirty = "  (uncommitted — will skip)"
		}
		fmt.Fprintf(w, "  %s %-50s  age: %-8s  size: %s%s\n", action, s.name, formatAge(s.age), s.size, dirty)

		if force {
			if err := removeSessionDir(s.path, s.name); err != nil {
				fmt.Fprintf(os.Stderr, "  %v\n", err)
				skipped++
			} else {
				totalRemoved++
			}
		}
	}

	fmt.Fprintln(w)
	if force {
		fmt.Fprintf(w, "pruned %d sessions, kept %d", totalRemoved, kept)
		if skipped > 0 {
			fmt.Fprintf(w, ", skipped %d with changes", skipped)
		}
		fmt.Fprintln(w)
	} else {
		fmt.Fprintf(w, "%d sessions to prune, %d kept (use --force to remove)\n", len(stale), kept)
	}

	return nil
}

// isWithinRoot reports whether path is safely contained inside root. It guards
// destructive ops so a misconfigured $NOZ_ROOT can never point them outside the
// worktree tree (empty/"/"/"." roots are always rejected).
func isWithinRoot(root, path string) bool {
	root = filepath.Clean(root)
	if root == "" || root == "/" || root == "." {
		return false
	}
	rel, err := filepath.Rel(root, filepath.Clean(path))
	if err != nil {
		return false
	}
	return rel != "." && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

// worktreeHasChanges reports whether a git worktree has uncommitted OR
// untracked changes. Returns false for non-git dirs (e.g. scratch), which have
// no such concept.
func worktreeHasChanges(path string) bool {
	out, err := exec.Command("git", "-C", path, "status", "--porcelain").Output()
	if err != nil {
		return false
	}
	return len(strings.TrimSpace(string(out))) > 0
}

func removeSessionDir(path, name string) error {
	// Safety: never remove anything outside the noz worktree root.
	if !isWithinRoot(nozRoot(), path) {
		return fmt.Errorf("refusing to remove %q: outside the noz root", path)
	}
	_, slug := detectRepo(path, name)
	if slug == "" {
		slug = name // fallback label for error messages only
	}

	// Never discard uncommitted/untracked work — prune only removes clean ones.
	if worktreeHasChanges(path) {
		return fmt.Errorf("skipped %q: uncommitted/untracked changes (commit, or `noz rm %s --force`)", name, slug)
	}

	// Clean worktree → --force is safe here (nothing to lose; handles locks).
	// Falls back to RemoveAll for non-worktree scratch dirs.
	if err := exec.Command("git", "worktree", "remove", "--force", path).Run(); err != nil {
		if err := os.RemoveAll(path); err != nil {
			return err
		}
	}

	if tmuxHasSession(slug) && isNozSession(slug) {
		exec.Command("tmux", "kill-session", "-t", slug).Run()
	}

	return nil
}

func parseAge(s string) (time.Duration, error) {
	s = strings.TrimSpace(strings.ToLower(s))
	if rest, ok := strings.CutSuffix(s, "w"); ok {
		var weeks int
		if _, err := fmt.Sscanf(rest, "%d", &weeks); err != nil {
			return 0, err
		}
		return time.Duration(weeks) * 7 * 24 * time.Hour, nil
	}
	if rest, ok := strings.CutSuffix(s, "d"); ok {
		var days int
		if _, err := fmt.Sscanf(rest, "%d", &days); err != nil {
			return 0, err
		}
		return time.Duration(days) * 24 * time.Hour, nil
	}
	// Try Go duration format as fallback
	return time.ParseDuration(s)
}

func formatAge(d time.Duration) string {
	days := int(d.Hours() / 24)
	if days > 0 {
		return fmt.Sprintf("%dd", days)
	}
	hours := int(d.Hours())
	if hours > 0 {
		return fmt.Sprintf("%dh", hours)
	}
	return "<1h"
}

func dirSize(path string) string {
	out, err := exec.Command("du", "-sh", path).Output()
	if err != nil {
		return "?"
	}
	fields := strings.Fields(string(out))
	if len(fields) > 0 {
		return fields[0]
	}
	return "?"
}
