//go:build !windows

package main

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"syscall"

	"github.com/heeo-dev/askorus/agent/internal/config"
	"github.com/heeo-dev/askorus/agent/internal/logx"
)

// The background agent on macOS and Linux (#52). What Windows does with a
// named event, these systems do with a lock file per agent port: `serve`
// holds a lock on it for as long as it runs and writes its process id
// inside; `shutdown` reads the id and sends SIGTERM, which `serve` takes as
// a request to stop cleanly. The lock is the proof that the id belongs to
// a running agent of this user: a file left behind by a crash has no lock.

// held keeps the lock files of this process open: closing one would give
// the lock up.
var (
	heldMu sync.Mutex
	held   = map[int]*os.File{}
)

// runDir is where the lock files live: this user's cache folder for the
// agent (the one that also holds the ask folder).
func runDir() (string, error) {
	base, err := config.CacheBase()
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(base, 0o700); err != nil {
		return "", err
	}
	return base, nil
}

func lockPath(port int) (string, error) {
	dir, err := runDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "agent-"+strconv.Itoa(port)+".lock"), nil
}

// listenStop returns a channel closed when someone asks the agent to stop
// (`askorus-agent shutdown`, or the system ending the session). It also
// takes the port's lock, so the other commands can tell this agent runs.
func listenStop(port int) <-chan struct{} {
	ch := make(chan struct{})
	if p, err := lockPath(port); err == nil {
		if f, err := os.OpenFile(p, os.O_RDWR|os.O_CREATE, 0o600); err == nil {
			if syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB) == nil {
				_ = f.Truncate(0)
				_, _ = f.WriteAt([]byte(strconv.Itoa(os.Getpid())+"\n"), 0)
				heldMu.Lock()
				held[port] = f
				heldMu.Unlock()
			} else {
				f.Close() // another agent of this user holds it
			}
		}
	}
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGTERM, syscall.SIGHUP)
	go func() {
		<-sig
		close(ch)
	}()
	return ch
}

// lockedPID returns the process id of the agent that holds the port's
// lock, or 0 when no agent of this user serves the port.
func lockedPID(port int) int {
	p, err := lockPath(port)
	if err != nil {
		return 0
	}
	f, err := os.Open(p)
	if err != nil {
		return 0
	}
	defer f.Close()
	if syscall.Flock(int(f.Fd()), syscall.LOCK_SH|syscall.LOCK_NB) == nil {
		// Nobody holds it: a file left behind.
		_ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
		return 0
	}
	b := make([]byte, 32)
	n, _ := f.ReadAt(b, 0)
	pid, _ := strconv.Atoi(strings.TrimSpace(string(b[:n])))
	if pid <= 0 {
		return 0
	}
	return pid
}

// agentOnPort reports whether an agent of this user serves the port. A
// port that answers without the lock belongs to another program (#37:
// setup must not pass that one to the tailnet).
func agentOnPort(port int) bool { return lockedPID(port) > 0 }

// signalStop asks a running agent to stop.
func signalStop(port int) error {
	pid := lockedPID(port)
	if pid <= 0 {
		return errors.New("no running agent")
	}
	return syscall.Kill(pid, syscall.SIGTERM)
}

// startDetached starts exe with args in a session of its own, outliving
// this process and the terminal. Its console output goes to logFile, which
// first rolls over when it is past consoleMax (R-87), and it is told it
// runs detached.
func startDetached(exe string, args []string, dir, logFile string) (int, error) {
	_ = logx.RollOver(logFile, consoleMax)
	out, err := os.OpenFile(logFile, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return 0, err
	}
	defer out.Close()
	cmd := exec.Command(exe, args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), detachedEnv+"=1")
	cmd.Stdout, cmd.Stderr = out, out
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		return 0, err
	}
	pid := cmd.Process.Pid
	cmd.Process.Release()
	return pid, nil
}

func controlSupported() bool { return true }

// processRunning reports whether pid is still running.
func processRunning(pid int) bool {
	if pid <= 0 {
		return false
	}
	err := syscall.Kill(pid, 0)
	return err == nil || errors.Is(err, syscall.EPERM)
}

func holdConsole() (restore func()) { return func() {} } // QuickEdit is a Windows console thing (#37)

// keepAwake asks the system not to sleep while on is true (design: 절전),
// through a helper program that holds the request for as long as it runs:
// caffeinate on macOS, systemd-inhibit on Linux. Where neither is there it
// does nothing.
var (
	awakeMu  sync.Mutex
	awakeCmd *exec.Cmd
)

func keepAwake(on bool) {
	awakeMu.Lock()
	defer awakeMu.Unlock()
	if !on {
		if awakeCmd != nil {
			_ = awakeCmd.Process.Kill()
			_ = awakeCmd.Wait()
			awakeCmd = nil
		}
		return
	}
	if awakeCmd != nil {
		return
	}
	name, args := awakeCommand(os.Getpid())
	if name == "" {
		return
	}
	path, err := exec.LookPath(name)
	if err != nil {
		return
	}
	cmd := exec.Command(path, args...)
	// Its own process group: the terminal's Ctrl+C is for the agent, which ends the helper itself.
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if cmd.Start() == nil {
		awakeCmd = cmd
	}
}

// agentDirEnv names the agent's folder when it is not the usual one. An
// agent started at logon gets none of the terminal's variables, so the
// start file carries this one along, with the PATH.
const agentDirEnv = "ASKORUS_AGENT_DIR"

// goos is the system this agent was built for; the helpers take it as an
// argument so the tests can ask about the other one.
const goos = runtime.GOOS

// exeName is how the agent's program file is called in what the agent
// says: with .exe on Windows only.
const exeName = "askorus-agent"

// installFolderHint is the folder the install script uses, for the hints.
const installFolderHint = "~/.local/bin"

// selfCommand quotes a program path for a command line shown to a person
// or written into a start file.
func selfCommand(exe string, args ...string) string {
	return strings.Join(append([]string{fmt.Sprintf("%q", exe)}, args...), " ")
}
