//go:build !windows

package claude

import "errors"

// Folder servers need a pseudo terminal; macOS and Linux come with the
// agents for those systems (plan step D).
func startPTY(exe, dir string, args []string) (serverProcess, error) {
	return nil, errors.New("folder servers are only supported on Windows for now")
}

func ptySupported() bool { return false }

func processParents() map[int]int { return nil }

func processElevated(pid int) (elevated, known bool) { return false, false }

func selfElevated() bool { return false }
