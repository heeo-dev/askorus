//go:build !windows

package proc

import (
	"os"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// processRunning reports whether pid still runs. A process that was killed
// stays in the table until its new parent has collected it, so it is given
// a moment; one that only waits to be collected (a zombie) does not run.
func processRunning(pid int) bool {
	deadline := time.Now().Add(3 * time.Second)
	for {
		if !running(pid) {
			return false
		}
		if time.Now().After(deadline) {
			return true
		}
		time.Sleep(50 * time.Millisecond)
	}
}

func running(pid int) bool {
	if err := syscall.Kill(pid, 0); err != nil {
		return false
	}
	// Linux says in /proc whether it is a zombie; elsewhere the signal is all there is.
	if b, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/stat"); err == nil {
		if i := strings.LastIndexByte(string(b), ')'); i >= 0 && i+2 < len(b) {
			return b[i+2] != 'Z'
		}
	}
	return true
}
