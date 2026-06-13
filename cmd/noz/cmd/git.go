package cmd

import "strings"

// worktreeMainRepo parses the content of a git worktree's `.git` pointer file
// ("gitdir: /path/to/repo/.git/worktrees/<name>") and returns the main
// repository's working-directory path. Returns "" when the content is not a
// worktree pointer (e.g. a normal repo where `.git` is a directory).
func worktreeMainRepo(gitFileContent string) string {
	gitdir, ok := strings.CutPrefix(strings.TrimSpace(gitFileContent), "gitdir: ")
	if !ok {
		return ""
	}
	base, _, found := strings.Cut(gitdir, "/.git/worktrees/")
	if !found {
		return ""
	}
	return base
}
