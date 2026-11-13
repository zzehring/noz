package cmd

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"github.com/zzehring/noz/internal/config"
	"github.com/zzehring/noz/internal/gate"
	"github.com/zzehring/noz/internal/shellparse"
)

func newGateCmd() *cobra.Command {
	var inputFormat string
	var tool string
	var guardLog string

	cmd := &cobra.Command{
		Use:   "gate",
		Short: "Evaluate a command against CEL policy (for agent hook integration)",
		Long: `Reads tool input from stdin or agent-specific env vars, evaluates against
the active CEL policy, and exits with the appropriate code.

Exit codes:
  0 = ALLOW (proceed)
  2 = DENY  (block the tool call)
  3 = PAUSE (block + veto protocol)

Supports all agent tool types:
  --tool bash   → parses shell commands, evaluates each segment
  --tool read   → evaluates file path access
  --tool write  → evaluates file path + content
  --tool edit   → evaluates file path access

Designed to be called from agent pre-execution hooks:
  Claude Code: PreToolUse hook (Bash, Read, Write, Edit)
  Codex CLI:   PreToolUse hook
  Gemini CLI:  BeforeTool hook`,
		Args:         cobra.NoArgs,
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runGate(inputFormat, tool, guardLog)
		},
	}

	cmd.Flags().StringVar(&inputFormat, "input-format", "claude", "Input format: claude, codex, gemini")
	cmd.Flags().StringVar(&tool, "tool", "bash", "Tool type being gated: bash, read, write, edit, glob, grep")
	cmd.Flags().StringVar(&guardLog, "guard-log", "", "Path to append guard tower audit log")
	cmd.Flags().StringVar(&policyName, "policy", "", "CEL policy name or path")
	cmd.Flags().BoolVar(&jsonOutput, "json", false, "emit JSON verdict (for Gemini/Codex hook format)")
	_ = cmd.RegisterFlagCompletionFunc("policy", completePolicyNames)

	return cmd
}

func runGate(inputFormat, tool, guardLog string) error {
	// A gate that cannot load must block, not error out. The Claude Code hook
	// contract is exit 2 = block, exit 0 = proceed, and *any other code —
	// including the 1 a returned error becomes — is a non-blocking error*, so
	// the tool call would run. A missing policy path or malformed config would
	// then fail open: the gate looks installed and gates nothing. Fail closed
	// instead, loudly, so a misconfigured gate denies rather than waves through.
	cfg, err := config.Load(cfgFile)
	if err != nil {
		logGuard(guardLog, "DENY", tool, "config-load-failed", err.Error())
		fmt.Fprintf(os.Stderr, "noz: cannot load config (%v) — denied\n", err)
		os.Exit(2)
	}

	pp := policyName
	if pp == "" {
		pp = cfg.DefaultPolicy()
	}

	g, err := gate.New(pp)
	if err != nil {
		logGuard(guardLog, "DENY", tool, "policy-load-failed", err.Error())
		fmt.Fprintf(os.Stderr, "noz: cannot load policy %q (%v) — denied\n", pp, err)
		os.Exit(2)
	}

	// Route based on tool type
	switch tool {
	case "bash":
		return gateBash(g, inputFormat, guardLog)
	case "read", "write", "edit", "glob", "grep":
		return gateFile(g, tool, inputFormat, guardLog, cfg.WriteRoots())
	default:
		logGuard(guardLog, "DENY", tool, "unknown-tool", "")
		fmt.Fprintf(os.Stderr, "noz: unknown tool type %q — denied\n", tool)
		os.Exit(2)
		return nil
	}
}

// gateBash handles Bash tool calls — parses shell string, evaluates each segment.
func gateBash(g *gate.Gate, inputFormat, guardLog string) error {
	cmdStr, found, err := extractCommand(inputFormat, "bash")
	if err != nil {
		return err
	}
	if !found {
		// Fail closed: the gate was asked to vet a bash call but no command
		// string was present (empty input, or a command sent in a shape we
		// don't recognize). Deny rather than let it through unseen.
		logGuard(guardLog, "DENY", "", "no-command", "missing or malformed command input")
		fmt.Fprintln(os.Stderr, "noz: no command in tool input — denied")
		os.Exit(2)
		return nil
	}
	if cmdStr == "" {
		return nil // command field present but empty — a shell no-op
	}

	commands, err := shellparse.Parse(cmdStr)
	if err != nil {
		logGuard(guardLog, "DENY", redactString(cmdStr), "parse-error", err.Error())
		fmt.Fprintf(os.Stderr, "noz: parse error: %v\n", err)
		os.Exit(2)
		return nil
	}

	for _, cmd := range commands {
		req := &gate.CommandRequest{
			Tool: "bash",
			Cmd:  cmd.Name,
			Args: cmd.Args,
			Mode: "autonomous",
		}

		// Redacted here rather than at the log call: display also reaches
		// stderr, which agent harnesses capture into transcripts.
		display := redactArgs(cmd.Name, cmd.Args)

		if blocked := evalAndReport(g, req, display, guardLog); blocked {
			return nil
		}
	}

	if jsonOutput {
		writeGeminiResponse("allow", "")
	}
	return nil
}

// gateFile handles Read/Write/Edit/Glob/Grep tool calls — evaluates file path.
func gateFile(g *gate.Gate, tool, inputFormat, guardLog string, writeRoots []string) error {
	toolInput, err := readToolInput(inputFormat)
	if err != nil {
		return err
	}
	if toolInput == nil {
		// Fail closed: gate invoked for a file tool with no input at all.
		logGuard(guardLog, "DENY", tool, "no-input", "empty tool input")
		fmt.Fprintf(os.Stderr, "noz: no input for %s tool — denied\n", tool)
		os.Exit(2)
		return nil
	}

	path := extractPath(toolInput)
	if path == "" {
		// A read-shaped call with no path has nothing to evaluate. A write with
		// no path does, and allowing it unevaluated would be a hole beside the
		// fail-closed branch above.
		if tool == "write" || tool == "edit" {
			logGuard(guardLog, "DENY", tool, "no-path", "write with no path")
			fmt.Fprintf(os.Stderr, "noz: %s tool with no path — denied\n", tool)
			os.Exit(2)
		}
		return nil
	}

	workdir := sessionWorkDir()
	req := &gate.CommandRequest{
		Tool:    tool,
		Path:    normalizePath(path, workdir),
		WorkDir: workdir,
		// Roots a policy needs but cannot know for itself. Always set, so a
		// policy indexing these can't fail at evaluation time.
		Env:        map[string]string{"NOZ_TMPDIR": filepath.Clean(os.TempDir())},
		WriteRoots: writeRoots,
		Mode:       "autonomous",
	}

	// For write/edit, capture content for policy evaluation
	if tool == "write" || tool == "edit" {
		if content, ok := toolInput["content"].(string); ok {
			req.Content = content
		}
		if content, ok := toolInput["new_string"].(string); ok {
			req.Content = content
		}
	}

	display := fmt.Sprintf("%s %s", tool, path)
	evalAndReport(g, req, display, guardLog)
	return nil
}

// evalAndReport evaluates a request and handles the verdict. Returns true if blocked.
func evalAndReport(g *gate.Gate, req *gate.CommandRequest, display, guardLog string) bool {
	result := g.Evaluate(req)
	logGuard(guardLog, string(result.Verdict), display, result.Rule, result.Reason)

	switch result.Verdict {
	case gate.Deny:
		fmt.Fprintf(os.Stderr, "noz: DENY %s (rule: %s)\n", display, result.Rule)
		if jsonOutput {
			writeGeminiResponse("deny", result.Rule)
		}
		os.Exit(2)
		return true
	case gate.Pause:
		fmt.Fprintf(os.Stderr, "noz: PAUSE %s (rule: %s)\n", display, result.Rule)
		if jsonOutput {
			writeGeminiResponse("pause", "requires approval: "+result.Rule)
		}
		os.Exit(3)
		return true
	}
	return false
}

// readToolInput reads the raw JSON tool input from env var or stdin.
func readToolInput(format string) (map[string]any, error) {
	var raw string

	if envInput := os.Getenv("CLAUDE_TOOL_INPUT"); envInput != "" {
		raw = envInput
	} else {
		data, err := io.ReadAll(os.Stdin)
		if err != nil {
			return nil, fmt.Errorf("reading stdin: %w", err)
		}
		raw = string(data)
	}

	if strings.TrimSpace(raw) == "" {
		return nil, nil
	}

	var toolInput map[string]any
	if err := json.Unmarshal([]byte(raw), &toolInput); err != nil {
		return nil, fmt.Errorf("parsing tool input: %w", err)
	}
	return toolInput, nil
}

// extractCommand gets the shell command string from agent-specific input.
// The bool reports whether a command string was actually present: a false
// means no input, or a command sent in an unrecognized shape (e.g. an array),
// which the caller treats as a hard deny rather than an empty allow.
func extractCommand(format, tool string) (string, bool, error) {
	toolInput, err := readToolInput(format)
	if err != nil {
		return "", false, err
	}
	if toolInput == nil {
		return "", false, nil
	}

	// Bash tool sends {"command": "..."}
	if cmd, ok := toolInput["command"].(string); ok {
		return cmd, true, nil
	}

	return "", false, nil
}

// extractPath gets the file path from a tool input JSON.
func extractPath(toolInput map[string]any) string {
	// Claude Code uses "file_path" for Read/Write/Edit
	if p, ok := toolInput["file_path"].(string); ok {
		return p
	}
	// Also check "path" as a fallback
	if p, ok := toolInput["path"].(string); ok {
		return p
	}
	// Glob uses "pattern"
	if p, ok := toolInput["pattern"].(string); ok {
		return p
	}
	return ""
}

func writeGeminiResponse(decision, reason string) {
	resp := map[string]string{"decision": decision}
	if reason != "" {
		resp["reason"] = reason
	}
	_ = json.NewEncoder(os.Stdout).Encode(resp)
}

func logGuard(path, verdict, cmd, rule, reason string) {
	if path == "" {
		path = os.Getenv("NOZ_GUARD_LOG")
	}
	if path == "" {
		return
	}

	// 0600: the log records what the agent ran. Even redacted, that is not
	// material for every account on the machine to read.
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0600)
	if err != nil {
		return
	}
	defer func() { _ = f.Close() }()

	fmt.Fprintf(f, "%s %-6s %s (rule: %s)\n", time.Now().UTC().Format(time.RFC3339), verdict, cmd, rule)
}

// --- audit log redaction ---------------------------------------------------
//
// The guard log exists so you can answer "what did it do, and when?"
// (PRINCIPLES #7). Writing full argv to answer it put credentials on disk in
// plaintext: anything passed as a flag value — a bearer header, a URL with
// userinfo, --token — was persisted append-only and world-readable.
//
// Redaction here is best-effort by nature: it recognises known credential
// shapes and cannot recognise one it has never seen. So it is a reduction, not
// a guarantee, and two things carry the rest of the weight — the log is 0600,
// and credentials should reach a tool through the environment or stdin rather
// than argv, which is what the broker exists to make easy.

// secretFlags are flags whose *following value* is a credential.
var secretFlags = map[string]bool{
	"--token": true, "--password": true, "--passwd": true, "--secret": true,
	"--api-key": true, "--apikey": true, "--access-key": true,
	"--secret-access-key": true, "--private-key": true, "--passphrase": true,
	"--auth": true, "--bearer": true, "--credential": true, "-p": true,
}

// secretValuePrefixes are credential formats recognisable on their own, so they
// are caught even when passed positionally with no telltale flag.
var secretValuePrefixes = []string{
	"ghp_", "gho_", "ghu_", "ghs_", "ghr_", "github_pat_", // GitHub
	"glpat-",                           // GitLab
	"xoxb-", "xoxp-", "xoxa-", "xoxr-", // Slack
	"sk-", "sk_live_", "pk_live_", "rk_live_", // OpenAI / Stripe
	"AKIA", "ASIA", // AWS access key IDs
	"AIza",         // Google API
	"hvs.", "hvb.", // Vault
	"eyJhbGciOi", // a JWT header, base64'd
}

const redacted = "[redacted]"

var (
	// scheme://user:pass@host — keep the scheme and host, drop the userinfo.
	reURLUserinfo = regexp.MustCompile(`([a-zA-Z][a-zA-Z0-9+.\-]*://)[^/\s@]+@`)
	// Authorization: Bearer <token>, and the Basic variant.
	reAuthHeader = regexp.MustCompile(`(?i)(authorization\s*:\s*(?:bearer|basic|token)\s+)[^\s"']+`)
	// KEY=secret in an inline env assignment, for keys that look sensitive.
	reEnvAssign = regexp.MustCompile(`(?i)\b([A-Z0-9_]*(?:TOKEN|SECRET|PASSWORD|PASSWD|APIKEY|API_KEY|CREDENTIAL|PRIVATE_KEY)[A-Z0-9_]*=)[^\s"']+`)
)

// redactString masks credential shapes recognisable from the text alone.
func redactString(s string) string {
	s = reURLUserinfo.ReplaceAllString(s, "${1}"+redacted+"@")
	s = reAuthHeader.ReplaceAllString(s, "${1}"+redacted)
	s = reEnvAssign.ReplaceAllString(s, "${1}"+redacted)

	fields := strings.Fields(s)
	for i, f := range fields {
		if hasSecretPrefix(f) {
			fields[i] = redacted
		}
	}
	return strings.Join(fields, " ")
}

// hasSecretPrefix reports whether tok looks like a credential on its own. The
// length floor keeps a bare "sk-" or a word like "AKIAing" from matching.
func hasSecretPrefix(tok string) bool {
	t := strings.Trim(tok, `"'`)
	for _, p := range secretValuePrefixes {
		if len(t) > len(p)+8 && strings.HasPrefix(t, p) {
			return true
		}
	}
	return false
}

// redactArgs renders argv for the audit log with credential values masked. It
// adds what redactString can't see on its own: a value is secret because of the
// flag in front of it.
func redactArgs(cmd string, args []string) string {
	out := make([]string, 0, len(args)+1)
	if cmd != "" {
		out = append(out, cmd)
	}
	maskNext := false
	for _, a := range args {
		switch {
		case maskNext:
			out = append(out, redacted)
			maskNext = false
		case secretFlags[a]:
			out = append(out, a)
			maskNext = true
		default:
			// --flag=value, where the flag names a credential.
			if k, _, ok := strings.Cut(a, "="); ok && secretFlags[k] {
				out = append(out, k+"="+redacted)
				continue
			}
			out = append(out, redactString(a))
		}
	}
	// A trailing secret flag with no value must not look like it had one.
	if maskNext {
		out = append(out, redacted)
	}
	return strings.Join(out, " ")
}

// --- write containment ------------------------------------------------------
//
// A policy can only confine writes if it knows where the session lives, and
// request.workdir was exposed to CEL but never populated — so a containment
// rule written against it was startsWith(""), true for every path, protective
// only in appearance. That is the same shape as the rule-ordering bug in #24.

// sessionWorkDir returns the directory the session is rooted at, or "" when it
// can't be established — which the policies treat as "not contained" and deny,
// rather than as a free pass.
//
// Taking cwd on its own was the bug this replaces: run the gate from / or from
// $HOME and every path on the machine "starts with" the working directory, so
// containment read as enabled while allowing everything. A session root has to
// be something noz can vouch for, so candidates are only accepted when they sit
// strictly inside $NOZ_ROOT (isWithinRoot excludes the root itself and any ..
// walk) or are the top level of a real git worktree.
func sessionWorkDir() string {
	root := nozRoot()
	cwd, _ := os.Getwd()

	// NOZ_PROJECT_DIR first so a harness without Claude's variable can say where
	// the session is — the pi/omp gate extension sets it. CLAUDE_PROJECT_DIR is
	// kept because Claude Code sets it for hooks without being asked.
	for _, cand := range []string{os.Getenv("NOZ_PROJECT_DIR"), os.Getenv("CLAUDE_PROJECT_DIR"), cwd} {
		if cand == "" {
			continue
		}
		abs, err := filepath.Abs(cand)
		if err != nil {
			continue
		}
		if abs = filepath.Clean(abs); isWithinRoot(root, abs) {
			return abs
		}
	}

	// Outside $NOZ_ROOT — fall back to the repo's own top level, so the gate
	// stays usable in an ordinary checkout. Still never "/" or a bare $HOME
	// unless that is genuinely a repo root.
	if out, err := exec.Command("git", "rev-parse", "--show-toplevel").Output(); err == nil {
		if top := strings.TrimSpace(string(out)); top != "" {
			return filepath.Clean(top)
		}
	}
	return ""
}

// normalizePath makes a tool's path absolute and lexically clean, so a prefix
// comparison in a policy means what it reads as. Without Clean, a path like
// "<worktree>/../../.ssh/id_rsa" starts with the worktree as a string while
// pointing outside it.
//
// Containment is lexical on purpose: it does not resolve symlinks, because the
// per-repo brain is a symlink noz itself plants at "<worktree>/.noz", and
// resolving it would deny the writes noz set that link up to allow. The gap
// that leaves — the agent planting its own symlink and writing through it —
// is closed on the command side instead: `ln` is in no policy's allowlist, and
// the file tools cannot create links.
func normalizePath(p, workdir string) string {
	if !filepath.IsAbs(p) && workdir != "" {
		p = filepath.Join(workdir, p)
	}
	if abs, err := filepath.Abs(p); err == nil {
		return filepath.Clean(abs)
	}
	return filepath.Clean(p)
}
