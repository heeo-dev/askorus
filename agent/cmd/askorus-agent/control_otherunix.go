//go:build !windows && !linux && !darwin

package main

// Other unix systems (the BSDs): the agent runs in the background, but how
// a program starts at logon differs on each, so that part is left to the
// person.

var errNoAutostart = fail("starting at logon is not built in on this system; add `askorus-agent start` to your shell's start file",
	"이 시스템에서는 로그인할 때 켜기를 지원하지 않습니다. 셸 시작 파일에 `askorus-agent start`를 넣으세요")

func autostartCommand(exe string) string {
	return selfCommand(exe, "start")
}
func autostartSet(string) error           { return errNoAutostart }
func autostartRemove() error              { return nil }
func autostartGet() string                { return "" }
func awakeCommand(int) (string, []string) { return "", nil }
func sleepAfterAC() int                   { return -1 }
