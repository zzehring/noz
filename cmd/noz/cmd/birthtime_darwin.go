//go:build darwin

package cmd

import (
	"os"
	"syscall"
	"time"
)

// fileBirthtime returns a file's creation (birth) time, or the zero time if it
// isn't available. On Darwin/APFS it's recorded in Stat_t.Birthtimespec — a
// durable, reboot-surviving signal noz reads rather than stores. The path is
// unused here (Linux needs it for statx); Darwin reads it from the FileInfo.
func fileBirthtime(_ string, fi os.FileInfo) time.Time {
	st, ok := fi.Sys().(*syscall.Stat_t)
	if !ok {
		return time.Time{}
	}
	return time.Unix(st.Birthtimespec.Sec, st.Birthtimespec.Nsec)
}
