//go:build darwin

package main

import (
	"encoding/xml"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/heeo-dev/askorus/agent/internal/config"
)

// Starting at logon on macOS (#52): a launch agent of this user
// (~/Library/LaunchAgents), with normal rights (N3). launchd reads the
// folder at logon and runs `serve` in the foreground, as it wants, with the
// PATH of the terminal that turned it on: launchd's own PATH is only the
// system folders, and the AI programs usually live elsewhere (Homebrew,
// ~/.local/bin).
//
// Turning it on only writes the file, as the Run key does on Windows: the
// agent starts at the next logon, and `askorus-agent start` starts it now.

const launchLabel = "app.askorus.agent"

// commandMark opens the comment that holds the command as `autostart
// status` shows it.
const commandMark = "<!-- command: "

func plistPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, "Library", "LaunchAgents", launchLabel+".plist"), nil
}

func autostartCommand(exe string) string {
	return selfCommand(exe, "serve")
}

func xmlText(s string) string {
	var b strings.Builder
	_ = xml.EscapeText(&b, []byte(s))
	return b.String()
}

// plistText is the launch agent's file. The command in the comment has no
// "--" in it (a comment cannot hold one): the agent's flags start with one
// dash.
func plistText(exe, path, agentDir, outFile string) string {
	args := []string{exe, "serve"}
	var b strings.Builder
	b.WriteString(`<?xml version="1.0" encoding="UTF-8"?>` + "\n")
	b.WriteString(`<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">` + "\n")
	b.WriteString(commandMark + strings.ReplaceAll(autostartCommand(exe), "--", "- -") + " -->\n")
	b.WriteString("<!-- Written by `askorus-agent autostart on`; `askorus-agent autostart off` removes it. -->\n")
	b.WriteString(`<plist version="1.0">` + "\n<dict>\n")
	b.WriteString("\t<key>Label</key>\n\t<string>" + launchLabel + "</string>\n")
	b.WriteString("\t<key>ProgramArguments</key>\n\t<array>\n")
	for _, a := range args {
		b.WriteString("\t\t<string>" + xmlText(a) + "</string>\n")
	}
	b.WriteString("\t</array>\n")
	b.WriteString("\t<key>RunAtLoad</key>\n\t<true/>\n")
	b.WriteString("\t<key>EnvironmentVariables</key>\n\t<dict>\n")
	b.WriteString("\t\t<key>" + detachedEnv + "</key>\n\t\t<string>1</string>\n")
	if path != "" {
		b.WriteString("\t\t<key>PATH</key>\n\t\t<string>" + xmlText(path) + "</string>\n")
	}
	if agentDir != "" {
		b.WriteString("\t\t<key>" + agentDirEnv + "</key>\n\t\t<string>" + xmlText(agentDir) + "</string>\n")
	}
	// A test run without the Tailscale node stays one under launchd.
	if os.Getenv(noNodeEnv) == "1" {
		b.WriteString("\t\t<key>" + noNodeEnv + "</key>\n\t\t<string>1</string>\n")
	}
	b.WriteString("\t</dict>\n")
	if outFile != "" {
		b.WriteString("\t<key>StandardOutPath</key>\n\t<string>" + xmlText(outFile) + "</string>\n")
		b.WriteString("\t<key>StandardErrorPath</key>\n\t<string>" + xmlText(outFile) + "</string>\n")
	}
	b.WriteString("</dict>\n</plist>\n")
	return b.String()
}

func autostartSet(exe string) error {
	if strings.ContainsAny(exe, "\r\n") {
		return errors.New("the agent's path has a line break in it")
	}
	p, err := plistPath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	outFile := ""
	if base, err := config.CacheBase(); err == nil && os.MkdirAll(base, 0o700) == nil {
		outFile = filepath.Join(base, "background.out")
	}
	return os.WriteFile(p, []byte(plistText(exe, os.Getenv("PATH"), os.Getenv(agentDirEnv), outFile)), 0o644)
}

func autostartRemove() error {
	p, err := plistPath()
	if err != nil {
		return err
	}
	if err := os.Remove(p); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

// autostartGet returns the registered command, or "".
func autostartGet() string {
	p, err := plistPath()
	if err != nil {
		return ""
	}
	b, err := os.ReadFile(p)
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(string(b), "\n") {
		if strings.HasPrefix(line, commandMark) {
			return strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(strings.TrimPrefix(line, commandMark)), "-->"))
		}
	}
	return ""
}

// awakeCommand is the helper that keeps the system awake while it runs:
// caffeinate holds off idle sleep until the agent's process ends.
func awakeCommand(pid int) (string, []string) {
	return "caffeinate", []string{"-i", "-w", strconv.Itoa(pid)}
}

var pmsetSleepRe = regexp.MustCompile(`(?m)^\s*sleep\s+(\d+)`)

// sleepAfterAC returns how many seconds of idle put the Mac to sleep on AC
// power (0 = never), or -1 when unknown. `pmset -g custom` lists the
// settings per power source, in minutes.
func sleepAfterAC() int {
	out, err := exec.Command("/usr/bin/pmset", "-g", "custom").Output()
	if err != nil {
		return -1
	}
	return parsePmsetSleep(string(out))
}

func parsePmsetSleep(out string) int {
	i := strings.Index(out, "AC Power:")
	if i < 0 {
		return -1
	}
	ac := out[i+len("AC Power:"):]
	if j := strings.Index(ac, "Power:"); j >= 0 { // the next source's heading
		ac = ac[:j]
	}
	m := pmsetSleepRe.FindStringSubmatch(ac)
	if m == nil {
		return -1
	}
	n, err := strconv.Atoi(m[1])
	if err != nil {
		return -1
	}
	return n * 60
}
