//go:build darwin

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
