//go:build linux || darwin

package claude

import (
	"errors"
	"io"
	"os"
	"os/exec"
	"strconv"
	"syscall"

	"golang.org/x/sys/unix"
)

// ptyProc is a process running on a pseudo terminal (#52), the macOS and
// Linux counterpart of the Windows pseudo console: the folder server draws
// an interactive screen and reads Ctrl+C as a key, so it needs a real
// terminal to be stopped cleanly. Writing 0x03 to the terminal is pressing
// Ctrl+C: a program that reads keys itself gets the key, any other gets
// the interrupt signal from the terminal.
type ptyProc struct {
	cmd    *exec.Cmd
	master *os.File
}

// startPTY starts exe with args in dir on a new pseudo terminal, as the
// leader of a session of its own with that terminal as its own.
func startPTY(exe, dir string, args []string) (serverProcess, error) {
	master, slaveName, err := openPTY()
	if err != nil {
		return nil, err
	}
	slave, err := os.OpenFile(slaveName, os.O_RDWR|syscall.O_NOCTTY, 0)
	if err != nil {
		master.Close()
		return nil, err
	}
	defer slave.Close()
	// The size the Windows side gives its pseudo console.
	_ = unix.IoctlSetWinsize(int(master.Fd()), unix.TIOCSWINSZ, &unix.Winsize{Row: 40, Col: 120})

	cmd := exec.Command(exe, args...)
	cmd.Dir = dir
	cmd.Stdin, cmd.Stdout, cmd.Stderr = slave, slave, slave
	cmd.Env = os.Environ()
	// Started at logon the agent has no terminal type of its own.
	if os.Getenv("TERM") == "" {
		cmd.Env = append(cmd.Env, "TERM=xterm-256color")
	}
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true, Setctty: true, Ctty: 0}
	if err := cmd.Start(); err != nil {
		master.Close()
		return nil, err
	}
	return &ptyProc{cmd: cmd, master: master}, nil
}

func (p *ptyProc) PID() int          { return p.cmd.Process.Pid }
func (p *ptyProc) Output() io.Reader { return ptyReader{p.master} }
func (p *ptyProc) Interrupt() error  { _, err := p.master.Write([]byte{0x03}); return err }

// Kill ends the whole session: the server and what it started.
func (p *ptyProc) Kill() error {
	if err := syscall.Kill(-p.cmd.Process.Pid, syscall.SIGKILL); err != nil {
		return p.cmd.Process.Kill()
	}
	return nil
}

// Wait blocks until the process ends and returns an error for a non-zero
// exit code or a signal.
func (p *ptyProc) Wait() error {
	err := p.cmd.Wait()
	var exit *exec.ExitError
	if errors.As(err, &exit) && exit.ExitCode() > 0 {
		return errors.New("exit code " + strconv.Itoa(exit.ExitCode()))
	}
	return err
}

// Close releases the terminal; the output reader then sees EOF.
func (p *ptyProc) Close() { p.master.Close() }

func ptySupported() bool { return true }

// ptyReader reads the terminal's output. Once the program has closed its
// side Linux answers a read with EIO: that is the end, not a failure.
type ptyReader struct{ f *os.File }

func (r ptyReader) Read(b []byte) (int, error) {
	n, err := r.f.Read(b)
	if err != nil && (errors.Is(err, syscall.EIO) || errors.Is(err, os.ErrClosed)) {
		return n, io.EOF
	}
	return n, err
}
