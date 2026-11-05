//go:build live

package live

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/asksemble/asksemble/agent/internal/core"
	"github.com/asksemble/asksemble/agent/internal/proc"
)

// What the live tests LT-08 and LT-09 share: P0-6 Q12's cut-down answers,
// the agent log buffer, random markers and the count of markers in the
// AIs' own data files, and OpenCode's session list. Until C6d (#24) they
// were in the judging tests LT-06a, LT-06b and LT-07, which went with the
// judging.

const q12Question = "가장 최신 아이폰 모델은 무엇이고 언제 나왔나요?"

// q12Answers are P0-6 Q12's answers (docs/judge, 2026-10-30) cut down:
// Claude's first sentence and its 발표, 출시 and 칩 items and the line on
// the plain 18; Codex's two sentences without the link; OpenCode's first
// paragraph and its 정리 sentence. The models are those of the P0-6 run.
var q12Answers = []struct{ agent, model, text string }{
	{"claude", "claude-sonnet-5-5", "2026년 10월 30일 기준으로 가장 최신 아이폰은 **iPhone 18 Pro**와 **iPhone 18 Pro Max**입니다.\n\n" +
		"- **발표:** 2026년 9월 9일\n- **출시:** 2026년 9월 18일. 다른 20개 국가와 지역에는 9월 25일부터 출시됐습니다.\n- **칩:** A20 Pro\n\n" +
		"일반 iPhone 18은 이번 가을에 나오지 않았습니다. MacRumors에 따르면 2027년 봄에 출시될 예정입니다."},
	{"codex", "gpt-6.1-terra", "2026년 10월 30일 기준 가장 최근에 출시된 아이폰은 **아이폰 듀오(iPhone Duo)**입니다. 2026년 9월 9일 공개됐고, **10월 23일** 출시됐습니다."},
	{"opencode", "opencode-go/deepseek-v4.1-flash#max", "**가장 최신 아이폰은 '아이폰 듀오(iPhone Duo)'** 입니다. 애플의 첫 폴더블 아이폰으로, 2026년 9월 9일 공개됐고 **2026년 10월 23일 출시**됐습니다(한국도 같은 날 1차 출시국으로 판매 시작).\n\n" +
		"정리하면 \"현재 시점의 최신 모델\"은 10월 23일 나온 아이폰 듀오이고, 가장 최근에 대중적으로 풀린 주력 모델은 9월 18일 출시된 아이폰 18 프로 시리즈입니다."},
}

// lockedBuffer is the agent log, written by many goroutines.
type lockedBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (l *lockedBuffer) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.Write(p)
}

func (l *lockedBuffer) String() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.String()
}

func randomHex() string {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return hex.EncodeToString(b)
}

// liveMarkers counts the markers in the AIs' own data files, by name
// only: never a login file. The content marker fails the test in Claude
// transcripts, Codex rollouts and Codex thread history (they contradict
// --no-session-persistence and --ephemeral) and is only logged for
// opencode.db and Codex's logs and state; the secret marker fails anywhere.
// A count is a lower bound: a store that compresses shows 0.
func liveMarkers(t *testing.T, claudeDir string, since time.Time, m1, m2 []byte) {
	t.Helper()
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	dataHome := os.Getenv("XDG_DATA_HOME")
	if dataHome == "" {
		dataHome = filepath.Join(home, ".local", "share")
	}
	codexHome := os.Getenv("CODEX_HOME")
	if codexHome == "" {
		codexHome = filepath.Join(home, ".codex")
	}
	named := func(dir string, re *regexp.Regexp) []string {
		entries, _ := os.ReadDir(dir)
		var out []string
		for _, en := range entries {
			if en.Type().IsRegular() && re.MatchString(en.Name()) {
				out = append(out, filepath.Join(dir, en.Name()))
			}
		}
		return out
	}
	newer := func(root string) []string {
		var out []string
		filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
			if err != nil || !d.Type().IsRegular() || !strings.HasSuffix(d.Name(), ".jsonl") {
				return nil
			}
			if info, err := d.Info(); err == nil && !info.ModTime().Before(since) {
				out = append(out, p)
			}
			return nil
		})
		return out
	}
	groups := []struct {
		what     string
		files    []string
		failOnM1 bool
	}{
		{"opencode.db", named(filepath.Join(dataHome, "opencode"), regexp.MustCompile(`^opencode\.db(-wal)?$`)), false},
		{"codex logs", named(codexHome, regexp.MustCompile(`^logs_[0-9]+\.sqlite(-wal)?$`)), false},
		{"codex state", named(codexHome, regexp.MustCompile(`^state_[0-9]+\.sqlite(-wal)?$`)), false},
		{"codex thread history", named(codexHome, regexp.MustCompile(`^thread_history_[0-9]+\.sqlite(-wal)?$`)), true},
		{"codex rollouts since the start", newer(filepath.Join(codexHome, "sessions")), true},
		{"claude transcripts since the start", newer(filepath.Join(claudeDir, "projects")), true},
	}
	for _, g := range groups {
		c1, c2, unread := 0, 0, 0
		for _, f := range g.files {
			if !markerFileOK(f) {
				t.Fatalf("refusing to read %s", filepath.Base(f))
			}
			n, err := countMarkers(f, m1, m2)
			if err != nil {
				unread++
				continue
			}
			c1, c2 = c1+n[0], c2+n[1]
		}
		t.Logf("markers in %s: %d files (%d unread), content %d, secret %d", g.what, len(g.files), unread, c1, c2)
		if c2 > 0 {
			t.Errorf("the secret marker is in %s", g.what)
		}
		if g.failOnM1 && c1 > 0 {
			t.Errorf("the content marker is in %s: a conversation of a one-off call was kept", g.what)
		}
	}
}

// markerFileOK refuses login files, whatever pattern found them.
func markerFileOK(path string) bool {
	switch strings.ToLower(filepath.Base(path)) {
	case "auth.json", ".credentials.json", "credentials.json":
		return false
	}
	return true
}

// countMarkers counts each marker in the file, reading it in pieces.
func countMarkers(path string, markers ...[]byte) ([]int, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	keep := 0
	for _, m := range markers {
		keep = max(keep, len(m)-1)
	}
	counts := make([]int, len(markers))
	buf := make([]byte, 1<<20)
	var tail []byte
	for {
		n, err := f.Read(buf)
		if n > 0 {
			window := append(tail, buf[:n]...)
			for i, m := range markers {
				// Only matches that end in the new bytes: those in the tail
				// were counted with the piece before.
				from := max(len(tail)-(len(m)-1), 0)
				counts[i] += bytes.Count(window[from:], m)
			}
			tail = append([]byte(nil), window[max(len(window)-keep, 0):]...)
		}
		if err == io.EOF {
			return counts, nil
		}
		if err != nil {
			return counts, err
		}
	}
}

// openCodeExe finds the real opencode.exe as the adapter does: on PATH, or
// next to the npm opencode.cmd, which is never run.
func openCodeExe(t *testing.T) string {
	t.Helper()
	exe, shim, err := proc.LookExe("opencode")
	if err == nil {
		return exe
	}
	if shim != "" {
		for _, rel := range []string{
			filepath.Join("node_modules", "@opencode", "cli", "bin", "opencode.exe"),
			filepath.Join("node_modules", "opencode-ai", "node_modules", "opencode-windows-x64", "bin", "opencode.exe"),
			filepath.Join("node_modules", "opencode-windows-x64", "bin", "opencode.exe"),
		} {
			if p := filepath.Join(filepath.Dir(shim), rel); proc.CheckExe(p) == nil {
				return p
			}
		}
	}
	t.Fatalf("opencode.exe not found: %v", err)
	return ""
}

// openCodeJudgeSessionsIn counts the OpenCode conversations with the
// judge title left in folder (writers take it too), with the real
// opencode's session list (read only, no model call).
func openCodeJudgeSessionsIn(t *testing.T, folder string) int {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	res, err := proc.Run(ctx, proc.Cmd{Exe: openCodeExe(t), Dir: folder, Env: map[string]string{"PWD": folder},
		Args: []string{"session", "list", "--format", "json", "--standalone"}})
	if err != nil || res.ExitCode != 0 {
		t.Fatalf("opencode session list: %v, exit %d", err, res.ExitCode)
	}
	var list []struct {
		ID, Title, Directory string
	}
	if err := json.Unmarshal(bytes.TrimSpace(res.Stdout), &list); err != nil {
		t.Fatalf("opencode session list is not JSON: %v", err)
	}
	n := 0
	for _, s := range list {
		if s.Title == "asksemble-judge" && core.InFolder(s.Directory, folder) {
			n++
		}
	}
	t.Logf("opencode session list: %d sessions, %d judge-titled conversations in the folder", len(list), n)
	return n
}

// The marker count of the live tests finds markers across its read pieces and
// counts each once; a count that never finds anything would make the
// privacy check pass for nothing.
func TestCountMarkers(t *testing.T) {
	m1, m2 := []byte("LTMARK"+randomHex()), []byte("LTSECRET"+randomHex())
	data := bytes.Repeat([]byte{'.'}, 3<<20)
	put := func(at int, m []byte) { copy(data[at:], m) }
	put(0, m1)
	put(1<<20-5, m1)  // across the first piece's end
	put(2<<20-26, m2) // ends one byte into the third piece
	put(2<<20+100, m1)
	put(len(data)-len(m2), m2)
	f := filepath.Join(t.TempDir(), "store.sqlite")
	if err := os.WriteFile(f, data, 0o600); err != nil {
		t.Fatal(err)
	}
	n, err := countMarkers(f, m1, m2)
	if err != nil || n[0] != 3 || n[1] != 2 {
		t.Errorf("counts = %v, %v; want [3 2]", n, err)
	}
	if markerFileOK(filepath.Join("x", "auth.json")) || markerFileOK(filepath.Join("x", ".credentials.json")) || !markerFileOK("opencode.db") {
		t.Error("markerFileOK")
	}
}
