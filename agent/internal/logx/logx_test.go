package logx

import (
	"bytes"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// R-87 (security review 11-08): the log file rolls over instead of growing
// without end, and the lines go on in a new file.
func TestFileRollsOver(t *testing.T) {
	path := filepath.Join(t.TempDir(), "agent.log")
	f, err := OpenFile(path, 100)
	if err != nil {
		t.Fatal(err)
	}
	line := strings.Repeat("x", 39) + "\n" // 40 bytes
	for i := 0; i < 12; i++ {
		if _, err := f.Write([]byte(line)); err != nil {
			t.Fatalf("write %d: %v", i, err)
		}
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	cur, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	old, err := os.ReadFile(path + ".1")
	if err != nil {
		t.Fatalf("no rolled-over file: %v", err)
	}
	if len(cur) > 100 || len(old) > 100 || len(cur) == 0 {
		t.Errorf("sizes %d and %d, want each at most 100 and a current file", len(cur), len(old))
	}
	if len(cur)%len(line) != 0 || len(old)%len(line) != 0 {
		t.Error("a line was split between the files")
	}
	if _, err := os.Stat(path + ".2"); err == nil {
		t.Error("more than one old file kept")
	}
	// Reopening goes on from the size on disk.
	f, err = OpenFile(path, 100)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	for i := 0; i < 3; i++ {
		f.Write([]byte(line))
	}
	if fi, _ := os.Stat(path); fi.Size() > 100 {
		t.Errorf("reopened file grew to %d", fi.Size())
	}
}

// Codex cross-review 11-08: when the file cannot be moved aside (another
// program holds it on Windows), it starts over in place instead of growing.
func TestFileStaysBoundedWhenItCannotMove(t *testing.T) {
	path := filepath.Join(t.TempDir(), "agent.log")
	f, err := OpenFile(path, 100)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	f.rename = func(string, string) error { return os.ErrPermission }
	line := strings.Repeat("x", 39) + "\n"
	for i := 0; i < 30; i++ {
		if _, err := f.Write([]byte(line)); err != nil {
			t.Fatalf("write %d: %v", i, err)
		}
	}
	if fi, _ := os.Stat(path); fi.Size() > 100 || fi.Size() == 0 {
		t.Errorf("size %d, want at most 100", fi.Size())
	}
}

// Codex cross-review 11-08: a reopen that fails once does not stop the log
// for good; the next write opens the file again.
func TestFileReopensAfterAFailure(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "logs")
	os.MkdirAll(dir, 0o700)
	path := filepath.Join(dir, "agent.log")
	f, err := OpenFile(path, 100)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	f.rename = func(string, string) error { return os.RemoveAll(dir) } // the folder goes away
	line := strings.Repeat("x", 39) + "\n"
	f.Write([]byte(line))
	f.Write([]byte(line))
	if _, err := f.Write([]byte(line)); err == nil {
		t.Fatal("write into a missing folder did not fail")
	}
	os.MkdirAll(dir, 0o700)
	if _, err := f.Write([]byte("back\n")); err != nil {
		t.Fatalf("write after the folder came back: %v", err)
	}
	if b, _ := os.ReadFile(path); string(b) != "back\n" {
		t.Errorf("file = %q", b)
	}
	f.Close()
	if _, err := f.Write([]byte("late\n")); err == nil {
		t.Error("write after Close did not fail")
	}
}

func TestRollOver(t *testing.T) {
	dir := t.TempDir()
	small, big := filepath.Join(dir, "small.out"), filepath.Join(dir, "big.out")
	os.WriteFile(small, []byte("ok"), 0o600)
	os.WriteFile(big, bytes.Repeat([]byte("y"), 200), 0o600)
	if err := RollOver(small, 100); err != nil {
		t.Fatal(err)
	}
	if err := RollOver(big, 100); err != nil {
		t.Fatal(err)
	}
	if err := RollOver(filepath.Join(dir, "missing.out"), 100); err != nil {
		t.Errorf("missing file: %v", err)
	}
	if _, err := os.Stat(small); err != nil {
		t.Error("small file moved")
	}
	if _, err := os.Stat(big); err == nil {
		t.Error("big file still in place")
	}
	if fi, err := os.Stat(big + ".1"); err != nil || fi.Size() != 200 {
		t.Errorf("big file not rolled over: %v", err)
	}
}

func TestRedaction(t *testing.T) {
	var buf bytes.Buffer
	l := New(&buf, slog.LevelInfo)
	l.Info("request",
		"device", "phone",
		"route", "GET /v1/info",
		"Authorization", "Bearer ask_abcdef",
		"token", "ask_abcdef",
		"lastPrompt", "please read my diary",
		slog.Group("req", "pair_code", "123456"),
	)
	out := buf.String()
	for _, leak := range []string{"ask_abcdef", "diary", "123456"} {
		if strings.Contains(out, leak) {
			t.Errorf("log leaks %q: %s", leak, out)
		}
	}
	for _, keep := range []string{`"device":"phone"`, `"route":"GET /v1/info"`} {
		if !strings.Contains(out, keep) {
			t.Errorf("log lost %s: %s", keep, out)
		}
	}
}

func TestRedactsComparisonContent(t *testing.T) {
	var buf bytes.Buffer
	l := New(&buf, slog.LevelInfo)
	l.Info("comparison",
		"answers", 3, "sentences", 4, "apiKey", "x-key-1", "api_key", "x-key-2", "evidenceCount", 2,
		"question", "q-leak", "earlier", "e-leak", "claim", "c-leak", "reason", "r-leak", "answerText", "a-leak",
	)
	out := buf.String()
	for _, key := range []string{"answers", "sentences", "apiKey", "api_key", "evidenceCount", "question", "earlier", "claim", "reason", "answerText"} {
		if !strings.Contains(out, `"`+key+`":"`+Redacted+`"`) {
			t.Errorf("%s not redacted: %s", key, out)
		}
	}
	for _, leak := range []string{"x-key-1", "x-key-2", "q-leak", "e-leak", "c-leak", "r-leak", "a-leak"} {
		if strings.Contains(out, leak) {
			t.Errorf("log leaks %q: %s", leak, out)
		}
	}
}

func TestKeepsCountKeys(t *testing.T) {
	// The keys comparisons log, and those the agent logged before P2-3.
	keys := []string{
		"ais", "requests", "judges", "noJudge", "differ", "check", "agree", "onlyOne", "notCompared", "notJudged",
		"attempt", "ms", "code", "status", "judge", "source", "comparison",
		"mode", "agent", "agents", "tier", "followUp", "kind", "detail", "err", "folder",
	}
	var buf bytes.Buffer
	l := New(&buf, slog.LevelInfo)
	var args []any
	for _, k := range keys {
		if IsSensitive(k) {
			t.Errorf("%s is blocked", k)
		}
		args = append(args, k, "v")
	}
	l.Info("counts", args...)
	for _, k := range keys {
		if !strings.Contains(buf.String(), `"`+k+`":"v"`) {
			t.Errorf("%s lost: %s", k, buf.String())
		}
	}
	if strings.Contains(buf.String(), Redacted) {
		t.Errorf("a count key was redacted: %s", buf.String())
	}
}
