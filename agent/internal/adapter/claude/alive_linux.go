//go:build linux

package claude

import (
	"bytes"
	"os"
	"strconv"
)

// processAlive reports whether pid runs and, when procStart is given,
// started at that time. On Linux Claude Code writes the process's start
// time as /proc/<pid>/stat gives it (field 22, clock ticks since boot;
// seen in ~/.claude/sessions/<pid>.json with Claude Code 2.1.322), so a
// pid that another process took over later does not count (#52).
func processAlive(pid int, procStart string) bool {
	if pid <= 0 {
		return false
	}
	b, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/stat")
	if err != nil {
		return false
	}
	state, start, ok := parseProcStat(b)
	if !ok {
		return true // it is there; its line is just not one this reads
	}
	if state == 'Z' || state == 'X' {
		return false // ended, only not collected by its parent yet
	}
	return procStart == "" || procStart == start
}

// parseProcStat takes the state letter and the start time out of a
// /proc/<pid>/stat line. The program's name sits in brackets and may hold
// spaces and brackets itself, so the fields are counted after the last ")".
func parseProcStat(b []byte) (state byte, start string, ok bool) {
	i := bytes.LastIndexByte(b, ')')
	if i < 0 {
		return 0, "", false
	}
	f := bytes.Fields(b[i+1:])
	// f[0] is field 3 (state); the start time is field 22.
	if len(f) < 20 || len(f[0]) != 1 {
		return 0, "", false
	}
	return f[0][0], string(f[19]), true
}
