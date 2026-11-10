package core

import (
	"encoding/json"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
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
		{"hidden ids", Filter{HiddenIDs: map[string]bool{"claude:b": true}}, []string{"claude:c", "claude:d", "claude:a"}},
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
	if !InFolder(`D:\Work\Askorus`, `d:\work`) {
		t.Error("want case-insensitive match on Windows")
	}
	if InFolder(`D:\WorkX`, `D:\Work`) {
		t.Error("sibling with same prefix must not match")
	}
}

func TestJudgeFolder(t *testing.T) {
	if got := JudgeFolder(""); got != "" {
		t.Errorf("JudgeFolder(\"\") = %q, want \"\"", got)
	}
	ask := filepath.Join(t.TempDir(), "ask")
	jf := JudgeFolder(ask + string(filepath.Separator))
	if jf != filepath.Join(ask, JudgeFolderName) {
		t.Errorf("JudgeFolder = %q", jf)
	}
	if !InFolder(jf, ask) || InFolder(ask, jf) {
		t.Errorf("the judge folder %q must be inside the ask folder %q", jf, ask)
	}
	if !InFolder(filepath.Join(jf, "eval"), jf) || InFolder(jf+"-x", jf) {
		t.Error("judge folder subfolders and siblings")
	}
	if runtime.GOOS == "windows" {
		if !InFolder(strings.ToUpper(filepath.Join(jf, "eval")), JudgeFolder(strings.ToLower(ask))) {
			t.Error("want case-insensitive judge folder on Windows")
		}
	}
}

func TestVersionAtLeast(t *testing.T) {
	cases := []struct {
		have, min string
		want      bool
	}{
		{"2.1.316", "2.1.314", true},
		{"2.1.314", "2.1.314", true},
		{"2.1.313", "2.1.314", false},
		{"0.164.0", "0.164.0", true},
		{"0.163.9", "0.164.0", false},
		{"0.165.0", "0.164.0", true},
		{"2.2.1", "2.10.0", false},
		{"2.10.0", "2.2.1", true},
		{"3.0.0", "2.10.0", true},
		{"2.1", "2.1.0", true},
		{"2.1", "2.1.1", false},
		{"2.1.0.1", "2.1.0", true},
		{"", "2.1.314", false},
		{"x", "2.1.314", false},
		{"v2", "1.0.0", false},
		{"2.1.314 (Claude Code)", "2.1.314", false},
		{"2.1.314-beta", "2.1.314", false},
		{"2..1", "2.0.0", false},
		{"2.1.", "2.0.0", false},
		{"99999999999999999999.0", "1.0", false},
		{"2.1.314", "", false},
		{"2.1.314", "v2", false},
	}
	for _, c := range cases {
		if got := VersionAtLeast(c.have, c.min); got != c.want {
			t.Errorf("VersionAtLeast(%q, %q) = %v, want %v", c.have, c.min, got, c.want)
		}
	}
}

// Since C6d (#24) no AI judges: the capability judge and the judge info
// are gone, and the judging days' error codes stay as wire values.
func TestNoJudgeCapability(t *testing.T) {
	if slices.Contains(AllCapabilities, Capability("judge")) {
		t.Errorf("AllCapabilities = %v, want no judge in it", AllCapabilities)
	}
	b, _ := json.Marshal(AdapterInfo{Agent: "claude", Capabilities: []Capability{CapAsk, CapSynthesize}})
	if strings.Contains(string(b), "judge") {
		t.Errorf("an AI's info shows judge: %s", b)
	}
	codes := map[string]string{
		ErrCodeJudgeFailed: "judge_failed", ErrCodeJudgeTimeout: "judge_timeout", ErrCodeJudgeBadOutput: "judge_bad_output",
		ErrCodeJudgeUsedTool: "judge_used_tool", ErrCodeNoJudge: "no_judge", ErrCodeNeedTwoAnswers: "need_two_answers",
		ErrCodeTooManyComparisons: "too_many_comparisons", ErrCodeCancelled: "cancelled", ErrCodeInternal: "internal",
	}
	for got, want := range codes {
		if got != want {
			t.Errorf("code %q, want %q", got, want)
		}
	}
}
