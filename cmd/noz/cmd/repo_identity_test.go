package cmd

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// parseRemoteURL is the whole of #13's judgement, so it's tested as a pure
// function: every URL shape git accepts, plus the ones that must fall back to
// the directory basename rather than yield half an identity.
func TestParseRemoteURL(t *testing.T) {
	cases := map[string]string{
		// The forms a real clone produces.
		"https://github.com/zzehring/noz.git":    "zzehring/noz",
		"https://github.com/zzehring/noz":        "zzehring/noz",
		"http://github.com/zzehring/noz.git":     "zzehring/noz",
		"git@github.com:zzehring/noz.git":        "zzehring/noz",
		"git@github.com:zzehring/noz":            "zzehring/noz",
		"ssh://git@github.com/zzehring/noz.git":  "zzehring/noz",
		"ssh://git@github.com:2222/zz/noz.git":   "zz/noz",
		"https://user:tok@github.com/zz/noz.git": "zz/noz",
		"https://github.com/zzehring/noz.git/":   "zzehring/noz",
		"  https://github.com/zzehring/noz.git ": "zzehring/noz",
		// GitLab subgroups are part of the identity, so nesting is kept.
		"git@gitlab.com:acme/group/sub/repo.git": "acme/group/sub/repo",

		// Self-hosted forges (Forgejo/Gitea) — non-standard SSH and HTTP ports.
		"https://codeberg.org/zzehring/noz.git":     "zzehring/noz",
		"git@codeberg.org:zzehring/noz.git":         "zzehring/noz",
		"ssh://git@git.example.com:2222/zz/noz.git": "zz/noz",
		"https://git.example.com:3000/zz/noz.git":   "zz/noz",
		// A forge installed under a subpath keeps the subpath in the identity.
		// It stays deterministic and unique — which is all #13 needs — and from
		// the URL alone a subpath is indistinguishable from a GitLab subgroup,
		// so stripping it would be guessing (PRINCIPLES #6).
		"https://git.example.com/forgejo/zz/noz.git": "forgejo/zz/noz",
		// Not a hosted repo — caller falls back to the basename.
		"":                          "",
		"/Users/zz/projects/noz":    "",
		"../sibling-repo":           "",
		"file:///Users/zz/noz.git":  "",
		"https://github.com/single": "",
		"noz":                       "",
	}
	for in, want := range cases {
		if got := parseRemoteURL(in); got != want {
			t.Errorf("parseRemoteURL(%q) = %q, want %q", in, got, want)
		}
	}
}

// The fork/mirror cases from #13: same basename must not collide, and a repo
// cloned under a different directory name must not split.
func TestParseRemoteURLSeparatesForksAndJoinsRenames(t *testing.T) {
	upstream := parseRemoteURL("git@github.com:zzehring/noz.git")
	fork := parseRemoteURL("git@github.com:contributor/noz.git")
	if upstream == fork {
		t.Errorf("fork and upstream share identity %q — the false collision in #13", upstream)
	}
	// Cloned as "noz-fork" on disk; identity comes from the remote, not the dir.
	renamed := parseRemoteURL("git@github.com:contributor/noz.git")
	if renamed != fork {
		t.Errorf("a renamed clone got identity %q, want %q — the false split in #13", renamed, fork)
	}
}

// A session running across the upgrade carries the old bare-basename tag. It
// must keep matching its worktree, or `noz ls` reports a live session as idle —
// the looks-like-it-worked failure mode #21 is about.
func TestRepoTagMatches(t *testing.T) {
	cases := []struct {
		name     string
		tag      string
		identity string
		want     bool
	}{
		{"exact identity", "zzehring/noz", "zzehring/noz", true},
		{"pre-#13 basename tag still matches", "noz", "zzehring/noz", true},
		{"untagged session matches anything", "", "zzehring/noz", true},
		{"a different repo does not match", "other-org/tool", "zzehring/noz", false},
		{"same basename, different org, does not match", "contributor/noz", "zzehring/noz", false},
		{"basename-only identity (no remote)", "noz", "noz", true},
	}
	for _, c := range cases {
		if got := repoTagMatches(c.tag, c.identity); got != c.want {
			t.Errorf("%s: repoTagMatches(%q, %q) = %v, want %v", c.name, c.tag, c.identity, got, c.want)
		}
	}
}

// The tolerance in repoTagMatches is one-directional and narrow: it accepts a
// stale basename, but a same-basename repo under another org is still a
// different repo. This is the property that keeps the migration grace from
// re-introducing #13's false collision.
func TestRepoTagMatchesDoesNotReintroduceCollision(t *testing.T) {
	if repoTagMatches("noz", "zzehring/noz") != true {
		t.Error("a pre-#13 session in zzehring/noz should match its own worktree")
	}
	if repoTagMatches("contributor/noz", "zzehring/noz") {
		t.Error("a fork's session matched upstream's worktree — #13's collision, back again")
	}
}

// repoIdentityFor falls back to the basename when there is no usable remote,
// which is exactly noz's pre-#13 behaviour — a local-only repo is still a
// project, it just can't be distinguished from a same-named one elsewhere.
func TestRepoIdentityForFallsBackToBasename(t *testing.T) {
	dir := t.TempDir() // not a git repo, so no origin
	if got, want := repoIdentityFor(dir), pathBase(dir); got != want {
		t.Errorf("repoIdentityFor(%q) = %q, want the basename %q", dir, got, want)
	}
}

func pathBase(p string) string {
	for i := len(p) - 1; i >= 0; i-- {
		if p[i] == '/' {
			return p[i+1:]
		}
	}
	return p
}

func TestRemoteURLFor(t *testing.T) {
	cases := []struct {
		name string
		out  string
		want string
	}{
		{
			// The fork workflow: origin is your fork, and #13 wants your fork
			// and upstream to read as distinct projects.
			name: "origin wins over other remotes",
			out: "remote.upstream.url canonical.git\n" +
				"remote.origin.url fork.git",
			want: "fork.git",
		},
		{
			// A renamed remote still yields a real identity rather than
			// silently degrading to the directory basename.
			name: "no origin, exactly one remote is unambiguous",
			out:  "remote.upstream.url canonical.git",
			want: "canonical.git",
		},
		{
			// Genuine ambiguity — decline to guess; caller falls back.
			name: "no origin, several remotes",
			out: "remote.fork.url a.git\n" +
				"remote.upstream.url b.git",
			want: "",
		},
		{
			name: "remote names may contain dots",
			out:  "remote.my.fork.url dotted.git",
			want: "dotted.git",
		},
		{
			name: "entries without a URL are skipped",
			out: "remote.empty.url \n" +
				"remote.real.url real.git",
			want: "real.git",
		},
		{name: "garbage", out: "not a config line\n\n", want: ""},
		{name: "no remotes at all", out: "", want: ""},
	}
	for _, c := range cases {
		if got := remoteURLFor(c.out); got != c.want {
			t.Errorf("%s: remoteURLFor = %q, want %q", c.name, got, c.want)
		}
	}
}

// A bare repo has no working tree, so noz has nothing to open a session in and
// must report that rather than guess. mainRepoDir() resolves --git-common-dir,
// which succeeds in a bare repo and returns its *parent* directory — so without
// the working-tree guard in repoNames, `noz status` in a bare repo would
// silently name the parent folder as the repo.
func TestWorkingRepoDirRejectsBareRepo(t *testing.T) {
	bare := filepath.Join(t.TempDir(), "bare.git")
	if out, err := exec.Command("git", "init", "-q", "--bare", bare).CombinedOutput(); err != nil {
		t.Skipf("git init --bare unavailable: %v: %s", err, out)
	}

	restore := chdir(t, bare)
	defer restore()

	if dir, id, err := repoNames(); err == nil {
		t.Errorf("repoNames() in a bare repo returned %q/%q, want an error", dir, id)
	}
	if repo, err := repoDirName(); err == nil {
		t.Errorf("repoDirName() in a bare repo returned %q, want an error", repo)
	}
	if id, err := repoIdentity(); err == nil {
		t.Errorf("repoIdentity() in a bare repo returned %q, want an error", id)
	}
}

// chdir moves into dir for the duration of a test, returning a restore func.
func chdir(t *testing.T, dir string) func() {
	t.Helper()
	prev, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	return func() {
		if err := os.Chdir(prev); err != nil {
			t.Fatal(err)
		}
	}
}
