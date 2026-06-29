//go:build !darwin && !linux

package cmd

import (
	"os"
	"time"
)

// fileBirthtime degrades to the zero time on platforms without a birth-time
// path (everything but Darwin and Linux): the "created" column simply hides
// there rather than guessing.
func fileBirthtime(_ string, _ os.FileInfo) time.Time { return time.Time{} }
