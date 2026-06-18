package cmd

import (
	"strings"
	"testing"
	"time"
)

// metricLine returns the value of the first exposition line whose prefix
// matches (everything up to and including the closing brace / metric name),
// or "" if no line starts with prefix.
func metricLine(out, prefix string) string {
	for line := range strings.SplitSeq(out, "\n") {
		if strings.HasPrefix(line, prefix) {
			return line
		}
	}
	return ""
}

func TestRenderMetricsCountsByState(t *testing.T) {
	now := time.Unix(1_000_000, 0)
	sessions := []sessionInfo{
		{slug: "a", repo: "noz", hasTmux: true, state: "working", lastActive: now.Add(-5 * time.Second)},
		{slug: "b", repo: "noz", hasTmux: true, state: "waiting", lastActive: now.Add(-time.Hour)},
		{slug: "c", repo: "noz", hasTmux: true, state: "needs-you", lastActive: now.Add(-2 * time.Minute)},
		{slug: "d", repo: "noz"}, // idle, no tmux
	}

	out := renderMetrics(sessions, now)

	checks := map[string]string{
		`noz_sessions{state="working"}`: `noz_sessions{state="working"} 1`,
		// needs-you folds into waiting → 2.
		`noz_sessions{state="waiting"}`: `noz_sessions{state="waiting"} 2`,
		`noz_sessions{state="idle"}`:    `noz_sessions{state="idle"} 1`,
		`noz_sessions_total`:            `noz_sessions_total 4`,
	}
	for prefix, want := range checks {
		if got := metricLine(out, prefix); got != want {
			t.Errorf("line %q = %q, want %q", prefix, got, want)
		}
	}

	// HELP/TYPE headers present.
	for _, h := range []string{"# HELP noz_sessions ", "# TYPE noz_sessions gauge"} {
		if !strings.Contains(out, h) {
			t.Errorf("output missing header %q", h)
		}
	}

	// last_activity for a live session uses now - lastActive in seconds.
	if got := metricLine(out, `noz_session_last_activity_seconds{slug="a"`); got != `noz_session_last_activity_seconds{slug="a",repo="noz"} 5` {
		t.Errorf("last_activity for a = %q", got)
	}
	// idle session has no last_activity series.
	if got := metricLine(out, `noz_session_last_activity_seconds{slug="d"`); got != "" {
		t.Errorf("idle session should have no last_activity, got %q", got)
	}
}

func TestRenderMetricsByAgentAndRepo(t *testing.T) {
	now := time.Unix(2_000_000, 0)
	sessions := []sessionInfo{
		{slug: "a", repo: "noz", hasTmux: true, agent: "claude", lastActive: now},
		{slug: "b", repo: "noz", hasTmux: true, agent: "claude", lastActive: now},
		{slug: "c", repo: "other", hasTmux: true, agent: "", lastActive: now}, // empty agent skipped
	}
	out := renderMetrics(sessions, now)

	if got := metricLine(out, `noz_sessions_by_agent{agent="claude"}`); got != `noz_sessions_by_agent{agent="claude"} 2` {
		t.Errorf("by_agent claude = %q", got)
	}
	if strings.Contains(out, `noz_sessions_by_agent{agent=""}`) {
		t.Error("empty agent should be skipped")
	}
	if got := metricLine(out, `noz_sessions_by_repo{repo="noz"}`); got != `noz_sessions_by_repo{repo="noz"} 2` {
		t.Errorf("by_repo noz = %q", got)
	}
	if got := metricLine(out, `noz_sessions_by_repo{repo="other"}`); got != `noz_sessions_by_repo{repo="other"} 1` {
		t.Errorf("by_repo other = %q", got)
	}
}

func TestRenderMetricsEmpty(t *testing.T) {
	out := renderMetrics(nil, time.Now())

	// Empty input still emits the three state buckets at zero and a total.
	for _, want := range []string{
		`noz_sessions{state="working"} 0`,
		`noz_sessions{state="waiting"} 0`,
		`noz_sessions{state="idle"} 0`,
		`noz_sessions_total 0`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("empty output missing %q", want)
		}
	}
}

func TestEscapeLabel(t *testing.T) {
	cases := map[string]string{
		"plain":           "plain",
		`has"quote`:       `has\"quote`,
		`back\slash`:      `back\\slash`,
		"new\nline":       `new\nline`,
		`all"\` + "\nmix": `all\"\\\nmix`,
		"":                "",
	}
	for in, want := range cases {
		if got := escapeLabel(in); got != want {
			t.Errorf("escapeLabel(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestRenderMetricsEscapesLabels(t *testing.T) {
	now := time.Unix(3_000_000, 0)
	sessions := []sessionInfo{
		{slug: `we"ird`, repo: `re\po`, hasTmux: true, agent: "claude", lastActive: now.Add(-time.Second)},
	}
	out := renderMetrics(sessions, now)

	want := `noz_session_last_activity_seconds{slug="we\"ird",repo="re\\po"} 1`
	if !strings.Contains(out, want) {
		t.Errorf("output missing escaped line %q\ngot:\n%s", want, out)
	}
}

func TestSessionState(t *testing.T) {
	cases := []struct {
		s    sessionInfo
		want string
	}{
		{sessionInfo{}, "idle"},
		{sessionInfo{hasTmux: true, state: "working"}, "working"},
		{sessionInfo{hasTmux: true, state: "waiting"}, "waiting"},
		{sessionInfo{hasTmux: true, state: "needs-you"}, "waiting"},
		{sessionInfo{hasTmux: true, state: ""}, "waiting"},
	}
	for _, c := range cases {
		if got := sessionState(c.s); got != c.want {
			t.Errorf("sessionState(%+v) = %q, want %q", c.s, got, c.want)
		}
	}
}
