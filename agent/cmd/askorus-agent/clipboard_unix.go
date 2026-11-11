//go:build !windows

package main

import (
	"errors"
	"os"
	"os/exec"
	"strings"
)

// The clipboard on macOS and Linux (#52), for `pair`'s c + Enter: the
// system's own copy program takes the text on its standard input, so the
// pairing link never shows on a command line. macOS always has pbcopy; on
// Linux it is whichever of wl-copy (Wayland), xclip or xsel (X11) is
// installed and has a display to talk to. Without one, pair says so and
// the link is still on screen to select.

type copyTool struct {
	name string
	args []string
	env  string // the variable that must be set for it to work, or ""
}

func copyTools(goos string) []copyTool {
	if goos == "darwin" {
		return []copyTool{{name: "pbcopy"}}
	}
	return []copyTool{
		{name: "wl-copy", env: "WAYLAND_DISPLAY"},
		{name: "xclip", args: []string{"-selection", "clipboard"}, env: "DISPLAY"},
		{name: "xsel", args: []string{"--clipboard", "--input"}, env: "DISPLAY"},
	}
}

// pickCopyTool returns the first tool that is installed and has what it
// needs; look reports whether a program is on PATH.
func pickCopyTool(goos string, getenv func(string) string, look func(string) (string, error)) (string, []string, bool) {
	for _, t := range copyTools(goos) {
		if t.env != "" && getenv(t.env) == "" {
			continue
		}
		if path, err := look(t.name); err == nil {
			return path, t.args, true
		}
	}
	return "", nil, false
}

func clipboardSupported() bool {
	_, _, ok := pickCopyTool(goos, os.Getenv, exec.LookPath)
	return ok
}

func clipboardWrite(text string) error {
	path, args, ok := pickCopyTool(goos, os.Getenv, exec.LookPath)
	if !ok {
		return errors.New("no clipboard program was found")
	}
	cmd := exec.Command(path, args...)
	cmd.Stdin = strings.NewReader(text)
	return cmd.Run()
}
