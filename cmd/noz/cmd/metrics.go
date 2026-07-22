package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/spf13/cobra"
)

func newMetricsCmd() *cobra.Command {
	var textfile string

	cmd := &cobra.Command{
		Use:   "metrics",
		Short: "Emit Prometheus metrics for the current sessions",
		Long: `Renders the live session set as Prometheus text exposition format.

noz is daemonless, so metrics are produced on demand. Run this periodically
(cron / launchd) with --textfile pointing at the node_exporter or Alloy
textfile collector directory to feed Prometheus/Mimir:

  noz metrics --textfile /var/lib/node_exporter/textfile/noz.prom

With no flag the metrics are written to stdout.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			sessions, err := discoverSessions()
			if err != nil {
				return err
			}
			out := renderMetrics(sessions, time.Now())
			if textfile != "" {
				return writeTextfile(textfile, out)
			}
			_, err = fmt.Fprint(cmd.OutOrStdout(), out)
			return err
		},
	}

	cmd.Flags().StringVar(&textfile, "textfile", "", "write metrics atomically to this path (for the textfile collector) instead of stdout")

	return cmd
}

// writeTextfile writes content to path atomically (temp file in the same
// directory, then rename) so the textfile collector never reads a partial file.
func writeTextfile(path, content string) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".noz-metrics-*.tmp")
	if err != nil {
		return fmt.Errorf("creating temp file in %s: %w", dir, err)
	}
	tmpName := tmp.Name()
	defer func() { _ = os.Remove(tmpName) }() // no-op once renamed

	if _, err := tmp.WriteString(content); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("writing temp file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("closing temp file: %w", err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		return fmt.Errorf("renaming temp file to %s: %w", path, err)
	}
	return nil
}

// sessionState collapses a sessionInfo into one of three buckets for metrics:
// idle (no tmux), working, or waiting. "needs-you" and any other live state
// folds into "waiting" so the label set stays small and stable.
func sessionState(s sessionInfo) string {
	if !s.hasTmux {
		return "idle"
	}
	if s.state == "working" {
		return "working"
	}
	return "waiting"
}

// renderMetrics turns the session set into Prometheus text exposition format.
// Pure: no IO, deterministic ordering, so it's straightforward to unit-test.
func renderMetrics(sessions []sessionInfo, now time.Time) string {
	var b strings.Builder

	// noz_sessions{state} — always emit all three buckets so series don't
	// vanish (and gaps in graphs) when a state momentarily has no sessions.
	byState := map[string]int{"working": 0, "waiting": 0, "idle": 0}
	byAgent := map[string]int{}
	byRepo := map[string]int{}
	for _, s := range sessions {
		byState[sessionState(s)]++
		if s.agent != "" {
			byAgent[s.agent]++
		}
		if s.repo != "" {
			byRepo[s.repo]++
		}
	}

	b.WriteString("# HELP noz_sessions Number of noz sessions by state.\n")
	b.WriteString("# TYPE noz_sessions gauge\n")
	for _, state := range []string{"working", "waiting", "idle"} {
		fmt.Fprintf(&b, "noz_sessions{state=\"%s\"} %d\n", escapeLabel(state), byState[state])
	}

	b.WriteString("# HELP noz_sessions_by_agent Number of noz sessions by detected coding agent.\n")
	b.WriteString("# TYPE noz_sessions_by_agent gauge\n")
	for _, agent := range sortedKeys(byAgent) {
		fmt.Fprintf(&b, "noz_sessions_by_agent{agent=\"%s\"} %d\n", escapeLabel(agent), byAgent[agent])
	}

	b.WriteString("# HELP noz_sessions_by_repo Number of noz sessions by repo.\n")
	b.WriteString("# TYPE noz_sessions_by_repo gauge\n")
	for _, repo := range sortedKeys(byRepo) {
		fmt.Fprintf(&b, "noz_sessions_by_repo{repo=\"%s\"} %d\n", escapeLabel(repo), byRepo[repo])
	}

	// Per-session last-activity, live sessions only (idle worktrees have no
	// meaningful activity timestamp). Sorted by slug+repo for determinism.
	b.WriteString("# HELP noz_session_last_activity_seconds Seconds since a live session last produced output.\n")
	b.WriteString("# TYPE noz_session_last_activity_seconds gauge\n")
	live := make([]sessionInfo, 0, len(sessions))
	for _, s := range sessions {
		if s.hasTmux && !s.lastActive.IsZero() {
			live = append(live, s)
		}
	}
	sort.Slice(live, func(i, j int) bool {
		if live[i].slug != live[j].slug {
			return live[i].slug < live[j].slug
		}
		return live[i].repo < live[j].repo
	})
	for _, s := range live {
		secs := now.Sub(s.lastActive).Seconds()
		if secs < 0 {
			secs = 0
		}
		fmt.Fprintf(&b, "noz_session_last_activity_seconds{slug=\"%s\",repo=\"%s\"} %d\n",
			escapeLabel(s.slug), escapeLabel(s.repo), int64(secs))
	}

	b.WriteString("# HELP noz_sessions_total Total number of noz sessions (live and idle).\n")
	b.WriteString("# TYPE noz_sessions_total gauge\n")
	fmt.Fprintf(&b, "noz_sessions_total %d\n", len(sessions))

	return b.String()
}

// escapeLabel escapes a label value per the Prometheus text exposition format:
// backslash, double-quote, and newline.
func escapeLabel(v string) string {
	r := strings.NewReplacer(
		`\`, `\\`,
		`"`, `\"`,
		"\n", `\n`,
	)
	return r.Replace(v)
}

func sortedKeys(m map[string]int) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
