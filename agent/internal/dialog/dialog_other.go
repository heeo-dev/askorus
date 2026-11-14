//go:build !windows && !darwin

package dialog

import (
	"context"
	"errors"
	"os"
	"os/exec"
)

// confirm asks through zenity or kdialog when the agent has a display and
// one of them is installed. Many Linux PCs that run the agent have neither
// (servers, SSH): not shown, and the person uses `askorus-agent pair`.
func confirm(ctx context.Context, q Question) (allow, shown bool) {
	if os.Getenv("DISPLAY") == "" && os.Getenv("WAYLAND_DISPLAY") == "" {
		return false, false
	}
	var cmd *exec.Cmd
	if path, err := exec.LookPath("zenity"); err == nil {
		cmd = exec.CommandContext(ctx, path, zenityArgs(q)...)
	} else if path, err := exec.LookPath("kdialog"); err == nil {
		cmd = exec.CommandContext(ctx, path, kdialogArgs(q)...)
	} else {
		return false, false
	}
	err := cmd.Run()
	if ctx.Err() != nil {
		return false, true
	}
	if err == nil {
		return true, true
	}
	// Both end with 1 for the refusing button; anything else is a window
	// that did not open (no display after all).
	var exit *exec.ExitError
	if errors.As(err, &exit) && exit.ExitCode() == 1 {
		return false, true
	}
	return false, false
}

// Show is the Windows child; elsewhere there is nothing to show this way.
func Show(string, string) int { return ExitFailed }
