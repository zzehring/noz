package cmd

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

// statusInfo is the machine-readable shape of `noz status --json` (handy for a
// shell-prompt segment).
type statusInfo struct {
	InSession  bool   `json:"in_session"`
	Slug       string `json:"slug,omitempty"`
	Repo       string `json:"repo,omitempty"`
	Branch     string `json:"branch,omitempty"`
	Agent      string `json:"agent,omitempty"`
	State      string `json:"state,omitempty"`
	Windows    int    `json:"windows,omitempty"`
	Attached   bool   `json:"attached"`
	LastActive string `json:"last_active,omitempty"`
	Dir        string `json:"dir,omitempty"`
}

func newStatusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Show the current session's context (where am I)",
		Long: `Prints context for the session you're in: slug, repo, branch, Claude
state (working/waiting), and tmux activity. Derived live from tmux + git;
run it from inside a noz tmux session.

Use --json for a machine-readable form (e.g. a shell-prompt segment).`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runStatus(cmd, jsonOutput)
		},
	}
}

func runStatus(cmd *cobra.Command, jsonOut bool) error {
	w := cmd.OutOrStdout()

	cwd, _ := os.Getwd()
	repo, _ := repoName()
	branch := gitBranch()
	slug := currentTmuxSession()

	info := statusInfo{Repo: repo, Branch: branch, Dir: cwd}
	if slug != "" {
		td := getTmuxDetails()[slug]
		info.InSession = true
		info.Slug = slug
		info.Agent = td.agent
		info.Windows = td.windows
		info.Attached = td.attached
		if !td.lastActive.IsZero() {
			info.LastActive = relativeTime(td.lastActive)
		}
		info.State = claudeState(sessionInfo{slug: slug, hasTmux: true, lastActive: td.lastActive})
	}

	if jsonOut {
		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ")
		return enc.Encode(info)
	}

	initColors()

	if !info.InSession {
		fmt.Fprintf(w, "%snot in a noz tmux session%s\n", cGray, cReset)
		if repo != "" {
			fmt.Fprintf(w, "  repo    %s\n", repo)
			fmt.Fprintf(w, "  branch  %s\n", branchDisplay(branch))
			fmt.Fprintf(w, "  dir     %s\n", cwd)
		}
		return nil
	}

	marker := cGreen + "●" + cReset
	if info.Attached {
		marker = cGreen + "▶" + cReset
	}
	fmt.Fprintf(w, "%s %s%s%s", marker, cBold, info.Slug, cReset)
	if label, color := stateDisplay(info.State); label != "" {
		fmt.Fprintf(w, "  %s%s%s", color, label, cReset)
	}
	fmt.Fprintln(w)

	if repo != "" {
		fmt.Fprintf(w, "  repo    %s\n", repo)
	}
	fmt.Fprintf(w, "  branch  %s\n", branchDisplay(branch))
	if info.Agent != "" {
		fmt.Fprintf(w, "  agent   %s\n", info.Agent)
	}
	last := info.LastActive
	if last == "" {
		last = "?"
	}
	fmt.Fprintf(w, "  tmux    %d window(s), active %s\n", info.Windows, last)
	fmt.Fprintf(w, "  dir     %s\n", cwd)
	return nil
}

// branchDisplay renders a detached HEAD readably.
func branchDisplay(branch string) string {
	if branch == "" {
		return "—"
	}
	if branch == "HEAD" {
		return "(detached)"
	}
	return branch
}
