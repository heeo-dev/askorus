package dialog

import (
	"context"
	"os"
	"testing"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

// The agent shows the question by running itself with SelfArg. In this test
// the test binary stands in for the agent, so it has to answer to SelfArg
// the same way.
func TestMain(m *testing.M) {
	if len(os.Args) == 4 && os.Args[1] == SelfArg {
		os.Exit(Show(os.Args[2], os.Args[3]))
	}
	os.Exit(m.Run())
}

var (
	user32              = windows.NewLazySystemDLL("user32.dll")
	procIsWindowVisible = user32.NewProc("IsWindowVisible")
	procFindWindow      = user32.NewProc("FindWindowW")
)

// findWindow returns the top-level window with that title, or 0.
func findWindow(title *uint16) uintptr {
	h, _, _ := procFindWindow.Call(0, uintptr(unsafe.Pointer(title)))
	return h
}

// The window is really on the screen (#58). The child is started without a
// console window; a start-up setting that also hides the child's first
// window would leave a question nobody can see or answer, and pairing would
// just run into its time limit. The window is closed again as soon as it is
// seen (ending the context ends the child).
func TestTheQuestionIsVisibleAndGoesAwayWithTheContext(t *testing.T) {
	if os.Getenv("SESSIONNAME") == "" && os.Getenv("USERPROFILE") == "" {
		t.Skip("no desktop session")
	}
	const title = "Askorus dialog test (closes by itself)"
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	done := make(chan [2]bool, 1)
	go func() {
		allow, shown := confirm(ctx, Question{Title: title, Text: "This window is a test and closes by itself."})
		done <- [2]bool{allow, shown}
	}()

	name, _ := windows.UTF16PtrFromString(title)
	visible := false
	deadline := time.Now().Add(6 * time.Second)
	for time.Now().Before(deadline) && !visible {
		if hwnd := findWindow(name); hwnd != 0 {
			r, _, _ := procIsWindowVisible.Call(hwnd)
			visible = r != 0
		}
		if !visible {
			time.Sleep(20 * time.Millisecond)
		}
	}
	cancel() // takes the window away
	select {
	case got := <-done:
		if got[0] {
			t.Error("a window that was taken away counted as yes")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the window stayed after the context ended")
	}
	if !visible {
		t.Fatal("the question window never became visible")
	}
	if findWindow(name) != 0 {
		t.Error("the window is still there")
	}
}
