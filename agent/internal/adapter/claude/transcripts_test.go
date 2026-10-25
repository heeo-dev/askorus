package claude

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const (
	id1 = "11111111-1111-4111-8111-111111111111"
	id2 = "22222222-2222-4222-8222-222222222222"
	id5 = "55555555-5555-4555-8555-555555555555"
	id6 = "66666666-6666-4666-8666-666666666666"
	id7 = "77777777-7777-4777-8777-777777777777"
)

// copyProjects copies testdata/projects to a temp dir and adds an empty
// transcript, which git would not keep.
func copyProjects(t *testing.T) string {
	t.Helper()
	dst := filepath.Join(t.TempDir(), "projects")
	src := filepath.Join("testdata", "projects")
	err := filepath.WalkDir(src, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(src, p)
		if d.IsDir() {
			return os.MkdirAll(filepath.Join(dst, rel), 0o700)
		}
		b, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		return os.WriteFile(filepath.Join(dst, rel), b, 0o600)
	})
	if err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(dst, "C--work-app", "44444444-4444-4444-8444-444444444444.jsonl"), nil, 0o600)
	return dst
}

func byID(ts []Transcript) map[string]Transcript {
	m := map[string]Transcript{}
	for _, t := range ts {
		m[t.SessionID] = t
	}
	return m
}

func TestScan(t *testing.T) {
	root := copyProjects(t)
	ts, err := NewTranscripts(root).Scan()
	if err != nil {
		t.Fatal(err)
	}
	m := byID(ts)
	if len(m) != 4 {
		t.Fatalf("got %d transcripts %v, want 4 (orphaned, empty and non-uuid files skipped)", len(m), m)
	}

	a := m[id1]
	if a.Title != "Login fix" {
		t.Errorf("custom-title should win, got %q", a.Title)
	}
	if a.Cwd != `C:\work\app` {
		t.Errorf("sidechain cwd leaked: %q", a.Cwd)
	}
	if want := time.Date(2026, 10, 25, 1, 0, 7, 0, time.UTC); !a.LastActive.Equal(want) {
		t.Errorf("LastActive = %v, want %v (sidechain ignored)", a.LastActive, want)
	}
	if a.Model != "claude-opus-5-5" {
		t.Errorf("Model = %q (synthetic must be ignored)", a.Model)
	}
	if a.RemoteURL != "https://claude.ai/code/session_01AbCdEf" {
		t.Errorf("RemoteURL = %q", a.RemoteURL)
	}
	if a.LastPrompt != "fix the login bug please" {
		t.Errorf("LastPrompt = %q", a.LastPrompt)
	}

	if b := m[id2]; b.Title != "named-bg" || b.LastActive.IsZero() {
		t.Errorf("agent-name before ai-title, mtime fallback: %+v", b)
	}
	if c := m[id5]; c.Title != "Desktop title" || c.Entrypoint != "claude-desktop" {
		t.Errorf("side custom-title.json: %+v", c)
	}
	if d := m[id6]; d.RemoteURL != "" {
		t.Errorf("non-official link accepted: %q", d.RemoteURL)
	}
}

func TestScanIncremental(t *testing.T) {
	root := copyProjects(t)
	path := filepath.Join(root, "C--work-app", id2+".jsonl")
	tr := NewTranscripts(root)
	if _, err := tr.Scan(); err != nil {
		t.Fatal(err)
	}
	offset := tr.cache[path].offset

	appendTo := func(s string) {
		f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0)
		if err != nil {
			t.Fatal(err)
		}
		f.WriteString(s)
		f.Close()
		// make sure the mtime moves even on coarse clocks
		later := time.Now().Add(time.Duration(len(s)) * time.Second)
		os.Chtimes(path, later, later)
	}

	// A line still being written is not counted as read.
	appendTo(`{"type":"custom-title","customTitle":"Renamed","sessionId":"` + id2 + `"}`)
	m := scanByID(t, tr)
	if m[id2].Title != "Renamed" {
		t.Errorf("partial but valid line not applied: %q", m[id2].Title)
	}
	if tr.cache[path].offset != offset {
		t.Errorf("offset moved past a partial line")
	}

	appendTo("\n" + `{"type":"user","message":{"content":"x"},"timestamp":"2026-10-26T00:00:00.000Z","cwd":"C:\\work\\app2","sessionId":"` + id2 + `"}` + "\n")
	m = scanByID(t, tr)
	if m[id2].Title != "Renamed" || m[id2].Cwd != `C:\work\app2` {
		t.Errorf("after append: %+v", m[id2])
	}
	if tr.cache[path].offset <= offset {
		t.Errorf("offset did not advance")
	}

	// A rewritten (shorter) file is parsed from the start.
	os.WriteFile(path, []byte(`{"type":"ai-title","aiTitle":"Fresh","sessionId":"`+id2+`"}`+"\n"), 0o600)
	later := time.Now().Add(time.Hour)
	os.Chtimes(path, later, later)
	if m = scanByID(t, tr); m[id2].Title != "Fresh" || m[id2].Cwd != "" {
		t.Errorf("after rewrite: %+v", m[id2])
	}

	// Deleted files leave the cache.
	os.Remove(path)
	scanByID(t, tr)
	if _, ok := tr.cache[path]; ok {
		t.Error("deleted file still cached")
	}
}

func scanByID(t *testing.T, tr *Transcripts) map[string]Transcript {
	t.Helper()
	ts, err := tr.Scan()
	if err != nil {
		t.Fatal(err)
	}
	return byID(ts)
}

func TestScanMissingRoot(t *testing.T) {
	ts, err := NewTranscripts(filepath.Join(t.TempDir(), "nope")).Scan()
	if err != nil || len(ts) != 0 {
		t.Errorf("missing root = %v, %v", ts, err)
	}
}

func TestClip(t *testing.T) {
	if got := clip("  a\n\tb  ", 10); got != "a b" {
		t.Errorf("clip = %q", got)
	}
	if got := clip(strings.Repeat("가", 5), 3); got != "가가가…" {
		t.Errorf("clip runes = %q", got)
	}
}
