//go:build windows

package main

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"syscall"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
)

// The background agent is controlled through a named event in this logon
// session, one per agent port: `shutdown` sets it and `serve` stops
// cleanly when it fires.
const stopEventPrefix = `Local\AsksembleAgentStop-`

// listenStop returns a channel closed when someone asks the agent to stop.
func listenStop(port int) <-chan struct{} {
	ch := make(chan struct{})
	name, _ := windows.UTF16PtrFromString(stopEventPrefix + strconv.Itoa(port))
	h, err := windows.CreateEvent(nil, 1, 0, name) // manual reset, not set
	if err != nil {
		return ch
	}
	windows.ResetEvent(h) // a stale signal from an earlier stop must not end this run
	go func() {
		windows.WaitForSingleObject(h, windows.INFINITE)
		close(ch)
	}()
	return ch
}

// signalStop asks a running agent to stop.
func signalStop(port int) error {
	name, _ := windows.UTF16PtrFromString(stopEventPrefix + strconv.Itoa(port))
	h, err := windows.OpenEvent(windows.EVENT_MODIFY_STATE, false, name)
	if err != nil {
		return errors.New("no running agent")
	}
	defer windows.CloseHandle(h)
	return windows.SetEvent(h)
}

const (
	createNoWindow        = 0x08000000
	createNewProcessGroup = 0x00000200
)

// startDetached starts exe with args without a window, outliving this
// process. Its console output goes to logFile.
func startDetached(exe string, args []string, dir, logFile string) (int, error) {
	out, err := os.OpenFile(logFile, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return 0, err
	}
	defer out.Close()
	cmd := exec.Command(exe, args...)
	cmd.Dir = dir
	cmd.Stdout, cmd.Stderr = out, out
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: createNoWindow | createNewProcessGroup}
	if err := cmd.Start(); err != nil {
		return 0, err
	}
	pid := cmd.Process.Pid
	cmd.Process.Release()
	return pid, nil
}

// Autostart at logon for this user only, with normal rights: the Run key
// under HKCU needs no administrator and starts nothing elevated (N3).
const (
	runKey       = `Software\Microsoft\Windows\CurrentVersion\Run`
	runValueName = "Asksemble Agent"
)

func autostartCommand(exe string) string { return `"` + exe + `" start` }

func autostartSet(exe string) error {
	k, _, err := registry.CreateKey(registry.CURRENT_USER, runKey, registry.SET_VALUE)
	if err != nil {
		return err
	}
	defer k.Close()
	return k.SetStringValue(runValueName, autostartCommand(exe))
}

func autostartRemove() error {
	k, err := registry.OpenKey(registry.CURRENT_USER, runKey, registry.SET_VALUE)
	if err != nil {
		return err
	}
	defer k.Close()
	if err := k.DeleteValue(runValueName); err != nil && !errors.Is(err, registry.ErrNotExist) {
		return err
	}
	return nil
}

// autostartGet returns the registered command, or "".
func autostartGet() string {
	k, err := registry.OpenKey(registry.CURRENT_USER, runKey, registry.QUERY_VALUE)
	if err != nil {
		return ""
	}
	defer k.Close()
	v, _, err := k.GetStringValue(runValueName)
	if err != nil {
		return ""
	}
	return v
}

// keepAwake asks Windows not to sleep while on is true (design: 절전).
func keepAwake(on bool) {
	const esContinuous, esSystemRequired = 0x80000000, 0x00000001
	flags := uint32(esContinuous)
	if on {
		flags |= esSystemRequired
	}
	windows.NewLazySystemDLL("kernel32.dll").NewProc("SetThreadExecutionState").Call(uintptr(flags))
}

var hexRe = regexp.MustCompile(`0x[0-9a-fA-F]+`)

// sleepAfterAC returns how many seconds of idle put the PC to sleep on AC
// power (0 = never), or -1 when unknown. powercfg prints the range first
// and the current AC and DC values last, in any display language.
func sleepAfterAC() int {
	out, err := exec.Command(filepath.Join(os.Getenv("SystemRoot"), "System32", "powercfg.exe"), "/q", "SCHEME_CURRENT", "SUB_SLEEP", "STANDBYIDLE").Output()
	if err != nil {
		return -1
	}
	vals := hexRe.FindAllString(string(out), -1)
	if len(vals) < 2 {
		return -1
	}
	n, err := strconv.ParseInt(vals[len(vals)-2][2:], 16, 64)
	if err != nil {
		return -1
	}
	return int(n)
}

func controlSupported() bool { return true }
