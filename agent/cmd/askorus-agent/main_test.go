package main

import (
	"bytes"
	"context"
	"encoding/json"
	"maps"
	"net/url"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/heeo-dev/askorus/agent/internal/adapter/claude"
	"github.com/heeo-dev/askorus/agent/internal/adapter/codex"
	"github.com/heeo-dev/askorus/agent/internal/adapter/opencode"
	"github.com/heeo-dev/askorus/agent/internal/config"
	"github.com/heeo-dev/askorus/agent/internal/core"
)

func TestPairURL(t *testing.T) {
	link := PairURL("pc.example.ts.net", 8800, "ABCDEFGHIJKLMNOPQRSTUVWXYZ")
	u, err := url.Parse(link)
	if err != nil {
		t.Fatal(err)
	}
	q := u.Query()
	if u.Scheme != "askorus" || u.Host != "pair" || q.Get("v") != "1" || q.Get("host") != "pc.example.ts.net" || q.Get("port") != "8800" || q.Get("code") != "ABCDEFGHIJKLMNOPQRSTUVWXYZ" {
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

// R-90 (#37): the [y/N] question says how to answer on a whole line first,
// so it shows even when the output goes through a pipe that holds a line
// until it ends ("[y/N]: " itself has no line end, the answer follows it).
func TestYesNoPromptSaysHowToAnswerFirst(t *testing.T) {
	var b bytes.Buffer
	yesNoPrompt(&b, m("Type y and press Enter to allow it. Just Enter refuses.", "허용하려면 y를 누르고 Enter, 거절하려면 그냥 Enter를 누르세요."))
	want := "Type y and press Enter to allow it. Just Enter refuses.\n허용하려면 y를 누르고 Enter, 거절하려면 그냥 Enter를 누르세요.\n[y/N]: "
	if got := b.String(); got != want {
		t.Errorf("yesNoPrompt = %q, want %q", got, want)
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

// help names no compare command since the judging went (C6d, #24).
func TestHelpHasNoCompare(t *testing.T) {
	var b bytes.Buffer
	if err := run([]string{"help"}, strings.NewReader(""), &b); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(b.String(), "compare") || strings.Contains(b.String(), "판정") {
		t.Errorf("help still names compare or judging:\n%s", b.String())
	}
	if err := run([]string{"compare"}, strings.NewReader(""), &b); err == nil || !strings.Contains(err.Error(), "unknown command") {
		t.Errorf("compare: %v", err)
	}
}

// info shows every AI's ask tiers and, for each AI that writes combined
// answers, the capability synthesize, as /v1/info does; an AI too old to
// write has none, and no AI shows judge (C6d, #24).
func TestInfoShowsSynthesize(t *testing.T) {
	ais := threeWriterAIs()
	ais[1].version = "0.9.0" // older than its tested version
	var b bytes.Buffer
	if err := printInfo(context.Background(), adaptersOf(ais), testGate(t), &b); err != nil {
		t.Fatal(err)
	}
	var infos []core.AdapterInfo
	if err := json.Unmarshal(b.Bytes(), &infos); err != nil {
		t.Fatalf("%v\n%s", err, b.String())
	}
	if len(infos) != 3 {
		t.Fatalf("infos = %+v", infos)
	}
	c, x, o := infos[0], infos[1], infos[2]
	if !slices.Equal(c.Capabilities, []core.Capability{core.CapAsk, core.CapSynthesize}) || c.Ask == nil {
		t.Errorf("claude = %+v", c)
	}
	if slices.Contains(x.Capabilities, core.CapSynthesize) || x.Ask == nil {
		t.Errorf("an old codex writes: %+v", x)
	}
	if !slices.Contains(o.Capabilities, core.CapSynthesize) || o.Ask == nil || o.Ask.Tiers[core.TierStandard].Model != "opencode-standard" {
		t.Errorf("opencode = %+v", o)
	}
	if strings.Contains(b.String(), "judge") {
		t.Errorf("info JSON names judge:\n%s", b.String())
	}
	// The adapters' own capability lists are left as they were.
	if caps := ais[0].Info(context.Background()).Capabilities; slices.Contains(caps, core.CapSynthesize) {
		t.Errorf("the adapter's list changed: %v", caps)
	}
}

// The gate of the CLI: the settings, the adapters' tested versions and
// the judge folder inside the ask folder, or none without one.
func TestJudgeGate(t *testing.T) {
	cfg := config.Default()
	cfg.Ask.Folder = filepath.Join(t.TempDir(), "ask")
	cfg.Judge = config.JudgeConfig{PerAI: 1}
	g := judgeGate(cfg)
	if g.Folder != core.JudgeFolder(cfg.Ask.Folder) || g.Settings.PerAI != 1 {
		t.Errorf("gate = %+v", g)
	}
	want := map[string]string{claude.Name: claude.JudgeMinVersion, codex.Name: codex.JudgeMinVersion, opencode.Name: opencode.JudgeMinVersion}
	if !maps.Equal(g.MinVersions, want) {
		t.Errorf("min versions = %v, want %v", g.MinVersions, want)
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
