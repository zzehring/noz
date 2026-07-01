package cmd

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

// sample builds a small fleet of sessions across two repos with one offshoot
// tree, used by the filtering tests below.
func sampleSessions() []sessionInfo {
	return []sessionInfo{
		{slug: "dev", repo: "noz", hasTmux: true, category: "dev"},
		{slug: "session-picker", repo: "noz", parent: "dev", hasTmux: true, category: "session"},
		{slug: "docs-guide", repo: "noz", parent: "dev", hasTmux: true, category: "docs"},
		{slug: "idle-wt", repo: "noz", hasTmux: false, category: "idle"}, // no tmux → not switchable
		{slug: "api-cleanup", repo: "webapp", hasTmux: true, category: "api"},
	}
}

func slugsOf(sessions []sessionInfo) []string {
	out := make([]string, len(sessions))
	for i, s := range sessions {
		out[i] = s.slug
	}
	return out
}

func TestPickSessions(t *testing.T) {
	cases := []struct {
		name    string
		view    string
		current string
		repo    string
		want    []string
	}{
		{
			name: "repo scopes to the current repo, includes self, drops idle",
			view: viewRepo, current: "dev", repo: "noz",
			want: []string{"dev", "session-picker", "docs-guide"},
		},
		{
			name: "children matches NOZ_PARENT == current (parent itself isn't a child)",
			view: viewChildren, current: "dev", repo: "noz",
			want: []string{"session-picker", "docs-guide"},
		},
		{
			name: "children of a leaf session is empty",
			view: viewChildren, current: "session-picker", repo: "noz",
			want: nil,
		},
		{
			name: "all spans repos, includes self, drops idle",
			view: viewAll, current: "dev", repo: "noz",
			want: []string{"dev", "session-picker", "docs-guide", "api-cleanup"},
		},
		{
			name: "repo with unknown repo falls back to everything (drops idle)",
			view: viewRepo, current: "dev", repo: "",
			want: []string{"dev", "session-picker", "docs-guide", "api-cleanup"},
		},
		{
			name: "children with no current session yields nothing",
			view: viewChildren, current: "", repo: "",
			want: nil,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := slugsOf(pickSessions(sampleSessions(), tc.view, tc.current, tc.repo))
			if !equalUnordered(got, tc.want) {
				t.Errorf("pickSessions(%s, current=%q, repo=%q) = %v, want %v",
					tc.view, tc.current, tc.repo, got, tc.want)
			}
		})
	}
}

func equalUnordered(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	seen := map[string]int{}
	for _, s := range a {
		seen[s]++
	}
	for _, s := range b {
		seen[s]--
	}
	for _, n := range seen {
		if n != 0 {
			return false
		}
	}
	return true
}

func TestPickFilter(t *testing.T) {
	var buf bytes.Buffer
	cmd := newPickCmd()
	cmd.SetOut(&buf)
	matches := []sessionInfo{{slug: "dev"}, {slug: "docs-guide"}}
	if err := emitPickFilter(cmd, matches); err != nil {
		t.Fatal(err)
	}
	got := strings.TrimSpace(buf.String())
	want := "#{||:#{==:#{session_name},dev},#{==:#{session_name},docs-guide}}"
	if got != want {
		t.Errorf("filter =\n  %s\nwant\n  %s", got, want)
	}
	// The expression must be space-free so it survives the tmux binding's
	// unquoted command substitution.
	if strings.ContainsAny(got, " \t") {
		t.Errorf("filter must not contain whitespace: %q", got)
	}
}

func TestPickFilterSingleAndEmpty(t *testing.T) {
	render := func(matches []sessionInfo) string {
		var buf bytes.Buffer
		cmd := newPickCmd()
		cmd.SetOut(&buf)
		_ = emitPickFilter(cmd, matches)
		return strings.TrimSpace(buf.String())
	}
	if got := render([]sessionInfo{{slug: "dev"}}); got != "#{==:#{session_name},dev}" {
		t.Errorf("single = %q", got)
	}
	// No matches → "0", which tmux treats as false for every row.
	if got := render(nil); got != "0" {
		t.Errorf("empty = %q, want 0", got)
	}
}

func TestPickLabelRepoViewOmitsRepo(t *testing.T) {
	s := sessionInfo{slug: "x", repo: "noz", lastActive: time.Now()}
	if got := pickLabel(s, false); strings.Contains(got, "(noz)") {
		t.Errorf("repo view label should omit repo, got %q", got)
	}
	if got := pickLabel(s, true); !strings.Contains(got, "(noz)") {
		t.Errorf("cross-repo view label should include repo, got %q", got)
	}
}

func TestPickJSON(t *testing.T) {
	matches := []sessionInfo{
		{slug: "session-picker", repo: "noz", parent: "dev", agent: "claude"},
	}
	var buf bytes.Buffer
	cmd := newPickCmd()
	cmd.SetOut(&buf)
	if err := emitPickJSON(cmd, matches); err != nil {
		t.Fatal(err)
	}
	var got []pickSessionJSON
	if err := json.Unmarshal(buf.Bytes(), &got); err != nil {
		t.Fatalf("output is not valid JSON: %v\n%s", err, buf.String())
	}
	if len(got) != 1 || got[0].Session != "session-picker" || got[0].Parent != "dev" {
		t.Errorf("unexpected JSON: %+v", got)
	}
}
