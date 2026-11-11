//go:build linux || darwin

package claude

import (
	"os"

	"golang.org/x/sys/unix"
)

// fakeRawInput switches the terminal to raw input, as the real server
// screen does, so Ctrl+C arrives as the byte 0x03 instead of a signal
// (#52: the same on a pseudo terminal as on the Windows pseudo console).
func fakeRawInput() {
	fd := int(os.Stdin.Fd())
	t, err := unix.IoctlGetTermios(fd, termiosGet)
	if err != nil {
		return
	}
	t.Lflag &^= unix.ICANON | unix.ECHO | unix.ISIG
	t.Cc[unix.VMIN], t.Cc[unix.VTIME] = 1, 0
	_ = unix.IoctlSetTermios(fd, termiosSet, t)
}
