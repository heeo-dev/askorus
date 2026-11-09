//go:build windows

package main

import (
	"os"

	"golang.org/x/sys/windows"
)

// holdConsole turns off the console's QuickEdit while pair waits (#37):
// in a classic console window (conhost, Windows PowerShell) a click starts
// a selection and the window stops showing the program's output until a
// key is pressed, so the "Allow ...? [y/N]" question did not appear until
// the person typed y. Windows Terminal does not stop like that and ignores
// the flag. The returned function puts the old mode back. When stdin is
// not a console (a pipe or a file), it does nothing.
func holdConsole() (restore func()) {
	h := windows.Handle(os.Stdin.Fd())
	var mode uint32
	if windows.GetConsoleMode(h, &mode) != nil {
		return func() {}
	}
	// ENABLE_EXTENDED_FLAGS must be set for the QuickEdit bit to count.
	if windows.SetConsoleMode(h, quickEditOff(mode)) != nil {
		return func() {}
	}
	return func() { windows.SetConsoleMode(h, mode) }
}

// quickEditOff is mode without QuickEdit (with the extended flags that make
// the change take effect).
func quickEditOff(mode uint32) uint32 {
	return (mode | windows.ENABLE_EXTENDED_FLAGS) &^ windows.ENABLE_QUICK_EDIT_MODE
}
