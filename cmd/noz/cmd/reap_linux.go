//go:build linux

package cmd

import (
	"os"
	"strconv"
	"strings"
)

// footprintMiB returns a process's memory footprint in MiB on Linux. It prefers
// Pss (proportional set size) from /proc/<pid>/smaps_rollup — the closest analog
// to macOS phys_footprint, since it attributes shared pages fairly across the
// processes mapping them — and falls back to VmRSS from /proc/<pid>/status when
// smaps_rollup isn't present (pre-4.14 kernels) or readable. Returns 0 if
// neither is available. Reads /proc directly, so unlike Darwin's `footprint`
// shell-out it's fast enough to never need rationing.
func footprintMiB(pid string) int {
	base := "/proc/" + pid
	if data, err := os.ReadFile(base + "/smaps_rollup"); err == nil {
		if kib := parseProcKiB(string(data), "Pss:"); kib > 0 {
			return kib / 1024
		}
	}
	if data, err := os.ReadFile(base + "/status"); err == nil {
		if kib := parseProcKiB(string(data), "VmRSS:"); kib > 0 {
			return kib / 1024
		}
	}
	return 0
}

// parseProcKiB extracts the value (in KiB) of a "Key:   <n> kB" line from
// /proc-style status text, or 0 if the key is absent or malformed. The kernel
// reports these fields in KiB despite the "kB" label.
func parseProcKiB(text, key string) int {
	for line := range strings.SplitSeq(text, "\n") {
		after, ok := strings.CutPrefix(line, key)
		if !ok {
			continue
		}
		f := strings.Fields(after)
		if len(f) == 0 {
			return 0
		}
		kib, err := strconv.Atoi(f[0])
		if err != nil {
			return 0
		}
		return kib
	}
	return 0
}
