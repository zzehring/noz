// Command noz is a stateless CLI for managing AI-agent sessions as git
// worktrees and tmux sessions.
package main

import (
	"fmt"
	"os"

	"github.com/zzehring/noz/cmd/noz/cmd"
)

// Injected at build time by GoReleaser via -ldflags.
var (
	version = "dev"
	commit  = "none"
	date    = "unknown"
	builtBy = "local"
)

func main() {
	v := fmt.Sprintf("%s (commit %s, built %s by %s)", version, commit, date, builtBy)
	if err := cmd.Execute(v); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
