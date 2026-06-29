//go:build linux

package cmd

import (
	"os"
	"time"

	"golang.org/x/sys/unix"
)

// fileBirthtime returns a file's creation (birth) time via statx(2) with
// STATX_BTIME, or the zero time if the filesystem doesn't record it. Like
// Darwin's Birthtimespec it's a durable, reboot-surviving signal noz reads
// rather than stores. Some filesystems (e.g. tmpfs, older ext4) leave btime
// unset; statx then clears STATX_BTIME in the result mask and we fall back to
// the zero time, hiding the "created" column rather than guessing.
func fileBirthtime(path string, _ os.FileInfo) time.Time {
	var stx unix.Statx_t
	if err := unix.Statx(unix.AT_FDCWD, path, 0, unix.STATX_BTIME, &stx); err != nil {
		return time.Time{}
	}
	if stx.Mask&unix.STATX_BTIME == 0 {
		return time.Time{}
	}
	return time.Unix(stx.Btime.Sec, int64(stx.Btime.Nsec))
}
