//go:build windows

package proc

// LookUsual finds nothing on Windows: there the programs are on PATH, and
// which file may be run is decided by LookExe (an exe, never a script).
func LookUsual(name string) (string, bool) { return "", false }
