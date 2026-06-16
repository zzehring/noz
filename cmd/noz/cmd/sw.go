package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"sort"
	"strings"

	"github.com/spf13/cobra"
)

func newSwCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "sw [filter]",
		Aliases: []string{"switch"},
		Short:   "Fuzzy-switch to a live session",
		Long: `Opens an fzf picker over live noz sessions (across all repos) and
switches to the one you pick. If you're inside tmux it switch-clients;
otherwise it attaches.

Unlike tmux's own session picker, this lists only noz worktree sessions,
grouped by slug prefix, with last-activity hints. The preview pane shows
the session's tmux windows.

Pass a filter to pre-narrow the list (substring, or ^prefix):
  noz sw            # pick from all live sessions
  noz sw cf         # only cf-* sessions
  noz sw ^review    # only review-* sessions`,
		Args:              cobra.MaximumNArgs(1),
		ValidArgsFunction: completeTmuxSessions,
		RunE: func(cmd *cobra.Command, args []string) error {
			filter := ""
			if len(args) > 0 {
				filter = args[0]
			}
			return runSw(cmd, filter)
		},
	}
	return cmd
}

func runSw(cmd *cobra.Command, filter string) error {
	if _, err := exec.LookPath("fzf"); err != nil {
		return fmt.Errorf("fzf not found — install it (brew install fzf) or use 'noz pair <slug>'")
	}

	sessions, err := discoverSessions()
	if err != nil {
		return err
	}
	// Only live sessions are switchable.
	sessions = filterSessions(sessions, filter, true, false)

	if len(sessions) == 0 {
		fmt.Fprintln(cmd.OutOrStdout(), "No live sessions to switch to.")
		return nil
	}

	// Sort by category then slug so prefixes cluster together.
	sort.Slice(sessions, func(i, j int) bool {
		if sessions[i].category != sessions[j].category {
			return sessions[i].category < sessions[j].category
		}
		return sessions[i].slug < sessions[j].slug
	})

	slug, err := fzfPick(sessions)
	if err != nil {
		return err
	}
	if slug == "" {
		return nil // user cancelled
	}

	// If the picked session has no agent running (e.g. it was reaped) but has
	// prior Claude history, nudge to resume rather than start fresh.
	for _, s := range sessions {
		if s.slug == slug && s.agent == "" && hasClaudeHistory(s.dir) {
			resumeHint()
			break
		}
	}

	return switchToTmux(slug)
}

// fzfPick renders sessions into fzf and returns the chosen slug ("" = cancel).
// Each line is "<slug>\t<display>"; fzf shows only the display column and the
// preview pane lists that session's tmux windows.
func fzfPick(sessions []sessionInfo) (string, error) {
	maxSlug := 0
	for _, s := range sessions {
		if len(s.slug) > maxSlug {
			maxSlug = len(s.slug)
		}
	}

	var b strings.Builder
	for _, s := range sessions {
		meta := s.agent
		if s.state != "" {
			if meta != "" {
				meta += " "
			}
			meta += s.state
		}
		if !s.lastActive.IsZero() {
			meta = strings.TrimSpace(meta + "  " + relativeTime(s.lastActive))
		}
		display := fmt.Sprintf("%-*s  %-16s  %s", maxSlug, s.slug, s.repo, meta)
		fmt.Fprintf(&b, "%s\t%s\n", s.slug, display)
	}

	fzf := exec.Command("fzf",
		"--prompt", "noz> ",
		"--height", "40%",
		"--reverse",
		"--delimiter", "\t",
		"--with-nth", "2",
		"--preview", "tmux list-windows -t {1} 2>/dev/null",
		"--preview-window", "down,3",
	)
	fzf.Stdin = strings.NewReader(b.String())
	fzf.Stderr = os.Stderr

	out, err := fzf.Output()
	if err != nil {
		// fzf exits non-zero on cancel (130) — treat as no selection.
		return "", nil
	}

	line := strings.TrimSpace(string(out))
	if line == "" {
		return "", nil
	}
	slug, _, _ := strings.Cut(line, "\t")
	return slug, nil
}

// switchToTmux jumps to an existing session without re-tagging it (the
// session already carries its own NOZ_SLUG/NOZ_REPO from creation).
func switchToTmux(name string) error {
	tmuxBin, err := exec.LookPath("tmux")
	if err != nil {
		return fmt.Errorf("tmux not found")
	}
	if os.Getenv("TMUX") != "" {
		return exec.Command(tmuxBin, "switch-client", "-t", name).Run()
	}
	cmd := exec.Command(tmuxBin, "attach", "-t", name)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}
