//go:build !windows

package main

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// #52: `path on` on macOS and Linux writes two lines into the start file of
// the shell in use, and `path off` takes them out again, leaving the rest
// of the file as it was.
func TestStartFiles(t *testing.T) {
	for _, c := range []struct {
		shell, goos, want string
	}{
		{"/bin/zsh", "darwin", "/h/.zshrc"},
		{"/usr/bin/zsh", "linux", "/h/.zshrc"},
		{"/bin/bash", "linux", "/h/.bashrc"},
		{"/bin/bash", "darwin", "/h/.bash_profile"},
		{"/usr/bin/fish", "linux", "/h/.config/fish/conf.d/askorus-agent.fish"},
		{"/bin/sh", "linux", "/h/.profile"},
		{"", "darwin", "/h/.profile"},
	} {
		if got := startFiles("/h", c.shell, c.goos); !slices.Equal(got, []string{c.want}) {
			t.Errorf("startFiles(%q, %s) = %q, want %s", c.shell, c.goos, got, c.want)
		}
	}
	// Whatever `path on` may write, `path off` looks at.
	every := everyStartFile("/h")
	for _, shell := range []string{"zsh", "bash", "fish", "sh"} {
		for _, goos := range []string{"linux", "darwin"} {
			if f := startFiles("/h", shell, goos)[0]; !slices.Contains(every, f) {
				t.Errorf("path off would miss %s", f)
			}
		}
	}
}

func TestPathLinesAreQuoted(t *testing.T) {
	got := pathLines("/h/.zshrc", "/h/my \"apps\"/$bin/`x`")
	if !strings.HasPrefix(got, pathMark) || !strings.HasSuffix(got, "export PATH=\"$PATH\":\"/h/my \\\"apps\\\"/\\$bin/\\`x\\`\"\n") {
		t.Errorf("pathLines = %q", got)
	}
	if fish := pathLines("/h/.config/fish/conf.d/askorus-agent.fish", "/h/bin"); !strings.HasSuffix(fish, "set -gx PATH $PATH \"/h/bin\"\n") {
		t.Errorf("fish lines = %q", fish)
	}
}

func TestPathOnAndOff(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("SHELL", "/bin/zsh")
	t.Setenv("PATH", "/usr/bin:/bin")
	dir := filepath.Join(home, ".local", "bin")
	rc := filepath.Join(home, ".zshrc")
	mine := "# my own lines\nexport EDITOR=vi" // no line break at the end
	if err := os.WriteFile(rc, []byte(mine), 0o600); err != nil {
		t.Fatal(err)
	}

	if userPathHas(dir) {
		t.Fatal("on the PATH before path on")
	}
	added, err := userPathAdd(dir)
	if err != nil || !added {
		t.Fatalf("path on = %v, %v", added, err)
	}
	b, _ := os.ReadFile(rc)
	if want := mine + "\n" + pathLines(rc, dir); string(b) != want {
		t.Errorf(".zshrc = %q, want %q", b, want)
	}
	if info, _ := os.Stat(rc); info.Mode().Perm() != 0o600 {
		t.Errorf("the file's rights changed to %v", info.Mode().Perm())
	}
	if !userPathHas(dir) {
		t.Error("not on the PATH after path on")
	}
	// A second time changes nothing.
	if added, err := userPathAdd(dir); err != nil || added {
		t.Errorf("path on again = %v, %v", added, err)
	}
	if again, _ := os.ReadFile(rc); string(again) != string(b) {
		t.Error("path on twice wrote twice")
	}

	removed, err := userPathRemove(dir)
	if err != nil || !removed {
		t.Fatalf("path off = %v, %v", removed, err)
	}
	if after, _ := os.ReadFile(rc); string(after) != mine+"\n" {
		t.Errorf(".zshrc after path off = %q, want the person's own lines", after)
	}
	if userPathHas(dir) {
		t.Error("still on the PATH after path off")
	}
	if removed, err := userPathRemove(dir); err != nil || removed {
		t.Errorf("path off again = %v, %v", removed, err)
	}
}

func TestPathAlreadyThereNeedsNothing(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("SHELL", "/bin/bash")
	dir := filepath.Join(home, ".local", "bin")
	// Most Linux systems put ~/.local/bin on the PATH themselves.
	t.Setenv("PATH", "/usr/bin:"+dir+"/")
	if !userPathHas(dir) {
		t.Fatal("a folder on the terminal's PATH does not count")
	}
	if added, err := userPathAdd(dir); err != nil || added {
		t.Errorf("path on = %v, %v", added, err)
	}
	for _, f := range everyStartFile(home) {
		if _, err := os.Stat(f); !errors.Is(err, os.ErrNotExist) {
			t.Errorf("wrote %s although the folder was on the PATH", f)
		}
	}
	if _, err := userPathAdd("/tmp/a\nb"); err == nil {
		t.Error("a folder name with a line break was taken")
	}
}

func TestPathFishFileIsRemovedWhenEmpty(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("SHELL", "/usr/bin/fish")
	t.Setenv("PATH", "/usr/bin")
	dir := filepath.Join(home, "apps")
	if added, err := userPathAdd(dir); err != nil || !added {
		t.Fatalf("path on = %v, %v", added, err)
	}
	fish := filepath.Join(home, ".config", "fish", "conf.d", "askorus-agent.fish")
	if b, err := os.ReadFile(fish); err != nil || !strings.Contains(string(b), "set -gx PATH $PATH") {
		t.Fatalf("fish file = %q, %v", b, err)
	}
	if removed, err := userPathRemove(dir); err != nil || !removed {
		t.Fatalf("path off = %v, %v", removed, err)
	}
	if _, err := os.Stat(fish); !errors.Is(err, os.ErrNotExist) {
		t.Error("the agent's own fish file stayed, empty")
	}
}

func TestPickCopyTool(t *testing.T) {
	has := func(names ...string) func(string) (string, error) {
		return func(n string) (string, error) {
			if slices.Contains(names, n) {
				return "/usr/bin/" + n, nil
			}
			return "", errors.New("not found")
		}
	}
	env := func(m map[string]string) func(string) string { return func(k string) string { return m[k] } }
	for _, c := range []struct {
		name, goos string
		env        map[string]string
		look       func(string) (string, error)
		want       string
		ok         bool
	}{
		{"mac", "darwin", nil, has("pbcopy"), "/usr/bin/pbcopy", true},
		{"wayland first", "linux", map[string]string{"WAYLAND_DISPLAY": "wayland-0", "DISPLAY": ":0"}, has("wl-copy", "xclip"), "/usr/bin/wl-copy", true},
		{"x11", "linux", map[string]string{"DISPLAY": ":0"}, has("wl-copy", "xclip", "xsel"), "/usr/bin/xclip", true},
		{"x11, only xsel", "linux", map[string]string{"DISPLAY": ":0"}, has("xsel"), "/usr/bin/xsel", true},
		{"no display", "linux", nil, has("wl-copy", "xclip", "xsel"), "", false},
		{"nothing installed", "linux", map[string]string{"DISPLAY": ":0"}, has(), "", false},
	} {
		got, _, ok := pickCopyTool(c.goos, env(c.env), c.look)
		if got != c.want || ok != c.ok {
			t.Errorf("%s: %q, %v, want %q, %v", c.name, got, ok, c.want, c.ok)
		}
	}
}
