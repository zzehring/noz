package cmd

import (
	"strings"

	"github.com/spf13/cobra"
	"github.com/zzehring/noz/internal/config"
	"github.com/zzehring/noz/internal/gate"
)

// completeTmuxSessions provides tab completion for tmux session names.
func completeTmuxSessions(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	if len(args) > 0 {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}

	var sessions []string
	for _, name := range tmuxSessionNames() {
		if strings.HasPrefix(name, toComplete) {
			sessions = append(sessions, name)
		}
	}
	return sessions, cobra.ShellCompDirectiveNoFileComp
}

// completeSlugs completes any discovered session slug (live or idle).
func completeSlugs(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	if len(args) > 0 {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	sessions, err := discoverSessions()
	if err != nil {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	var out []string
	for _, s := range sessions {
		if strings.HasPrefix(s.slug, toComplete) {
			out = append(out, s.slug)
		}
	}
	return out, cobra.ShellCompDirectiveNoFileComp
}

// completePolicyNames provides tab completion for policy names.
func completePolicyNames(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	var all []string

	// Local policies/ dir
	if local, err := gate.ListPolicies("policies"); err == nil {
		all = append(all, local...)
	}

	// ~/.config/noz/policies/
	if cfg, err := config.Load(""); err == nil {
		if global, err := gate.ListPolicies(cfg.PoliciesDir()); err == nil {
			all = append(all, global...)
		}
	}

	var matches []string
	for _, p := range all {
		if strings.HasPrefix(p, toComplete) {
			matches = append(matches, p)
		}
	}
	return matches, cobra.ShellCompDirectiveNoFileComp
}
