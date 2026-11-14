//go:build linux

package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// Starting at logon on Linux (#52): a systemd user unit, for this user
// only and with normal rights (N3). It runs `serve` in the foreground, as
// systemd wants, with the PATH of the terminal that turned it on: a user
// unit starts with a short PATH of its own, and the AI programs usually
// live in folders such as ~/.local/bin that only the shell adds.
//
// The unit starts when this user's systemd starts, which is at the first
// logon; on a machine nobody logs on to, `loginctl enable-linger` makes
// that the boot.

const unitName = "askorus-agent.service"

// commandMark opens the line that holds the command as `autostart status`
// shows it.
const commandMark = "# command: "

func unitPath() (string, error) {
	base := os.Getenv("XDG_CONFIG_HOME")
	if base == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		base = filepath.Join(home, ".config")
	}
	return filepath.Join(base, "systemd", "user", unitName), nil
}

func autostartCommand(exe string) string {
	return selfCommand(exe, "serve")
}

// unitQuote quotes a value for a unit file: inside double quotes, with
// backslash, quote and percent escaped (% starts a specifier there).
func unitQuote(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `"`, `\"`)
	s = strings.ReplaceAll(s, `%`, `%%`)
	return `"` + s + `"`
}

// unitText is the unit file. A value with a line break in it is left out
// rather than written: it could add a line of its own to the file.
func unitText(exe, path, agentDir string) string {
	exec := unitQuote(exe) + " serve"
	var b strings.Builder
	b.WriteString(commandMark + autostartCommand(exe) + "\n")
	b.WriteString("# Written by `askorus-agent autostart on`; `askorus-agent autostart off` removes it.\n")
	b.WriteString("[Unit]\nDescription=Askorus agent\n\n[Service]\nType=simple\n")
	b.WriteString("ExecStart=" + exec + "\n")
	b.WriteString("Environment=" + unitQuote(detachedEnv+"=1") + "\n")
	if path != "" && !strings.ContainsAny(path, "\r\n") {
		b.WriteString("Environment=" + unitQuote("PATH="+path) + "\n")
	}
	if agentDir != "" && !strings.ContainsAny(agentDir, "\r\n") {
		b.WriteString("Environment=" + unitQuote(agentDirEnv+"="+agentDir) + "\n")
	}
	// A test run without the Tailscale node stays one under systemd.
	if os.Getenv(noNodeEnv) == "1" {
		b.WriteString("Environment=" + unitQuote(noNodeEnv+"=1") + "\n")
	}
	b.WriteString("Restart=on-failure\nRestartSec=5\n\n[Install]\nWantedBy=default.target\n")
	return b.String()
}

// systemctlUser runs systemctl for this user's units; a variable so tests
// run nothing.
var systemctlUser = func(args ...string) error {
	path, err := exec.LookPath("systemctl")
	if err != nil {
		return errors.New("systemctl was not found: starting at logon needs systemd")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, path, append([]string{"--user"}, args...)...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("systemctl --user %s: %s", strings.Join(args, " "), unitErrLine(string(out), err))
	}
	return nil
}

func unitErrLine(out string, err error) string {
	if s := strings.TrimSpace(strings.SplitN(strings.TrimSpace(out), "\n", 2)[0]); s != "" {
		return s
	}
	return err.Error()
}

func autostartSet(exe string) error {
	if strings.ContainsAny(exe, "\r\n") {
		return errors.New("the agent's path has a line break in it")
	}
	// Asked first: without this user's systemd there is nothing to turn on.
	if err := systemctlUser("show-environment"); err != nil {
		return fmt.Errorf("this user's systemd does not answer, so the agent cannot start at logon here (%v); keep `askorus-agent start` in your shell's start file instead", err)
	}
	p, err := unitPath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(p, []byte(unitText(exe, os.Getenv("PATH"), os.Getenv(agentDirEnv))), 0o644); err != nil {
		return err
	}
	err = systemctlUser("daemon-reload")
	if err == nil {
		err = systemctlUser("enable", unitName)
	}
	if err != nil {
		// Not turned on: no file is left that says it is.
		_ = os.Remove(p)
		_ = systemctlUser("daemon-reload")
	}
	return err
}

func autostartRemove() error {
	p, err := unitPath()
	if err != nil {
		return err
	}
	if _, err := os.Stat(p); errors.Is(err, os.ErrNotExist) {
		return nil
	}
	_ = systemctlUser("disable", unitName) // the link goes with the file anyway
	if err := os.Remove(p); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	_ = systemctlUser("daemon-reload")
	return nil
}

// autostartGet returns the registered command, or "".
func autostartGet() string {
	p, err := unitPath()
	if err != nil {
		return ""
	}
	b, err := os.ReadFile(p)
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(string(b), "\n") {
		if strings.HasPrefix(line, commandMark) {
			return strings.TrimSpace(strings.TrimPrefix(line, commandMark))
		}
	}
	return ""
}

// awakeCommand is the helper that keeps the system awake while it runs.
func awakeCommand(pid int) (string, []string) {
	return "systemd-inhibit", []string{"--what=sleep:idle", "--who=askorus-agent", "--why=A session is running", "--mode=block",
		"tail", "--pid=" + strconv.Itoa(pid), "-f", "/dev/null"}
}

// sleepAfterAC is not known on Linux: every desktop keeps it elsewhere.
func sleepAfterAC() int { return -1 }
