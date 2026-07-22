package cmd

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
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
	// Load policy
	cfg, err := config.Load(cfgFile)
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}

	pp := policyName
	if pp == "" {
		pp = cfg.DefaultPolicy()
	}

	g, err := gate.New(pp)
	if err != nil {
		return fmt.Errorf("loading policy: %w", err)
	}

	// Route based on tool type
	switch tool {
	case "bash":
		return gateBash(g, inputFormat, guardLog)
	case "read", "write", "edit", "glob", "grep":
		return gateFile(g, tool, inputFormat, guardLog)
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
		logGuard(guardLog, "DENY", cmdStr, "parse-error", err.Error())
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

		display := cmd.Name
		if len(cmd.Args) > 0 {
			display += " " + strings.Join(cmd.Args, " ")
		}

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
func gateFile(g *gate.Gate, tool, inputFormat, guardLog string) error {
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
		return nil // no path to evaluate, allow
	}

	req := &gate.CommandRequest{
		Tool: tool,
		Path: path,
		Mode: "autonomous",
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

	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return
	}
	defer func() { _ = f.Close() }()

	fmt.Fprintf(f, "%s %-6s %s (rule: %s)\n", time.Now().UTC().Format(time.RFC3339), verdict, cmd, rule)
}
