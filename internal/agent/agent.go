// Package agent is noz's registry of coding agents it can launch and
// recognize. Adding support for a new agent is a matter of adding a descriptor
// here — no switch statements elsewhere. Today only Claude has gate-hook
// integration; the rest are launch/detect only.
package agent

import "strings"

// Agent describes a coding agent.
type Agent struct {
	Name   string   // canonical id: "claude", "opencode", "codex", ...
	Launch []string // command to start it in a session (argv; [0] is the binary)

	// match reports whether a tmux pane_current_command belongs to this agent.
	match func(paneCmd string) bool
}

// registry is the single source of truth for known agents.
var registry = []Agent{
	{Name: "claude", Launch: []string{"claude"}, match: isClaudeCmd},
	{Name: "opencode", Launch: []string{"opencode"}, match: exact("opencode")},
	{Name: "codex", Launch: []string{"codex"}, match: exact("codex")},
	{Name: "gemini", Launch: []string{"gemini"}, match: exact("gemini")},
	{Name: "pi", Launch: []string{"pi"}, match: exact("pi")},
}

// Lookup returns the agent with the given name.
func Lookup(name string) (Agent, bool) {
	for _, a := range registry {
		if a.Name == name {
			return a, true
		}
	}
	return Agent{}, false
}

// Detect maps a tmux pane_current_command to a known agent name, or "" if it
// doesn't look like any known agent.
func Detect(paneCmd string) string {
	for _, a := range registry {
		if a.match != nil && a.match(paneCmd) {
			return a.Name
		}
	}
	return ""
}

// Names lists all known agent names, in registry order.
func Names() []string {
	names := make([]string, len(registry))
	for i, a := range registry {
		names[i] = a.Name
	}
	return names
}

func exact(name string) func(string) bool {
	return func(c string) bool { return c == name }
}

// isClaudeCmd recognizes Claude Code, whose tmux pane command shows either the
// binary name or its version string (e.g. "2.1.78").
func isClaudeCmd(c string) bool {
	if c == "claude" {
		return true
	}
	if len(c) > 0 && c[0] >= '0' && c[0] <= '9' && strings.Contains(c, ".") {
		return true
	}
	return false
}
