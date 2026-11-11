//go:build !windows && !linux && !darwin

package claude

import "errors"

// Folder servers need a pseudo terminal, which every unix opens in its own
// way; only Linux and macOS are written (pty_linux.go, pty_darwin.go).
func startPTY(exe, dir string, args []string) (serverProcess, error) {
	return nil, errors.New("folder servers are not supported on this system")
}

func ptySupported() bool { return false }

func processParents() map[int]int { return nil }

func processElevated(pid int) (elevated, known bool) { return false, false }
