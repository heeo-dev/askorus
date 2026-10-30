//go:build windows

package proc

import "golang.org/x/sys/windows"

// processRunning reports whether pid is still running, waiting up to 2 s
// for it to end.
func processRunning(pid int) bool {
	h, err := windows.OpenProcess(windows.SYNCHRONIZE, false, uint32(pid))
	if err != nil {
		return false // gone
	}
	defer windows.CloseHandle(h)
	ev, _ := windows.WaitForSingleObject(h, 2000)
	return ev != windows.WAIT_OBJECT_0
}
