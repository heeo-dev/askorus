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
// the person typed y. Mouse input goes off with it (R-95): with QuickEdit
// off and mouse input on, a console hands every click and drag to the
// program, and Windows Terminal (and any terminal behind ConPTY) then stops
// selecting text with the mouse, so the link pair prints could not be
// copied. pair reads no mouse events; with both off the terminal keeps the
// mouse and a drag selects as usual. The returned function puts the old
// mode back. When stdin is not a console (a pipe or a file), it does
// nothing.
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

// quickEditOff is mode without QuickEdit and without mouse input (with the
// extended flags that make the change take effect).
func quickEditOff(mode uint32) uint32 {
	return (mode | windows.ENABLE_EXTENDED_FLAGS) &^ (windows.ENABLE_QUICK_EDIT_MODE | windows.ENABLE_MOUSE_INPUT)
}
