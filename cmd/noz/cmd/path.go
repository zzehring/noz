package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

func newPathCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "path <slug>",
		Short: "Print a session's worktree directory (for cd)",
		Long: `Prints the absolute worktree path for a session so you can jump to it:

  cd "$(noz path my-slug)"

A handy shell helper:

  nzcd() { cd "$(noz path "$1")"; }

When the same slug exists in multiple repos, the current repo's wins.`,
		Args:              cobra.ExactArgs(1),
		ValidArgsFunction: completeSlugs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runPath(cmd, args[0])
		},
	}
}

func runPath(cmd *cobra.Command, slug string) error {
	sessions, err := discoverSessions()
	if err != nil {
		return err
	}

	curRepo, _ := repoName()
	var match *sessionInfo
	for i := range sessions {
		if sessions[i].slug != slug {
			continue
		}
		if sessions[i].repo == curRepo {
			match = &sessions[i]
			break // current-repo match wins
		}
		if match == nil {
			match = &sessions[i]
		}
	}
	if match == nil {
		return fmt.Errorf("no session %q found under %s", slug, nozRoot())
	}

	fmt.Fprintln(cmd.OutOrStdout(), match.dir)
	return nil
}
