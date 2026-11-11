//go:build !windows && !linux

package claude

import (
	"errors"
	"syscall"
)

// processAlive reports whether pid runs. The start time Claude Code
// writes next to a pid is not compared here: how it is written on macOS
// has not been seen on a real Mac yet (#52), so a pid that another
// process took over counts as alive until then.
func processAlive(pid int, procStart string) bool {
	if pid <= 0 {
		return false
	}
	err := syscall.Kill(pid, 0)
	return err == nil || errors.Is(err, syscall.EPERM)
}
