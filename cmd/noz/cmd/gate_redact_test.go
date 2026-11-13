package cmd

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// The guard log is append-only and permanent, so anything written there is
// written for good. These are the credential shapes that reached it before.
func TestRedactArgs(t *testing.T) {
	cases := []struct {
		name string
		cmd  string
		args []string
		want string
	}{
		{
			name: "bearer header",
			cmd:  "curl",
			args: []string{"-H", "Authorization: Bearer ghp_AbCdEfGhIjKlMnOpQrStUvWx", "https://api.github.com/user"},
			want: "curl -H Authorization: Bearer [redacted] https://api.github.com/user",
		},
		{
			name: "url userinfo keeps the host, drops the credential",
			cmd:  "git",
			args: []string{"clone", "https://zzehring:ghp_SuperSecretValue123456@github.com/zzehring/noz.git"},
			want: "git clone https://[redacted]@github.com/zzehring/noz.git",
		},
		{
			name: "secret flag masks the following value",
			cmd:  "gh",
			args: []string{"auth", "login", "--token", "ghp_AnotherSecretTokenHere123"},
			want: "gh auth login --token [redacted]",
		},
		{
			name: "--flag=value form",
			cmd:  "tool",
			args: []string{"--password=hunter2"},
			want: "tool --password=[redacted]",
		},
		{
			name: "a token passed positionally, recognised by its own shape",
			cmd:  "aws",
			args: []string{"configure", "set", "aws_access_key_id", "AKIAIOSFODNN7EXAMPLE"},
			want: "aws configure set aws_access_key_id [redacted]",
		},
		{
			name: "postgres dsn",
			cmd:  "psql",
			args: []string{"postgres://admin:hunter2@db.internal/prod"},
			want: "psql postgres://[redacted]@db.internal/prod",
		},
		{
			name: "a trailing secret flag must not look like it had no value",
			cmd:  "gh",
			args: []string{"auth", "--token"},
			want: "gh auth --token [redacted]",
		},
		{
			name: "ordinary commands are untouched",
			cmd:  "go",
			args: []string{"test", "./..."},
			want: "go test ./...",
		},
		{
			name: "no args",
			cmd:  "env",
			args: nil,
			want: "env",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := redactArgs(c.cmd, c.args); got != c.want {
				t.Errorf("redactArgs() =\n  %q\nwant\n  %q", got, c.want)
			}
		})
	}
}

// hasSecretPrefix needs a length floor, or ordinary words beginning with a
// known prefix ("sk-", "AKIA") get masked and the log becomes useless.
func TestHasSecretPrefixNeedsLength(t *testing.T) {
	for _, tok := range []string{"sk-", "AKIA", "ghp_", "skip", "AKIAing", "github_pat_"} {
		if hasSecretPrefix(tok) {
			t.Errorf("hasSecretPrefix(%q) = true; too short to be a credential", tok)
		}
	}
	for _, tok := range []string{"ghp_AbCdEfGhIjKlMnOp", "AKIAIOSFODNN7EXAMPLE", "xoxb-1234567890-abcdef"} {
		if !hasSecretPrefix(tok) {
			t.Errorf("hasSecretPrefix(%q) = false; want true", tok)
		}
	}
}

func TestRedactStringEnvAssignment(t *testing.T) {
	got := redactString(`export GITHUB_TOKEN=ghp_LeakedSecretValue123 && echo done`)
	if want := "export GITHUB_TOKEN=[redacted] && echo done"; got != want {
		t.Errorf("redactString() = %q, want %q", got, want)
	}
	// A non-sensitive assignment is left readable.
	if got := redactString("GOFLAGS=-mod=mod go build"); got != "GOFLAGS=-mod=mod go build" {
		t.Errorf("redactString() masked a harmless assignment: %q", got)
	}
}

// A prefix comparison in a policy only means what it reads as if the path has
// been made absolute and lexically clean first.
func TestNormalizePath(t *testing.T) {
	wd := "/Users/x/worktrees/repo-slug"
	cases := map[string]string{
		"main.go":                 wd + "/main.go",
		"./main.go":               wd + "/main.go",
		"sub/../main.go":          wd + "/main.go",
		wd + "/cmd/main.go":       wd + "/cmd/main.go",
		wd + "/../../.ssh/id_rsa": "/Users/x/.ssh/id_rsa",
		wd + "/./x/../y.go":       wd + "/y.go",
		"/etc/passwd":             "/etc/passwd",
	}
	for in, want := range cases {
		if got := normalizePath(in, wd); got != want {
			t.Errorf("normalizePath(%q) = %q, want %q", in, got, want)
		}
	}
}

// Taking cwd unconditionally was the bug: from / or $HOME, every path on the
// machine "starts with" the working directory, so containment allowed
// everything while reading as enabled. A session root must be somewhere noz
// can vouch for.
func TestSessionWorkDirRejectsUselessRoots(t *testing.T) {
	t.Setenv("NOZ_ROOT", t.TempDir())

	for _, dir := range []string{"/", os.TempDir()} {
		t.Setenv("CLAUDE_PROJECT_DIR", dir)
		got := sessionWorkDir()
		if got == "/" {
			t.Errorf("CLAUDE_PROJECT_DIR=%q accepted as a session root", dir)
		}
	}

	// A directory strictly inside $NOZ_ROOT is accepted.
	root := os.Getenv("NOZ_ROOT")
	session := filepath.Join(root, "repo-slug")
	if err := os.MkdirAll(session, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CLAUDE_PROJECT_DIR", session)
	if got := sessionWorkDir(); got != session {
		t.Errorf("sessionWorkDir() = %q, want %q", got, session)
	}

	// $NOZ_ROOT itself is not a session.
	t.Setenv("CLAUDE_PROJECT_DIR", root)
	if got := sessionWorkDir(); got == root {
		t.Errorf("sessionWorkDir() accepted $NOZ_ROOT itself as a session root")
	}
}

// A gate that cannot load its policy or config must block, not error out: the
// Claude Code hook treats any non-2, non-0 exit as a non-blocking error, so a
// returned error (exit 1) would let the tool run. This exercises the paths
// through the built binary, since exit codes are the contract.
func TestGateFailsClosedOnLoadError(t *testing.T) {
	bin := buildNoz(t)

	run := func(args ...string) int {
		cmd := exec.Command(bin, args...)
		cmd.Stdin = strings.NewReader(`{"command":"go test"}`)
		if err := cmd.Run(); err != nil {
			if ee, ok := err.(*exec.ExitError); ok {
				return ee.ExitCode()
			}
			t.Fatalf("running gate: %v", err)
		}
		return 0
	}

	if code := run("gate", "--tool", "bash", "--policy", filepath.Join(t.TempDir(), "absent.yaml")); code != 2 {
		t.Errorf("missing policy: exit %d, want 2 (block)", code)
	}

	bad := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(bad, []byte("write_roots: [unterminated\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if code := run("-c", bad, "gate", "--tool", "bash", "--policy", "../../policies/brr.yaml"); code != 2 {
		t.Errorf("malformed config: exit %d, want 2 (block)", code)
	}
}

// buildNoz compiles the CLI into a temp dir so a test can assert on real exit
// codes, which are the gate's contract with the agent harness.
func buildNoz(t *testing.T) string {
	t.Helper()
	bin := filepath.Join(t.TempDir(), "noz")
	out, err := exec.Command("go", "build", "-o", bin, "..").CombinedOutput()
	if err != nil {
		t.Fatalf("building noz: %v\n%s", err, out)
	}
	return bin
}
