//go:build !darwin && !linux

package cmd

// footprintMiB has no portable implementation outside Darwin and Linux, so it
// reports 0 (unknown). reap still works — it just can't show or total the
// memory it would reclaim. macOS and Linux both have real paths.
func footprintMiB(_ string) int { return 0 }
