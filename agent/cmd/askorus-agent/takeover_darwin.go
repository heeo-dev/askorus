//go:build darwin

package main

// What update-finish needs on macOS to take over (#71). launchd ends only
// the process group of a job that ended, and update-finish runs in a
// session of its own (Setsid), so it has nothing to leave.

import (
	"bytes"
	"encoding/binary"
	"path/filepath"

	"golang.org/x/sys/unix"

	"github.com/heeo-dev/askorus/agent/internal/update"
)

func leaveService() error { return nil }
func inService(int) bool  { return false }
func stopService() error  { return nil }

// heldProcessID is a process held for a signal by its start time: macOS
// has no pidfd.
type heldProcessID struct {
	pid     int
	started unix.Timeval
}

func holdProcess(pid int) (*heldProcessID, error) {
	k, err := unix.SysctlKinfoProc("kern.proc.pid", pid)
	if err != nil {
		return nil, err
	}
	return &heldProcessID{pid: pid, started: k.Proc.P_starttime}, nil
}

func (h *heldProcessID) kill() error {
	k, err := unix.SysctlKinfoProc("kern.proc.pid", h.pid)
	if err != nil || k.Proc.P_starttime != h.started {
		return unix.ESRCH // it ended, and the pid may be someone else's
	}
	return unix.Kill(h.pid, unix.SIGKILL)
}

func (h *heldProcessID) release() {}

// szomb is SZOMB of <sys/proc.h>.
const szomb = 5

func isZombie(pid int) bool {
	k, err := unix.SysctlKinfoProc("kern.proc.pid", pid)
	return err == nil && k.Proc.P_stat == szomb
}

// installedServe reports whether pid is a serve of the program installed
// at exe. macOS keeps the path the program was started by, so it is
// compared with the folders' links resolved (/var is /private/var).
func installedServe(exe string, pid int) bool {
	b, err := unix.SysctlRaw("kern.procargs2", pid)
	if err != nil {
		return false
	}
	runs, args, ok := parseProcArgs(b)
	if !ok || len(args) < 2 || args[1] != "serve" || !filepath.IsAbs(runs) {
		return false
	}
	runs = realDir(runs)
	return runs == realDir(exe) || runs == realDir(update.Sibling(exe, "darwin", "prev"))
}

// realDir is path with the links of its folder resolved. The file itself
// may have been renamed by now.
func realDir(path string) string {
	if dir, err := filepath.EvalSymlinks(filepath.Dir(path)); err == nil {
		return filepath.Join(dir, filepath.Base(path))
	}
	return filepath.Clean(path)
}

// parseProcArgs reads kern.procargs2: the number of arguments, the
// program's path, zeros, and the arguments, each ending in a zero.
func parseProcArgs(b []byte) (exe string, args []string, ok bool) {
	if len(b) < 4 {
		return "", nil, false
	}
	n := int(binary.LittleEndian.Uint32(b))
	path, rest, found := bytes.Cut(b[4:], []byte{0})
	if !found {
		return "", nil, false
	}
	rest = bytes.TrimLeft(rest, "\x00")
	for range n {
		arg, more, found := bytes.Cut(rest, []byte{0})
		if !found {
			return "", nil, false
		}
		args = append(args, string(arg))
		rest = more
	}
	return string(path), args, true
}
