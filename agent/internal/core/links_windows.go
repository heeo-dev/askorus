package core

import (
	"os"
	"syscall"
)

// linkCount is how many names the open file f has (its hard links), from
// the handle itself; false when it cannot be read.
func linkCount(f *os.File, _ os.FileInfo) (uint32, bool) {
	var d syscall.ByHandleFileInformation
	if err := syscall.GetFileInformationByHandle(syscall.Handle(f.Fd()), &d); err != nil {
		return 0, false
	}
	return d.NumberOfLinks, true
}
