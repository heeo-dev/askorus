//go:build windows

package claude

import (
	"os"

	"golang.org/x/sys/windows"
)

// fakeRawInput switches the console input to raw mode, as the real server
// screen does, so Ctrl+C arrives as the byte 0x03 instead of a signal.
func fakeRawInput() {
	h := windows.Handle(os.Stdin.Fd())
	var mode uint32
	if windows.GetConsoleMode(h, &mode) == nil {
		mode &^= windows.ENABLE_LINE_INPUT | windows.ENABLE_ECHO_INPUT | windows.ENABLE_PROCESSED_INPUT
		windows.SetConsoleMode(h, mode|windows.ENABLE_VIRTUAL_TERMINAL_INPUT)
	}
}
