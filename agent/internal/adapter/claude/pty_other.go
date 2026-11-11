//go:build !windows

package claude

import "os"

// selfElevated: on macOS and Linux the administrator is root (#52). The
// agent is meant to run as the person, with the person's AI sign-ins.
func selfElevated() bool { return os.Geteuid() == 0 }
