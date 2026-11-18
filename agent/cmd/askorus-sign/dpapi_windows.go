package main

import (
	"bytes"
	"errors"
	"unsafe"

	"golang.org/x/sys/windows"
)

// The release key is locked to the Windows user who made it (DPAPI): only
// that user, on this PC (or with that user's roaming profile), opens it.
// The extra bytes keep another program's DPAPI blob from opening as ours.
var dpapiEntropy = []byte("askorus-release-key-v1")

func dpapiLock(data []byte) ([]byte, error) {
	return dpapi(data, true)
}

func dpapiUnlock(data []byte) ([]byte, error) {
	return dpapi(data, false)
}

func dpapi(data []byte, lock bool) ([]byte, error) {
	if len(data) == 0 {
		return nil, errors.New("nothing to lock")
	}
	in := windows.DataBlob{Size: uint32(len(data)), Data: &data[0]}
	ent := windows.DataBlob{Size: uint32(len(dpapiEntropy)), Data: &dpapiEntropy[0]}
	var out windows.DataBlob
	var err error
	if lock {
		err = windows.CryptProtectData(&in, nil, &ent, 0, nil, windows.CRYPTPROTECT_UI_FORBIDDEN, &out)
	} else {
		err = windows.CryptUnprotectData(&in, nil, &ent, 0, nil, windows.CRYPTPROTECT_UI_FORBIDDEN, &out)
	}
	if err != nil {
		return nil, err
	}
	defer windows.LocalFree(windows.Handle(unsafe.Pointer(out.Data)))
	return bytes.Clone(unsafe.Slice(out.Data, out.Size)), nil
}
