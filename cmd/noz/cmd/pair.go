package cmd

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
	"github.com/zzehring/nozey/internal/agent"
)

func newPairCmd() *cobra.Command {
	var prNumber string
	var baseBranch string
	var noRepo bool
	var depth int
	var profile string
	var agentName string

	cmd := &cobra.Command{
		Use:   "pair <slug>",
		Short: "Start a pairing session (git worktree + tmux)",
		Long: `Creates a workspace and drops you into a tmux session. In a git repo it
creates a worktree; otherwise a scratch directory. If you're already inside
tmux it switches to the session instead of nesting.

Reuses existing sessions — if the tmux session already exists, attaches to it.

Examples:
  noz pair feature-auth          # worktree + tmux
  noz pair --pr 456              # PR review (shallow, 'review' profile)
  noz pair investigate           # scratch dir (no repo)
  noz pair feature-auth main     # worktree from a specific base branch
  noz pair bug-123 --agent claude  # open the agent in a window`,
		Args:              cobra.RangeArgs(0, 2),
		ValidArgsFunction: completeTmuxSessions,
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 && prNumber == "" {
				return fmt.Errorf("usage: noz pair <slug> or noz pair --pr <number>")
			}

			var slug string
			var base string

			if prNumber != "" {
				if profile == "" {
					profile = "review" // auto-select for PR sessions
				}
				return runPairPR(prNumber, depth, profile, agentName)
			}

			slug = args[0]
			if len(args) > 1 {
				base = args[1]
			}
			if baseBranch != "" {
				base = baseBranch
			}

			if noRepo || !inGitRepo() {
				return runPairScratch(slug, agentName)
			}
			return runPairWorktree(slug, base, profile, agentName)
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

	return cmd
}

// agentPrimary returns window 0's spec that launches the named agent, or nil
// when name is empty. Errors on an unknown agent.
func agentPrimary(name string) (*profileWindow, error) {
	if name == "" {
		return nil, nil
	}
	a, ok := agent.Lookup(name)
	if !ok {
		return nil, fmt.Errorf("unknown agent %q (known: %s)", name, strings.Join(agent.Names(), ", "))
	}
	return &profileWindow{Name: a.Name, Cmd: strings.Join(a.Launch, " ")}, nil
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

func runPairWorktree(slug, baseBranch, profile, agentName string) error {
	repo, err := repoName()
	if err != nil {
		return err
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
	if created && profile != "" {
		data := ProfileData{Slug: slug, Repo: repo, Branch: slug}
		w, err := applyProfile(wtDir, profile, data)
		if err != nil {
			fmt.Fprintf(os.Stderr, "noz: warning: %v\n", err)
		}
		windows = w
	}

	primary, err := agentPrimary(agentName)
	if err != nil {
		return err
	}

	if agentName == "" && !tmuxHasSession(slug) && hasClaudeHistory(wtDir) {
		resumeHint()
	}

	return tmuxSession(slug, wtDir, primary, windows)
}

func runPairPR(prNumber string, depth int, profile, agentName string) error {
	if _, err := exec.LookPath("gh"); err != nil {
		return fmt.Errorf("gh CLI not found (needed for --pr)")
	}

	repo, err := repoName()
	if err != nil {
		return err
	}

	// Get PR branch name
	out, err := exec.Command("gh", "pr", "view", prNumber, "--json", "headRefName", "-q", ".headRefName").Output()
	if err != nil {
		return fmt.Errorf("could not look up PR #%s: %w", prNumber, err)
	}
	branch := strings.TrimSpace(string(out))

	slug := "review-" + prNumber
	root := nozRoot()
	wtDir := filepath.Join(root, repo+"-"+slug)

	if err := os.MkdirAll(root, 0755); err != nil {
		return fmt.Errorf("creating root dir: %w", err)
	}

	var windows []profileWindow
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
			w, err := applyProfile(wtDir, profile, data)
			if err != nil {
				fmt.Fprintf(os.Stderr, "noz: warning: %v\n", err)
			}
			windows = w
		}
	}

	primary, err := agentPrimary(agentName)
	if err != nil {
		return err
	}

	if agentName == "" && !tmuxHasSession(slug) && hasClaudeHistory(wtDir) {
		resumeHint()
	}

	linkNozDir(root, repo, wtDir)
	return tmuxSession(slug, wtDir, primary, windows)
}

func runPairScratch(slug, agentName string) error {
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

	primary, err := agentPrimary(agentName)
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
			fmt.Fprintf(os.Stderr, "noz: session %s exists (NOZ_NO_ATTACH, not attaching)\n", name)
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
	openWindows(tmuxBin, dir, shellWindow, windows)

	if noAttach {
		fmt.Fprintf(os.Stderr, "noz: created %s detached (NOZ_NO_ATTACH set)\n", name)
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

func inGitRepo() bool {
	err := exec.Command("git", "rev-parse", "--show-toplevel").Run()
	return err == nil
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
// given dir (a .jsonl transcript under ~/.claude/projects/<encoded>). Best
// effort: a miss just means no resume hint is shown.
func hasClaudeHistory(dir string) bool {
	abs, err := filepath.Abs(dir)
	if err != nil {
		abs = dir
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return false
	}
	pd := filepath.Join(home, ".claude", "projects", encodeClaudeProject(abs))
	entries, err := os.ReadDir(pd)
	if err != nil {
		return false
	}
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".jsonl") {
			return true
		}
	}
	return false
}

// resumeHint nudges the user to resume a prior Claude conversation rather than
// start fresh, when entering a session that has history but no agent running.
func resumeHint() {
	fmt.Fprintln(os.Stderr, "noz: a previous Claude conversation exists here —")
	fmt.Fprintln(os.Stderr, "noz:   resume the latest:  claude --continue")
	fmt.Fprintln(os.Stderr, "noz:   or pick one:        claude --resume")
}
