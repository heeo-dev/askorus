package claude

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeTranscript adds one session that ran in cwd at the given time.
func writeTranscript(t *testing.T, projects, id, cwd, at string) {
	t.Helper()
	dir := filepath.Join(projects, "p")
	os.MkdirAll(dir, 0o700)
	line := `{"type":"user","message":{"content":"x"},"timestamp":"` + at + `","cwd":` + jsonString(cwd) + `,"sessionId":"` + id + `"}` + "\n"
	if err := os.WriteFile(filepath.Join(dir, id+".jsonl"), []byte(line), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestRecentFolders(t *testing.T) {
	root := t.TempDir()
	projects := filepath.Join(root, "projects")
	work, docs := filepath.Join(root, "work"), filepath.Join(root, "docs")
	tree := filepath.Join(work, ".claude", "worktrees", "feature")
	for _, d := range []string{work, docs, tree} {
		os.MkdirAll(d, 0o700)
	}
	writeTranscript(t, projects, id1, work, "2026-10-20T00:00:00.000Z")
	writeTranscript(t, projects, id2, strings.ToUpper(work), "2026-10-25T00:00:00.000Z") // same folder, other case
	writeTranscript(t, projects, id5, docs, "2026-10-22T00:00:00.000Z")
	writeTranscript(t, projects, id6, tree, "2026-10-26T00:00:00.000Z")
	writeTranscript(t, projects, "77777777-7777-4777-8777-777777777777", filepath.Join(root, "gone"), "2026-10-27T00:00:00.000Z")

	a := New(Options{ClaudeDir: root})
	got, err := a.RecentFolders(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("got %+v, want work and docs only", got)
	}
	if !strings.EqualFold(got[0].Path, work) || got[0].LastActive.Format("2006-01-02") != "2026-10-25" {
		t.Errorf("first = %+v, want work at its newest session", got[0])
	}
	if got[1].Path != docs {
		t.Errorf("second = %+v", got[1])
	}
}
