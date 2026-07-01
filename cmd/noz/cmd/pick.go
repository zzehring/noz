package cmd

import (
	"encoding/json"
	"fmt"
	"sort"

	"github.com/spf13/cobra"
)

// Picker views. Each is a first-class lens over live noz sessions, derived
// purely from the NOZ_* session tags — no stored state. The interactive tmux
// picker renders these with a native, filtered `choose-tree` (see
// `noz setup tmux`); this command is the scriptable / MCP-facing equivalent.
const (
	viewRepo     = "repo"     // sessions sharing the current session's NOZ_REPO
	viewChildren = "children" // sessions whose NOZ_PARENT == current session name
	viewAll      = "all"      // every live noz session
)

func newPickCmd() *cobra.Command {
	var (
		view     string
		asJSON   bool
		asFilter bool
		current  string
		repo     string
	)

	cmd := &cobra.Command{
		Use:   "pick [repo|children|all]",
		Short: "List switchable sessions for a view (repo/children/all)",
		Long: `Resolves which live noz sessions match a view, reading the NOZ_REPO /
NOZ_SLUG / NOZ_PARENT session tags.

This backs the interactive tmux picker (see 'noz setup tmux'): noz resolves the
matching session names here, and the binding feeds them to a native, filtered
'choose-tree'. Filtering happens in noz on purpose — tmux's own format filter
falls back to the server's global environment for any untagged session, so
filtering 'choose-tree' directly on NOZ_* is unreliable; matching on the
resolved session names is not.

Views (the current session is included — choose-tree highlights where you are):
  repo      sessions sharing this session's repo (default)
  children  offshoots spawned from this session (NOZ_PARENT == me)
  all       every live noz session, across every repo

Output forms:
  (default) one "<session>\t<label>" line per match, for scripting
  --json    a JSON array of session objects
  --filter  a tmux format-filter (matches the resolved names by session_name),
            for: choose-tree -f. Empty match set emits "0" (matches nothing).

Only live sessions are listed (idle worktrees aren't switchable).`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) > 0 {
				view = args[0]
			}
			switch view {
			case viewRepo, viewChildren, viewAll:
			default:
				return fmt.Errorf("unknown view %q (want: repo, children, all)", view)
			}
			if asJSON && asFilter {
				return fmt.Errorf("--json and --filter are mutually exclusive")
			}
			return runPick(cmd, view, asJSON, asFilter, current, repo)
		},
	}

	cmd.Flags().BoolVar(&asJSON, "json", false, "emit a JSON array of sessions")
	cmd.Flags().BoolVar(&asFilter, "filter", false, "emit a tmux format-filter over session_name (for choose-tree -f)")
	cmd.Flags().StringVar(&current, "current", "", "current session name (default: detected from tmux)")
	cmd.Flags().StringVar(&repo, "repo", "", "current session's repo (default: detected from current session's tag)")
	return cmd
}

func runPick(cmd *cobra.Command, view string, asJSON, asFilter bool, current, repo string) error {
	sessions, err := discoverSessions()
	if err != nil {
		return err
	}

	// Resolve the vantage point: who am I, and what repo am I in. Flags win
	// (handy for tests and scripts); otherwise derive from tmux tags.
	if current == "" {
		current = currentTmuxSession()
	}
	if repo == "" {
		for _, s := range sessions {
			if s.slug == current {
				repo = s.repo
				break
			}
		}
	}

	matches := pickSessions(sessions, view, current, repo)

	// Cluster by category then slug so prefixes group together.
	sort.Slice(matches, func(i, j int) bool {
		if matches[i].category != matches[j].category {
			return matches[i].category < matches[j].category
		}
		return matches[i].slug < matches[j].slug
	})

	switch {
	case asFilter:
		return emitPickFilter(cmd, matches)
	case asJSON:
		return emitPickJSON(cmd, matches)
	default:
		return emitPickPlain(cmd, matches, view)
	}
}

// emitPickFilter writes a tmux format-filter expression that is true only for
// the resolved sessions, matched by session_name — a real per-row format
// primitive, immune to the global-environment fallback that makes filtering on
// NOZ_* directly unreliable. Shape: a right-folded OR of equality checks, e.g.
//
//	#{||:#{==:#{session_name},a},#{==:#{session_name},b}}
//
// An empty match set emits "0" so `choose-tree -f` simply shows nothing rather
// than erroring. The expression contains no spaces, so it survives unquoted
// word-splitting in the tmux binding.
func emitPickFilter(cmd *cobra.Command, matches []sessionInfo) error {
	expr := "0"
	for i := len(matches) - 1; i >= 0; i-- {
		eq := "#{==:#{session_name}," + matches[i].slug + "}"
		if i == len(matches)-1 {
			expr = eq
		} else {
			expr = "#{||:" + eq + "," + expr + "}"
		}
	}
	fmt.Fprintln(cmd.OutOrStdout(), expr)
	return nil
}

// pickSessions filters discovered sessions down to those switchable under the
// given view. Only live (tmux-backed) sessions are switchable. The current
// session is kept — choose-tree shows (and highlights) where you are, like the
// native prefix+s tree it mirrors.
func pickSessions(sessions []sessionInfo, view, current, repo string) []sessionInfo {
	var out []sessionInfo
	for _, s := range sessions {
		if !s.hasTmux {
			continue
		}
		switch view {
		case viewRepo:
			// Scope to the current repo when we know it; if untagged, fall
			// back to showing everything rather than nothing.
			if repo != "" && s.repo != repo {
				continue
			}
		case viewChildren:
			if current == "" || s.parent != current {
				continue
			}
		case viewAll:
			// no filter
		}
		out = append(out, s)
	}
	return out
}

// pickLabel renders a single line label: slug, optional repo, agent/state, and
// a relative-time hint. showRepo is false for the repo view (constant repo).
func pickLabel(s sessionInfo, showRepo bool) string {
	label := s.slug
	if showRepo && s.repo != "" {
		label += fmt.Sprintf("  (%s)", s.repo)
	}
	meta := s.agent
	if stateLabel, _ := stateDisplay(s.state); stateLabel != "" {
		if meta != "" {
			meta += " · "
		}
		meta += stateLabel
	}
	if meta != "" {
		label += "  " + meta
	}
	if !s.lastActive.IsZero() {
		label += "  " + relativeTime(s.lastActive)
	}
	return label
}

func emitPickPlain(cmd *cobra.Command, matches []sessionInfo, view string) error {
	w := cmd.OutOrStdout()
	showRepo := view != viewRepo
	for _, s := range matches {
		fmt.Fprintf(w, "%s\t%s\n", s.slug, pickLabel(s, showRepo))
	}
	return nil
}

// pickSessionJSON is the stable shape emitted by `noz pick --json`.
type pickSessionJSON struct {
	Session string `json:"session"`
	Repo    string `json:"repo"`
	Parent  string `json:"parent,omitempty"`
	Agent   string `json:"agent,omitempty"`
	State   string `json:"state,omitempty"`
}

func emitPickJSON(cmd *cobra.Command, matches []sessionInfo) error {
	out := make([]pickSessionJSON, 0, len(matches))
	for _, s := range matches {
		out = append(out, pickSessionJSON{
			Session: s.slug,
			Repo:    s.repo,
			Parent:  s.parent,
			Agent:   s.agent,
			State:   s.state,
		})
	}
	enc := json.NewEncoder(cmd.OutOrStdout())
	enc.SetIndent("", "  ")
	return enc.Encode(out)
}
