//go:build !windows

package proc

import (
	"os"
	"path/filepath"
	"runtime"
)

// usualDirs are the folders the AI programs' own installers use on macOS
// and Linux, looked at when a program is not on PATH (#52). An agent that
// starts at logon keeps the PATH of the day it was told to: a program
// installed later into a folder of its own (OpenCode's ~/.opencode/bin)
// would not be found until `autostart on` was run again.
func usualDirs(home, goos string) []string {
	dirs := []string{
		filepath.Join(home, ".local", "bin"),
		filepath.Join(home, ".opencode", "bin"),
		filepath.Join(home, ".claude", "local"),
		filepath.Join(home, ".npm-global", "bin"),
		filepath.Join(home, ".bun", "bin"),
		filepath.Join(home, "bin"),
		"/usr/local/bin",
	}
	if goos == "darwin" {
		dirs = append(dirs, "/opt/homebrew/bin")
	}
	return dirs
}

// LookUsual finds the program called name in the usual folders: a file
// this user may run, not a folder.
func LookUsual(name string) (string, bool) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", false
	}
	for _, dir := range usualDirs(home, runtime.GOOS) {
		p := filepath.Join(dir, name)
		if info, err := os.Stat(p); err == nil && info.Mode().IsRegular() && info.Mode().Perm()&0o111 != 0 {
			return p, true
		}
	}
	return "", false
}
