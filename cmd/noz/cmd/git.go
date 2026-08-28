package cmd

import (
	"fmt"
	"os/exec"
	"path"
	"path/filepath"
	"strings"
	"sync"
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

// --- repo identity ---------------------------------------------------------
//
// noz needs two names for a repo, and conflating them is issue #13.
//
//   - The directory name composes worktree paths ("<repo>-<slug>") and brain
//     paths (".noz/<repo>/..."). Both are load-bearing on disk: Claude keys
//     transcripts by absolute cwd, and the brain holds briefs and back-reports
//     that cannot regenerate. Renaming either orphans real user data.
//
//   - The identity tags the session (NOZ_REPO), scopes `noz ls` and the
//     prefix+g picker, and shows in the status bar. It comes from the remote,
//     so a fork and its upstream stay distinct even when both clone under the
//     same directory name, and one repo cloned under two different directory
//     names is recognised as one project.
//
// Only the identity changed in #13. Nothing on disk moved.

// repoNames resolves the repo containing cwd to both of its names in one git
// call. identity falls back to dirName when there is no usable remote, which is
// how noz behaved everywhere before #13.
//
// --show-toplevel doubles as the working-tree guard: it fails in a bare repo,
// where --git-common-dir alone would succeed and yield the repo's *parent*
// directory, silently naming the wrong thing.
func repoNames() (dirName, identity string, err error) {
	out, err := exec.Command("git", "rev-parse", "--show-toplevel", "--git-common-dir").Output()
	if err != nil {
		return "", "", fmt.Errorf("not in a git repo")
	}
	_, commonDir, ok := strings.Cut(strings.TrimSpace(string(out)), "\n")
	if !ok {
		return "", "", fmt.Errorf("not in a git repo")
	}
	// --git-common-dir comes back relative to cwd when cwd is inside the main
	// checkout. Its parent is that checkout, whether we started there or in a
	// worktree pointing at it.
	abs, err := filepath.Abs(strings.TrimSpace(commonDir))
	if err != nil {
		return "", "", fmt.Errorf("not in a git repo")
	}
	dir := filepath.Dir(abs)
	return filepath.Base(dir), repoIdentityFor(dir), nil
}

// repoDirName names directories, and must stay the basename. See above.
func repoDirName() (string, error) {
	dir, _, err := repoNames()
	return dir, err
}

// repoIdentity names the project. See above.
func repoIdentity() (string, error) {
	_, id, err := repoNames()
	return id, err
}

// repoIdentityFor derives the identity of the repo rooted at repoDir, falling
// back to the directory basename when there is no usable remote. A local-only
// repo is still a project; it just can't be told apart from a same-named one
// elsewhere.
func repoIdentityFor(repoDir string) string {
	if id := parseRemoteURL(identityRemoteURL(repoDir)); id != "" {
		return id
	}
	return filepath.Base(repoDir)
}

// remoteURLFor picks the remote that defines identity, from the output of
// `git config --get-regexp ^remote\..*\.url` ("remote.<name>.url <url>" per
// line; remote names may themselves contain dots).
//
// origin wins when present: in the fork workflow origin is *your* fork, and #13
// wants your fork and its upstream to read as distinct projects. Otherwise a
// single remote is unambiguous and is used, so a repo whose remote was renamed
// still gets a real identity instead of silently falling back. Several remotes
// and no origin is a genuine ambiguity, and noz declines to guess — the same
// rule it already applies to branch recovery: one candidate is a match, several
// are a question.
func remoteURLFor(configOut string) string {
	var sole string
	seen := 0
	for _, line := range strings.Split(configOut, "\n") {
		key, url, ok := strings.Cut(strings.TrimSpace(line), " ")
		if !ok {
			continue
		}
		name, ok := strings.CutPrefix(key, "remote.")
		if !ok {
			continue
		}
		name, ok = strings.CutSuffix(name, ".url")
		if !ok || name == "" {
			continue
		}
		if url = strings.TrimSpace(url); url == "" {
			continue
		}
		if name == "origin" {
			return url
		}
		sole = url
		seen++
	}
	if seen == 1 {
		return sole
	}
	return ""
}

// remoteURLCache memoises lookups for the life of the process: `noz ls` asks
// once per session and sessions share main repos, so this is one lookup per
// repo rather than per session. Nothing is written to disk (PRINCIPLES #1). The
// mutex is because nothing about this map would stop a future caller from
// resolving sessions concurrently.
var (
	remoteURLMu    sync.Mutex
	remoteURLCache = map[string]string{}
)

// identityRemoteURL returns the URL of the remote that defines repoDir's
// identity, or "" when there is none or the choice is ambiguous.
func identityRemoteURL(repoDir string) string {
	remoteURLMu.Lock()
	defer remoteURLMu.Unlock()
	if url, ok := remoteURLCache[repoDir]; ok {
		return url
	}
	// git exits non-zero when the repo has no remotes at all; that yields ""
	// and the caller falls back to the directory name.
	url := ""
	if out, err := exec.Command("git", "-C", repoDir, "config", "--get-regexp", `^remote\..*\.url`).Output(); err == nil {
		url = remoteURLFor(string(out))
	}
	remoteURLCache[repoDir] = url
	return url
}

// parseRemoteURL normalizes a git remote URL to "org/repo", keeping any deeper
// nesting ("group/subgroup/repo") because that is part of the identity on
// GitLab. The host is deliberately dropped: the tag is shown in the tmux status
// bar where width is scarce, and one user having the same org/repo on two hosts
// is rare enough to accept. Returns "" for anything that isn't confidently a
// hosted repo — a local path, a bare name, a file:// URL — so callers fall back
// to the directory basename rather than inventing an identity (PRINCIPLES #6).
func parseRemoteURL(raw string) string {
	s := strings.TrimSpace(raw)
	if s == "" {
		return ""
	}
	if scheme, rest, found := strings.Cut(s, "://"); found {
		// A file:// remote is a local path, not a hosted identity.
		if strings.EqualFold(scheme, "file") {
			return ""
		}
		// scheme://[user@]host[:port]/org/repo.git
		_, p, ok := strings.Cut(rest, "/")
		if !ok {
			return ""
		}
		return cleanRepoPath(p)
	}
	// scp-like syntax: [user@]host:org/repo.git
	_, p, found := strings.Cut(s, ":")
	if !found {
		return "" // a bare path or name; nothing hosted to read
	}
	return cleanRepoPath(p)
}

// cleanRepoPath trims a remote URL's path to "org/repo". Fewer than two
// segments, or any empty segment, means we misread the URL — return "" and let
// the caller fall back rather than emit half an identity.
func cleanRepoPath(p string) string {
	p = strings.TrimSuffix(strings.Trim(p, "/"), ".git")
	p = strings.Trim(p, "/")
	if !strings.Contains(p, "/") {
		return ""
	}
	for _, seg := range strings.Split(p, "/") {
		if seg == "" {
			return ""
		}
	}
	return p
}

// repoTagMatches reports whether a live session's NOZ_REPO tag refers to the
// same repo as identity. Sessions created before #13 carry the bare directory
// basename, so that is accepted too — otherwise every session running across
// the upgrade would render as idle while plainly alive, which is the
// looks-like-it-worked failure #21 is about. An untagged session matches
// anything, as it always has. The tolerance costs nothing and lapses on its own
// as sessions are re-opened.
func repoTagMatches(tag, identity string) bool {
	if tag == "" || tag == identity {
		return true
	}
	return tag == path.Base(identity)
}
