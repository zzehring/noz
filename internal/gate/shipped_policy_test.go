package gate

import "testing"

// bash builds a bash CommandRequest the way cmd/noz/cmd/gate.go does.
func bash(cmd string, args ...string) CommandRequest {
	return CommandRequest{Tool: "bash", Cmd: cmd, Args: args, Mode: "autonomous"}
}

func file(tool, path string) CommandRequest {
	return CommandRequest{Tool: tool, Path: path, Mode: "autonomous"}
}

// sessionFile mirrors how gate.go builds a file request: workdir populated and
// NOZ_TMPDIR always present, so a policy indexing it cannot fail at eval time.
const testWorkDir = "/Users/x/worktrees/repo-slug"

func sessionFile(tool, path string) CommandRequest {
	return CommandRequest{
		Tool:    tool,
		Path:    path,
		WorkDir: testWorkDir,
		Env:     map[string]string{"NOZ_TMPDIR": "/tmp"},
		Mode:    "autonomous",
	}
}

func load(t *testing.T, name string) *Gate {
	t.Helper()
	g, err := New("../../policies/" + name + ".yaml")
	if err != nil {
		t.Fatalf("loading %s: %v", name, err)
	}
	return g
}

func check(t *testing.T, g *Gate, want Verdict, req CommandRequest, label string) {
	t.Helper()
	got := g.Evaluate(&req)
	if got.Verdict != want {
		t.Errorf("%s: got %s (rule %q), want %s", label, got.Verdict, got.Rule, want)
	}
}

// The dev policy let bash read a credential that the file tools refused,
// because the secret guards sat below safe-tools and rules are
// first-match-wins. Same file, two answers (#24).
func TestDevPolicyBashCannotBypassSecretGuard(t *testing.T) {
	g := load(t, "dev")

	for _, c := range []struct {
		label string
		req   CommandRequest
	}{
		{"cat a credentials file", bash("cat", "/Users/x/.aws/credentials")},
		{"cat a .env", bash("cat", ".env")},
		{"grep an ssh key", bash("grep", "-r", "x", "/Users/x/.ssh/id_rsa")},
		{"read tool, same target", file("read", "/Users/x/.aws/credentials")},
		{"dump the environment", bash("env")},
	} {
		check(t, g, Deny, c.req, c.label)
	}

	// The guards must not have swallowed the ordinary working set.
	for _, c := range []struct {
		label string
		req   CommandRequest
	}{
		{"cat a source file", bash("cat", "README.md")},
		{"run the tests", bash("go", "test", "./...")},
		{"local git", bash("git", "status")},
		{"read a source file", file("read", "/repo/main.go")},
	} {
		check(t, g, Allow, c.req, c.label)
	}
}

// brr is allowlist-only: the working set is allowed, everything outward or
// unenumerated is denied outright. PAUSE is never a brr verdict — an
// unattended run that pauses blocks forever.
func TestBrrPolicyIsAllowlistOnly(t *testing.T) {
	g := load(t, "brr")

	denied := []struct {
		label string
		req   CommandRequest
	}{
		{"curl the merge endpoint", bash("curl", "-X", "PUT", "https://api.github.com/repos/o/r/pulls/1/merge")},
		{"python as an interpreter", bash("python", "-c", "import os")},
		{"node as an interpreter", bash("node", "-e", "1")},
		{"nested shell", bash("bash", "-c", "echo hi")},
		{"gh pr merge", bash("gh", "pr", "merge", "8")},
		{"gh issue comment", bash("gh", "issue", "comment", "13", "--body", "x")},
		{"gh api, an arbitrary call", bash("gh", "api", "/repos/o/r")},
		{"force push", bash("git", "push", "--force", "origin", "main")},
		{"credential read", bash("cat", "/Users/x/.aws/credentials")},
		{"environment dump", bash("printenv")},
		{"privilege escalation", bash("sudo", "rm", "-rf", "/")},
		{"editing its own policy", file("write", "/repo/policies/brr.yaml")},
		{"editing hook settings", file("edit", "/repo/.claude/settings.json")},
		{"a tool nobody enumerated", bash("brew", "install", "jq")},
	}
	for _, c := range denied {
		check(t, g, Deny, c.req, c.label)
	}

	allowed := []struct {
		label string
		req   CommandRequest
	}{
		{"run the tests", bash("go", "test", "./...")},
		{"gofmt", bash("gofmt", "-l", ".")},
		{"lint", bash("golangci-lint", "run", "./...")},
		{"commit", bash("git", "commit", "-m", "x")},
		{"push a feature branch", bash("git", "push", "origin", "feature")},
		{"open a PR — the point of brr", bash("gh", "pr", "create", "--draft")},
		{"read the issue it was given", bash("gh", "issue", "view", "13")},
		{"search the tree", bash("rg", "TODO")},
		{"edit source", sessionFile("edit", testWorkDir+"/cmd/noz/main.go")},
	}
	for _, c := range allowed {
		check(t, g, Allow, c.req, c.label)
	}

	// No rule in brr may yield PAUSE.
	for _, c := range append(denied, allowed...) {
		if v := g.Evaluate(&c.req).Verdict; v == Pause {
			t.Errorf("%s: brr returned PAUSE; unattended runs cannot be paused", c.label)
		}
	}
}

// Writes outside the session worktree defeat every other rule in brr:
// ~/.gitconfig installs a credential helper, ~/.zshrc hijacks PATH, the noz
// binary *is* the broker, and the audit log is the only trail. All four were
// ALLOW before confine-writes.
func TestBrrConfinesWrites(t *testing.T) {
	g := load(t, "brr")

	for _, c := range []struct {
		label string
		path  string
	}{
		{"the noz binary", "/Users/x/go/bin/noz"},
		{"shell rc — PATH hijack", "/Users/x/.zshrc"},
		{"gitconfig — credential helper", "/Users/x/.gitconfig"},
		{"tmux config", "/Users/x/.tmux.conf"},
		{"a sibling session", "/Users/x/worktrees/repo-other/main.go"},
		{"$NOZ_ROOT itself", "/Users/x/worktrees/evil.sh"},
	} {
		check(t, g, Deny, sessionFile("write", c.path), "write "+c.label)
		check(t, g, Deny, sessionFile("edit", c.path), "edit "+c.label)
	}

	// The working set, including the brain noz symlinks in at <worktree>/.noz.
	for _, c := range []struct {
		label string
		path  string
	}{
		{"source in the session", testWorkDir + "/cmd/noz/main.go"},
		{"the per-repo brain", testWorkDir + "/.noz/brain/notes.md"},
		{"a temp file", "/tmp/scratch.txt"},
	} {
		check(t, g, Allow, sessionFile("write", c.path), "write "+c.label)
	}

	// Reads are deliberately not confined — only writes are.
	check(t, g, Allow, sessionFile("read", "/Users/x/.tmux.conf"), "read outside the session")
}

// startsWith("") is true for every path, so an unresolvable workdir must deny
// rather than silently switch containment off.
func TestBrrContainmentFailsClosedWithoutWorkdir(t *testing.T) {
	g := load(t, "brr")
	req := CommandRequest{
		Tool:    "write",
		Path:    "/Users/x/.gitconfig",
		WorkDir: "",
		Env:     map[string]string{"NOZ_TMPDIR": "/tmp"},
		Mode:    "autonomous",
	}
	check(t, g, Deny, req, "write with no resolvable session root")
}

// A cross-repo brain lives outside the session by definition (#31), so write
// containment has to be widenable — but narrowly. The brain is readable
// without being granted at all, since reads are not confined; write_roots
// grants only the subpath that should accept writes.
func TestBrrHonorsConfiguredWriteRoots(t *testing.T) {
	g := load(t, "brr")

	const brain = "/Users/x/brain-zack"
	withRoots := func(tool, path string) CommandRequest {
		r := sessionFile(tool, path)
		r.WriteRoots = []string{brain + "/inbox"}
		return r
	}

	check(t, g, Allow, withRoots("write", brain+"/inbox/note.md"), "write the granted subpath")
	check(t, g, Deny, withRoots("write", brain+"/decisions/adr-001.md"), "write outside the granted subpath")
	check(t, g, Allow, withRoots("read", brain+"/decisions/adr-001.md"), "read the brain without a grant")

	// A grant for one directory must not widen anything else.
	check(t, g, Deny, withRoots("write", "/Users/x/.gitconfig"), "a brain grant does not widen gitconfig")

	// And with no grant configured, the brain is read-only.
	check(t, g, Deny, sessionFile("write", brain+"/inbox/note.md"), "write the brain with no grant")
}

// brr may take exactly one outward action, and only because it is narrow
// enough to be unabusable: publish pushes the branch and opens a draft, with
// no parameter that turns it into a merge. The credential never enters the
// session, so allowing this is not a grant of forge access.
func TestBrrAllowsOnlyTheBrokeredOutwardAction(t *testing.T) {
	g := load(t, "brr")

	check(t, g, Allow, bash("noz", "publish"), "noz publish")
	check(t, g, Allow, bash("noz", "publish", "--dry-run"), "noz publish --dry-run")
	check(t, g, Allow, bash("noz", "ls"), "noz ls — navigation is free")

	// The helper relays a credential. The pipe is what stops it being an
	// oracle; this rule is defence in depth.
	check(t, g, Deny, bash("noz", "credential-helper", "get", "--fd", "3"), "noz credential-helper")

	// Create and destroy stay gated, and brr cannot rewrite its own gate.
	check(t, g, Deny, bash("noz", "rm", "a-session", "-f"), "noz rm")
	check(t, g, Deny, bash("noz", "setup", "claude"), "noz setup")
	check(t, g, Deny, bash("noz", "policy", "show"), "noz policy")

	// And the unbrokered outward paths are still shut, so publish is an
	// addition to the allowlist rather than a loosening of it.
	check(t, g, Deny, bash("gh", "pr", "merge", "8"), "gh pr merge")
	check(t, g, Deny, bash("curl", "https://api.github.com"), "curl")
}

// The secret list is an enumeration, and this test is the record of it losing
// once: it covered ~/.aws and ~/.ssh but not ~/.config/sops/age/keys.txt, the
// key that decrypts every other secret on the machine. Each entry here is a
// credential store something on a dev machine actually uses.
func TestSecretEnumerationCoversRealCredentialStores(t *testing.T) {
	for _, policy := range []string{"brr", "dev"} {
		g := load(t, policy)
		for _, path := range []string{
			"/Users/x/.config/sops/age/keys.txt", // decrypts everything else
			"/Users/x/.config/age/keys.txt",
			"/Users/x/.age/key.txt",
			"/Users/x/.config/gh/hosts.yml",   // gh writes a token here
			"/Users/x/.config/tea/config.yml", // tea writes a forgejo token here
			"/Users/x/.git-credentials",
			"/Users/x/.docker/config.json",
			"/Users/x/.npmrc",
			"/Users/x/Library/Keychains/login.keychain-db",
			"/Users/x/.aws/credentials",
			"/Users/x/.ssh/id_ed25519",
			"/Users/x/.netrc",
		} {
			check(t, g, Deny, file("read", path), policy+": read "+path)
		}

		// Reads the toolchain needs must survive: reads are not confined to an
		// allowlist, only denied by this list.
		check(t, g, Allow, file("read", "/usr/local/go/src/fmt/print.go"), policy+": Go stdlib")
	}
}

// pi and omp load extensions from these directories, and an extension runs
// in-process with the agent — writing one replaces the gate. This must hold
// even when the user has granted the directory as a write root, so the rule is
// stated unconditionally rather than left to write containment.
func TestBrrRefusesToLetTheAgentReplaceItsOwnGate(t *testing.T) {
	g := load(t, "brr")

	for _, path := range []string{
		"/Users/x/.omp/agent/config.yml",
		"/Users/x/.omp/agent/extensions/evil.ts",
		"/Users/x/.pi/agent/extensions/evil.ts",
		"/Users/x/worktrees/repo-slug/.pi/extensions/evil.ts", // project-local, inside the session
		"/Users/x/.claude/settings.json",
		"/repo/policies/brr.yaml",
	} {
		// Granted as a write root, which would otherwise satisfy containment.
		req := sessionFile("write", path)
		req.WriteRoots = []string{"/Users/x/.omp", "/Users/x/.pi", "/repo/policies"}
		check(t, g, Deny, req, "write "+path+" with the directory granted")
	}
}

// A policy that cannot run the project's own CI gate is unusable for
// development, however safe it is. dev allowed "go" but not "gofmt" or
// "golangci-lint", so an agent under it could not run what AGENTS.md requires.
func TestPoliciesAllowTheProjectsOwnGate(t *testing.T) {
	for _, policy := range []string{"dev", "brr"} {
		g := load(t, policy)
		for _, c := range [][]string{
			{"go", "build", "./cmd/noz"},
			{"go", "test", "./..."},
			{"go", "vet", "./..."},
			{"gofmt", "-l", "."},
			{"golangci-lint", "run", "./..."},
		} {
			check(t, g, Allow, bash(c[0], c[1:]...), policy+": "+c[0])
		}
	}
}

// A file literally named "credentials" (no extension) slipped past the suffix
// regex — found by a real agent QA-testing the policy against a decoy. The
// basename cases are the ones that matter: git credential stores and the like.
func TestSecretMatchCatchesBareBasenames(t *testing.T) {
	for _, policy := range []string{"dev", "brr"} {
		g := load(t, policy)
		check(t, g, Deny, file("read", "/home/u/project/credentials"), policy+": bare credentials")
		check(t, g, Deny, file("read", "/home/u/project/secrets"), policy+": bare secrets")
		check(t, g, Deny, bash("cat", "/home/u/project/credentials"), policy+": cat credentials")
		// Not a false positive on an ordinary file that merely contains the word.
		check(t, g, Allow, file("read", "/home/u/project/credentials-helper.go"), policy+": helper source")
	}
}
