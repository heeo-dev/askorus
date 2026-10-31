package main

import (
	"bytes"
	"net/url"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/asksemble/asksemble/agent/internal/config"
	"github.com/asksemble/asksemble/agent/internal/core"
)

func TestPairURL(t *testing.T) {
	link := PairURL("pc.example.ts.net", 8800, "ABCDEFGHIJKLMNOPQRSTUVWXYZ")
	u, err := url.Parse(link)
	if err != nil {
		t.Fatal(err)
	}
	q := u.Query()
	if u.Scheme != "asksemble" || u.Host != "pair" || q.Get("v") != "1" || q.Get("host") != "pc.example.ts.net" || q.Get("port") != "8800" || q.Get("code") != "ABCDEFGHIJKLMNOPQRSTUVWXYZ" {
		t.Errorf("link = %s", link)
	}
}

func TestTestPageURL(t *testing.T) {
	if got := TestPageURL("pc.example.ts.net", 8790, "ABC234"); got != "https://pc.example.ts.net:8790/test/#code=ABC234" {
		t.Errorf("TestPageURL = %q", got)
	}
}

func TestNormalizeAnswer(t *testing.T) {
	for in, want := range map[string]string{"y": "y", "Y\r": "y", string(rune(0xFEFF)) + "y": "y", " Yes ": "yes", "": ""} {
		if got := normalizeAnswer(in); got != want {
			t.Errorf("normalizeAnswer(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestBilingualErrors(t *testing.T) {
	var b bytes.Buffer
	printError(&b, fail("code %s expired", "코드 %s가 만료됐습니다", "X"))
	if got := b.String(); got != "error: code X expired\n오류: 코드 X가 만료됐습니다\n" {
		t.Errorf("printError = %q", got)
	}
	b.Reset()
	err := run([]string{"nope"}, strings.NewReader(""), &b)
	if err == nil || !strings.Contains(err.Error(), "모르는 명령") || !strings.Contains(err.Error(), "unknown command") {
		t.Errorf("unknown command error = %v", err)
	}
	b.Reset()
	if err := run([]string{"help"}, strings.NewReader(""), &b); err != nil || !strings.Contains(b.String(), "사용법") || strings.Contains(b.String(), "%!") {
		t.Errorf("help = %v %q", err, b.String())
	}
}

func TestGroupCode(t *testing.T) {
	if got := groupCode("ABCDEFGHIJKLMNOPQRSTUVWXYZ"); got != "ABCD-EFGH-IJKL-MNOP-QRST-UVWX-YZ" {
		t.Errorf("groupCode = %q", got)
	}
}

func TestPrintQR(t *testing.T) {
	for _, dark := range []bool{true, false} {
		var b bytes.Buffer
		if err := printQR(&b, PairURL("pc.example.ts.net", 8800, "ABCDEFGHIJKLMNOPQRSTUVWXYZ"), dark); err != nil {
			t.Fatal(err)
		}
		lines := strings.Split(strings.TrimRight(b.String(), "\n"), "\n")
		width := len([]rune(lines[0]))
		if width < 25 || len(lines) < width/2 {
			t.Errorf("QR looks wrong: %d lines of %d", len(lines), width)
		}
		for _, l := range lines {
			if len([]rune(l)) != width {
				t.Fatal("ragged QR lines")
			}
		}
	}
	if png, err := qrPNG("x"); err != nil || !bytes.HasPrefix(png, []byte("\x89PNG")) {
		t.Errorf("png = %v", err)
	}
}

// The CLI sessions list hides the judge folder like GET /v1/sessions does
// (P2-3), with the settings' hidden folders, and never appends to them.
func TestSessionFilterHidesJudgeFolder(t *testing.T) {
	root := t.TempDir()
	ask := filepath.Join(root, "ask")
	secret := filepath.Join(root, "secret")
	hiddenFolders := make([]string, 1, 8)
	hiddenFolders[0] = secret
	cfg := config.Default()
	cfg.Ask.Folder = ask
	cfg.HiddenFolders = hiddenFolders
	judgeDir := core.JudgeFolder(ask)
	evalDir := filepath.Join(judgeDir, "eval")
	if runtime.GOOS == "windows" {
		evalDir = strings.ToUpper(evalDir)
	}
	now := time.Now()
	in := []core.Session{
		{ID: "claude:ask", Folder: ask, LastActive: now},
		{ID: "claude:judge", Folder: judgeDir, LastActive: now.Add(-time.Minute)},
		{ID: "claude:eval", Folder: evalDir, LastActive: now.Add(-2 * time.Minute)},
		{ID: "claude:judge-x", Folder: judgeDir + "-x", LastActive: now.Add(-3 * time.Minute)},
		{ID: "claude:work", Folder: filepath.Join(root, "work"), LastActive: now.Add(-4 * time.Minute), Title: "work"},
		{ID: "claude:secret", Folder: filepath.Join(secret, "sub"), LastActive: now.Add(-5 * time.Minute)},
	}
	ids := func(ss []core.Session) (out []string) {
		for _, s := range ss {
			out = append(out, s.ID)
		}
		return out
	}
	for _, c := range []struct {
		q, folder string
		want      []string
	}{
		{"", "", []string{"claude:ask", "claude:judge-x", "claude:work"}},
		{"", ask, []string{"claude:ask", "claude:judge-x"}},
		{"", judgeDir, nil},
		{"work", "", []string{"claude:work"}},
	} {
		f := sessionFilter(cfg, c.q, c.folder)
		if got := ids(f.Apply(in)); !slices.Equal(got, c.want) {
			t.Errorf("q=%q folder=%q: %v, want %v", c.q, c.folder, got, c.want)
		}
	}
	if extra := hiddenFolders[:cap(hiddenFolders)][1:]; slices.ContainsFunc(extra, func(s string) bool { return s != "" }) {
		t.Errorf("the settings' hidden folders were appended to: %q", extra)
	}
}
