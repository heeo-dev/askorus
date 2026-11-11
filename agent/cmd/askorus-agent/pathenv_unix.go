//go:build !windows

package main

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
)

// This user's PATH on macOS and Linux (#52) is set by the shell's start
// file. `path on` adds two lines to the file of the shell in use, a marker
// and the line that extends PATH; `path off` takes them out of every start
// file it may have written. A folder the terminal already has on its PATH
// (~/.local/bin on most Linux systems) needs nothing.

const pathMark = "# askorus-agent: PATH"

// startFiles are the start files `path on` writes for a shell, by the
// shell's program name; every other shell reads ~/.profile.
func startFiles(home, shell, goos string) []string {
	switch filepath.Base(shell) {
	case "zsh":
		return []string{filepath.Join(home, ".zshrc")}
	case "bash":
		// A terminal on macOS opens a login shell, which reads .bash_profile and not .bashrc.
		if goos == "darwin" {
			return []string{filepath.Join(home, ".bash_profile")}
		}
		return []string{filepath.Join(home, ".bashrc")}
	case "fish":
		return []string{filepath.Join(home, ".config", "fish", "conf.d", "askorus-agent.fish")}
	}
	return []string{filepath.Join(home, ".profile")}
}

// everyStartFile lists the files `path off` cleans, whichever shell was in
// use when the lines were written.
func everyStartFile(home string) []string {
	return []string{
		filepath.Join(home, ".zshrc"),
		filepath.Join(home, ".bashrc"),
		filepath.Join(home, ".bash_profile"),
		filepath.Join(home, ".profile"),
		filepath.Join(home, ".config", "fish", "conf.d", "askorus-agent.fish"),
	}
}

// shellQuote puts s inside double quotes for sh, bash, zsh and fish.
func shellQuote(s string) string {
	r := strings.NewReplacer(`\`, `\\`, `"`, `\"`, `$`, `\$`, "`", "\\`")
	return `"` + r.Replace(s) + `"`
}

// pathLines are the two lines for dir, in the start file's own language.
func pathLines(file, dir string) string {
	mark := pathMark + " (added by `askorus-agent path on`; `askorus-agent path off` removes these two lines)\n"
	if strings.HasSuffix(file, ".fish") {
		return mark + "set -gx PATH $PATH " + shellQuote(dir) + "\n"
	}
	return mark + "export PATH=\"$PATH\":" + shellQuote(dir) + "\n"
}

// withoutPathLines takes the marker lines and the line after each out of a
// start file's text.
func withoutPathLines(text string) (string, bool) {
	lines := strings.SplitAfter(text, "\n")
	var out []string
	changed := false
	for i := 0; i < len(lines); i++ {
		if strings.HasPrefix(lines[i], pathMark) {
			changed = true
			if i+1 < len(lines) && strings.Contains(lines[i+1], "PATH") {
				i++
			}
			continue
		}
		out = append(out, lines[i])
	}
	return strings.Join(out, ""), changed
}

func envPathHas(dir string) bool {
	for _, p := range filepath.SplitList(os.Getenv("PATH")) {
		if p != "" && filepath.Clean(p) == filepath.Clean(dir) {
			return true
		}
	}
	return false
}

func fileHasPathLines(file, dir string) bool {
	b, err := os.ReadFile(file)
	if err != nil {
		return false
	}
	return strings.Contains(string(b), pathLines(file, dir))
}

func userPathHas(dir string) bool {
	if envPathHas(dir) {
		return true
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return false
	}
	for _, f := range everyStartFile(home) {
		if fileHasPathLines(f, dir) {
			return true
		}
	}
	return false
}

func userPathAdd(dir string) (bool, error) {
	if strings.ContainsAny(dir, "\r\n") {
		return false, errors.New("the folder's name has a line break in it")
	}
	if userPathHas(dir) {
		return false, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return false, err
	}
	for _, f := range startFiles(home, os.Getenv("SHELL"), goos) {
		if err := os.MkdirAll(filepath.Dir(f), 0o755); err != nil {
			return false, err
		}
		old, err := os.ReadFile(f)
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return false, err
		}
		text := string(old)
		if text != "" && !strings.HasSuffix(text, "\n") {
			text += "\n"
		}
		if err := os.WriteFile(f, []byte(text+pathLines(f, dir)), 0o644); err != nil {
			return false, err
		}
	}
	return true, nil
}

func userPathRemove(dir string) (bool, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return false, err
	}
	removed := false
	for _, f := range everyStartFile(home) {
		b, err := os.ReadFile(f)
		if err != nil {
			continue
		}
		text, changed := withoutPathLines(string(b))
		if !changed {
			continue
		}
		info, err := os.Stat(f)
		if err != nil {
			return removed, err
		}
		// The fish file is the agent's own: nothing left in it, nothing to keep.
		if strings.TrimSpace(text) == "" && strings.HasSuffix(f, "askorus-agent.fish") {
			if err := os.Remove(f); err != nil {
				return removed, err
			}
		} else if err := os.WriteFile(f, []byte(text), info.Mode().Perm()); err != nil {
			return removed, err
		}
		removed = true
	}
	return removed, nil
}
