package cmd

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"text/template"

	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
)

func newProfileCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "profile",
		Short: "Manage session profiles",
	}

	cmd.AddCommand(newProfileListCmd())
	cmd.AddCommand(newProfileCreateCmd())
	cmd.AddCommand(newProfileEditCmd())
	cmd.AddCommand(newProfileShowCmd())

	return cmd
}

func newProfileListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List available profiles",
		RunE: func(cmd *cobra.Command, args []string) error {
			initColors()
			profiles := listAvailableProfiles()
			sort.Strings(profiles)

			userDir := profilesDir()
			for _, name := range profiles {
				source := "builtin"
				if _, err := os.Stat(filepath.Join(userDir, name+".md")); err == nil {
					source = "custom"
				}

				// Surface the windows a profile opens as a quick preview.
				wins := ""
				if _, windows, err := resolveProfile(name, ProfileData{}); err == nil && len(windows) > 0 {
					var labels []string
					for _, w := range windows {
						if w.Cmd != "" {
							labels = append(labels, w.Cmd)
						} else if w.Name != "" {
							labels = append(labels, w.Name)
						}
					}
					wins = "+ " + strings.Join(labels, " ")
				}

				fmt.Fprintf(cmd.OutOrStdout(), "  %-16s %s%-8s%s  %s%s%s\n",
					name, cGray, source, cReset, cCyan, wins, cReset)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "\n%sProfiles dir: %s%s\n", cGray, userDir, cReset)
			return nil
		},
	}
}

func newProfileCreateCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "create <name>",
		Short: "Create a custom profile (opens in $EDITOR)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := args[0]
			if err := validSlug(name); err != nil {
				return err
			}
			dir := profilesDir()
			if err := os.MkdirAll(dir, 0755); err != nil {
				return fmt.Errorf("creating profiles dir: %w", err)
			}
			path := filepath.Join(dir, name+".md")

			if _, err := os.Stat(path); err == nil {
				return fmt.Errorf("profile %q already exists at %s (use 'noz profile edit %s')", name, path, name)
			}

			// Seed with a template demonstrating frontmatter windows +
			// template vars ({{.Slug}}, {{.Repo}}, {{.PR}}, {{.Branch}}).
			seed := `---
# Windows opened alongside your shell when this profile is applied.
# Drop the block if you only want the session context below.
windows:
  - name: agent
    cmd: claude
  # - name: k9s
  #   cmd: k9s
---
# Session Context

Session: **{{.Slug}}** in {{.Repo}}.
{{- if .PR}}
PR: #{{.PR}}
{{- end}}
{{- if .Branch}}
Branch: {{.Branch}}
{{- end}}

## Focus
- TODO: describe what this profile is for

## Workflow
- TODO: list useful commands or patterns
`
			if err := os.WriteFile(path, []byte(seed), 0644); err != nil {
				return fmt.Errorf("writing profile: %w", err)
			}

			fmt.Fprintf(os.Stderr, "noz: created %s\n", path)
			return openEditor(path)
		},
	}
}

func newProfileEditCmd() *cobra.Command {
	return &cobra.Command{
		Use:               "edit <name>",
		Short:             "Edit a profile in $EDITOR",
		Args:              cobra.ExactArgs(1),
		ValidArgsFunction: completeProfileNames,
		RunE: func(cmd *cobra.Command, args []string) error {
			name := args[0]
			if err := validSlug(name); err != nil {
				return err
			}
			path := filepath.Join(profilesDir(), name+".md")
			if _, err := os.Stat(path); os.IsNotExist(err) {
				return fmt.Errorf("profile %q not found at %s (use 'noz profile create %s')", name, path, name)
			}
			return openEditor(path)
		},
	}
}

func newProfileShowCmd() *cobra.Command {
	return &cobra.Command{
		Use:               "show <name>",
		Short:             "Print a profile template",
		Args:              cobra.ExactArgs(1),
		ValidArgsFunction: completeProfileNames,
		RunE: func(cmd *cobra.Command, args []string) error {
			name := args[0]
			if err := validSlug(name); err != nil {
				return err
			}
			// Try user profile first
			path := filepath.Join(profilesDir(), name+".md")
			if content, err := os.ReadFile(path); err == nil {
				fmt.Fprint(cmd.OutOrStdout(), string(content))
				return nil
			}
			if builtin, ok := builtinProfiles[name]; ok {
				fmt.Fprint(cmd.OutOrStdout(), builtin)
				return nil
			}
			return fmt.Errorf("unknown profile %q", name)
		},
	}
}

func profilesDir() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config", "noz", "profiles")
}

func openEditor(path string) error {
	editor := os.Getenv("EDITOR")
	if editor == "" {
		editor = "vi"
	}
	cmd := exec.Command(editor, path)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// completeProfiles provides tab completion for --profile flag.
func completeProfiles(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	return completeProfileNames(cmd, args, toComplete)
}

func completeProfileNames(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	profiles := listAvailableProfiles()
	var matches []string
	for _, p := range profiles {
		if strings.HasPrefix(p, toComplete) {
			matches = append(matches, p)
		}
	}
	return matches, cobra.ShellCompDirectiveNoFileComp
}

// ProfileData is the template context for profiles.
type ProfileData struct {
	Slug   string
	Repo   string
	PR     string
	Branch string
}

// profileWindow is a tmux window to open when a profile is applied.
// Declared in a profile's YAML frontmatter.
type profileWindow struct {
	Name string `yaml:"name"`
	Cmd  string `yaml:"cmd"`
}

// profileFrontmatter is the parsed YAML header of a profile file.
type profileFrontmatter struct {
	Windows []profileWindow `yaml:"windows"`
}

// splitFrontmatter separates a leading `---` fenced YAML block from the
// markdown body. Returns ("", src) when there is no frontmatter.
func splitFrontmatter(src string) (front, body string) {
	src = strings.ReplaceAll(src, "\r\n", "\n")
	rest, ok := strings.CutPrefix(src, "---\n")
	if !ok {
		return "", src
	}
	front, body, found := strings.Cut(rest, "\n---")
	if !found {
		return "", src // no closing fence — treat whole thing as body
	}
	// drop the remainder of the closing fence line
	if _, after, ok := strings.Cut(body, "\n"); ok {
		body = after
	} else {
		body = ""
	}
	return front, strings.TrimPrefix(body, "\n")
}

// renderTemplate runs src through text/template with the given data.
func renderTemplate(name, src string, data ProfileData) (string, error) {
	tmpl, err := template.New(name).Parse(src)
	if err != nil {
		return "", err
	}
	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		return "", err
	}
	return buf.String(), nil
}

// Built-in profiles. Users can override by placing a file with the same
// name in ~/.config/noz/profiles/<name>.md
var builtinProfiles = map[string]string{
	"profilesmith": `---
windows:
  - name: agent
    cmd: claude
---
# Authoring a noz profile

You are helping the user write a **noz profile**. Profiles live in
~/.config/noz/profiles/<name>.md and define what a session looks
like: the agent context (the markdown body) and the tmux windows that open
with the session.

## Format

A profile is markdown with optional YAML frontmatter:

    ---
    windows:
      - name: agent
        cmd: claude
      - name: k9s
        cmd: k9s
    ---
    # Session Context

    ...markdown handed to the agent as this session's context...

- The frontmatter "windows" list opens tmux windows alongside the shell
  (window 0). A window with a "cmd" runs it; without one it's just a shell.
- The body is written to the session's .noz context file (never the repo tree),
  and the agent is launched with a directive to read it first.
- Drop the frontmatter entirely if the profile only sets context.

## Template variables

The body and each window cmd are rendered through Go text/template. Available:
- {{ "{{.Slug}}" }} — the session slug
- {{ "{{.Repo}}" }} — the repo name
- {{ "{{.PR}}" }} — PR number (PR-review sessions only)
- {{ "{{.Branch}}" }} — branch name

## Your task
- Ask what kind of work this profile is for (a review flavor, an incident
  runbook, a language-specific dev loop, etc).
- Keep the body tight — it is injected into every session that uses it.
- Write the result to ~/.config/noz/profiles/<name>.md, then tell the user
  to try it with: noz open <slug> --profile <name>
`,

	"troubleshoot": `---
windows:
  - name: k9s
    cmd: k9s
  - name: agent
    cmd: claude
---
# Session Context

Troubleshooting session: **{{.Slug}}** in {{.Repo}}.

## Focus
- Understand the blast radius before changing anything
- Gather evidence: cluster state, logs, recent deploys
- Note findings as you go — this is an incident trail

## Useful commands
- k9s — live cluster view (open in its own window)
- kubectl get events --sort-by=.lastTimestamp
- git log --oneline -20 — recent changes
`,

	"review": `# Session Context

You are reviewing PR #{{.PR}} in the **{{.Repo}}** repository.
Session: {{.Slug}}

## Focus
- Review the diff for correctness, security, and best practices
- Check terraform plans if present (look for Atlantis bot comments)
- Verify manifest/config changes match the stated intent
- Flag risks, missing tests, or unclear changes

## Workflow
- gh pr diff — full diff
- gh pr view — description, labels, comments
- gh pr checks — CI status
- gh pr view --comments — reviewer discussion
`,

	"investigate": `# Session Context

Investigation session: **{{.Slug}}** in {{.Repo}}.

## Focus
- Understand the problem before proposing fixes
- Gather evidence: logs, metrics, recent changes
- Document findings as you go

## Useful commands
- git log --oneline -20 — recent changes
- kubectl get events --sort-by=.lastTimestamp — cluster events
- gh pr list --state merged --limit 10 — recent merges
`,
}

// resolveProfile finds a profile, splits its frontmatter, and renders the
// markdown body plus any declared windows against the template data.
// Checks ~/.config/noz/profiles/<name>.md first, falls back to builtins.
func resolveProfile(name string, data ProfileData) (body string, windows []profileWindow, err error) {
	var src string

	home, _ := os.UserHomeDir()
	userProfile := filepath.Join(home, ".config", "noz", "profiles", name+".md")
	if content, err := os.ReadFile(userProfile); err == nil {
		src = string(content)
	} else if builtin, ok := builtinProfiles[name]; ok {
		src = builtin
	} else {
		available := listAvailableProfiles()
		return "", nil, fmt.Errorf("unknown profile %q (available: %s)", name, strings.Join(available, ", "))
	}

	front, mdBody := splitFrontmatter(src)

	body, err = renderTemplate(name, mdBody, data)
	if err != nil {
		return "", nil, fmt.Errorf("rendering profile: %w", err)
	}

	if front != "" {
		var fm profileFrontmatter
		if err := yaml.Unmarshal([]byte(front), &fm); err != nil {
			return "", nil, fmt.Errorf("parsing profile frontmatter: %w", err)
		}
		for _, win := range fm.Windows {
			if win.Cmd != "" {
				if rendered, err := renderTemplate(name+":win", win.Cmd, data); err == nil {
					win.Cmd = rendered
				}
			}
			windows = append(windows, win)
		}
	}

	return body, windows, nil
}

// applyProfile renders a profile, writes the body to the session's noz context
// file (in the .noz brain — never the repo tree), and returns the windows to
// open plus whether a context file was written. The agent is launched with a
// directive to read that file (see agentPrimary), so context is delivered
// deterministically instead of as an ambient CLAUDE.md.
func applyProfile(wtDir, profileName string, data ProfileData) (windows []profileWindow, wroteContext bool, err error) {
	body, windows, err := resolveProfile(profileName, data)
	if err != nil {
		return nil, false, err
	}

	if strings.TrimSpace(body) == "" {
		return windows, false, nil
	}

	ctxPath := contextFilePath(data.Repo, data.Slug)
	if err := os.MkdirAll(filepath.Dir(ctxPath), 0755); err != nil {
		return windows, false, fmt.Errorf("writing session context: %w", err)
	}
	if err := os.WriteFile(ctxPath, []byte(body), 0644); err != nil {
		return windows, false, fmt.Errorf("writing session context: %w", err)
	}
	fmt.Fprintf(os.Stderr, "noz: wrote session context to %s\n", contextRef(data.Slug))
	return windows, true, nil
}

// contextFilePath is the canonical location of a session's noz-authored context
// file: a dedicated context/ subdir of the shared .noz brain
// (root/.noz/<repo>/context/<slug>.md). Kept in its own subdir so it doesn't
// clutter the brain root (ROADMAP etc., or per-user dirs). Written there — not
// the repo tree — so it never dirties the worktree; reachable from inside the
// worktree via the .noz symlink as contextRef(slug).
func contextFilePath(repo, slug string) string {
	return filepath.Join(nozRoot(), ".noz", repo, "context", slug+".md")
}

// contextRef is how the context file is referenced from inside the worktree
// (via the .noz symlink) — what we tell the agent to read.
func contextRef(slug string) string {
	return ".noz/context/" + slug + ".md"
}

// grantContextRead lets an agent in this worktree read its .noz context without
// a per-read permission prompt. The .noz symlink points outside the workspace,
// which Claude Code gates by default — so the agent's very first act (reading
// its own seeded marching orders) would otherwise prompt.
//
// It writes a scoped, gitignored .claude/settings.local.json: reads of the brain
// allowed (both the symlink path and its resolved target, which Claude checks
// together), edits of the brain denied. A new file in a noz-created worktree,
// scoped read-only to noz's own brain — not trampling user config, and trivially
// reversible. Best-effort and idempotent; opt out with NOZ_NO_GRANT_CONTEXT.
func grantBrainAccess(wtDir, repo string) {
	if os.Getenv("NOZ_NO_GRANT_CONTEXT") != "" {
		return
	}
	brain := filepath.Join(nozRoot(), ".noz", repo)
	abs := brain + "/**"

	dir := filepath.Join(wtDir, ".claude")
	path := filepath.Join(dir, "settings.local.json")

	m := map[string]any{}
	if data, err := os.ReadFile(path); err == nil {
		json.Unmarshal(data, &m) // best-effort merge into any existing file
	}
	mergeStringList(m, "additionalDirectories", brain)
	perms := childMap(m, "permissions")

	// The .noz brain is shared and *bidirectional* — agents read their seeded
	// context AND write back-reports/notes. Grant read+write (both the symlink
	// path and its resolved target, which Claude checks together for allow
	// rules) so an offshoot never hits a wall and routes around the gate with a
	// hacky write. Read-only here defeats the whole point of a shared brain.
	mergeStringList(perms, "allow",
		"Read(.noz/**)", "Read("+abs+")",
		"Edit(.noz/**)", "Edit("+abs+")",
		"Write(.noz/**)", "Write("+abs+")",
	)
	// Strip the old read-only deny (migration from earlier versions); deny wins
	// over allow, so leaving it would keep the brain unwritable.
	removeFromStringList(perms, "deny", "Edit(.noz/**)", "Edit("+abs+")")

	if err := os.MkdirAll(dir, 0755); err != nil {
		return // best-effort — the agent will just prompt, no harm
	}
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return
	}
	os.WriteFile(path, append(data, '\n'), 0644)

	// Keep it out of git so noz never dirties the tree. settings.local.json is
	// Claude's own gitignore convention, but don't assume the repo set it up.
	if mainGitDir := resolveMainGitDir(wtDir); mainGitDir != "" {
		addToExclude(filepath.Join(mainGitDir, "info", "exclude"), ".claude/settings.local.json")
	}
}

// childMap returns m[key] as a map, creating it if absent.
func childMap(m map[string]any, key string) map[string]any {
	if c, ok := m[key].(map[string]any); ok {
		return c
	}
	c := map[string]any{}
	m[key] = c
	return c
}

// mergeStringList adds vals to m[key] (a JSON string array) without duplicates.
func mergeStringList(m map[string]any, key string, vals ...string) {
	var list []any
	if existing, ok := m[key].([]any); ok {
		list = existing
	}
	seen := map[string]bool{}
	for _, v := range list {
		if s, ok := v.(string); ok {
			seen[s] = true
		}
	}
	for _, v := range vals {
		if !seen[v] {
			list = append(list, v)
			seen[v] = true
		}
	}
	m[key] = list
}

// removeFromStringList drops the given values from m[key] (a JSON string array).
func removeFromStringList(m map[string]any, key string, vals ...string) {
	existing, ok := m[key].([]any)
	if !ok {
		return
	}
	drop := map[string]bool{}
	for _, v := range vals {
		drop[v] = true
	}
	kept := []any{}
	for _, v := range existing {
		if s, ok := v.(string); ok && drop[s] {
			continue
		}
		kept = append(kept, v)
	}
	m[key] = kept
}

func listAvailableProfiles() []string {
	seen := make(map[string]bool)
	var names []string

	// Builtins
	for name := range builtinProfiles {
		seen[name] = true
		names = append(names, name)
	}

	// User profiles
	home, _ := os.UserHomeDir()
	dir := filepath.Join(home, ".config", "noz", "profiles")
	entries, err := os.ReadDir(dir)
	if err == nil {
		for _, e := range entries {
			if e.IsDir() {
				continue
			}
			name := strings.TrimSuffix(e.Name(), filepath.Ext(e.Name()))
			if !seen[name] {
				names = append(names, name)
			}
		}
	}

	return names
}
