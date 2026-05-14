package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
)

func newSetupCmd() *cobra.Command {
	var remove bool
	var projectOnly bool
	var dryRun bool

	cmd := &cobra.Command{
		Use:   "setup [agent]",
		Short: "Configure agent hooks to use the noz CEL gate",
		Long: `Auto-configures pre-execution hooks for coding agents so every command
is evaluated against your CEL policy.

Supported agents: claude, codex, gemini

Examples:
  noz setup claude --policy readonly          # global hooks
  noz setup claude --policy dev --project-only  # this repo only
  noz setup claude --remove                   # undo
  noz setup --dry-run                         # preview only`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			agentName := "claude"
			if len(args) > 0 {
				agentName = args[0]
			}
			return runSetup(agentName, remove, projectOnly, dryRun)
		},
	}

	cmd.Flags().BoolVar(&remove, "remove", false, "Remove noz hooks from agent config")
	cmd.Flags().BoolVar(&projectOnly, "project-only", false, "Only configure for current project (.claude/settings.json)")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "Preview changes without writing")

	return cmd
}

func runSetup(agentName string, remove, projectOnly, dryRun bool) error {
	switch agentName {
	case "claude":
		return setupClaude(remove, projectOnly, dryRun)
	case "codex":
		return fmt.Errorf("codex setup not yet implemented")
	case "gemini":
		return fmt.Errorf("gemini setup not yet implemented")
	default:
		return fmt.Errorf("unknown agent: %s (supported: claude, codex, gemini)", agentName)
	}
}

func setupClaude(remove, projectOnly, dryRun bool) error {
	// Find noz binary path
	nozBin, err := findNozBinary()
	if err != nil {
		return err
	}

	// Resolve policy path
	policyPath := policyName
	if policyPath == "" {
		policyPath = "readonly"
	}
	policyPath, err = resolvePolicyPath(policyPath)
	if err != nil {
		return err
	}

	// Determine settings file path
	settingsPath := claudeSettingsPath(projectOnly)
	fmt.Fprintf(os.Stderr, "noz: target config: %s\n", settingsPath)

	// Load existing settings
	settings, err := loadJSONFile(settingsPath)
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("reading %s: %w", settingsPath, err)
	}
	if settings == nil {
		settings = make(map[string]interface{})
	}

	if remove {
		return removeCloudeHooks(settings, settingsPath, dryRun)
	}

	// Build the hook command
	hookCmd := fmt.Sprintf("echo \"$CLAUDE_TOOL_INPUT\" | %s gate --policy %s", nozBin, policyPath)

	// Build the hook config
	hook := map[string]interface{}{
		"matcher": "Bash",
		"hooks": []interface{}{
			map[string]interface{}{
				"type":    "command",
				"command": hookCmd,
			},
		},
	}

	// Merge into existing settings
	hooks, _ := settings["hooks"].(map[string]interface{})
	if hooks == nil {
		hooks = make(map[string]interface{})
	}

	// Check for existing noz hooks and replace
	preToolUse := getOrCreatePreToolUse(hooks)
	preToolUse = upsertNozHook(preToolUse, hook)
	hooks["PreToolUse"] = preToolUse
	settings["hooks"] = hooks

	// Preview
	preview, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		return err
	}

	fmt.Fprintf(os.Stderr, "\nnoz: hook command:\n  %s\n\n", hookCmd)

	if dryRun {
		fmt.Fprintf(os.Stderr, "noz: dry-run — would write to %s:\n", settingsPath)
		fmt.Println(string(preview))
		return nil
	}

	// Write
	if err := writeJSONFile(settingsPath, settings); err != nil {
		return fmt.Errorf("writing %s: %w", settingsPath, err)
	}

	fmt.Fprintf(os.Stderr, "noz: hooks configured in %s\n", settingsPath)
	fmt.Fprintf(os.Stderr, "noz: policy: %s\n", policyPath)
	fmt.Fprintf(os.Stderr, "noz: restart Claude Code for changes to take effect\n")

	return nil
}

func removeCloudeHooks(settings map[string]interface{}, settingsPath string, dryRun bool) error {
	hooks, ok := settings["hooks"].(map[string]interface{})
	if !ok {
		fmt.Fprintf(os.Stderr, "noz: no hooks found in %s\n", settingsPath)
		return nil
	}

	preToolUse, ok := hooks["PreToolUse"].([]interface{})
	if !ok {
		fmt.Fprintf(os.Stderr, "noz: no PreToolUse hooks found\n")
		return nil
	}

	// Filter out noz hooks
	var filtered []interface{}
	removed := 0
	for _, entry := range preToolUse {
		if !isNozHook(entry) {
			filtered = append(filtered, entry)
		} else {
			removed++
		}
	}

	if removed == 0 {
		fmt.Fprintf(os.Stderr, "noz: no noz hooks found to remove\n")
		return nil
	}

	if len(filtered) == 0 {
		delete(hooks, "PreToolUse")
	} else {
		hooks["PreToolUse"] = filtered
	}
	if len(hooks) == 0 {
		delete(settings, "hooks")
	}

	if dryRun {
		preview, _ := json.MarshalIndent(settings, "", "  ")
		fmt.Fprintf(os.Stderr, "noz: dry-run — would remove %d hook(s) from %s:\n", removed, settingsPath)
		fmt.Println(string(preview))
		return nil
	}

	if err := writeJSONFile(settingsPath, settings); err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "noz: removed %d noz hook(s) from %s\n", removed, settingsPath)
	return nil
}

func findNozBinary() (string, error) {
	// Check if noz is in PATH
	if p, err := exec.LookPath("noz"); err == nil {
		return p, nil
	}

	// Check common locations
	home, _ := os.UserHomeDir()
	candidates := []string{
		filepath.Join(home, "go", "bin", "noz"),
		filepath.Join(home, ".local", "bin", "noz"),
		"/usr/local/bin/noz",
	}
	for _, c := range candidates {
		if _, err := os.Stat(c); err == nil {
			return c, nil
		}
	}

	// Fall back to just "noz" and hope it's in PATH at hook runtime
	return "noz", nil
}

func resolvePolicyPath(name string) (string, error) {
	// If it's already an absolute path or has an extension, use as-is
	if filepath.IsAbs(name) || filepath.Ext(name) != "" {
		if _, err := os.Stat(name); err != nil {
			return "", fmt.Errorf("policy file not found: %s", name)
		}
		return name, nil
	}

	// Check policies/ in current directory
	local := filepath.Join("policies", name+".cel")
	if _, err := os.Stat(local); err == nil {
		abs, _ := filepath.Abs(local)
		return abs, nil
	}

	// Check ~/.config/noz/policies/
	home, _ := os.UserHomeDir()
	global := filepath.Join(home, ".config", "noz", "policies", name+".cel")
	if _, err := os.Stat(global); err == nil {
		return global, nil
	}

	return "", fmt.Errorf("policy %q not found in ./policies/ or ~/.config/noz/policies/", name)
}

func claudeSettingsPath(projectOnly bool) string {
	if projectOnly {
		return filepath.Join(".claude", "settings.json")
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".claude", "settings.json")
}

func loadJSONFile(path string) (map[string]interface{}, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var result map[string]interface{}
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, fmt.Errorf("parsing JSON: %w", err)
	}
	return result, nil
}

func writeJSONFile(path string, data map[string]interface{}) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	out, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(out, '\n'), 0644)
}

func getOrCreatePreToolUse(hooks map[string]interface{}) []interface{} {
	if existing, ok := hooks["PreToolUse"].([]interface{}); ok {
		return existing
	}
	return []interface{}{}
}

// upsertNozHook replaces an existing noz hook or appends a new one.
func upsertNozHook(preToolUse []interface{}, newHook map[string]interface{}) []interface{} {
	for i, entry := range preToolUse {
		if isNozHook(entry) {
			preToolUse[i] = newHook
			return preToolUse
		}
	}
	return append(preToolUse, newHook)
}

// isNozHook checks if a PreToolUse entry is a noz hook.
func isNozHook(entry interface{}) bool {
	m, ok := entry.(map[string]interface{})
	if !ok {
		return false
	}
	hooks, ok := m["hooks"].([]interface{})
	if !ok {
		return false
	}
	for _, h := range hooks {
		hm, ok := h.(map[string]interface{})
		if !ok {
			continue
		}
		if cmd, ok := hm["command"].(string); ok {
			if strings.Contains(cmd, "noz gate") || strings.Contains(cmd, "noz-gate") {
				return true
			}
		}
	}
	return false
}
