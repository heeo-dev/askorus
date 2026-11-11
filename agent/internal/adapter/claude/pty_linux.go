//go:build linux

package claude

import (
	"os"
	"strconv"
	"strings"
	"syscall"

	"golang.org/x/sys/unix"
)

// openPTY opens a new pseudo terminal and returns its controlling side and
// the name of the side the program gets.
func openPTY() (*os.File, string, error) {
	m, err := os.OpenFile("/dev/ptmx", os.O_RDWR|syscall.O_NOCTTY, 0)
	if err != nil {
		return nil, "", err
	}
	n, err := unix.IoctlGetInt(int(m.Fd()), unix.TIOCGPTN)
	if err == nil {
		err = unix.IoctlSetPointerInt(int(m.Fd()), unix.TIOCSPTLCK, 0) // unlock
	}
	if err != nil {
		m.Close()
		return nil, "", err
	}
	return m, "/dev/pts/" + strconv.Itoa(n), nil
}

// processParents maps every process id to its parent's: the sessions a
// folder server creates run as its child processes.
func processParents() map[int]int {
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return nil
	}
	m := map[int]int{}
	for _, e := range entries {
		pid, err := strconv.Atoi(e.Name())
		if err != nil {
			continue
		}
		b, err := os.ReadFile("/proc/" + e.Name() + "/stat")
		if err != nil {
			continue
		}
		// "pid (name) state ppid ...": the name may hold spaces and brackets.
		i := strings.LastIndexByte(string(b), ')')
		if i < 0 {
			continue
		}
		f := strings.Fields(string(b[i+1:]))
		if len(f) < 2 {
			continue
		}
		if ppid, err := strconv.Atoi(f[1]); err == nil {
			m[pid] = ppid
		}
	}
	return m
}

// processElevated reports whether pid runs as root, the administrator here.
func processElevated(pid int) (elevated, known bool) {
	info, err := os.Stat("/proc/" + strconv.Itoa(pid))
	if err != nil {
		return false, false
	}
	st, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return false, false
	}
	return st.Uid == 0, true
}
