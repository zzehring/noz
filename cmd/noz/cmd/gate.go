package cmd

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"github.com/zzehring/nozey/internal/config"
	"github.com/zzehring/nozey/internal/gate"
	"github.com/zzehring/nozey/internal/shellparse"
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

Designed to be called from agent pre-execution hooks:
  Claude Code: PreToolUse hook
  Codex CLI:   PreToolUse hook
  Gemini CLI:  BeforeTool hook`,
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runGate(inputFormat, tool, guardLog)
		},
	}

	cmd.Flags().StringVar(&inputFormat, "input-format", "claude", "Input format: claude, codex, gemini")
	cmd.Flags().StringVar(&tool, "tool", "bash", "Tool type being gated: bash, write, edit")
	cmd.Flags().StringVar(&guardLog, "guard-log", "", "Path to append guard tower audit log")

	return cmd
}

func runGate(inputFormat, tool, guardLog string) error {
	// Extract the command string from agent-specific input
	cmdStr, err := extractCommand(inputFormat, tool)
	if err != nil {
		return err
	}

	if cmdStr == "" {
		return nil // empty command, allow
	}

	// Parse shell command into segments
	commands, err := shellparse.Parse(cmdStr)
	if err != nil {
		logGuard(guardLog, "DENY", cmdStr, "parse-error", err.Error())
		fmt.Fprintf(os.Stderr, "noz: parse error: %v\n", err)
		os.Exit(2)
		return nil
	}

	// Load policy
	cfg, err := config.Load(cfgFile)
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}

	policyPath := policyName
	if policyPath == "" {
		policyPath = cfg.DefaultPolicy()
	}

	g, err := gate.New(policyPath)
	if err != nil {
		return fmt.Errorf("loading policy: %w", err)
	}

	// Evaluate each command segment
	for _, cmd := range commands {
		req := &gate.CommandRequest{
			Cmd:  cmd.Name,
			Args: cmd.Args,
			Mode: "autonomous",
		}

		result := g.Evaluate(req)
		cmdDisplay := cmd.Name
		if len(cmd.Args) > 0 {
			cmdDisplay += " " + strings.Join(cmd.Args, " ")
		}

		logGuard(guardLog, string(result.Verdict), cmdDisplay, result.Rule, result.Reason)

		switch result.Verdict {
		case gate.Deny:
			msg := fmt.Sprintf("noz: DENY %s (rule: %s)", cmdDisplay, result.Rule)
			fmt.Fprintln(os.Stderr, msg)

			if jsonOutput {
				writeGeminiResponse("deny", result.Rule)
			}
			os.Exit(2)
		case gate.Pause:
			msg := fmt.Sprintf("noz: PAUSE %s (rule: %s)", cmdDisplay, result.Rule)
			fmt.Fprintln(os.Stderr, msg)

			if jsonOutput {
				writeGeminiResponse("deny", "requires approval: "+result.Rule)
			}
			os.Exit(2) // hooks don't support PAUSE natively, use DENY for now
		case gate.Allow:
			// continue to next segment
		}
	}

	// All segments allowed
	if jsonOutput {
		writeGeminiResponse("allow", "")
	}

	return nil
}

// extractCommand gets the shell command string from agent-specific input.
func extractCommand(format, tool string) (string, error) {
	switch format {
	case "claude", "codex":
		return extractClaudeCommand(tool)
	case "gemini":
		return extractGeminiCommand()
	default:
		return extractClaudeCommand(tool)
	}
}

// extractClaudeCommand reads from CLAUDE_TOOL_INPUT env var or stdin.
func extractClaudeCommand(tool string) (string, error) {
	var input string

	// Try env var first
	if envInput := os.Getenv("CLAUDE_TOOL_INPUT"); envInput != "" {
		input = envInput
	} else {
		// Fall back to stdin
		data, err := io.ReadAll(os.Stdin)
		if err != nil {
			return "", fmt.Errorf("reading stdin: %w", err)
		}
		input = string(data)
	}

	if input == "" {
		return "", nil
	}

	// Parse the JSON to extract the command
	var toolInput map[string]interface{}
	if err := json.Unmarshal([]byte(input), &toolInput); err != nil {
		// Not JSON — treat as raw command string
		return strings.TrimSpace(input), nil
	}

	// Claude/Codex Bash tool sends {"command": "..."}
	if cmd, ok := toolInput["command"].(string); ok {
		return cmd, nil
	}

	// For Write/Edit tools, extract the path
	if tool == "write" || tool == "edit" {
		if path, ok := toolInput["file_path"].(string); ok {
			return path, nil
		}
	}

	return "", nil
}

// extractGeminiCommand reads Gemini's JSON protocol from stdin.
func extractGeminiCommand() (string, error) {
	data, err := io.ReadAll(os.Stdin)
	if err != nil {
		return "", fmt.Errorf("reading stdin: %w", err)
	}

	var geminiInput struct {
		Tool  string                 `json:"tool"`
		Input map[string]interface{} `json:"input"`
	}
	if err := json.Unmarshal(data, &geminiInput); err != nil {
		return "", fmt.Errorf("parsing gemini input: %w", err)
	}

	if cmd, ok := geminiInput.Input["command"].(string); ok {
		return cmd, nil
	}

	return "", nil
}

func writeGeminiResponse(decision, reason string) {
	resp := map[string]string{"decision": decision}
	if reason != "" {
		resp["reason"] = reason
	}
	json.NewEncoder(os.Stdout).Encode(resp)
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
	defer f.Close()

	fmt.Fprintf(f, "%-6s %s (rule: %s)\n", verdict, cmd, rule)
}
