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
	"github.com/zzehring/nozey/internal/agent"
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
	agent      string // detected coding agent (claude, opencode, ...), or ""
	state      string // working | waiting | needs-you (live sessions only)
	memKiB     int    // resident memory of the session's process tree, in KiB
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
		Short: "List pairing sessions",
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
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			filter := ""
			if len(args) > 0 {
				filter = args[0]
			}
			return runLs(cmd, filter, activeOnly, staleOnly, all, groupMin)
		},
	}

	cmd.Flags().BoolVarP(&all, "all", "A", false, "show sessions across all repos")
	cmd.Flags().BoolVar(&activeOnly, "active", false, "only show sessions with a live tmux session")
	cmd.Flags().BoolVar(&staleOnly, "idle", false, "only show worktrees without a tmux session")
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

	// Compute max slug width for alignment
	maxSlug := 0
	for _, s := range sessions {
		if len(s.slug) > maxSlug {
			maxSlug = len(s.slug)
		}
	}
	if maxSlug > 45 {
		maxSlug = 45
	}

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
		fmt.Fprintf(w, "%s    %-*s  %-9s  win  last    %-5s%s%s\n", cGray, maxSlug, "", "state", "mem", repoHdr, cReset)
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
			renderSession(w, s, maxSlug, showRepo)
		}
	}

	// Summary
	fmt.Fprintf(w, "\n%s%d active · %d idle · %d total%s\n",
		cGray, activeCount, staleCount, activeCount+staleCount, cReset)

	return nil
}

func renderSession(w io.Writer, s sessionInfo, slugWidth int, showRepo bool) {
	name := s.slug
	if len(name) > slugWidth {
		name = name[:slugWidth-1] + "…"
	}

	repoStr := ""
	if showRepo && s.repo != "" {
		repoStr = fmt.Sprintf("  %s%s%s", cGray, s.repo, cReset)
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

		fmt.Fprintf(w, "  %s %-*s  %s%-9s%s  %s%-3s%s  %s%-6s%s  %s%-5s%s%s\n",
			marker, slugWidth, name,
			stateColor, stateLabel, cReset,
			cCyan, winStr, cReset,
			cYellow, idleStr, cReset,
			cGray, humanMem(s.memKiB), cReset,
			repoStr)
	} else {
		marker := cGray + "○" + cReset
		fmt.Fprintf(w, "  %s %s%-*s%s  %-9s  %-3s  %-6s  %-5s%s\n",
			marker, cDim, slugWidth, name, cReset,
			"", "", "", "",
			repoStr)
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

		if td, ok := tmux[slug]; ok {
			s.hasTmux = true
			s.windows = td.windows
			s.lastActive = td.lastActive
			s.attached = td.attached
			s.agent = td.agent
			s.memKiB = td.memKiB
		}

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
	agent      string // detected coding agent (claude, opencode, ...), or ""
	memKiB     int    // resident memory of all panes' process trees, in KiB
}

func getTmuxDetails() map[string]tmuxDetail {
	details := make(map[string]tmuxDetail)

	// Session-level info
	out, err := exec.Command("tmux", "ls", "-F", "#{session_name}\t#{session_windows}\t#{session_activity}\t#{session_attached}").Output()
	if err != nil {
		return details
	}

	for line := range strings.SplitSeq(strings.TrimSpace(string(out)), "\n") {
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, "\t", 4)
		if len(parts) < 4 {
			continue
		}

		name := parts[0]
		windows, _ := strconv.Atoi(parts[1])
		epoch, _ := strconv.ParseInt(parts[2], 10, 64)
		attachCount, _ := strconv.Atoi(parts[3])

		details[name] = tmuxDetail{
			windows:    windows,
			lastActive: time.Unix(epoch, 0),
			attached:   attachCount > 0,
		}
	}

	enrichFromPanes(details)
	return details
}

// enrichFromPanes walks every pane to (a) detect the coding agent running in
// each session and (b) sum the resident memory of each session's process
// trees. Best-effort — leaves fields zero/empty on any failure.
func enrichFromPanes(details map[string]tmuxDetail) {
	out, err := exec.Command("tmux", "list-panes", "-a", "-F", "#{session_name}\t#{pane_pid}\t#{pane_current_command}").Output()
	if err != nil {
		return
	}

	panePIDs := make(map[string][]string)
	for line := range strings.SplitSeq(strings.TrimSpace(string(out)), "\n") {
		parts := strings.SplitN(line, "\t", 3)
		if len(parts) < 3 {
			continue
		}
		name, pid, cmd := parts[0], parts[1], parts[2]
		d, known := details[name]
		if !known {
			continue
		}
		if d.agent == "" {
			if ag := agent.Detect(cmd); ag != "" {
				d.agent = ag
				details[name] = d
			}
		}
		panePIDs[name] = append(panePIDs[name], pid)
	}

	rss, children := readProcTree()
	if rss == nil {
		return
	}
	for name, pids := range panePIDs {
		seen := make(map[string]bool)
		total := 0
		for _, p := range pids {
			total += subtreeRSS(p, rss, children, seen)
		}
		d := details[name]
		d.memKiB = total
		details[name] = d
	}
}

// readProcTree snapshots all processes' RSS (KiB) and parent→children edges.
func readProcTree() (rss map[string]int, children map[string][]string) {
	out, err := exec.Command("ps", "-axo", "pid=,ppid=,rss=").Output()
	if err != nil {
		return nil, nil
	}
	rss = make(map[string]int)
	children = make(map[string][]string)
	for line := range strings.SplitSeq(strings.TrimSpace(string(out)), "\n") {
		f := strings.Fields(line)
		if len(f) < 3 {
			continue
		}
		pid, ppid := f[0], f[1]
		kib, _ := strconv.Atoi(f[2])
		rss[pid] = kib
		children[ppid] = append(children[ppid], pid)
	}
	return rss, children
}

// subtreeRSS sums the RSS of pid and all its descendants. The seen set guards
// against cycles and double-counting a pid reached via multiple panes.
func subtreeRSS(pid string, rss map[string]int, children map[string][]string, seen map[string]bool) int {
	if seen[pid] {
		return 0
	}
	seen[pid] = true
	total := rss[pid]
	for _, c := range children[pid] {
		total += subtreeRSS(c, rss, children, seen)
	}
	return total
}

// humanMem renders a KiB count as a compact MiB/GiB string ("397M", "1.2G").
func humanMem(kib int) string {
	if kib <= 0 {
		return ""
	}
	mib := float64(kib) / 1024
	if mib >= 1024 {
		return fmt.Sprintf("%.1fG", mib/1024)
	}
	return fmt.Sprintf("%.0fM", mib)
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
	// Fallback: infer from how recently the session produced output.
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
