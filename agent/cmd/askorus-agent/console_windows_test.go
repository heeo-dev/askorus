//go:build windows

package main

import (
	"testing"

	"golang.org/x/sys/windows"
)

// R-90 (#37): pair turns QuickEdit off (with the extended flags that make
// it count) and leaves the other console input modes as they were. R-95:
// mouse input goes off with it, or a terminal behind ConPTY (Windows
// Terminal) gives the mouse to the program and a drag no longer selects
// the link.
func TestQuickEditOff(t *testing.T) {
	// A console's usual input mode: mouse input is on by default.
	on := uint32(windows.ENABLE_PROCESSED_INPUT | windows.ENABLE_LINE_INPUT | windows.ENABLE_ECHO_INPUT | windows.ENABLE_MOUSE_INPUT |
		windows.ENABLE_INSERT_MODE | windows.ENABLE_QUICK_EDIT_MODE | windows.ENABLE_EXTENDED_FLAGS)
	got := quickEditOff(on)
	if got&windows.ENABLE_QUICK_EDIT_MODE != 0 {
		t.Errorf("QuickEdit still on: %#x", got)
	}
	if got&windows.ENABLE_MOUSE_INPUT != 0 {
		t.Errorf("mouse input still on, the terminal would stop selecting with the mouse: %#x", got)
	}
	if got&windows.ENABLE_INSERT_MODE == 0 {
		t.Errorf("insert mode dropped: %#x", got)
	}
	if got&windows.ENABLE_EXTENDED_FLAGS == 0 {
		t.Errorf("extended flags dropped: %#x", got)
	}
	keep := uint32(windows.ENABLE_PROCESSED_INPUT | windows.ENABLE_LINE_INPUT | windows.ENABLE_ECHO_INPUT)
	if got&keep != keep {
		t.Errorf("line input or echo dropped: %#x", got)
	}
	// Without the extended flags, QuickEdit was not in effect; they are added so the change counts.
	if got := quickEditOff(windows.ENABLE_LINE_INPUT); got != windows.ENABLE_LINE_INPUT|windows.ENABLE_EXTENDED_FLAGS {
		t.Errorf("quickEditOff(line input) = %#x", got)
	}
}
