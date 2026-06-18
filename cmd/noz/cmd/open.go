package cmd

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"github.com/zzehring/noz/internal/agent"
)

func newOpenCmd() *cobra.Command {
	var prNumber string
	var baseBranch string
	var noRepo bool
	var depth int
	var profile string
	var agentName string
	var force bool
	var detach bool

	cmd := &cobra.Command{
		Use:   "open <slug>",
		Short: "Open a session — create it, or attach if it exists (git worktree + tmux)",
		Long: `Creates a workspace and drops you into a tmux session. In a git repo it
creates a worktree; otherwise a scratch directory. If you're already inside
tmux it switches to the session instead of nesting.

Reuses existing sessions — if the tmux session already exists, attaches to it.

Examples:
  noz open feature-auth          # worktree + tmux
  noz open --pr 456              # PR review (shallow, 'review' profile)
  noz open investigate           # scratch dir (no repo)
  noz open feature-auth main     # worktree from a specific base branch
  noz open bug-123 --agent claude  # open the agent in a window`,
		Args:              cobra.RangeArgs(0, 2),
		ValidArgsFunction: completeTmuxSessions,
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 && prNumber == "" {
				return fmt.Errorf("usage: noz open <slug> or noz open --pr <number>")
			}

			// --detach is the documented form of NOZ_NO_ATTACH: create the
			// session but don't attach/switch. Reuses the existing detached path.
			if detach {
				os.Setenv("NOZ_NO_ATTACH", "1")
			}

			var slug string
			var base string

			if prNumber != "" {
				if profile == "" {
					profile = "review" // auto-select for PR sessions
				}
				return runOpenPR(prNumber, depth, profile, agentName, force)
			}

			slug = args[0]
			if err := validSlug(slug); err != nil {
				return err
			}
			if len(args) > 1 {
				base = args[1]
			}
			if baseBranch != "" {
				base = baseBranch
			}

			if noRepo || !inGitRepo() {
				return runOpenScratch(slug, agentName)
			}
			return runOpenWorktree(slug, base, profile, agentName, force)
		},
	}

	cmd.Flags().StringVar(&prNumber, "pr", "", "PR number to review (creates detached worktree)")
	cmd.Flags().StringVar(&baseBranch, "base", "", "base branch for worktree")
	cmd.Flags().BoolVar(&noRepo, "no-repo", false, "force scratch directory (skip git worktree)")
	cmd.Flags().IntVar(&depth, "depth", 1, "git fetch depth for PR reviews (0 = full history)")
	cmd.Flags().StringVar(&profile, "profile", "", "apply a session profile (noz profile list to see available)")
	cmd.RegisterFlagCompletionFunc("profile", completeProfiles)
	cmd.Flags().StringVar(&agentName, "agent", "", "open a coding agent in a window (claude, opencode, codex, gemini, pi)")
	cmd.RegisterFlagCompletionFunc("agent", completeAgents)
	cmd.Flags().BoolVarP(&force, "force", "f", false, "proceed even if the slug is a live session in another repo")
	cmd.Flags().BoolVarP(&detach, "detach", "d", false, "create the session but don't attach/switch to it")

	return cmd
}

// agentPrimary returns window 0's spec that launches the named agent, or nil
// when name is empty. Errors on an unknown agent. When contextRef is non-empty
// and the agent supports an initial prompt, the agent is launched with a
// directive to read that context file first.
func agentPrimary(name, contextRef string) (*profileWindow, error) {
	if name == "" {
		return nil, nil
	}
	a, ok := agent.Lookup(name)
	if !ok {
		return nil, fmt.Errorf("unknown agent %q (known: %s)", name, strings.Join(agent.Names(), ", "))
	}
	argv := a.Launch
	if contextRef != "" {
		directive := fmt.Sprintf("Read %s for this session's context, then help me with it.", contextRef)
		argv = a.LaunchWith(directive)
	}
	return &profileWindow{Name: a.Name, Cmd: shellJoin(argv)}, nil
}

// shellJoin renders argv as a POSIX shell command string, quoting each argument
// so it survives tmux's shell. Used only for noz-authored launch commands and
// short directives — never to inline untrusted file contents (those live in the
// context file, which is why only a safe directive ever reaches the shell).
func shellJoin(argv []string) string {
	parts := make([]string, len(argv))
	for i, a := range argv {
		parts[i] = shellQuote(a)
	}
	return strings.Join(parts, " ")
}

func shellQuote(s string) string {
	if s == "" {
		return "''"
	}
	if !strings.ContainsAny(s, " \t\n'\"\\$`*?[]{}()&|;<>#~!") {
		return s // simple token — leave it bare (keeps `claude` clean)
	}
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

func completeAgents(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	var matches []string
	for _, n := range agent.Names() {
		if strings.HasPrefix(n, toComplete) {
			matches = append(matches, n)
		}
	}
	return matches, cobra.ShellCompDirectiveNoFileComp
}

func runOpenWorktree(slug, baseBranch, profile, agentName string, force bool) error {
	repo, err := repoName()
	if err != nil {
		return err
	}

	if !force {
		if other, live := liveSessionRepo(slug); live && other != "" && other != repo {
			return fmt.Errorf("slug %q is already a live session in repo %q — pick a different slug, or use --force", slug, other)
		}
	}

	root := nozRoot()
	wtDir := filepath.Join(root, repo+"-"+slug)

	if err := os.MkdirAll(root, 0755); err != nil {
		return fmt.Errorf("creating root dir: %w", err)
	}

	created := false
	if dirExists(wtDir) {
		fmt.Fprintf(os.Stderr, "noz: worktree exists at %s, reusing\n", wtDir)
	} else {
		var args []string
		if branchExists(slug) {
			// Branch already exists (e.g. `noz rm` kept it) — check it out
			// rather than failing on `-b`.
			args = []string{"worktree", "add", wtDir, slug}
			fmt.Fprintf(os.Stderr, "noz: reusing existing branch %s\n", slug)
		} else {
			args = []string{"worktree", "add", wtDir, "-b", slug}
			if baseBranch != "" {
				args = append(args, baseBranch)
			}
		}
		if err := runGit(args...); err != nil {
			return fmt.Errorf("creating worktree: %w", err)
		}
		fmt.Fprintf(os.Stderr, "noz: created worktree at %s on branch %s\n", wtDir, slug)
		created = true
	}

	linkNozDir(root, repo, wtDir)

	// Apply profile only on first creation
	var windows []profileWindow
	var ctxRef string
	if created && profile != "" {
		data := ProfileData{Slug: slug, Repo: repo, Branch: slug}
		w, wrote, err := applyProfile(wtDir, profile, data)
		if err != nil {
			fmt.Fprintf(os.Stderr, "noz: warning: %v\n", err)
		}
		windows = w
		if wrote {
			ctxRef = contextRef(slug)
			grantContextRead(wtDir, repo)
		}
	}

	primary, err := agentPrimary(agentName, ctxRef)
	if err != nil {
		return err
	}

	if agentName == "" && !tmuxHasSession(slug) && hasClaudeHistory(wtDir) {
		resumeHint()
	}

	return tmuxSession(slug, wtDir, primary, windows)
}

func runOpenPR(prNumber string, depth int, profile, agentName string, force bool) error {
	if _, err := exec.LookPath("gh"); err != nil {
		return fmt.Errorf("gh CLI not found (needed for --pr)")
	}

	repo, err := repoName()
	if err != nil {
		return err
	}

	slug := "review-" + prNumber
	if err := validSlug(slug); err != nil {
		return err
	}
	if !force {
		if other, live := liveSessionRepo(slug); live && other != "" && other != repo {
			return fmt.Errorf("slug %q is already a live session in repo %q — use --force", slug, other)
		}
	}

	// Get PR branch name
	out, err := exec.Command("gh", "pr", "view", prNumber, "--json", "headRefName", "-q", ".headRefName").Output()
	if err != nil {
		return fmt.Errorf("could not look up PR #%s: %w", prNumber, err)
	}
	branch := strings.TrimSpace(string(out))

	root := nozRoot()
	wtDir := filepath.Join(root, repo+"-"+slug)

	if err := os.MkdirAll(root, 0755); err != nil {
		return fmt.Errorf("creating root dir: %w", err)
	}

	var windows []profileWindow
	var ctxRef string
	if dirExists(wtDir) {
		fmt.Fprintf(os.Stderr, "noz: worktree exists at %s, reusing\n", wtDir)
	} else {
		fetchArgs := []string{"fetch"}
		if depth > 0 {
			fetchArgs = append(fetchArgs, "--depth", fmt.Sprintf("%d", depth))
		}
		fetchArgs = append(fetchArgs, "origin", branch)
		if err := runGit(fetchArgs...); err != nil {
			return fmt.Errorf("fetching branch: %w", err)
		}
		if err := runGit("worktree", "add", "--detach", wtDir, "origin/"+branch); err != nil {
			return fmt.Errorf("creating worktree: %w", err)
		}
		fmt.Fprintf(os.Stderr, "noz: created worktree at %s (detached at origin/%s)\n", wtDir, branch)

		// Apply profile on first creation
		if profile != "" {
			data := ProfileData{Slug: slug, Repo: repo, PR: prNumber, Branch: branch}
			w, wrote, err := applyProfile(wtDir, profile, data)
			if err != nil {
				fmt.Fprintf(os.Stderr, "noz: warning: %v\n", err)
			}
			windows = w
			if wrote {
				ctxRef = contextRef(slug)
				grantContextRead(wtDir, repo)
			}
		}
	}

	primary, err := agentPrimary(agentName, ctxRef)
	if err != nil {
		return err
	}

	if agentName == "" && !tmuxHasSession(slug) && hasClaudeHistory(wtDir) {
		resumeHint()
	}

	linkNozDir(root, repo, wtDir)
	return tmuxSession(slug, wtDir, primary, windows)
}

func runOpenScratch(slug, agentName string) error {
	root := nozRoot()
	dir := filepath.Join(root, "scratch-"+slug)

	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("creating scratch dir: %w", err)
	}

	if !dirExists(filepath.Join(dir, ".git")) {
		fmt.Fprintf(os.Stderr, "noz: created scratch workspace at %s\n", dir)
	} else {
		fmt.Fprintf(os.Stderr, "noz: reusing scratch workspace at %s\n", dir)
	}

	primary, err := agentPrimary(agentName, "")
	if err != nil {
		return err
	}

	if agentName == "" && !tmuxHasSession(slug) && hasClaudeHistory(dir) {
		resumeHint()
	}

	return tmuxSession(slug, dir, primary, nil)
}

// tmuxSession creates, attaches, or switches to a tmux session.
// If already inside tmux, switches client instead of nesting.
// Tags the session with NOZ_SLUG and NOZ_REPO env vars.
//
// Window 0 runs `primary` (e.g. the chosen agent) named after it; when
// primary is nil it's a plain shell left unnamed so tmux auto-renames it to
// whatever's running — never the redundant session name. Extra profile
// windows open alongside.
func tmuxSession(name, dir string, primary *profileWindow, windows []profileWindow) error {
	tmuxBin, err := exec.LookPath("tmux")
	if err != nil {
		return fmt.Errorf("tmux not found")
	}

	// Detect repo for the env var
	repo := ""
	if r, err := repoName(); err == nil {
		repo = r
	}

	insideTmux := os.Getenv("TMUX") != ""
	noAttach := os.Getenv("NOZ_NO_ATTACH") != ""

	if tmuxHasSession(name) {
		// Tag on re-attach (backfills older sessions)
		tagNozSession(tmuxBin, name, name, repo)
		if noAttach {
			fmt.Fprintf(os.Stderr, "noz: session %s exists (detached, not attaching)\n", name)
			return nil
		}
		if insideTmux {
			fmt.Fprintf(os.Stderr, "noz: switching to %s\n", name)
			return exec.Command(tmuxBin, "switch-client", "-t", name).Run()
		}
		cmd := exec.Command(tmuxBin, "attach", "-t", name)
		cmd.Stdin = os.Stdin
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		return cmd.Run()
	}

	// Create the session detached, capturing window 0's id so we can return
	// focus there regardless of the user's base-index. Window 0 runs the
	// primary command (the agent) if given, named after it; otherwise it's a
	// shell left unnamed so tmux auto-renames it to whatever's running.
	args := []string{"new", "-d", "-P", "-F", "#{window_id}", "-s", name, "-c", dir}
	if primary != nil {
		if primary.Name != "" {
			args = append(args, "-n", primary.Name)
		}
		if primary.Cmd != "" {
			args = append(args, primary.Cmd)
		}
	}
	create := exec.Command(tmuxBin, args...)
	create.Env = append(os.Environ(), "NOZ_SLUG="+name, "NOZ_REPO="+repo)
	out, err := create.Output()
	if err != nil {
		return fmt.Errorf("creating session: %w", err)
	}
	shellWindow := strings.TrimSpace(string(out))
	tagNozSession(tmuxBin, name, name, repo)

	// Record lineage: when spawning from inside another session, remember it as
	// the parent so `noz close` (and offshoot auto-return) can send you back
	// where you came from instead of a guess.
	if insideTmux {
		if parent := currentTmuxSession(); parent != "" && parent != name {
			exec.Command(tmuxBin, "set-environment", "-t", name, "NOZ_PARENT", parent).Run()
		}
	}

	openWindows(tmuxBin, dir, shellWindow, windows)

	if noAttach {
		fmt.Fprintf(os.Stderr, "noz: created %s detached\n", name)
		return nil
	}

	if insideTmux {
		fmt.Fprintf(os.Stderr, "noz: switching to %s\n", name)
		return exec.Command(tmuxBin, "switch-client", "-t", name).Run()
	}

	cmd := exec.Command(tmuxBin, "attach", "-t", name)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// openWindows creates the profile-declared windows in a session, then
// returns focus to the shell window. A window with a cmd runs it; without
// one it's just a shell. Best-effort — failures are non-fatal but surfaced.
func openWindows(tmuxBin, dir, shellWindow string, windows []profileWindow) {
	if len(windows) == 0 {
		return
	}
	// Anchor each window after the previous one so they keep profile order.
	// -a (insert-after) is required: a bare append can fail under non-default
	// base-index configs. -d keeps focus on the shell. -P -F prints the new
	// window id so it becomes the next anchor.
	anchor := shellWindow
	for _, win := range windows {
		args := []string{"new-window", "-d", "-a", "-P", "-F", "#{window_id}", "-c", dir}
		if anchor != "" {
			args = append(args, "-t", anchor)
		}
		if win.Name != "" {
			args = append(args, "-n", win.Name)
		}
		if win.Cmd != "" {
			args = append(args, win.Cmd)
		}
		cmd := exec.Command(tmuxBin, args...)
		var stderr bytes.Buffer
		cmd.Stderr = &stderr
		out, err := cmd.Output()
		if err != nil {
			fmt.Fprintf(os.Stderr, "noz: warning: could not open window %q: %v (%s)\n",
				win.Name, err, strings.TrimSpace(stderr.String()))
			continue
		}
		if id := strings.TrimSpace(string(out)); id != "" {
			anchor = id
		}
	}
	if shellWindow != "" {
		exec.Command(tmuxBin, "select-window", "-t", shellWindow).Run()
	}
}

// tagNozSession sets session-level env vars so we can identify noz sessions.
func tagNozSession(tmuxBin, session, slug, repo string) {
	exec.Command(tmuxBin, "set-environment", "-t", session, "NOZ_SLUG", slug).Run()
	exec.Command(tmuxBin, "set-environment", "-t", session, "NOZ_REPO", repo).Run()
}

// linkNozDir creates a persistent .noz dir for the repo and symlinks it
// into the worktree. Also excludes .noz from git via the main repo's
// .git/info/exclude (works reliably across all worktrees).
// Safe to call repeatedly — idempotent.
func linkNozDir(root, repo, wtDir string) {
	nozDir := filepath.Join(root, ".noz", repo)
	link := filepath.Join(wtDir, ".noz")

	// Create persistent dir if it doesn't exist
	if err := os.MkdirAll(nozDir, 0755); err != nil {
		fmt.Fprintf(os.Stderr, "noz: warning: could not create .noz dir: %v\n", err)
		return
	}

	// Symlink .noz -> persistent dir (skip if already exists)
	if _, err := os.Lstat(link); err == nil {
		return // already linked
	}
	if err := os.Symlink(nozDir, link); err != nil {
		fmt.Fprintf(os.Stderr, "noz: warning: could not symlink .noz: %v\n", err)
		return
	}

	// Add .noz to the main repo's .git/info/exclude (not the worktree's).
	// This is the only exclude location that works reliably for all worktrees.
	mainGitDir := resolveMainGitDir(wtDir)
	if mainGitDir != "" {
		addToExclude(filepath.Join(mainGitDir, "info", "exclude"), ".noz")
	}

	fmt.Fprintf(os.Stderr, "noz: linked .noz -> %s\n", nozDir)
}

// resolveMainGitDir finds the main repo's .git directory from a worktree.
func resolveMainGitDir(wtDir string) string {
	data, err := os.ReadFile(filepath.Join(wtDir, ".git"))
	if err != nil {
		return "" // .git is a dir (not a worktree) or doesn't exist
	}
	if base := worktreeMainRepo(string(data)); base != "" {
		return base + "/.git"
	}
	return ""
}

func addToExclude(path, pattern string) {
	os.MkdirAll(filepath.Dir(path), 0755)

	if data, err := os.ReadFile(path); err == nil {
		for line := range strings.SplitSeq(string(data), "\n") {
			if strings.TrimSpace(line) == pattern {
				return
			}
		}
	}

	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return
	}
	defer f.Close()
	f.WriteString(pattern + "\n")
}

// validSlug rejects names that could escape a directory or confuse tmux/git.
// Anything that becomes a path or a tmux session name (slugs, profile names)
// must be simple: no separators, no traversal, no leading dash/dot, no spaces.
func validSlug(name string) error {
	if name == "" {
		return fmt.Errorf("empty name")
	}
	if strings.ContainsAny(name, `/\`) {
		return fmt.Errorf("invalid name %q: no '/' or '\\'", name)
	}
	if strings.ContainsAny(name, " \t\n") {
		return fmt.Errorf("invalid name %q: no whitespace", name)
	}
	if strings.HasPrefix(name, "-") || strings.HasPrefix(name, ".") {
		return fmt.Errorf("invalid name %q: cannot start with '-' or '.'", name)
	}
	return nil
}

func inGitRepo() bool {
	err := exec.Command("git", "rev-parse", "--show-toplevel").Run()
	return err == nil
}

// isNozSession reports whether a live tmux session was created by noz (it
// carries the NOZ_SLUG tag). Used to avoid acting on unrelated tmux sessions.
func isNozSession(slug string) bool {
	out, err := exec.Command("tmux", "show-environment", "-t", slug, "NOZ_SLUG").Output()
	if err != nil {
		return false
	}
	return strings.HasPrefix(strings.TrimSpace(string(out)), "NOZ_SLUG=")
}

// tmuxSessionEnv returns the value of a session-level tmux environment variable,
// or "" if the session or variable doesn't exist (or is set but empty). Stateless
// lineage/metadata (NOZ_PARENT, NOZ_AUTO_RETURN) rides on these.
func tmuxSessionEnv(slug, key string) string {
	out, err := exec.Command("tmux", "show-environment", "-t", slug, key).Output()
	if err != nil {
		return ""
	}
	if v, ok := strings.CutPrefix(strings.TrimSpace(string(out)), key+"="); ok {
		return v
	}
	return ""
}

// liveSessionRepo reports the NOZ_REPO of a live tmux session named slug, and
// whether such a session exists. repo is "" for a session that exists but
// isn't noz-tagged.
func liveSessionRepo(slug string) (repo string, live bool) {
	out, err := exec.Command("tmux", "show-environment", "-t", slug, "NOZ_REPO").Output()
	if err != nil {
		return "", false // no such session
	}
	if r, ok := strings.CutPrefix(strings.TrimSpace(string(out)), "NOZ_REPO="); ok {
		return r, true
	}
	return "", true // session exists but untagged ("-NOZ_REPO")
}

func repoName() (string, error) {
	out, err := exec.Command("git", "rev-parse", "--show-toplevel").Output()
	if err != nil {
		return "", fmt.Errorf("not in a git repo")
	}
	topLevel := strings.TrimSpace(string(out))

	// If we're in a worktree, resolve the main repo name from its .git file.
	if data, err := os.ReadFile(filepath.Join(topLevel, ".git")); err == nil {
		if base := worktreeMainRepo(string(data)); base != "" {
			return filepath.Base(base), nil
		}
	}

	return filepath.Base(topLevel), nil
}

func runGit(args ...string) error {
	cmd := exec.Command("git", args...)
	cmd.Stdout = os.Stderr
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func nozRoot() string {
	if r := os.Getenv("NOZ_ROOT"); r != "" {
		return r
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, "worktrees")
}

func dirExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

// encodeClaudeProject maps a worktree path to Claude Code's per-project
// transcript directory name — it replaces path separators with '-'.
func encodeClaudeProject(absDir string) string {
	return strings.ReplaceAll(absDir, "/", "-")
}

// hasClaudeHistory reports whether Claude Code has a saved conversation for the
// given dir. Best effort: a miss just means no resume hint is shown.
func hasClaudeHistory(dir string) bool {
	return !claudeHistoryMtime(dir).IsZero()
}

// claudeHistoryMtime returns the mtime of the newest Claude Code transcript for
// the given worktree (a .jsonl under ~/.claude/projects/<encoded>), or the zero
// time if there is none. This is a durable, reboot-surviving signal that an
// agent session happened here — and when — so noz can derive "what was I
// recently working on" without persisting any state of its own.
func claudeHistoryMtime(dir string) time.Time {
	abs, err := filepath.Abs(dir)
	if err != nil {
		abs = dir
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return time.Time{}
	}
	pd := filepath.Join(home, ".claude", "projects", encodeClaudeProject(abs))
	entries, err := os.ReadDir(pd)
	if err != nil {
		return time.Time{}
	}
	var newest time.Time
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".jsonl") {
			continue
		}
		if fi, err := e.Info(); err == nil && fi.ModTime().After(newest) {
			newest = fi.ModTime()
		}
	}
	return newest
}

// resumeHint nudges the user to resume a prior Claude conversation rather than
// start fresh, when entering a session that has history but no agent running.
func resumeHint() {
	fmt.Fprintln(os.Stderr, "noz: a previous Claude conversation exists here —")
	fmt.Fprintln(os.Stderr, "noz:   resume the latest:  claude --continue")
	fmt.Fprintln(os.Stderr, "noz:   or pick one:        claude --resume")
}
