//go:build !windows

package core

import (
	"os"
	"syscall"
)

// linkCount is how many names the open file has (its hard links), from the
// handle's own stat; false when it cannot be read.
func linkCount(_ *os.File, fi os.FileInfo) (uint32, bool) {
	st, ok := fi.Sys().(*syscall.Stat_t)
	if !ok {
		return 0, false
	}
	return uint32(st.Nlink), true
}
