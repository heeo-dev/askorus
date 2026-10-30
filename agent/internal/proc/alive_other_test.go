//go:build !windows

package proc

func processRunning(pid int) bool { return false }
