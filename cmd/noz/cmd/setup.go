package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
	"github.com/zzehring/noz/internal/agent"
)

// tmuxKeys are the prefix keys bound by the `noz setup tmux` snippet. They're
// just defaults — every one is overridable via a flag so the snippet never
// assumes a key is free in the user's own config.
type tmuxKeys struct {
	repo     string // native picker, current repo
	all      string // native picker, all repos
	children string // native picker, offshoots of this session
}

func newSetupCmd() *cobra.Command {
	var remove bool
	var projectOnly bool
	var dryRun bool
	var keys tmuxKeys

	cmd := &cobra.Command{
		Use:   "setup [target]",
		Short: "Configure editor/agent integrations",
		Long: `Set up an integration target:

  tmux     prints a status-bar + picker/jump-key snippet to add to ~/.tmux.conf
  mcp      prints how to register noz as an MCP server (agent session-awareness)
  claude   installs PreToolUse gate hooks into ~/.claude/settings.json

Other agents (opencode, codex, gemini, pi) are known to noz for launch and
detection, but gate hooks aren't implemented for them yet.

The tmux snippet is print-only — noz never edits ~/.tmux.conf. The picker keys
are defaults; pass --*-key to pick others if they clash with your macros (set a
key to "" to drop that binding):

Examples:
  noz setup tmux                               # print tmux snippet (g/G/C-g)
  noz setup tmux --repo-key C-s --all-key C-a  # rebind the picker keys
  noz setup tmux --children-key ""             # drop the children binding
  noz setup claude --policy readonly           # global gate hooks
  noz setup claude --policy dev --project-only # this repo only
  noz setup claude --remove                    # undo`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			agentName := "claude"
			if len(args) > 0 {
				agentName = args[0]
			}
			return runSetup(agentName, remove, projectOnly, dryRun, keys)
		},
	}

	cmd.Flags().BoolVar(&remove, "remove", false, "Remove noz hooks from agent config")
	cmd.Flags().BoolVar(&projectOnly, "project-only", false, "Only configure for current project (.claude/settings.json)")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "Preview changes without writing")
	cmd.Flags().StringVar(&policyName, "policy", "", "CEL policy name or path")
	cmd.Flags().StringVar(&keys.repo, "repo-key", "g", "tmux: prefix key for the repo-local picker (\"\" to omit)")
	cmd.Flags().StringVar(&keys.all, "all-key", "G", "tmux: prefix key for the all-sessions picker (\"\" to omit)")
	cmd.Flags().StringVar(&keys.children, "children-key", "C-g", "tmux: prefix key for the children picker (\"\" to omit)")
	_ = cmd.RegisterFlagCompletionFunc("policy", completePolicyNames)

	return cmd
}

func runSetup(agentName string, remove, projectOnly, dryRun bool, keys tmuxKeys) error {
	if agentName == "tmux" {
		return setupTmux(remove, keys)
	}
	if agentName == "mcp" {
		return setupMCP()
	}
	a, ok := agent.Lookup(agentName)
	if !ok {
		return fmt.Errorf("unknown agent %q (known: %s; or 'tmux', 'mcp')", agentName, strings.Join(agent.Names(), ", "))
	}
	if a.Name == "claude" {
		return setupClaude(remove, projectOnly, dryRun)
	}
	return fmt.Errorf("gate hooks for %q aren't implemented yet — noz can launch and detect it, but not gate it", a.Name)
}

// nozMCPConfig is the Claude Code .mcp.json snippet for the noz MCP server.
const nozMCPConfig = `{
  "mcpServers": {
    "noz": { "command": "noz", "args": ["mcp"] }
  }
}`

// setupMCP prints how to register noz as an MCP server (print-only, like tmux).
func setupMCP() error {
	fmt.Fprintln(os.Stderr, "noz: register noz as an MCP server so your agent can see your sessions.")
	fmt.Fprintln(os.Stderr)
	fmt.Fprintln(os.Stderr, "Project scope — add to .mcp.json in your repo root:")
	fmt.Fprintln(os.Stdout, nozMCPConfig)
	fmt.Fprintln(os.Stderr)
	fmt.Fprintln(os.Stderr, "User scope (Claude Code) — run:")
	fmt.Fprintln(os.Stderr, "  claude mcp add noz -- noz mcp")
	fmt.Fprintln(os.Stderr)
	fmt.Fprintln(os.Stderr, "Then reload your agent. Tools exposed: noz_sessions, noz_status, noz_peek, noz_switch, noz_back, noz_spawn, noz_rm, noz_close.")
	return nil
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
		hookCmd := fmt.Sprintf("echo \"$CLAUDE_TOOL_INPUT\" | %s gate --tool %s --policy %s", shellQuote(nozBin), th.tool, shellQuote(policyPath))
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
	defer func() { _ = os.Remove(tmpName) }() // no-op once the rename succeeds
	if _, err := tmp.Write(out); err != nil {
		_ = tmp.Close()
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
// nozTmuxSnippet builds the ~/.tmux.conf block printed by `noz setup tmux`.
// Every binding is gated on its key being non-empty so a user can drop any of
// them with --<name>-key "". The default prefix+s (all-sessions tree) is never
// touched. noz resolves the matching session names from the NOZ_* tags; the
// binding stays a thin one-liner that just renders a native tmux display-menu.
func nozTmuxSnippet(keys tmuxKeys) string {
	var b strings.Builder
	b.WriteString("# --- noz: session context + picker keys ---\n")
	b.WriteString("# Print-only: noz never edits this file. These keys are defaults —\n")
	b.WriteString("# rebind freely (noz setup tmux --repo-key ... ) if they clash with your macros.\n")
	b.WriteString("# Appends to status-right (-ga) so it won't replace your existing status bar.\n")
	b.WriteString("# Shows the session's repo + offshoot parent (the slug is already the tmux\n")
	b.WriteString("# session name, #S). Guarded, so it's blank for non-noz sessions.\n")
	b.WriteString("set -ga status-right '#[fg=yellow]#{?NOZ_REPO,#{NOZ_REPO} ,}#[fg=magenta]#{?NOZ_PARENT,↳#{NOZ_PARENT} ,}#[default]'\n")

	if keys.repo != "" || keys.all != "" || keys.children != "" {
		b.WriteString("\n# Native session picker: tmux's own choose-tree (full-screen + preview),\n")
		b.WriteString("# filtered to a view. noz resolves the matching session NAMES (filtering on\n")
		b.WriteString("# the NOZ_* tags in Go, where it's reliable); choose-tree then filters on\n")
		b.WriteString("# session_name via #{E:} double-expansion. The default prefix+s is untouched.\n")
		if keys.repo != "" {
			fmt.Fprintf(&b, "#   prefix+%-3s sessions in THIS repo\n", keys.repo)
		}
		if keys.all != "" {
			fmt.Fprintf(&b, "#   prefix+%-3s every noz session (all repos)\n", keys.all)
		}
		if keys.children != "" {
			fmt.Fprintf(&b, "#   prefix+%-3s offshoots spawned from THIS session\n", keys.children)
		}
	}
	for _, bind := range []struct{ key, view string }{
		{keys.repo, "repo"},
		{keys.all, "all"},
		{keys.children, "children"},
	} {
		if bind.key == "" {
			continue
		}
		// run-shell (synchronous) stashes the resolved filter, then choose-tree
		// runs natively in the client and double-expands it via #{E:}.
		fmt.Fprintf(&b, "bind-key %s run-shell \"tmux set-option -g @noz_pick \\\"\\$(noz pick %s --filter)\\\"\" \\; choose-tree -Zs -f \"#{E:#{@noz_pick}}\"\n", bind.key, bind.view)
	}
	return strings.TrimRight(b.String(), "\n")
}

func setupTmux(remove bool, keys tmuxKeys) error {
	if remove {
		fmt.Fprintln(os.Stderr, "noz: `noz setup tmux` doesn't edit your config — nothing to remove.")
		fmt.Fprintln(os.Stderr, "noz: if you added the snippet to ~/.tmux.conf, delete those lines by hand.")
		return nil
	}

	fmt.Fprintln(os.Stderr, "noz: add the following to your ~/.tmux.conf, then reload it")
	fmt.Fprintln(os.Stderr, "noz: (prefix + : then `source-file ~/.tmux.conf`):")
	fmt.Fprintln(os.Stderr)
	fmt.Fprintln(os.Stdout, nozTmuxSnippet(keys))
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
