//go:build darwin

package cmd

import (
	"os/exec"
	"strconv"
	"strings"
)

// footprintMiB returns a process's physical footprint (resident + compressed)
// in MiB via macOS `footprint`, or 0 if unavailable. Slow (~1s) — call only on
// reap candidates, never on the ls hot path.
func footprintMiB(pid string) int {
	out, err := exec.Command("footprint", pid).Output()
	if err != nil {
		return 0
	}
	return parseFootprintMiB(string(out))
}

// parseFootprintMiB extracts the phys_footprint value from `footprint` output
// and normalizes it to MiB. Returns 0 when no value is found.
func parseFootprintMiB(out string) int {
	for line := range strings.SplitSeq(out, "\n") {
		// e.g. "    phys_footprint: 53 MB"
		_, after, ok := strings.Cut(line, "phys_footprint:")
		if !ok {
			continue
		}
		f := strings.Fields(after)
		if len(f) == 0 {
			continue
		}
		val, err := strconv.ParseFloat(f[0], 64)
		if err != nil {
			continue
		}
		unit := ""
		if len(f) > 1 {
			unit = f[1]
		}
		switch unit {
		case "GB", "G":
			return int(val * 1024)
		case "KB", "K":
			return int(val / 1024)
		default: // MB / M / bytes-ish
			return int(val)
		}
	}
	return 0
}
