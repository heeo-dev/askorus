//go:build !windows

package claude

import (
	"errors"
	"os"
)

// Folder servers need a pseudo terminal; macOS and Linux come with the
// agents for those systems (plan step D).
func startPTY(exe, dir string, args []string) (serverProcess, error) {
	return nil, errors.New("folder servers are only supported on Windows for now")
}

func ptySupported() bool { return false }

func processParents() map[int]int { return nil }

func processElevated(pid int) (elevated, known bool) { return false, false }

// selfElevated: on macOS and Linux the administrator is root (#52). The
// agent is meant to run as the person, with the person's AI sign-ins.
func selfElevated() bool { return os.Geteuid() == 0 }
