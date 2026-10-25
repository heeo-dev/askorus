package core

import (
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

func TestParseID(t *testing.T) {
	good := []string{"claude:0b6f2a8e-1111-4222-8333-444455556666", "codex:abc"}
	for _, id := range good {
		if _, _, err := ParseID(id); err != nil {
			t.Errorf("ParseID(%q) = %v, want ok", id, err)
		}
	}
	bad := []string{"", "claude", ":abc", "claude:", "Claude:abc", "1x:abc", "cl aude:abc"}
	for _, id := range bad {
		if _, _, err := ParseID(id); err == nil {
			t.Errorf("ParseID(%q) ok, want error", id)
		}
	}
	a, n, _ := ParseID(MakeID("claude", "x:y"))
	if a != "claude" || n != "x:y" {
		t.Errorf("round trip = %q %q", a, n)
	}
}

func TestFilter(t *testing.T) {
	root := filepath.Join(string(filepath.Separator), "work")
	if runtime.GOOS == "windows" {
		root = `C:\work`
	}
	now := time.Date(2026, 10, 26, 9, 0, 0, 0, time.UTC)
	in := []Session{
		{ID: "claude:a", Agent: "claude", Title: "Alpha", Folder: filepath.Join(root, "app"), State: StateRunning, LastActive: now.Add(-3 * time.Hour)},
		{ID: "claude:b", Agent: "claude", Title: "Beta", Folder: filepath.Join(root, "app", "sub"), State: StateHistory, LastActive: now.Add(-1 * time.Hour), SearchText: "fix the login bug"},
		{ID: "claude:c", Agent: "claude", Title: "Gamma", Folder: filepath.Join(root, "secret"), State: StateStopped, LastActive: now},
		{ID: "claude:d", Agent: "claude", Title: "Delta", Folder: filepath.Join(root, "application"), State: StateTerminal, LastActive: now.Add(-2 * time.Hour)},
	}
	ids := func(ss []Session) (out []string) {
		for _, s := range ss {
			out = append(out, s.ID)
		}
		return out
	}
	cases := []struct {
		name string
		f    Filter
		want []string
	}{
		{"all, newest first", Filter{}, []string{"claude:c", "claude:b", "claude:d", "claude:a"}},
		{"folder includes subfolders only", Filter{Folder: filepath.Join(root, "app")}, []string{"claude:b", "claude:a"}},
		{"state", Filter{States: []State{StateRunning, StateStopped}}, []string{"claude:c", "claude:a"}},
		{"query title", Filter{Query: "alp"}, []string{"claude:a"}},
		{"query search text", Filter{Query: "LOGIN"}, []string{"claude:b"}},
		{"hidden", Filter{Hidden: []string{filepath.Join(root, "secret")}}, []string{"claude:b", "claude:d", "claude:a"}},
		{"agent", Filter{Agent: "codex"}, nil},
	}
	for _, c := range cases {
		got := ids(c.f.Apply(in))
		if len(got) != len(c.want) {
			t.Errorf("%s: got %v, want %v", c.name, got, c.want)
			continue
		}
		for i := range got {
			if got[i] != c.want[i] {
				t.Errorf("%s: got %v, want %v", c.name, got, c.want)
				break
			}
		}
	}
}

func TestInFolderCase(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("case-insensitive paths are Windows only")
	}
	if !InFolder(`D:\ClaudeHome\Asksemble`, `d:\claudehome`) {
		t.Error("want case-insensitive match on Windows")
	}
	if InFolder(`D:\ClaudeHomeX`, `D:\ClaudeHome`) {
		t.Error("sibling with same prefix must not match")
	}
}
