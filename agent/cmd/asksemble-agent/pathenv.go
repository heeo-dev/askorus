package main

import (
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

// The user PATH gets the folder that holds asksemble-agent.exe, so a new
// terminal finds `asksemble-agent` (2026-10-28). Only this user's PATH
// (HKCU\Environment on Windows): the system PATH needs administrator.

// cleanPathEntry makes PATH entries comparable: no quotes, spaces or
// trailing separator, and environment variables expanded.
func cleanPathEntry(s string, expand func(string) string) string {
	s = strings.Trim(strings.TrimSpace(s), `"`)
	if expand != nil {
		s = expand(s)
	}
	s = strings.TrimRight(s, `\/`)
	return filepath.Clean(s)
}

func samePathEntry(a, b string, expand func(string) string) bool {
	return strings.EqualFold(cleanPathEntry(a, expand), cleanPathEntry(b, expand))
}

func splitPathList(list string) []string {
	var parts []string
	for _, p := range strings.Split(list, ";") {
		if strings.TrimSpace(p) != "" {
			parts = append(parts, p)
		}
	}
	return parts
}

func pathListHas(list, dir string, expand func(string) string) bool {
	return slices.ContainsFunc(splitPathList(list), func(p string) bool { return samePathEntry(p, dir, expand) })
}

// pathListWith appends dir unless it is already there; the other entries
// keep their order and spelling.
func pathListWith(list, dir string, expand func(string) string) (string, bool) {
	if pathListHas(list, dir, expand) {
		return list, false
	}
	return strings.Join(append(splitPathList(list), dir), ";"), true
}

// pathListWithout drops every entry that names dir.
func pathListWithout(list, dir string, expand func(string) string) (string, bool) {
	parts := splitPathList(list)
	kept := slices.DeleteFunc(slices.Clone(parts), func(p string) bool { return samePathEntry(p, dir, expand) })
	if len(kept) == len(parts) {
		return list, false
	}
	return strings.Join(kept, ";"), true
}

// exeDir is the folder that holds this executable.
func exeDir() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	if r, err := filepath.EvalSymlinks(exe); err == nil {
		exe = r
	}
	return filepath.Dir(exe), nil
}

// looksTemporary reports whether dir is a place files usually do not stay
// (Downloads or the temp folder), where a PATH entry would break later.
func looksTemporary(dir string) bool {
	var roots []string
	if home, err := os.UserHomeDir(); err == nil {
		roots = append(roots, filepath.Join(home, "Downloads"))
	}
	roots = append(roots, os.TempDir())
	for _, r := range roots {
		if rel, err := filepath.Rel(r, dir); err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return true
		}
	}
	return false
}

func pathCmd(args []string, out io.Writer) error {
	if len(args) != 1 || !slices.Contains([]string{"on", "off", "status"}, args[0]) {
		return fail("usage: asksemble-agent path on | off | status", "사용법: asksemble-agent path on | off | status")
	}
	if !controlSupported() {
		return fail("path is only supported on Windows for now", "path는 지금 Windows에서만 됩니다")
	}
	dir, err := exeDir()
	if err != nil {
		return err
	}
	switch args[0] {
	case "on":
		if looksTemporary(dir) {
			say(out, m("note: %s looks temporary; move asksemble-agent.exe to a folder you keep (for example %%LOCALAPPDATA%%\\Programs\\Asksemble) and run this again from there",
				"참고: %s는 임시 폴더로 보입니다. asksemble-agent.exe를 계속 둘 폴더(예: %%LOCALAPPDATA%%\\Programs\\Asksemble)로 옮기고 거기서 다시 실행하세요", dir))
		}
		added, err := userPathAdd(dir)
		if err != nil {
			return err
		}
		if added {
			say(out, m("added %s to this user's PATH; open a new terminal to use `asksemble-agent`",
				"%s를 이 사용자의 PATH에 넣었습니다. 새 터미널을 열면 `asksemble-agent`로 쓸 수 있습니다", dir))
		} else {
			say(out, m("%s is already on this user's PATH", "%s는 이미 이 사용자의 PATH에 있습니다", dir))
		}
	case "off":
		removed, err := userPathRemove(dir)
		if err != nil {
			return err
		}
		if removed {
			say(out, m("removed %s from this user's PATH", "%s를 이 사용자의 PATH에서 뺐습니다", dir))
		} else {
			say(out, m("%s was not on this user's PATH", "%s는 이 사용자의 PATH에 없었습니다", dir))
		}
	default:
		if userPathHas(dir) {
			say(out, m("on: %s", "켜짐: %s", dir))
		} else {
			say(out, m("off (asksemble-agent path on)", "꺼짐 (asksemble-agent path on)"))
		}
	}
	return nil
}
