package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"sort"
	"time"

	"github.com/spf13/cobra"
)

func newRestoreCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "restore [filter]",
		Short: "Re-create tmux sessions that were live before (e.g. after a reboot)",
		Long: `Re-creates tmux sessions for worktrees you were recently working in —
handy after a reboot, when the worktrees survive but the tmux layer is gone.

With no argument, noz brings back idle worktrees with recent activity,
derived live from durable signals (agent transcripts, worktree mtime) —
nothing is persisted, so there's no state to drift or clobber. The window
defaults to 48h (override with NOZ_RESTORE_WINDOW, e.g. 1h, 72h) and is
capped so a reboot can't spin up dozens of sessions. With a filter, every
matching idle worktree is restored — naming something is intent.

Sessions are created detached and tagged; it does NOT launch coding agents
(that would spin up many at once). Resume an agent yourself with
'claude --continue' in its worktree, or jump in with 'noz open <slug>'.

  noz restore          # bring back recently-active worktrees
  noz restore cf       # only cf-* sessions
  noz restore ^review  # only review-* sessions`,
		Args:              cobra.MaximumNArgs(1),
		ValidArgsFunction: completeSlugs,
		RunE: func(cmd *cobra.Command, args []string) error {
			filter := ""
			if len(args) > 0 {
				filter = args[0]
			}
			return runRestore(cmd, filter)
		},
	}
	return cmd
}

func runRestore(cmd *cobra.Command, filter string) error {
	initColors()
	w := cmd.OutOrStdout()

	sessions, err := discoverSessions()
	if err != nil {
		return err
	}
	sessions = filterSessions(sessions, filter, false, false)

	tmuxBin, err := exec.LookPath("tmux")
	if err != nil {
		return fmt.Errorf("tmux not found")
	}

	// Which idle worktrees do we bring back? With an explicit filter, all that
	// match — naming something is intent. With no filter, derive "what was I
	// recently working on" from durable activity signals within a recency
	// window, capped, so a reboot doesn't spin up dozens of sessions.
	window := restoreWindow()
	skipped := 0
	if filter == "" {
		var cand []sessionInfo
		for _, s := range sessions {
			if s.hasTmux {
				continue // already running — not a restore candidate
			}
			act := sessionActivity(s.dir)
			if act.IsZero() || time.Since(act) > window {
				skipped++
				continue
			}
			s.lastActive = act
			cand = append(cand, s)
		}
		sort.Slice(cand, func(i, j int) bool { return cand[i].lastActive.After(cand[j].lastActive) })
		if len(cand) > restoreMaxAuto {
			skipped += len(cand) - restoreMaxAuto
			cand = cand[:restoreMaxAuto]
		}
		sessions = cand
	}

	restored, live := 0, 0
	var landed []sessionInfo // now-live sessions (restored or already running) — attach targets
	for _, s := range sessions {
		if s.hasTmux {
			live++
			landed = append(landed, s)
			continue
		}
		if err := createDetachedSession(tmuxBin, s.slug, s.dir, s.repo); err != nil {
			fmt.Fprintf(os.Stderr, "noz: warning: could not restore %s: %v\n", s.slug, err)
			continue
		}
		fmt.Fprintf(w, "  %s●%s %s\n", cGreen, cReset, s.slug)
		restored++
		landed = append(landed, s)
	}

	if restored+live == 0 {
		switch {
		case filter != "":
			fmt.Fprintf(w, "noz: nothing to restore for %q — no matching idle worktree.\n", filter)
		case skipped > 0:
			fmt.Fprintf(w, "noz: nothing active in the last %s to restore — name one with `noz restore <slug>` (see `noz ls`).\n", window)
		default:
			fmt.Fprintln(w, "noz: nothing to restore.")
		}
		return nil
	}

	if live > 0 {
		fmt.Fprintf(w, "\n%snoz: restored %d session(s), %d already live%s\n", cGray, restored, live, cReset)
	} else {
		fmt.Fprintf(w, "\n%snoz: restored %d session(s)%s\n", cGray, restored, cReset)
	}
	if skipped > 0 {
		fmt.Fprintf(w, "%snoz: %d other idle worktree(s) not restored — name one to bring it back (e.g. `noz restore <slug>`)%s\n", cGray, skipped, cReset)
	}

	// Land you on the session you asked for — the one you named, or the
	// most-recently-active of what we brought back. NEVER a bare `tmux attach`,
	// which grabs tmux's arbitrary last session (not what you restored).
	target := pickLandingTarget(landed)
	if target == "" {
		return nil
	}
	if os.Getenv("NOZ_NO_ATTACH") != "" || !stdoutIsTerminal() {
		fmt.Fprintf(w, "%snoz: jump in with 'noz open %s'; resume an agent with 'claude --continue'%s\n", cGray, target, cReset)
		return nil
	}
	if os.Getenv("TMUX") != "" {
		// Already in tmux — switch the client to the target rather than nesting.
		return exec.Command(tmuxBin, "switch-client", "-t", sessionTarget(target)).Run()
	}
	fmt.Fprintf(cmd.ErrOrStderr(), "noz: attaching to %s — 'noz open <slug>' to switch, 'noz back' to return, 'claude --continue' to resume an agent\n", target)
	att := exec.Command(tmuxBin, "attach", "-t", sessionTarget(target))
	att.Stdin, att.Stdout, att.Stderr = os.Stdin, os.Stdout, os.Stderr
	return att.Run()
}

// pickLandingTarget chooses which restored/live session to land the user on:
// the single match when there's exactly one, else the most-recently-active.
func pickLandingTarget(landed []sessionInfo) string {
	if len(landed) == 0 {
		return ""
	}
	best := landed[0]
	bestAct := sessionActivity(best.dir)
	for _, s := range landed[1:] {
		if a := sessionActivity(s.dir); a.After(bestAct) {
			best, bestAct = s, a
		}
	}
	return best.slug
}

// createDetachedSession creates a tagged tmux session without attaching.
// Window 0 is left unnamed so tmux auto-renames it to its running command.
func createDetachedSession(tmuxBin, slug, dir, repo string) error {
	c := exec.Command(tmuxBin, "new", "-d", "-s", slug, "-c", dir)
	c.Env = append(os.Environ(), "NOZ_SLUG="+slug, "NOZ_REPO="+repo)
	if err := c.Run(); err != nil {
		return err
	}
	tagNozSession(tmuxBin, slug, slug, repo)
	return nil
}

// --- stateless restore: derive "recently worked on" from durable signals ---

// restoreMaxAuto caps how many idle worktrees a no-argument `noz restore` will
// bring back, so a reboot can't spin up an unbounded number of sessions. Name
// a worktree explicitly to restore beyond the cap.
const restoreMaxAuto = 12

// restoreWindow is how far back a no-argument `noz restore` looks for activity.
func restoreWindow() time.Duration {
	if v := os.Getenv("NOZ_RESTORE_WINDOW"); v != "" {
		if d, err := time.ParseDuration(v); err == nil && d > 0 {
			return d
		}
	}
	return 48 * time.Hour
}

// sessionActivity returns the most recent durable signal that a worktree was
// being worked in — surviving a reboot, derived, never persisted by noz. It
// prefers the agent transcript (high-fidelity: "a session happened here"), and
// falls back to the worktree directory's own mtime.
func sessionActivity(dir string) time.Time {
	newest := claudeHistoryMtime(dir)
	if fi, err := os.Stat(dir); err == nil && fi.ModTime().After(newest) {
		newest = fi.ModTime()
	}
	return newest
}
