// Package dialog asks the person at the PC a yes or no question in a small
// window of its own (#58). The agent runs in the background with no window
// and no terminal, so this is how it reaches whoever sits at the PC: a phone
// of the same Tailscale account asks to pair, and the PC shows who asks.
//
// The window belongs to a child process (the agent itself on Windows,
// osascript on macOS, zenity or kdialog on Linux), so it can be taken away
// the moment the answer came from elsewhere: ending the context ends the
// child. A PC with no screen to show it on (a server, an SSH session)
// reports that instead of an answer.
package dialog

import (
	"context"
	"strings"
)

// Question is what the window says. Text may have several lines.
type Question struct {
	Title string
	Text  string
	// Allow and Deny are the button labels where the system lets a
	// program choose them (macOS, Linux). Windows shows its own Yes and No.
	Allow string
	Deny  string
}

// SelfArg is the hidden first argument with which the agent runs itself to
// show the window on Windows: askorus-agent <SelfArg> <title> <text>. The
// child ends with exit code 0 for yes, 1 for no and 2 when it could not
// show anything.
const SelfArg = "confirm-dialog"

// Exit codes of the child on Windows.
const (
	ExitAllow  = 0
	ExitDeny   = 1
	ExitFailed = 2
)

// Confirm shows q and waits for the answer, or for ctx to end (the window is
// closed then). shown is false when no window could be shown.
func Confirm(ctx context.Context, q Question) (allow, shown bool) {
	return confirm(ctx, clean(q))
}

// clean keeps control characters out of what is shown: the device name in
// the text comes from the phone.
func clean(q Question) Question {
	strip := func(s string, lines bool) string {
		return strings.Map(func(r rune) rune {
			if r == '\n' && lines {
				return r
			}
			if r < 0x20 || r == 0x7f {
				return ' '
			}
			return r
		}, s)
	}
	return Question{Title: strip(q.Title, false), Text: strip(q.Text, true), Allow: strip(q.Allow, false), Deny: strip(q.Deny, false)}
}

// appleScript is the script osascript runs on macOS. The words reach it as
// arguments (title, text, allow, deny), never as part of the script, so
// nothing in them is read as AppleScript. The first button is the default:
// Return refuses.
var appleScript = []string{
	"on run argv",
	"set r to display dialog (item 2 of argv) with title (item 1 of argv) buttons {item 4 of argv, item 3 of argv} default button 1 with icon note",
	"return button returned of r",
	"end run",
}

// osascriptArgs are the arguments for osascript.
func osascriptArgs(q Question) []string {
	var a []string
	for _, line := range appleScript {
		a = append(a, "-e", line)
	}
	return append(a, q.Title, q.Text, q.Allow, q.Deny)
}

// zenityArgs are the arguments for zenity. --no-markup: the text is shown
// as it is, not read as markup.
func zenityArgs(q Question) []string {
	return []string{"--question", "--no-markup", "--title=" + q.Title, "--text=" + q.Text, "--ok-label=" + q.Allow, "--cancel-label=" + q.Deny, "--default-cancel"}
}

// kdialogArgs are the arguments for kdialog.
func kdialogArgs(q Question) []string {
	return []string{"--title", q.Title, "--yes-label", q.Allow, "--no-label", q.Deny, "--warningyesno", q.Text}
}
