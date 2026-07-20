package cmd

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"github.com/zzehring/noz/internal/agent"
)

type sessionInfo struct {
	slug       string
	repo       string
	dir        string
	category   string
	hasTmux    bool
	windows    int
	lastActive time.Time
	attached   bool
	agent      string    // detected coding agent (claude, opencode, ...), or ""
	state      string    // working | waiting | needs-you (live sessions only)
	created    time.Time // worktree birth time — durable, survives reboot/restore
	parent     string    // NOZ_PARENT tag — spawning session name ("" for top-level)
	task       string    // first line of ## Task from context file, or ""
}

// defaultMinCategorySize is the minimum number of sessions for a prefix
// to get its own category group. Smaller groups collapse into "other".
const defaultMinCategorySize = 2

func newLsCmd() *cobra.Command {
	var activeOnly bool
	var staleOnly bool
	var groupMin int
	var all bool

	cmd := &cobra.Command{
		Use:   "ls [filter]",
		Short: "List sessions",
		Long: `Lists sessions by scanning worktree directories and cross-referencing
with tmux. Completely stateless — derives everything from filesystem, git,
and tmux metadata.

Scoped to the current repo by default. Use -A to see all repos.

Sessions are grouped by slug prefix (cf-, review-, i-, etc).
  ● = live tmux session    ○ = idle (worktree exists, no tmux)

Filter supports substring match by default, or prefix match with ^:
  noz ls review       # substring: matches anything containing "review"
  noz ls ^cf          # prefix: only slugs starting with "cf"

Examples:
  noz ls              # current repo sessions
  noz ls -A           # all repos
  noz ls cf           # filter to cf-* sessions
  noz ls ^review      # only review-* (not dd-review-*)
  noz ls -a           # only live tmux sessions
  noz ls -i           # only idle worktrees (cleanup candidates)`,
		Args:              cobra.MaximumNArgs(1),
		ValidArgsFunction: completeSlugs,
		RunE: func(cmd *cobra.Command, args []string) error {
			filter := ""
			if len(args) > 0 {
				filter = args[0]
			}
			return runLs(cmd, filter, activeOnly, staleOnly, all, groupMin)
		},
	}

	cmd.Flags().BoolVarP(&all, "all", "A", false, "show sessions across all repos")
	cmd.Flags().BoolVarP(&activeOnly, "active", "a", false, "only show sessions with a live tmux session")
	cmd.Flags().BoolVarP(&staleOnly, "idle", "i", false, "only show worktrees without a tmux session")
	cmd.Flags().IntVarP(&groupMin, "group-min", "g", defaultMinCategorySize, "min sessions for a prefix to get its own group")

	return cmd
}

func runLs(cmd *cobra.Command, filter string, activeOnly, staleOnly, all bool, groupMin int) error {
	initColors()

	sessions, err := discoverSessions()
	if err != nil {
		return err
	}

	// Default: scope to current repo if in one (unless -A)
	if !all && inGitRepo() {
		repo, err := repoName()
		if err == nil {
			var scoped []sessionInfo
			for _, s := range sessions {
				if s.repo == repo {
					scoped = append(scoped, s)
				}
			}
			sessions = scoped
		}
	}

	// Apply filters
	sessions = filterSessions(sessions, filter, activeOnly, staleOnly)

	if len(sessions) == 0 {
		fmt.Fprintln(cmd.OutOrStdout(), "No sessions found.")
		return nil
	}

	// Group by category
	groups, order := groupByCategory(sessions, groupMin)

	w := cmd.OutOrStdout()

	// Determine if repo column is needed (multiple repos present)
	showRepo := hasMultipleRepos(sessions)

	// Column width fits the longest slug — never truncate a name, since a long
	// slug you can't read defeats the point of naming the session.
	maxSlug := 0
	for _, s := range sessions {
		if len(s.slug) > maxSlug {
			maxSlug = len(s.slug)
		}
	}

	// The session this command was invoked from, so we can mark it (there can
	// be several attached at once — "attached" alone doesn't say which is you).
	current := currentTmuxSession()

	// Column header
	hasActive := false
	for _, s := range sessions {
		if s.hasTmux {
			hasActive = true
			break
		}
	}
	if hasActive {
		repoHdr := ""
		if showRepo {
			repoHdr = "  repo"
		}
		fmt.Fprintf(w, "%s    %-*s  %-9s  win  last    created%s%s\n", cGray, maxSlug, "", "state", repoHdr, cReset)
	}

	// Render
	activeCount := 0
	staleCount := 0

	for ci, cat := range order {
		items := groups[cat]

		// Sort: active first, then alphabetically
		sort.Slice(items, func(i, j int) bool {
			if items[i].hasTmux != items[j].hasTmux {
				return items[i].hasTmux
			}
			return items[i].slug < items[j].slug
		})

		catActive := 0
		for _, s := range items {
			if s.hasTmux {
				catActive++
			}
		}

		// Category header
		if ci > 0 {
			fmt.Fprintln(w)
		}
		fmt.Fprintf(w, "%s%s%s", cBold, cat, cReset)
		fmt.Fprintf(w, " %s(%d/%d)%s\n", cGray, catActive, len(items), cReset)

		for _, s := range items {
			if s.hasTmux {
				activeCount++
			} else {
				staleCount++
			}
			renderSession(w, s, maxSlug, showRepo, current)
		}
	}

	// Summary
	fmt.Fprintf(w, "\n%s%d active · %d idle · %d total%s\n",
		cGray, activeCount, staleCount, activeCount+staleCount, cReset)

	// Nudge to recover sessions that were live last time but aren't now —
	// typically a reboot killed tmux while the worktrees survived.
	if stranded := strandedSessions(sessions); len(stranded) > 0 {
		fmt.Fprintf(w, "%s%d session(s) were active recently but aren't running (%s) — `noz restore <slug>` brings one back (e.g. after a reboot)%s\n",
			cYellow, len(stranded), strings.Join(stranded, ", "), cReset)
	}

	return nil
}

// strandedSessions returns idle worktrees that were recently active but aren't
// running now — restore candidates, typically after a reboot. Derived from
// durable activity signals (agent transcripts, worktree mtime), no persisted
// state.
func strandedSessions(sessions []sessionInfo) []string {
	window := restoreWindow()
	var out []string
	for _, s := range sessions {
		if s.hasTmux {
			continue
		}
		act := sessionActivity(s.dir)
		if act.IsZero() || time.Since(act) > window {
			continue
		}
		out = append(out, s.slug)
	}
	return out
}

// createdStr renders a session's creation time as a short relative string, or
// "" when birth time isn't available (e.g. a filesystem without btime).
func createdStr(s sessionInfo) string {
	if s.created.IsZero() {
		return ""
	}
	return relativeTime(s.created)
}

func renderSession(w io.Writer, s sessionInfo, slugWidth int, showRepo bool, current string) {
	name := s.slug
	if len(name) > slugWidth {
		name = name[:slugWidth-1] + "…"
	}

	repoStr := ""
	if showRepo && s.repo != "" {
		repoStr = fmt.Sprintf("  %s%s%s", cGray, s.repo, cReset)
	}

	hereStr := ""
	if current != "" && s.slug == current {
		hereStr = "  " + cBold + cGreen + "← here" + cReset
	}

	taskStr := ""
	if s.task != "" {
		t := s.task
		runes := []rune(t)
		if len(runes) > 40 {
			t = string(runes[:39]) + "…"
		}
		taskStr = fmt.Sprintf("  %s%s%s", cDim, t, cReset)
	}

	if s.hasTmux {
		marker := cGreen + "●" + cReset
		if s.attached {
			marker = cGreen + "▶" + cReset
		}

		stateLabel, stateColor := stateDisplay(s.state)
		// A blocked session is worth shouting about — flip the marker.
		if stateLabel == "needs you" {
			marker = cRed + cBold + "!" + cReset
		}

		winStr := fmt.Sprintf("%d", s.windows)
		idleStr := ""
		if !s.lastActive.IsZero() {
			idleStr = relativeTime(s.lastActive)
		}

		fmt.Fprintf(w, "  %s %-*s  %s%-9s%s  %s%-3s%s  %s%-6s%s  %s%-7s%s%s%s%s\n",
			marker, slugWidth, name,
			stateColor, stateLabel, cReset,
			cCyan, winStr, cReset,
			cYellow, idleStr, cReset,
			cGray, createdStr(s), cReset,
			repoStr, taskStr, hereStr)
	} else {
		marker := cGray + "○" + cReset
		fmt.Fprintf(w, "  %s %s%-*s%s  %-9s  %-3s  %-6s  %s%-7s%s%s%s%s\n",
			marker, cDim, slugWidth, name, cReset,
			"", "", "",
			cGray, createdStr(s), cReset,
			repoStr, taskStr, hereStr)
	}
}

// discoverSessions scans the worktree root and cross-references with tmux.
// Entirely stateless — reads filesystem and tmux on every call.
func discoverSessions() ([]sessionInfo, error) {
	root := nozRoot()

	entries, err := os.ReadDir(root)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("reading %s: %w", root, err)
	}

	tmux := getTmuxDetails()

	var sessions []sessionInfo
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}

		name := e.Name()
		if strings.HasPrefix(name, ".") {
			continue
		}
		dir := filepath.Join(root, name)
		repo, slug := detectRepo(dir, name)

		s := sessionInfo{
			slug: slug,
			repo: repo,
			dir:  dir,
		}

		// Match a live session to this worktree only when its NOZ_REPO tag
		// agrees (or is absent) — so a same-slug session in another repo
		// doesn't get claimed by both worktrees.
		if td, ok := tmux[slug]; ok && (td.repo == "" || td.repo == repo) {
			s.hasTmux = true
			s.windows = td.windows
			s.lastActive = td.lastActive
			s.attached = td.attached
			s.agent = td.agent
			s.parent = td.parent
		}

		if fi, err := e.Info(); err == nil {
			s.created = fileBirthtime(dir, fi)
		}
		s.task = readSessionTask(s.repo, s.slug)
		s.state = claudeState(s)
		s.category = categorizeSlug(slug)
		sessions = append(sessions, s)
	}

	return sessions, nil
}

// detectRepo reads the .git file in a worktree dir to find the parent repo.
func detectRepo(dir, name string) (repo, slug string) {
	slug = name

	gitPath := filepath.Join(dir, ".git")
	fi, err := os.Stat(gitPath)
	if err != nil || fi.IsDir() {
		return "", slug
	}

	// .git is a file = this is a git worktree
	data, err := os.ReadFile(gitPath)
	if err != nil {
		return "", slug
	}

	base := worktreeMainRepo(string(data))
	if base == "" {
		return "", slug
	}
	repo = filepath.Base(base)
	slug = strings.TrimPrefix(name, repo+"-")
	return repo, slug
}

// categorizeSlug derives a category from the first segment of the slug (before the first hyphen).
func categorizeSlug(slug string) string {
	if before, _, found := strings.Cut(slug, "-"); found {
		return before
	}
	return slug
}

func filterSessions(sessions []sessionInfo, filter string, activeOnly, staleOnly bool) []sessionInfo {
	var out []sessionInfo
	for _, s := range sessions {
		if activeOnly && !s.hasTmux {
			continue
		}
		if staleOnly && s.hasTmux {
			continue
		}
		if filter != "" {
			var match bool
			if strings.HasPrefix(filter, "^") {
				// Prefix match on slug
				prefix := filter[1:]
				match = strings.HasPrefix(s.slug, prefix)
			} else {
				match = strings.Contains(s.slug, filter) ||
					strings.Contains(s.repo, filter) ||
					strings.Contains(s.category, filter)
			}
			if !match {
				continue
			}
		}
		out = append(out, s)
	}
	return out
}

func groupByCategory(sessions []sessionInfo, groupMin int) (map[string][]sessionInfo, []string) {
	groups := make(map[string][]sessionInfo)

	for _, s := range sessions {
		groups[s.category] = append(groups[s.category], s)
	}

	// Collapse small categories into "other"
	var collapsed []string
	for cat, items := range groups {
		if cat != "other" && len(items) < groupMin {
			collapsed = append(collapsed, cat)
		}
	}
	if len(collapsed) > 1 {
		for _, cat := range collapsed {
			groups["other"] = append(groups["other"], groups[cat]...)
			delete(groups, cat)
		}
	}

	// Build sorted category list (alphabetical, "other" last)
	var order []string
	for cat := range groups {
		order = append(order, cat)
	}
	sort.Slice(order, func(i, j int) bool {
		if order[i] == "other" {
			return false
		}
		if order[j] == "other" {
			return true
		}
		return order[i] < order[j]
	})

	return groups, order
}

func hasMultipleRepos(sessions []sessionInfo) bool {
	seen := ""
	for _, s := range sessions {
		if s.repo == "" {
			continue
		}
		if seen == "" {
			seen = s.repo
		} else if s.repo != seen {
			return true
		}
	}
	return false
}

// tmux metadata

type tmuxDetail struct {
	windows    int
	lastActive time.Time
	attached   bool
	repo       string // NOZ_REPO tag — which repo this session belongs to ("" if untagged)
	agent      string // detected coding agent (claude, opencode, ...), or ""
	parent     string // NOZ_PARENT tag — spawning session name ("" for top-level)
}

func getTmuxDetails() map[string]tmuxDetail {
	details := make(map[string]tmuxDetail)

	// Session-level info (incl. the NOZ_REPO tag, which disambiguates same-slug
	// sessions across repos).
	out, err := exec.Command("tmux", "ls", "-F", "#{session_name}\t#{session_windows}\t#{session_activity}\t#{session_attached}\t#{NOZ_REPO}\t#{NOZ_PARENT}").Output()
	if err != nil {
		return details
	}

	for line := range strings.SplitSeq(strings.TrimSpace(string(out)), "\n") {
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, "\t", 6)
		if len(parts) < 4 {
			continue
		}

		name := parts[0]
		windows, _ := strconv.Atoi(parts[1])
		epoch, _ := strconv.ParseInt(parts[2], 10, 64)
		attachCount, _ := strconv.Atoi(parts[3])
		repo := ""
		if len(parts) >= 5 {
			repo = parts[4]
		}
		parent := ""
		if len(parts) >= 6 {
			parent = parts[5]
		}

		details[name] = tmuxDetail{
			windows:    windows,
			lastActive: time.Unix(epoch, 0),
			attached:   attachCount > 0,
			repo:       repo,
			parent:     parent,
		}
	}

	detectAgents(details)
	return details
}

// detectAgents scans every pane's current command and tags each session with
// the coding agent running in it (first match wins). Best-effort.
func detectAgents(details map[string]tmuxDetail) {
	out, err := exec.Command("tmux", "list-panes", "-a", "-F", "#{session_name}\t#{pane_current_command}").Output()
	if err != nil {
		return
	}
	for line := range strings.SplitSeq(strings.TrimSpace(string(out)), "\n") {
		name, cmd, ok := strings.Cut(line, "\t")
		if !ok {
			continue
		}
		d, known := details[name]
		if !known || d.agent != "" {
			continue
		}
		if ag := agent.Detect(cmd); ag != "" {
			d.agent = ag
			details[name] = d
		}
	}
}

// workingWindow is how recently a live session must have produced output
// to count as actively "working" under the activity heuristic. Claude's
// TUI redraws its spinner/timer ~every second while busy, so a tight
// window cleanly separates working from waiting-for-input.
const workingWindow = 20 * time.Second

// claudeStateDir holds optional per-session state files. They're written by
// Claude Code hooks (if installed) and read here; absent that, ls falls back
// to the activity heuristic, so this works with no setup at all.
func claudeStateDir() string {
	if d := os.Getenv("NOZ_STATE_DIR"); d != "" {
		return d
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".cache", "noz", "state")
}

// claudeState returns a coarse activity state for a live session. A cached
// state file (event-driven, written by hooks) wins when present; otherwise
// it's derived from recent tmux output. Empty for sessions with no tmux.
func claudeState(s sessionInfo) string {
	if !s.hasTmux {
		return ""
	}
	// Authoritative state from hooks, if any.
	if data, err := os.ReadFile(filepath.Join(claudeStateDir(), s.slug)); err == nil {
		if st := strings.TrimSpace(string(data)); st != "" {
			return st
		}
	}
	// Fallback: infer from how recently the session produced output. Only
	// meaningful when an agent is actually running in the session — a plain
	// shell that printed something 5s ago isn't "working".
	if s.agent == "" {
		return ""
	}
	if s.lastActive.IsZero() {
		return ""
	}
	if time.Since(s.lastActive) < workingWindow {
		return "working"
	}
	return "waiting"
}

// stateDisplay maps a state word to its label and color. Returns empty
// strings for unknown/empty states so the column simply stays blank.
func stateDisplay(state string) (label, color string) {
	switch state {
	case "working":
		return "working", cGreen
	case "waiting":
		return "waiting", cYellow
	case "needs-you", "needs you", "blocked":
		return "needs you", cRed
	default:
		return "", ""
	}
}

// currentTmuxSession returns the name of the tmux session this process is
// running inside, or "" if not in tmux. Session names are noz slugs.
func currentTmuxSession() string {
	if os.Getenv("TMUX") == "" {
		return ""
	}
	out, err := exec.Command("tmux", "display-message", "-p", "#S").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// currentSession resolves the noz session this process is "in". It prefers the
// attached tmux client's session ($TMUX), but falls back to deriving it from the
// current worktree (cwd) when there's no client — so it also works from a
// detached/background process (e.g. an MCP server running outside the tmux
// pane), which has a cwd but no $TMUX. Returns "" if neither resolves to a noz
// worktree. Used by `noz close` and `noz status` so they self-target the same
// way regardless of tmux attachment.
func currentSession() string {
	if s := currentTmuxSession(); s != "" {
		return s
	}
	out, err := exec.Command("git", "rev-parse", "--show-toplevel").Output()
	if err != nil {
		return ""
	}
	return sessionFromDir(strings.TrimSpace(string(out)), nozRoot())
}

// sessionFromDir returns the noz session slug for a worktree that is a direct
// child of root (named "<repo>-<slug>"), or "" if dir isn't such a worktree —
// so we never guess a session from an unrelated directory.
func sessionFromDir(dir, root string) string {
	if dir == "" || filepath.Clean(filepath.Dir(dir)) != filepath.Clean(root) {
		return ""
	}
	_, slug := detectRepo(dir, filepath.Base(dir))
	return slug
}

// lastTmuxSession returns the session this client was attached to just before
// the current one. tmux tracks this per client, so noz keeps no state of its
// own. Empty when not in tmux or there's no previous session.
func lastTmuxSession() string {
	if os.Getenv("TMUX") == "" {
		return ""
	}
	out, err := exec.Command("tmux", "display-message", "-p", "#{client_last_session}").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// tmuxSessionNames lists the names of all active tmux sessions.
func tmuxSessionNames() []string {
	out, err := exec.Command("tmux", "ls", "-F", "#S").Output()
	if err != nil {
		return nil
	}
	var names []string
	for name := range strings.SplitSeq(strings.TrimSpace(string(out)), "\n") {
		if name != "" {
			names = append(names, name)
		}
	}
	return names
}

// tmuxSessions returns active tmux session names as a set. Used by
// `noz prune` to decide which worktrees still have a live session.
func tmuxSessions() map[string]bool {
	set := make(map[string]bool)
	for _, name := range tmuxSessionNames() {
		set[name] = true
	}
	return set
}

// Time formatting

func relativeTime(t time.Time) string {
	d := time.Since(t)
	switch {
	case d < time.Minute:
		return "now"
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh", int(d.Hours()))
	case d < 7*24*time.Hour:
		return fmt.Sprintf("%dd", int(d.Hours()/24))
	default:
		w := int(d.Hours() / (24 * 7))
		if w > 52 {
			return fmt.Sprintf("%dy", w/52)
		}
		return fmt.Sprintf("%dw", w)
	}
}

// ANSI colors — disabled when NO_COLOR is set or stdout is not a terminal.

var (
	cReset  = "\033[0m"
	cBold   = "\033[1m"
	cDim    = "\033[2m"
	cRed    = "\033[31m"
	cGreen  = "\033[32m"
	cYellow = "\033[33m"
	cCyan   = "\033[36m"
	cGray   = "\033[90m"
)

func initColors() {
	if os.Getenv("NO_COLOR") != "" || !stdoutIsTerminal() {
		cReset = ""
		cBold = ""
		cDim = ""
		cRed = ""
		cGreen = ""
		cYellow = ""
		cCyan = ""
		cGray = ""
	}
}

func stdoutIsTerminal() bool {
	fi, err := os.Stdout.Stat()
	if err != nil {
		return false
	}
	return fi.Mode()&os.ModeCharDevice != 0
}

func stdinIsTerminal() bool {
	fi, err := os.Stdin.Stat()
	if err != nil {
		return false
	}
	return fi.Mode()&os.ModeCharDevice != 0
}
