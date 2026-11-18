package main

import (
	"strings"
	"syscall"

	"golang.org/x/sys/windows"
)

// finalPath is where an existing file or folder really is, junctions and
// links followed. filepath.EvalSymlinks leaves junctions alone since Go
// 1.23, so Windows is asked (R-139).
func finalPath(p string) (string, error) {
	name, err := windows.UTF16PtrFromString(p)
	if err != nil {
		return "", err
	}
	h, err := windows.CreateFile(name, 0, windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE, nil, windows.OPEN_EXISTING, windows.FILE_FLAG_BACKUP_SEMANTICS, 0)
	if err != nil {
		return "", err
	}
	defer windows.CloseHandle(h)
	buf := make([]uint16, windows.MAX_LONG_PATH)
	const volumeNameDOS = 0x0 // VOLUME_NAME_DOS | FILE_NAME_NORMALIZED: a drive-letter path
	n, err := windows.GetFinalPathNameByHandle(h, &buf[0], uint32(len(buf)), volumeNameDOS)
	if err != nil {
		return "", err
	}
	if int(n) > len(buf) {
		return "", syscall.ENAMETOOLONG
	}
	s := windows.UTF16ToString(buf[:n])
	if rest, ok := strings.CutPrefix(s, `\\?\UNC\`); ok {
		return `\\` + rest, nil
	}
	return strings.TrimPrefix(s, `\\?\`), nil
}
