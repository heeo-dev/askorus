package dialog

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"syscall"

	"golang.org/x/sys/windows"
)

const createNoWindow = 0x08000000

// confirm runs the agent itself as a child that shows the message box, so
// the box goes away when ctx ends.
func confirm(ctx context.Context, q Question) (allow, shown bool) {
	exe, err := os.Executable()
	if err != nil {
		return false, false
	}
	cmd := exec.CommandContext(ctx, exe, SelfArg, q.Title, q.Text)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: createNoWindow}
	err = cmd.Run()
	if ctx.Err() != nil {
		return false, true // closed because the answer came from elsewhere, or the time is up
	}
	if err == nil {
		return true, true
	}
	var exit *exec.ExitError
	if errors.As(err, &exit) && exit.ExitCode() == ExitDeny {
		return false, true
	}
	return false, false
}

// Show is the child: it shows the message box and returns the exit code.
// No is the default button, so a stray Enter refuses; the box comes to the
// front and stays above other windows.
func Show(title, text string) int {
	t, err1 := windows.UTF16PtrFromString(text)
	c, err2 := windows.UTF16PtrFromString(title)
	if err1 != nil || err2 != nil {
		return ExitFailed
	}
	const flags = windows.MB_YESNO | windows.MB_ICONQUESTION | windows.MB_DEFBUTTON2 | windows.MB_SETFOREGROUND | windows.MB_TOPMOST
	ret, _ := windows.MessageBox(0, t, c, flags)
	switch ret {
	case 6: // IDYES
		return ExitAllow
	case 7: // IDNO
		return ExitDeny
	}
	return ExitFailed
}
