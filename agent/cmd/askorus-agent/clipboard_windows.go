//go:build windows

package main

import (
	"errors"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

// The clipboard, for pair's "c + Enter" (R-95): in a classic console window
// pair keeps the mouse from selecting text (R-90), so the link it prints
// could not be copied there. The Win32 calls are made directly; no other
// program is started.

var (
	user32                     = windows.NewLazySystemDLL("user32.dll")
	kernel32                   = windows.NewLazySystemDLL("kernel32.dll")
	procOpenClipboard          = user32.NewProc("OpenClipboard")
	procCloseClipboard         = user32.NewProc("CloseClipboard")
	procEmptyClipboard         = user32.NewProc("EmptyClipboard")
	procSetClipboardData       = user32.NewProc("SetClipboardData")
	procRegisterClipboardFormW = user32.NewProc("RegisterClipboardFormatW")
	procGlobalAlloc            = kernel32.NewProc("GlobalAlloc")
	procGlobalFree             = kernel32.NewProc("GlobalFree")
	procGlobalLock             = kernel32.NewProc("GlobalLock")
	procGlobalUnlock           = kernel32.NewProc("GlobalUnlock")
	procRtlMoveMemory          = kernel32.NewProc("RtlMoveMemory")
)

const (
	cfUnicodeText = 13
	gmemMoveable  = 0x0002
)

func clipboardSupported() bool { return true }

// clipboardWrite puts text on the clipboard as Unicode text. It also marks
// the entry as one the clipboard history and the cloud clipboard leave out:
// the pairing link carries the pairing code.
func clipboardWrite(text string) error {
	u, err := windows.UTF16FromString(text)
	if err != nil {
		return err
	}
	// Another program may hold the clipboard for a moment.
	opened := false
	for i := 0; i < 10 && !opened; i++ {
		if r, _, _ := procOpenClipboard.Call(0); r != 0 {
			opened = true
		} else {
			time.Sleep(30 * time.Millisecond)
		}
	}
	if !opened {
		return errors.New("the clipboard is in use by another program")
	}
	defer procCloseClipboard.Call()
	if r, _, _ := procEmptyClipboard.Call(); r == 0 {
		return errors.New("the clipboard could not be cleared")
	}
	if err := setClipboardBytes(cfUnicodeText, unsafe.Pointer(&u[0]), uintptr(len(u))*2); err != nil {
		return err
	}
	// Best effort: without it the link still copies.
	if name, err := windows.UTF16PtrFromString("ExcludeClipboardContentFromMonitorProcessing"); err == nil {
		if f, _, _ := procRegisterClipboardFormW.Call(uintptr(unsafe.Pointer(name))); f != 0 {
			var zero uint32
			_ = setClipboardBytes(f, unsafe.Pointer(&zero), unsafe.Sizeof(zero))
		}
	}
	return nil
}

// setClipboardBytes hands a copy of size bytes at p to the open clipboard
// in the given format; the clipboard owns the memory once it took it.
func setClipboardBytes(format uintptr, p unsafe.Pointer, size uintptr) error {
	h, _, _ := procGlobalAlloc.Call(gmemMoveable, size)
	if h == 0 {
		return errors.New("no memory for the clipboard")
	}
	dst, _, _ := procGlobalLock.Call(h)
	if dst == 0 {
		procGlobalFree.Call(h)
		return errors.New("the clipboard memory could not be locked")
	}
	procRtlMoveMemory.Call(dst, uintptr(p), size)
	procGlobalUnlock.Call(h)
	if r, _, _ := procSetClipboardData.Call(format, h); r == 0 {
		procGlobalFree.Call(h)
		return errors.New("the clipboard did not take the text")
	}
	return nil
}
