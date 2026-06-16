package cmd

import (
	"os/exec"
	"strings"
)

// branchExists reports whether a local branch with the given name exists.
func branchExists(name string) bool {
	return exec.Command("git", "show-ref", "--verify", "--quiet", "refs/heads/"+name).Run() == nil
}

// gitBranch returns the current branch name in the working directory, or ""
// if not in a repo. Returns "HEAD" when in a detached-HEAD state.
func gitBranch() string {
	out, err := exec.Command("git", "rev-parse", "--abbrev-ref", "HEAD").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

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
