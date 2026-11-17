package proc

// Whether this process runs in the Mac's screen session (#62). macOS keeps
// Claude Code's sign-in in the login keychain, which only the session of
// the person logged in at the Mac's screen can read (launchd names it
// Aqua). A process started over SSH lives in a session of its own
// (Background) and stays in it after SSH ends, and so does every program
// it starts: the claude an agent started over SSH runs sees no sign-in.
// Windows and Linux keep the sign-in in a file, so this is about macOS
// only.

import (
	"os"
	"os/exec"
	"runtime"
	"strings"
	"sync"
)

var (
	screenOnce sync.Once
	offScreen  bool
)

// OutsideScreenSession reports whether this process runs on a Mac outside
// the screen's session, as when it was started over SSH. It asks once; on
// other systems it is false.
func OutsideScreenSession() bool {
	if runtime.GOOS != "darwin" {
		return false
	}
	screenOnce.Do(func() {
		out, err := exec.Command("/bin/launchctl", "managername").Output()
		offScreen = outsideScreenOf(string(out), err, os.Getenv)
	})
	return offScreen
}

// outsideScreenOf reads `launchctl managername`: Aqua is the screen's
// session, and Background (SSH), StandardIO, LoginWindow and System are
// not. When launchctl cannot tell, the variables an SSH session sets do.
func outsideScreenOf(manager string, err error, getenv func(string) string) bool {
	if m := strings.TrimSpace(manager); err == nil && m != "" {
		return m != "Aqua"
	}
	for _, k := range []string{"SSH_CONNECTION", "SSH_CLIENT", "SSH_TTY"} {
		if getenv(k) != "" {
			return true
		}
	}
	return false
}
