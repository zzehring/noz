package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"
)

// closeOptions controls how `noz close` (and noz_close) tears down the current
// session: discard-if-dirty, keep the worktree, delete the branch, local
// fast-forward merge first, and an optional report saved to the brain.
type closeOptions struct {
	force        bool
	keepWorktree bool
	deleteBranch bool
	merge        bool
	report       string
}

func newCloseCmd() *cobra.Command {
	var opts closeOptions
	var reportFile string

	cmd := &cobra.Command{
		Use:   "close",
		Short: "Close the session you're in (hop away, then tear it down)",
		Long: `Closes the current noz session: hops your tmux client to your parent (or last
session), then removes this session's worktree and tmux session.

The in-session counterpart to ` + "`noz rm`" + `, which refuses to remove the session
you're sitting in. close gets you out safely first.

  noz close                          # tear down the current session, hop away
  noz close --report "fixed X; ..."  # leave a report in the brain for the parent
  noz close --merge                  # fast-forward this branch into main, then tear down
  noz close --keep-worktree          # just end the tmux session, keep the worktree
  noz close -f                       # discard a dirty worktree`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if reportFile != "" {
				data, err := os.ReadFile(reportFile)
				if err != nil {
					return fmt.Errorf("reading --report-file: %w", err)
				}
				opts.report = string(data)
			}
			return runClose(opts)
		},
	}

	cmd.Flags().BoolVarP(&opts.force, "force", "f", false, "discard a dirty worktree without confirming")
	cmd.Flags().BoolVar(&opts.keepWorktree, "keep-worktree", false, "only end the tmux session, keep the worktree")
	cmd.Flags().BoolVar(&opts.deleteBranch, "delete-branch", false, "also delete the local branch")
	cmd.Flags().BoolVar(&opts.merge, "merge", false, "fast-forward this branch into the main checkout first (local; implies --delete-branch)")
	cmd.Flags().StringVar(&opts.report, "report", "", "save a report to the brain before closing (surfaced to the parent)")
	cmd.Flags().StringVar(&reportFile, "report-file", "", "read the report body from a file")

	return cmd
}

func runClose(opts closeOptions) error {
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
	repo := ""
	if wtDir != "" {
		repo = strings.TrimSuffix(filepath.Base(wtDir), "-"+slug)
	}

	// Refuse a dirty teardown up front — before we hop away — so we never block
	// on a confirmation prompt in a pane the user can no longer see.
	if !opts.keepWorktree && !opts.force && wtDir != "" && dirExists(wtDir) && worktreeIsDirty(wtDir) {
		return fmt.Errorf("%s has uncommitted changes — commit them, or `noz close -f` to discard", wtDir)
	}

	// Resolve the main repo dir while we're still inside the worktree, so the
	// git ops below (and our CWD) stay valid after we delete this worktree.
	mainRepo := mainRepoDir()
	parent := sessionParent(slug)
	deleteBranch := opts.deleteBranch

	// Save a report to the brain before teardown, so the parent (or future-you)
	// has the context without re-entering the session.
	reportSaved := false
	if strings.TrimSpace(opts.report) != "" && repo != "" {
		if p, err := writeSessionReport(repo, slug, parent, opts.report); err != nil {
			fmt.Fprintf(os.Stderr, "noz: warning: could not save report: %v\n", err)
		} else {
			reportSaved = true
			fmt.Fprintf(os.Stderr, "noz: saved report to %s\n", p)
		}
	}

	// Local fast-forward merge into the main checkout, before we move or tear
	// down — a failure leaves you exactly where you are, with a clear reason.
	if opts.merge {
		branch, base, err := localMerge(wtDir, mainRepo)
		if err != nil {
			return err
		}
		fmt.Fprintf(os.Stderr, "noz: merged %s into %s\n", branch, base)
		deleteBranch = true // merged — safe to drop the branch
	}

	// Land where you'd actually want to be: the parent you spawned this from
	// (lineage), then your last session. If neither is known, detach to the
	// shell — don't guess, and don't let tmux fling you to an arbitrary session.
	target := parent
	if target == "" || target == slug || !tmuxHasSession(target) {
		target = lastTmuxSession()
	}
	if target != "" && target != slug && tmuxHasSession(target) {
		exec.Command(tmuxBin, "switch-client", "-t", target).Run()
		msg := fmt.Sprintf("noz: closed %s → %s", slug, target)
		if reportSaved {
			msg += fmt.Sprintf(" (report: .noz/reports/%s.md)", slug)
		}
		exec.Command(tmuxBin, "display-message", msg).Run()
	} else {
		exec.Command(tmuxBin, "detach-client", "-s", slug).Run()
	}

	// Step out of the doomed worktree so git/filesystem ops stay valid.
	if mainRepo != "" {
		os.Chdir(mainRepo)
	} else {
		os.Chdir(root)
	}

	return teardownSession(slug, wtDir, scratchDir, root, opts.force, opts.keepWorktree, deleteBranch)
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

// reportFilePath is where a session's back-report lives in the shared .noz brain
// (root/.noz/<repo>/reports/<slug>.md) — the up-channel from a session to its
// parent, alongside the context/ down-channel.
func reportFilePath(repo, slug string) string {
	return filepath.Join(nozRoot(), ".noz", repo, "reports", slug+".md")
}

// writeSessionReport saves a session's back-report to the brain and returns its
// path. The body is whatever the closer passed (a summary of what happened here).
func writeSessionReport(repo, slug, parent, body string) (string, error) {
	var b strings.Builder
	fmt.Fprintf(&b, "# Report: %s\n\n_", slug)
	b.WriteString(time.Now().Format("2006-01-02 15:04"))
	if parent != "" {
		fmt.Fprintf(&b, " · from %s", parent)
	}
	b.WriteString("_\n\n")
	b.WriteString(strings.TrimSpace(body))
	b.WriteString("\n")

	path := reportFilePath(repo, slug)
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return "", err
	}
	if err := os.WriteFile(path, []byte(b.String()), 0644); err != nil {
		return "", err
	}
	return path, nil
}

// localMerge fast-forwards the session's branch into the main checkout's current
// branch. Returns the (branch, base) it merged. Errors on a detached HEAD (e.g.
// a --pr review session) or a non-fast-forward (divergence).
func localMerge(wtDir, mainRepo string) (branch, base string, err error) {
	if mainRepo == "" {
		return "", "", fmt.Errorf("not in a git repo — nothing to merge")
	}
	branch = worktreeBranchName(wtDir)
	if branch == "" || branch == "HEAD" {
		return "", "", fmt.Errorf("no branch to merge (detached HEAD)")
	}
	base = worktreeBranchName(mainRepo)
	out, e := exec.Command("git", "-C", mainRepo, "merge", "--ff-only", branch).CombinedOutput()
	if e != nil {
		return branch, base, fmt.Errorf("merge %s into %s failed (not a fast-forward — rebase the branch, or merge it manually): %s", branch, base, strings.TrimSpace(string(out)))
	}
	return branch, base, nil
}

// worktreeBranchName returns the current branch of a worktree, or "" / "HEAD"
// when detached.
func worktreeBranchName(dir string) string {
	out, err := exec.Command("git", "-C", dir, "rev-parse", "--abbrev-ref", "HEAD").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}
