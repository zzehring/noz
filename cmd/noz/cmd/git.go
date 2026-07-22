package cmd

import (
	"os/exec"
	"path/filepath"
	"strings"
)

// branchExists reports whether a local branch with the given name exists.
func branchExists(name string) bool {
	return exec.Command("git", "show-ref", "--verify", "--quiet", "refs/heads/"+name).Run() == nil
}

// isWorktreeDir reports whether dir is itself the root of a git working tree (a
// worktree or a repo), as opposed to a plain directory. A directory can exist on
// disk without being a registered worktree — e.g. crumbs left behind by an
// interrupted `noz open` — and `dirExists` alone can't tell them apart. We
// require git's top-level to equal dir so a plain directory nested inside some
// unrelated repo doesn't read as a worktree.
func isWorktreeDir(dir string) bool {
	out, err := exec.Command("git", "-C", dir, "rev-parse", "--show-toplevel").Output()
	if err != nil {
		return false
	}
	top := strings.TrimSpace(string(out))
	// Resolve symlinks on both sides so macOS's /var → /private/var (and similar)
	// don't produce a spurious mismatch.
	if resolved, err := filepath.EvalSymlinks(top); err == nil {
		top = resolved
	}
	want := dir
	if resolved, err := filepath.EvalSymlinks(dir); err == nil {
		want = resolved
	}
	return top == want
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
