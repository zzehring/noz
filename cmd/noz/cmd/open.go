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
	var task string

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
				_ = os.Setenv("NOZ_NO_ATTACH", "1")
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
			if err := validNewSlug(slug); err != nil {
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
			return runOpenWorktree(slug, base, profile, agentName, task, force)
		},
	}

	cmd.Flags().StringVar(&prNumber, "pr", "", "PR number to review (creates detached worktree)")
	cmd.Flags().StringVar(&baseBranch, "base", "", "base branch for worktree")
	cmd.Flags().BoolVar(&noRepo, "no-repo", false, "force scratch directory (skip git worktree)")
	cmd.Flags().IntVar(&depth, "depth", 1, "git fetch depth for PR reviews (0 = full history)")
	cmd.Flags().StringVar(&profile, "profile", "", "apply a session profile (noz profile list to see available)")
	_ = cmd.RegisterFlagCompletionFunc("profile", completeProfiles)
	cmd.Flags().StringVar(&agentName, "agent", "", "open a coding agent in a window (claude, opencode, codex, gemini, pi)")
	_ = cmd.RegisterFlagCompletionFunc("agent", completeAgents)
	cmd.Flags().BoolVarP(&force, "force", "f", false, "proceed even if the slug is a live session in another repo")
	cmd.Flags().BoolVarP(&detach, "detach", "d", false, "create the session but don't attach/switch to it")
	cmd.Flags().StringVar(&task, "task", "", "seed a task brief for the session (new sessions only)")

	return cmd
}

// agentPrimary returns window 0's spec that launches the named agent, or nil
// when name is empty. Errors on an unknown agent. When briefRef is non-empty
// and the agent supports an initial prompt, the agent is launched with a
// directive to read that context file first.
func agentPrimary(name, briefRef string) (*profileWindow, error) {
	if name == "" {
		return nil, nil
	}
	a, ok := agent.Lookup(name)
	if !ok {
		return nil, fmt.Errorf("unknown agent %q (known: %s)", name, strings.Join(agent.Names(), ", "))
	}
	argv := a.Launch
	if briefRef != "" {
		directive := fmt.Sprintf("Read %s for this session's context, then help me with it.", briefRef)
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

func runOpenWorktree(slug, baseBranch, profile, agentName, task string, force bool) error {
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
	grantBrainAccess(wtDir, repo, agentName) // Claude-only: read+write the brain without prompting

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
			ctxRef = briefRef(slug)
		}
	}

	// Seed task context on new session (when no profile already wrote context).
	if created && task != "" && ctxRef == "" {
		if err := writeSessionBrief(repo, slug, task); err != nil {
			fmt.Fprintf(os.Stderr, "noz: warning: could not write task context: %v\n", err)
		} else {
			ctxRef = briefRef(slug)
		}
	}

	// A session with a seeded brief (a staged offshoot, or an earlier --task)
	// should point its agent at that brief even on reuse — not just at creation.
	if ctxRef == "" {
		if _, err := os.Stat(briefPath(repo, slug)); err == nil {
			ctxRef = briefRef(slug)
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
	if err := validNewSlug(slug); err != nil {
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
				ctxRef = briefRef(slug)
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
	grantBrainAccess(wtDir, repo, agentName) // Claude-only: read+write the brain without prompting
	return tmuxSession(slug, wtDir, primary, windows)
}

func runOpenScratch(slug, agentName string) error {
	root := nozRoot()
	dir := filepath.Join(root, "scratch-"+slug)

	existed := dirExists(dir)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("creating scratch dir: %w", err)
	}

	if existed {
		fmt.Fprintf(os.Stderr, "noz: reusing scratch workspace at %s\n", dir)
	} else {
		fmt.Fprintf(os.Stderr, "noz: created scratch workspace at %s\n", dir)
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
// sessionTarget formats a session name as an EXACT tmux target (leading "=").
// tmux's default `-t` matching is exact -> prefix -> fnmatch, so a bare
// `-t feature-b` prefix-matches (and can kill!) `feature-b-spike` when no exact
// `feature-b` exists. Every noz command that looks a session up by name must use
// this. (Go passes it to tmux via exec with no shell, so the leading "=" is not
// subject to zsh's =-expansion.)
func sessionTarget(name string) string {
	return "=" + name
}

// sessionHasAgent reports whether a coding agent is already running in any pane
// of the session, so we don't double-start one when activating it.
func sessionHasAgent(tmuxBin, name string) bool {
	out, err := exec.Command(tmuxBin, "list-panes", "-s", "-t", sessionTarget(name), "-F", "#{pane_current_command}").Output()
	if err != nil {
		return false
	}
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if agent.Detect(strings.TrimSpace(line)) != "" {
			return true
		}
	}
	return false
}

// activeWindowCmd returns the current foreground command of the session's
// active pane, or "" if it can't be read. Uses list-panes rather than
// display-message: display-message -t needs a client to anchor, so it returns
// empty when noz runs outside tmux (e.g. `open --agent --detach` from a script).
func activeWindowCmd(tmuxBin, name string) string {
	out, err := exec.Command(tmuxBin, "list-panes", "-t", sessionTarget(name), "-F", "#{pane_active} #{pane_current_command}").Output()
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if cmd, ok := strings.CutPrefix(line, "1 "); ok {
			return strings.TrimSpace(cmd)
		}
	}
	return ""
}

// isShell reports whether cmd is an interactive shell — i.e. an idle window
// safe to replace with an agent rather than a running command.
func isShell(cmd string) bool {
	switch cmd {
	case "zsh", "bash", "sh", "fish", "dash", "tcsh", "ksh":
		return true
	}
	return false
}

// primaryLabel is a human name for the window-0 command (the agent), for logs.
func primaryLabel(primary *profileWindow) string {
	if primary != nil && primary.Name != "" {
		return primary.Name
	}
	return "agent"
}

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

		// Start the requested agent into the existing session if none is
		// running yet — this is how a staged session (spawned without --launch)
		// gets activated. respawn-window runs the agent as the window's own
		// process (like creation does), avoiding the send-keys race with shell
		// startup. Idempotent, and it won't clobber a window running real work.
		started := false
		if primary != nil && primary.Cmd != "" {
			switch {
			case sessionHasAgent(tmuxBin, name):
				fmt.Fprintf(os.Stderr, "noz: an agent is already running in %s — leaving it\n", name)
			case !isShell(activeWindowCmd(tmuxBin, name)):
				fmt.Fprintf(os.Stderr, "noz: %s is running %q in its active window — not replacing it; start the agent yourself if you want it there\n", name, activeWindowCmd(tmuxBin, name))
			default:
				if err := exec.Command(tmuxBin, "respawn-window", "-k", "-t", sessionTarget(name), primary.Cmd).Run(); err != nil {
					fmt.Fprintf(os.Stderr, "noz: warning: could not start the agent in %s: %v\n", name, err)
				} else {
					fmt.Fprintf(os.Stderr, "noz: started %s in %s\n", primaryLabel(primary), name)
					started = true
				}
			}
		}

		if noAttach {
			if !started {
				fmt.Fprintf(os.Stderr, "noz: session %s exists (detached, not attaching)\n", name)
			}
			return nil
		}
		if insideTmux {
			fmt.Fprintf(os.Stderr, "noz: switching to %s\n", name)
			return exec.Command(tmuxBin, "switch-client", "-t", sessionTarget(name)).Run()
		}
		cmd := exec.Command(tmuxBin, "attach", "-t", sessionTarget(name))
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
			_ = exec.Command(tmuxBin, "set-environment", "-t", sessionTarget(name), "NOZ_PARENT", parent).Run()
		}
	}

	openWindows(tmuxBin, dir, shellWindow, windows)

	if noAttach {
		fmt.Fprintf(os.Stderr, "noz: created %s detached\n", name)
		return nil
	}

	if insideTmux {
		fmt.Fprintf(os.Stderr, "noz: switching to %s\n", name)
		return exec.Command(tmuxBin, "switch-client", "-t", sessionTarget(name)).Run()
	}

	cmd := exec.Command(tmuxBin, "attach", "-t", sessionTarget(name))
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
		_ = exec.Command(tmuxBin, "select-window", "-t", shellWindow).Run()
	}
}

// tagNozSession sets session-level env vars so we can identify noz sessions.
func tagNozSession(tmuxBin, session, slug, repo string) {
	if err := exec.Command(tmuxBin, "set-environment", "-t", sessionTarget(session), "NOZ_SLUG", slug).Run(); err != nil {
		fmt.Fprintf(os.Stderr, "noz: warning: could not tag session %s (NOZ_SLUG): %v\n", session, err)
	}
	if err := exec.Command(tmuxBin, "set-environment", "-t", sessionTarget(session), "NOZ_REPO", repo).Run(); err != nil {
		fmt.Fprintf(os.Stderr, "noz: warning: could not tag session %s (NOZ_REPO): %v\n", session, err)
	}
	// Defensively clear any stale global NOZ_* the server inherited from a shell
	// that had them exported (starting tmux from inside a noz session leaks
	// them). A polluted global otherwise shadows untagged sessions in the status
	// bar and the picker with a wrong repo/slug.
	for _, v := range []string{"NOZ_SLUG", "NOZ_REPO", "NOZ_PARENT"} {
		_ = exec.Command(tmuxBin, "set-environment", "-gu", v).Run()
	}
}

// linkNozDir creates a persistent .noz dir for the repo and symlinks it
// into the worktree. Also excludes .noz from git via the main repo's
// .git/info/exclude (works reliably across all worktrees).
// Safe to call repeatedly — idempotent.
func linkNozDir(root, repo, wtDir string) {
	nozDir := filepath.Join(root, ".noz", repo)
	link := filepath.Join(wtDir, ".noz")

	// Create persistent dir and standard subdirs if they don't exist.
	// brain/ is user-owned; brief/ and reports/ are noz-managed.
	if err := os.MkdirAll(nozDir, 0755); err != nil {
		fmt.Fprintf(os.Stderr, "noz: warning: could not create .noz dir: %v\n", err)
		return
	}
	for _, sub := range []string{"brain", "brief", "reports"} {
		os.MkdirAll(filepath.Join(nozDir, sub), 0755) //nolint:errcheck
	}
	writeBrainReadme(nozDir)

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

// readSessionTask returns the first non-empty line of the ## Task section from
// a session's context file, or "" if the file is absent or has no task.
func readSessionTask(repo, slug string) string {
	data, err := os.ReadFile(briefPath(repo, slug))
	if err != nil {
		return ""
	}
	return extractTaskLine(string(data))
}

// extractTaskLine returns the first non-empty line after a ## Task heading.
func extractTaskLine(content string) string {
	inTask := false
	for _, line := range strings.Split(content, "\n") {
		if strings.HasPrefix(line, "## Task") {
			inTask = true
			continue
		}
		if inTask {
			if strings.HasPrefix(line, "#") {
				break
			}
			if t := strings.TrimSpace(line); t != "" {
				return t
			}
		}
	}
	return ""
}

// brainReadme documents the brain layout so the ownership split is discoverable
// from inside the dir itself, not just the docs. noz writes only brief/ and
// reports/; everything else — including brain/ — is yours.
const brainReadme = `# .noz brain

Per-repo workspace shared across this repo's worktrees, symlinked into each
as ` + "`.noz/`" + `. Three directories, three distinct jobs:

- ` + "`brain/`" + `   — YOURS. Durable knowledge you carry across sessions: notes,
             conventions, scratch. noz never reads or writes here.

- ` + "`brief/`" + `   — the BRIEF (noz -> agent). One file per session, ` + "`<slug>.md`" + `,
             written once when a session is created with a task
             (` + "`noz open --task`" + `, ` + "`noz spawn --task`" + `). The agent's marching
             orders: the task, and for offshoots how to return when done.
             The agent reads it once at launch. noz doesn't update it as work
             proceeds, so keep durable knowledge in brain/ and write outcomes
             to a report.

- ` + "`reports/`" + ` — the DEBRIEF (agent -> you). One file per session, ` + "`<slug>.md`" + `,
             written by ` + "`noz close --report`" + ` when a session ends. What the
             session did, found, and what's left — so you can read the outcome
             without re-entering, or after the session is torn down.

noz recreates these directories on ` + "`noz open`" + `, never their contents. Deleting
a report loses it for good; deleting a brief won't return unless you re-seed
the task. The dirs are disposable; the files are history.
`

// writeBrainReadme drops a layout marker in the brain root. Best-effort and
// write-once — never clobbers an edited README.
func writeBrainReadme(nozDir string) {
	path := filepath.Join(nozDir, "README.md")
	if _, err := os.Stat(path); err == nil {
		return // already present — leave any user edits alone
	}
	os.WriteFile(path, []byte(brainReadme), 0644) //nolint:errcheck
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
	_ = os.MkdirAll(filepath.Dir(path), 0755)

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
	defer func() { _ = f.Close() }()
	_, _ = f.WriteString(pattern + "\n")
}

// validSlug rejects names that could escape a directory or confuse tmux/git.
// Anything that becomes a path or a tmux session name (slugs, profile names)
// must be simple: no separators, no traversal, no leading dash/dot, no spaces.
// maxSlugLen bounds slug length so names stay readable and the `noz ls` table
// never has to truncate or blow out its width. Enforced only when creating a
// slug (validNewSlug) — an existing over-length session must still be
// removable/renamable, so plain validSlug (used by rm/mv/path) skips it.
const maxSlugLen = 48

// validNewSlug validates a slug that is about to be created: safety checks plus
// the length cap. Use this in open/spawn/mv-target; use validSlug to reference
// an existing session.
func validNewSlug(name string) error {
	if err := validSlug(name); err != nil {
		return err
	}
	if len(name) > maxSlugLen {
		return fmt.Errorf("invalid name %q: too long (%d chars, max %d)", name, len(name), maxSlugLen)
	}
	return nil
}

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
	out, err := exec.Command("tmux", "show-environment", "-t", sessionTarget(slug), "NOZ_SLUG").Output()
	if err != nil {
		return false
	}
	return strings.HasPrefix(strings.TrimSpace(string(out)), "NOZ_SLUG=")
}

// tmuxSessionEnv returns the value of a session-level tmux environment variable,
// or "" if the session or variable doesn't exist (or is set but empty). Stateless
// lineage/metadata (NOZ_PARENT) rides on these.
func tmuxSessionEnv(slug, key string) string {
	out, err := exec.Command("tmux", "show-environment", "-t", sessionTarget(slug), key).Output()
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
	out, err := exec.Command("tmux", "show-environment", "-t", sessionTarget(slug), "NOZ_REPO").Output()
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
