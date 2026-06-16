package cmd

import "testing"

func TestParseFootprintMiB(t *testing.T) {
	cases := map[string]int{
		"    phys_footprint:        393 MB\n":               393,
		"phys_footprint: 53 MB":                             53,
		"node [1]:\n    phys_footprint: 1.5 GB\n":           1536,
		"    phys_footprint: 2048 KB":                       2,
		"phys_footprint_peak: 999 MB\nphys_footprint: 7 MB": 7,
		"no footprint here":                                 0,
		"":                                                  0,
	}
	for out, want := range cases {
		if got := parseFootprintMiB(out); got != want {
			t.Errorf("parseFootprintMiB(%q) = %d, want %d", out, got, want)
		}
	}
}

func TestFindAgent(t *testing.T) {
	// pane shell (100) -> node running claude (200) -> child (300)
	pi := procInfo{
		cmd: map[string]string{
			"100": "-zsh",
			"200": "node /Users/user/.local/share/claude/cli.js",
			"300": "rg --files",
		},
		children: map[string][]string{
			"100": {"200"},
			"200": {"300"},
		},
	}
	if got := pi.findAgent("100", "claude", map[string]bool{}); got != "200" {
		t.Errorf("findAgent = %q, want 200", got)
	}
	// no match
	if got := pi.findAgent("100", "opencode", map[string]bool{}); got != "" {
		t.Errorf("findAgent(opencode) = %q, want \"\"", got)
	}
	// cycle guard: 100 <-> 200, neither mentions the agent
	cyc := procInfo{
		cmd:      map[string]string{"100": "-zsh", "200": "vim"},
		children: map[string][]string{"100": {"200"}, "200": {"100"}},
	}
	if got := cyc.findAgent("100", "claude", map[string]bool{}); got != "" {
		t.Errorf("findAgent with cycle = %q, want \"\"", got)
	}
}
