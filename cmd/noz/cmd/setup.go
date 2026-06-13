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
	cmd.Flags().StringVar(&policyName, "policy", "", "CEL policy name or path")
	cmd.RegisterFlagCompletionFunc("policy", completePolicyNames)

	return cmd
}

func runSetup(agentName string, remove, projectOnly, dryRun bool) error {
	switch agentName {
	case "claude":
		return setupClaude(remove, projectOnly, dryRun)
	case "tmux":
		return setupTmux(remove)
	case "codex":
		return fmt.Errorf("codex setup not yet implemented")
	case "gemini":
		return fmt.Errorf("gemini setup not yet implemented")
	default:
		return fmt.Errorf("unknown agent: %s (supported: claude, tmux, codex, gemini)", agentName)
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
		settings = make(map[string]any)
	}

	if remove {
		return removeClaudeHooks(settings, settingsPath, dryRun)
	}

	installNozHooks(settings, nozBin, policyPath)

	// Preview
	preview, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		return err
	}

	fmt.Fprintf(os.Stderr, "\nnoz: hooks for: Bash, Read, Write, Edit\n")

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

// installNozHooks merges noz PreToolUse gate hooks (Bash/Read/Write/Edit) into
// a Claude settings map, upserting by matcher so re-running is idempotent and
// never touches non-noz entries. Mutates and returns settings.
func installNozHooks(settings map[string]any, nozBin, policyPath string) map[string]any {
	toolHooks := []struct{ matcher, tool string }{
		{"Bash", "bash"},
		{"Read", "read"},
		{"Write", "write"},
		{"Edit", "edit"},
	}

	hooks, _ := settings["hooks"].(map[string]any)
	if hooks == nil {
		hooks = make(map[string]any)
	}
	preToolUse := getOrCreatePreToolUse(hooks)

	for _, th := range toolHooks {
		hookCmd := fmt.Sprintf("echo \"$CLAUDE_TOOL_INPUT\" | %s gate --tool %s --policy %s", nozBin, th.tool, policyPath)
		hook := map[string]any{
			"matcher": th.matcher,
			"hooks": []any{
				map[string]any{"type": "command", "command": hookCmd},
			},
		}
		preToolUse = upsertNozHookByMatcher(preToolUse, hook, th.matcher)
	}

	hooks["PreToolUse"] = preToolUse
	settings["hooks"] = hooks
	return settings
}

// stripNozHooks removes only noz gate hooks from a Claude settings map,
// cleaning up now-empty PreToolUse/hooks containers. Mutates and returns
// settings plus the count removed. Inverse of installNozHooks.
func stripNozHooks(settings map[string]any) (map[string]any, int) {
	hooks, ok := settings["hooks"].(map[string]any)
	if !ok {
		return settings, 0
	}
	preToolUse, ok := hooks["PreToolUse"].([]any)
	if !ok {
		return settings, 0
	}

	var filtered []any
	removed := 0
	for _, entry := range preToolUse {
		if !isNozHook(entry) {
			filtered = append(filtered, entry)
		} else {
			removed++
		}
	}
	if removed == 0 {
		return settings, 0
	}
	if len(filtered) == 0 {
		delete(hooks, "PreToolUse")
	} else {
		hooks["PreToolUse"] = filtered
	}
	if len(hooks) == 0 {
		delete(settings, "hooks")
	}
	return settings, removed
}

func removeClaudeHooks(settings map[string]any, settingsPath string, dryRun bool) error {
	settings, removed := stripNozHooks(settings)
	if removed == 0 {
		fmt.Fprintf(os.Stderr, "noz: no noz hooks found to remove in %s\n", settingsPath)
		return nil
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

	// Search order: YAML preferred over CEL, local preferred over global
	home, _ := os.UserHomeDir()
	candidates := []string{
		filepath.Join("policies", name+".yaml"),
		filepath.Join("policies", name+".yml"),
		filepath.Join("policies", name+".cel"),
		filepath.Join(home, ".config", "noz", "policies", name+".yaml"),
		filepath.Join(home, ".config", "noz", "policies", name+".yml"),
		filepath.Join(home, ".config", "noz", "policies", name+".cel"),
	}

	for _, c := range candidates {
		if _, err := os.Stat(c); err == nil {
			abs, _ := filepath.Abs(c)
			return abs, nil
		}
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

func loadJSONFile(path string) (map[string]any, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var result map[string]any
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, fmt.Errorf("parsing JSON: %w", err)
	}
	return result, nil
}

// writeJSONFile writes settings atomically (temp file + rename) so a crash or
// full disk can never leave a half-written, corrupt config. It also keeps a
// one-shot backup of any existing file at <path>.noz.bak.
func writeJSONFile(path string, data map[string]any) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	out, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		return err
	}
	out = append(out, '\n')

	// Back up the existing file before we touch it.
	if existing, err := os.ReadFile(path); err == nil {
		if err := os.WriteFile(path+".noz.bak", existing, 0644); err != nil {
			return fmt.Errorf("writing backup: %w", err)
		}
	}

	// Write to a temp file in the same dir, then rename over the target.
	tmp, err := os.CreateTemp(dir, ".noz-*.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName) // no-op once the rename succeeds
	if _, err := tmp.Write(out); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpName, path)
}

func getOrCreatePreToolUse(hooks map[string]any) []any {
	if existing, ok := hooks["PreToolUse"].([]any); ok {
		return existing
	}
	return []any{}
}

// upsertNozHookByMatcher replaces an existing noz hook with matching matcher, or appends.
func upsertNozHookByMatcher(preToolUse []any, newHook map[string]any, matcher string) []any {
	for i, entry := range preToolUse {
		if isNozHookWithMatcher(entry, matcher) {
			preToolUse[i] = newHook
			return preToolUse
		}
	}
	return append(preToolUse, newHook)
}

func isNozHookWithMatcher(entry any, matcher string) bool {
	m, ok := entry.(map[string]any)
	if !ok {
		return false
	}
	if m["matcher"] != matcher {
		return false
	}
	return isNozHook(entry)
}

// nozTmuxSnippet is printed by `noz setup tmux` for the user to paste into
// their own ~/.tmux.conf. It uses `status-right -ga` (append) and a guarded
// keybind so it never clobbers an existing status bar or binding — noz does
// not edit the user's tmux config for them.
const nozTmuxSnippet = `# --- noz: session context + prefix+j to fuzzy-jump sessions ---
# Appends to status-right (-ga) so it won't replace your existing status bar.
set -ga status-right '#[fg=cyan]#{?NOZ_SLUG,#{NOZ_SLUG} ,}#[fg=yellow]#{?NOZ_REPO,#{NOZ_REPO} ,}#[default]'
bind-key j display-popup -E -w 60% -h 50% "noz sw"`

func setupTmux(remove bool) error {
	if remove {
		fmt.Fprintln(os.Stderr, "noz: `noz setup tmux` doesn't edit your config — nothing to remove.")
		fmt.Fprintln(os.Stderr, "noz: if you added the snippet to ~/.tmux.conf, delete those lines by hand.")
		return nil
	}

	fmt.Fprintln(os.Stderr, "noz: add the following to your ~/.tmux.conf, then reload it")
	fmt.Fprintln(os.Stderr, "noz: (prefix + : then `source-file ~/.tmux.conf`):")
	fmt.Fprintln(os.Stderr)
	fmt.Fprintln(os.Stdout, nozTmuxSnippet)
	return nil
}

// isNozHook checks if a PreToolUse entry is a noz hook.
func isNozHook(entry any) bool {
	m, ok := entry.(map[string]any)
	if !ok {
		return false
	}
	hooks, ok := m["hooks"].([]any)
	if !ok {
		return false
	}
	for _, h := range hooks {
		hm, ok := h.(map[string]any)
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
