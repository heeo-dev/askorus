//go:build darwin

package claude

import "golang.org/x/sys/unix"

const (
	termiosGet = unix.TIOCGETA
	termiosSet = unix.TIOCSETA
)
