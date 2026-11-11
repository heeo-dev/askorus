//go:build !windows && !linux && !darwin

package main

import "errors"

// Other unix systems (the BSDs): the agent runs in the background, but how
// a program starts at logon differs on each, so that part is left to the
// person.

var errNoAutostart = errors.New("starting at logon is not built in on this system; add `askorus-agent start` to your shell's start file")

func autostartCommand(exe string, testPage bool) string {
	if testPage {
		return selfCommand(exe, "start", "-test-page")
	}
	return selfCommand(exe, "start")
}
func autostartSet(string, bool) error     { return errNoAutostart }
func autostartRemove() error              { return nil }
func autostartGet() string                { return "" }
func awakeCommand(int) (string, []string) { return "", nil }
func sleepAfterAC() int                   { return -1 }
