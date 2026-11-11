//go:build darwin

package claude

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
	"unsafe"
)

// openPTY opens a new pseudo terminal and returns its controlling side and
// the name of the side the program gets. macOS grants, unlocks and names
// the other side through three requests on the first.
func openPTY() (*os.File, string, error) {
	m, err := os.OpenFile("/dev/ptmx", os.O_RDWR|syscall.O_NOCTTY, 0)
	if err != nil {
		return nil, "", err
	}
	name := make([]byte, 128) // the size TIOCPTYGNAME writes
	for _, step := range []struct {
		req uintptr
		arg uintptr
	}{
		{syscall.TIOCPTYGRANT, 0},
		{syscall.TIOCPTYUNLK, 0},
		{syscall.TIOCPTYGNAME, uintptr(unsafe.Pointer(&name[0]))},
	} {
		if _, _, errno := syscall.Syscall(syscall.SYS_IOCTL, m.Fd(), step.req, step.arg); errno != 0 {
			m.Close()
			return nil, "", errno
		}
	}
	end := bytes.IndexByte(name, 0)
	if end <= 0 {
		m.Close()
		return nil, "", errors.New("the pseudo terminal has no name")
	}
	return m, string(name[:end]), nil
}

// psColumns asks ps for two number columns of every process (or one).
func psColumns(args ...string) [][2]int {
	out, err := exec.Command("/bin/ps", args...).Output()
	if err != nil {
		return nil
	}
	var rows [][2]int
	for _, line := range strings.Split(string(out), "\n") {
		f := strings.Fields(line)
		if len(f) != 2 {
			continue
		}
		a, err1 := strconv.Atoi(f[0])
		b, err2 := strconv.Atoi(f[1])
		if err1 == nil && err2 == nil {
			rows = append(rows, [2]int{a, b})
		}
	}
	return rows
}

// processParents maps every process id to its parent's: the sessions a
// folder server creates run as its child processes.
func processParents() map[int]int {
	rows := psColumns("-axo", "pid=,ppid=")
	if rows == nil {
		return nil
	}
	m := make(map[int]int, len(rows))
	for _, r := range rows {
		m[r[0]] = r[1]
	}
	return m
}

// processElevated reports whether pid runs as root, the administrator here.
func processElevated(pid int) (elevated, known bool) {
	rows := psColumns("-o", "pid=,uid=", "-p", strconv.Itoa(pid))
	if len(rows) != 1 {
		return false, false
	}
	return rows[0][1] == 0, true
}
