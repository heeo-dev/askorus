package dialog

import (
	"context"
	"os/exec"
	"strings"
)

// confirm asks through osascript, which every Mac has. With no screen
// session (SSH, a server) osascript ends with an error: not shown.
func confirm(ctx context.Context, q Question) (allow, shown bool) {
	path, err := exec.LookPath("osascript")
	if err != nil {
		return false, false
	}
	out, err := exec.CommandContext(ctx, path, osascriptArgs(q)...).Output()
	if ctx.Err() != nil {
		return false, true
	}
	if err != nil {
		return false, false
	}
	return strings.TrimSpace(string(out)) == q.Allow, true
}

// Show is the Windows child; elsewhere there is nothing to show this way.
func Show(string, string) int { return ExitFailed }

// Missing names what this PC lacks to show the question: nothing here.
func Missing() string { return "" }
