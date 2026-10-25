package claude

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// Runner runs the claude executable directly, never through a shell, and
// only with arguments that pass checkArgs (N2, principle 7).
type Runner struct {
	exe     string
	timeout time.Duration
}

// Result is the captured output of one run.
type Result struct {
	Stdout   []byte
	Stderr   []byte
	ExitCode int
}

var (
	ErrNotFound    = errors.New("claude executable not found")
	ErrScript      = errors.New("claude must be an executable, not a shell script")
	ErrArgRejected = errors.New("argument not allowed")
)

const (
	defaultTimeout = 15 * time.Second
	maxStdout      = 8 << 20
	maxStderr      = 64 << 10
)

// NewRunner finds claude: exe if given, otherwise "claude" on PATH.
// Scripts such as claude.cmd are refused because Windows would run them
// through cmd.exe.
func NewRunner(exe string) (*Runner, error) {
	if exe == "" {
		p, err := exec.LookPath("claude")
		if err != nil {
			return nil, ErrNotFound
		}
		exe = p
	}
	exe, err := filepath.Abs(exe)
	if err != nil {
		return nil, err
	}
	switch strings.ToLower(filepath.Ext(exe)) {
	case ".cmd", ".bat", ".ps1", ".sh", ".vbs", ".js":
		return nil, ErrScript
	}
	return &Runner{exe: exe, timeout: defaultTimeout}, nil
}

// Path is the resolved executable.
func (r *Runner) Path() string { return r.exe }

// Subcommands and flags the agent may pass. Everything else is refused, so
// a permission-relaxing flag can only get in by being added here.
var (
	allowedSubcommands = map[string]bool{"agents": true}
	allowedFlags       = map[string]bool{"--version": true, "--json": true, "--all": true}
	flagsWithValue     = map[string]bool{}
)

// deniedFlags are refused even if someone adds them to allowedFlags.
var deniedFlags = []string{
	"--dangerously", "--allow-dangerously", "--permission-mode", "--permission-prompt-tool",
	"--allowedtools", "--allowed-tools", "--disallowedtools", "--disallowed-tools",
	"--add-dir", "--settings", "--mcp-config", "--strict-mcp-config", "--plugin-dir",
}

func checkArgs(args []string) error {
	for i := 0; i < len(args); i++ {
		a := args[i]
		if strings.ContainsAny(a, "\x00\r\n") {
			return fmt.Errorf("%w: control character", ErrArgRejected)
		}
		lower := strings.ToLower(a)
		for _, d := range deniedFlags {
			if strings.HasPrefix(lower, d) {
				return fmt.Errorf("%w: %s", ErrArgRejected, a)
			}
		}
		switch {
		case strings.HasPrefix(a, "-"):
			if !allowedFlags[a] {
				return fmt.Errorf("%w: %s", ErrArgRejected, a)
			}
			if flagsWithValue[a] {
				i++
				if i >= len(args) || strings.HasPrefix(args[i], "-") {
					return fmt.Errorf("%w: %s needs a value", ErrArgRejected, a)
				}
			}
		case i == 0 && allowedSubcommands[a]:
		default:
			return fmt.Errorf("%w: %q", ErrArgRejected, a)
		}
	}
	return nil
}

// Run executes claude with args in dir (empty: current directory).
func (r *Runner) Run(ctx context.Context, dir string, args ...string) (Result, error) {
	if err := checkArgs(args); err != nil {
		return Result{}, err
	}
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, r.exe, args...)
	cmd.Dir = dir
	stdout := &capBuffer{max: maxStdout}
	stderr := &capBuffer{max: maxStderr}
	cmd.Stdout, cmd.Stderr = stdout, stderr
	cmd.WaitDelay = 2 * time.Second
	hideWindow(cmd)
	err := cmd.Run()
	res := Result{Stdout: stdout.Bytes(), Stderr: stderr.Bytes()}
	var exitErr *exec.ExitError
	switch {
	case ctx.Err() == context.DeadlineExceeded:
		return res, fmt.Errorf("claude %s: timed out after %s", strings.Join(args, " "), r.timeout)
	case errors.As(err, &exitErr):
		res.ExitCode = exitErr.ExitCode()
		return res, fmt.Errorf("claude %s: exit code %d", strings.Join(args, " "), res.ExitCode)
	case err != nil:
		return res, err
	}
	return res, nil
}

var versionRe = regexp.MustCompile(`\d+\.\d+\.\d+`)

// Version runs claude --version and returns "2.1.310".
func (r *Runner) Version(ctx context.Context) (string, error) {
	res, err := r.Run(ctx, "", "--version")
	if err != nil {
		return "", err
	}
	v := versionRe.Find(res.Stdout)
	if v == nil {
		return "", errors.New("claude --version: no version number")
	}
	return string(v), nil
}

// Agents runs claude agents --json --all. This never starts the Claude
// background service.
func (r *Runner) Agents(ctx context.Context) ([]agentEntry, error) {
	res, err := r.Run(ctx, "", "agents", "--json", "--all")
	if err != nil {
		return nil, err
	}
	return parseAgents(res.Stdout)
}

// capBuffer keeps the first max bytes and drops the rest.
type capBuffer struct {
	bytes.Buffer
	max int
}

func (b *capBuffer) Write(p []byte) (int, error) {
	if room := b.max - b.Len(); room > 0 {
		if len(p) > room {
			b.Buffer.Write(p[:room])
		} else {
			b.Buffer.Write(p)
		}
	}
	return len(p), nil
}
