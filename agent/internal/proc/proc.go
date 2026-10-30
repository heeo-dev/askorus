// Package proc runs one AI command line for an ask: the executable itself,
// never a shell or a .cmd script (P0-7: cmd.exe drops what follows a line
// break and may read the question as commands), an argument array, a
// working folder, extra environment and the prompt on stdin. On Windows the
// process and everything it starts share a job object, so an ask that is
// ended (time limit, agent shutdown) leaves no processes behind.
//
// It knows nothing about any one AI; adapters decide the arguments.
package proc

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

const (
	maxStdout = 8 << 20
	maxStderr = 64 << 10
)

// ErrScript is returned for an executable that a shell would run.
var ErrScript = errors.New("must be an executable, not a script")

// CheckExe accepts an absolute path to an existing executable. Scripts
// (.cmd, .bat, .ps1 …) are refused; on Windows only .exe is accepted.
func CheckExe(path string) error {
	if !filepath.IsAbs(path) {
		return fmt.Errorf("%s: not an absolute path", path)
	}
	ext := strings.ToLower(filepath.Ext(path))
	switch ext {
	case ".cmd", ".bat", ".ps1", ".sh", ".vbs", ".js", ".mjs", ".cjs":
		return fmt.Errorf("%s: %w", path, ErrScript)
	}
	if runtime.GOOS == "windows" && ext != ".exe" {
		return fmt.Errorf("%s: %w", path, ErrScript)
	}
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	if info.IsDir() {
		return fmt.Errorf("%s: is a folder", path)
	}
	return nil
}

// ErrNotFound is returned by LookExe when name is not on PATH.
var ErrNotFound = errors.New("not found on PATH")

// LookExe finds the executable called name on PATH. On Windows it looks
// for name.exe only, so an npm name.cmd earlier on PATH is passed over;
// shim is that .cmd (or "") so the caller can find the exe it starts.
func LookExe(name string) (exe, shim string, err error) {
	if runtime.GOOS != "windows" {
		p, err := exec.LookPath(name)
		if err != nil {
			return "", "", ErrNotFound
		}
		p, err = filepath.Abs(p)
		return p, "", err
	}
	if p, err := exec.LookPath(name + ".exe"); err == nil {
		if p, err = filepath.Abs(p); err == nil {
			return p, "", nil
		}
	}
	if p, err := exec.LookPath(name + ".cmd"); err == nil {
		p, _ = filepath.Abs(p)
		return "", p, ErrScript
	}
	return "", "", ErrNotFound
}

// Cmd is one run.
type Cmd struct {
	Exe   string            // checked with CheckExe
	Dir   string            // absolute, existing working folder
	Args  []string          // passed as they are, no shell
	Env   map[string]string // added to (or replacing) the agent's own environment
	Stdin string            // the prompt
}

// Result is the captured output. Stdout is cut at 8 MiB, Stderr at 64 KiB.
type Result struct {
	Stdout   []byte
	Stderr   []byte
	ExitCode int
}

// ErrTimeout is returned when ctx ended before the command did.
var ErrTimeout = errors.New("ended before the answer")

// Run executes c until it ends or ctx ends. A non-zero exit code is not an
// error here: the AI's output usually says why, and the adapter reads it.
func Run(ctx context.Context, c Cmd) (Result, error) {
	if err := CheckExe(c.Exe); err != nil {
		return Result{}, err
	}
	if info, err := os.Stat(c.Dir); err != nil || !info.IsDir() || !filepath.IsAbs(c.Dir) {
		return Result{}, fmt.Errorf("working folder %q is not an absolute existing folder", c.Dir)
	}
	cmd := exec.CommandContext(ctx, c.Exe, c.Args...)
	cmd.Dir = c.Dir
	cmd.Env = mergeEnv(os.Environ(), c.Env)
	cmd.Stdin = strings.NewReader(c.Stdin)
	stdout := &capBuffer{max: maxStdout}
	stderr := &capBuffer{max: maxStderr}
	cmd.Stdout, cmd.Stderr = stdout, stderr
	cmd.WaitDelay = 2 * time.Second
	prepare(cmd)
	if err := cmd.Start(); err != nil {
		return Result{}, err
	}
	release := contain(cmd)
	err := cmd.Wait()
	release() // ends whatever the command left running
	res := Result{Stdout: stdout.Bytes(), Stderr: stderr.Bytes()}
	var exitErr *exec.ExitError
	switch {
	case ctx.Err() != nil:
		return res, ErrTimeout
	case errors.As(err, &exitErr):
		res.ExitCode = exitErr.ExitCode()
		return res, nil
	case err != nil && !errors.Is(err, exec.ErrWaitDelay):
		return res, err
	}
	return res, nil
}

// mergeEnv returns base with extra set. Windows names compare without case.
func mergeEnv(base []string, extra map[string]string) []string {
	if len(extra) == 0 {
		return base
	}
	same := func(a, b string) bool {
		if runtime.GOOS == "windows" {
			return strings.EqualFold(a, b)
		}
		return a == b
	}
	out := make([]string, 0, len(base)+len(extra))
	for _, kv := range base {
		k, _, _ := strings.Cut(kv, "=")
		replaced := false
		for name := range extra {
			if same(k, name) {
				replaced = true
				break
			}
		}
		if !replaced {
			out = append(out, kv)
		}
	}
	for k, v := range extra {
		out = append(out, k+"="+v)
	}
	return out
}

// capBuffer keeps the first max bytes and drops the rest. The buffer is a
// field, not embedded: an embedded bytes.Buffer brings its ReadFrom along,
// and io.Copy (which exec uses) would call that and skip the cap (R-18).
type capBuffer struct {
	buf bytes.Buffer
	max int
}

func (b *capBuffer) Write(p []byte) (int, error) {
	if room := b.max - b.buf.Len(); room > 0 {
		if len(p) > room {
			b.buf.Write(p[:room])
		} else {
			b.buf.Write(p)
		}
	}
	return len(p), nil
}

func (b *capBuffer) Bytes() []byte { return b.buf.Bytes() }
