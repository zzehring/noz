//go:build !darwin

package cmd

import (
	"os"
	"time"
)

// fileBirthtime degrades to the zero time off Darwin: birth time isn't portably
// available without statx (Linux), so the "created" column simply hides there.
// TODO: Linux support via golang.org/x/sys/unix Statx (STATX_BTIME).
func fileBirthtime(_ os.FileInfo) time.Time { return time.Time{} }
