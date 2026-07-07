// Package agent is noz's registry of coding agents it can launch and
// recognize. Adding support for a new agent is a matter of adding a descriptor
// here — no switch statements elsewhere. Today only Claude has gate-hook
// integration; the rest are launch/detect only.
package agent

import "regexp"

// Agent describes a coding agent.
type Agent struct {
	Name   string   // canonical id: "claude", "opencode", "codex", ...
	Launch []string // command to start it in a session (argv; [0] is the binary)

	// promptArgs returns the extra argv to pass an initial prompt to this agent,
	// or nil if its prompt-passing convention isn't known yet. Only set for
	// agents we've verified; others simply get no prompt injected.
	promptArgs func(prompt string) []string

	// match reports whether a tmux pane_current_command belongs to this agent.
	match func(paneCmd string) bool
}

// registry is the single source of truth for known agents.
var registry = []Agent{
	{Name: "claude", Launch: []string{"claude"}, promptArgs: positionalPrompt, match: isClaudeCmd},
	{Name: "opencode", Launch: []string{"opencode"}, match: exact("opencode")},
	{Name: "codex", Launch: []string{"codex"}, match: exact("codex")},
	{Name: "gemini", Launch: []string{"gemini"}, match: exact("gemini")},
	{Name: "pi", Launch: []string{"pi"}, match: exact("pi")},
}

// LaunchWith returns the argv to start the agent with an initial prompt. If the
// prompt is empty or the agent has no known prompt-passing convention, this is
// just the plain Launch argv.
func (a Agent) LaunchWith(prompt string) []string {
	if prompt == "" || a.promptArgs == nil {
		return a.Launch
	}
	return append(append([]string{}, a.Launch...), a.promptArgs(prompt)...)
}

// positionalPrompt passes the prompt as a trailing positional arg
// (e.g. `claude "<prompt>"`), Claude Code's convention.
func positionalPrompt(p string) []string { return []string{p} }

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

// claudeVersionRe matches Claude Code's version-string pane command (e.g. "2.1.78").
var claudeVersionRe = regexp.MustCompile(`^\d+\.\d+\.\d+`)

// isClaudeCmd recognizes Claude Code, whose tmux pane command shows either the
// binary name or its version string (e.g. "2.1.78").
func isClaudeCmd(c string) bool {
	return c == "claude" || claudeVersionRe.MatchString(c)
}
