//go:build linux

package cmd

import "testing"

func TestParseProcKiB(t *testing.T) {
	const smapsRollup = `00400000-7fff00000000 ---p 00000000 00:00 0   [rollup]
Rss:               12345 kB
Pss:                8192 kB
Pss_Anon:           4096 kB
Shared_Clean:        512 kB
`
	const status = `Name:	claude
State:	S (sleeping)
VmPeak:	  1048576 kB
VmRSS:	   65536 kB
Threads:	8
`
	cases := []struct {
		name string
		text string
		key  string
		want int
	}{
		{"pss from smaps_rollup", smapsRollup, "Pss:", 8192},
		{"rss from status", status, "VmRSS:", 65536},
		{"prefix not mistaken (Pss vs Pss_Anon)", smapsRollup, "Pss:", 8192},
		{"missing key", status, "Pss:", 0},
		{"empty text", "", "Pss:", 0},
		{"malformed value", "Pss:\tnotanumber kB\n", "Pss:", 0},
	}
	for _, c := range cases {
		if got := parseProcKiB(c.text, c.key); got != c.want {
			t.Errorf("%s: parseProcKiB(key=%q) = %d, want %d", c.name, c.key, got, c.want)
		}
	}
}

// footprintMiB on a nonexistent pid must report 0, never error or panic.
func TestFootprintMiBMissingPID(t *testing.T) {
	if got := footprintMiB("0"); got != 0 {
		t.Errorf("footprintMiB(missing) = %d, want 0", got)
	}
}
