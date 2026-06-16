package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"

	"github.com/spf13/cobra"
)

func newRestoreCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "restore [filter]",
		Short: "Re-create tmux sessions that were live before (e.g. after a reboot)",
		Long: `Re-creates tmux sessions for the worktrees that were live the last time
noz saw them — handy after a reboot, when the worktrees survive but the
tmux layer is gone. noz records the live set as you use it
(~/.cache/noz/live.json).

Sessions are created detached and tagged; it does NOT launch coding agents
(that would spin up many at once). Resume an agent yourself with
'claude --continue' in its worktree, or jump in with 'noz sw'.

  noz restore          # bring back the whole saved set
  noz restore cf       # only cf-* sessions
  noz restore ^review  # only review-* sessions`,
		Args: cobra.MaximumNArgs(1),
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

	want := loadLiveManifest()
	if len(want) == 0 {
		fmt.Fprintln(w, "noz: no saved session set to restore yet (noz records live sessions as you use it).")
		return nil
	}
	wantSet := make(map[string]bool, len(want))
	for _, s := range want {
		wantSet[s] = true
	}

	sessions, err := discoverSessions()
	if err != nil {
		return err
	}
	sessions = filterSessions(sessions, filter, false, false)

	tmuxBin, err := exec.LookPath("tmux")
	if err != nil {
		return fmt.Errorf("tmux not found")
	}

	restored, live := 0, 0
	for _, s := range sessions {
		if !wantSet[s.slug] {
			continue
		}
		if s.hasTmux {
			live++
			continue
		}
		if err := createDetachedSession(tmuxBin, s.slug, s.dir, s.repo); err != nil {
			fmt.Fprintf(os.Stderr, "noz: warning: could not restore %s: %v\n", s.slug, err)
			continue
		}
		fmt.Fprintf(w, "  %s●%s %s\n", cGreen, cReset, s.slug)
		restored++
	}

	fmt.Fprintf(w, "\n%snoz: restored %d session(s), %d already live%s\n", cGray, restored, live, cReset)
	if restored > 0 {
		fmt.Fprintf(w, "%snoz: jump in with 'noz sw'; resume an agent with 'claude --continue' in its worktree%s\n", cGray, cReset)
	}
	return nil
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

// --- live-session manifest -------------------------------------------------

func nozCacheDir() string {
	if d := os.Getenv("NOZ_CACHE_DIR"); d != "" {
		return d
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".cache", "noz")
}

func liveManifestPath() string {
	return filepath.Join(nozCacheDir(), "live.json")
}

// saveLiveManifest records the currently-live session slugs so `noz restore`
// can bring them back after a reboot. It never overwrites a good set with an
// empty one — so running `noz ls` post-reboot (when nothing is live) won't
// erase what was there. Best-effort.
func saveLiveManifest(slugs []string) {
	if len(slugs) == 0 {
		return
	}
	sort.Strings(slugs)
	data, err := json.MarshalIndent(slugs, "", "  ")
	if err != nil {
		return
	}
	if err := os.MkdirAll(nozCacheDir(), 0755); err != nil {
		return
	}
	os.WriteFile(liveManifestPath(), data, 0644)
}

func loadLiveManifest() []string {
	data, err := os.ReadFile(liveManifestPath())
	if err != nil {
		return nil
	}
	var slugs []string
	if err := json.Unmarshal(data, &slugs); err != nil {
		return nil
	}
	return slugs
}
