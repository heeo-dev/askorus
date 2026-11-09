package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/heeo-dev/askorus/agent/internal/config"
	"github.com/heeo-dev/askorus/agent/internal/core"
	"github.com/heeo-dev/askorus/agent/internal/devices"
)

// Tests never touch the real logon entry or PATH in the registry, and
// never run the real opencode against the real judge folder.
func init() {
	uninstallAutostart = func() error { return nil }
	uninstallPath = func(string) (bool, error) { return false, nil }
	uninstallJudgeCleanup = judgeCleanups.record
}

// judgeCleanups records uninstall's judge folder cleanups.
var judgeCleanups cleanupRecorder

type cleanupRecorder struct {
	mu      sync.Mutex
	folders []string
	err     error // what the next cleanups return
}

func (r *cleanupRecorder) record(_ context.Context, _ config.Config, folder string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.folders = append(r.folders, folder)
	return r.err
}

// reset forgets the cleanups so far and makes the next ones return err.
func (r *cleanupRecorder) reset(err error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.folders, r.err = nil, err
}

func (r *cleanupRecorder) calls() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return slices.Clone(r.folders)
}

// uninstallDir is an agent folder whose settings name an ask folder, with
// the judge schema file codex read until C6d (#24), which a PC may still
// have.
func uninstallDir(t *testing.T) (dir, ask string) {
	t.Helper()
	dir = filepath.Join(t.TempDir(), "agent")
	ask = filepath.Join(t.TempDir(), "my asks")
	cfg := config.Default()
	cfg.Port = 8799
	cfg.Ask.Folder = ask
	if err := config.Save(dir, cfg); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, oldJudgeSchemaFile), []byte(`{"type":"object"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	return dir, ask
}

// Saying no to uninstall changes nothing.
func TestUninstallDeclined(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "agent.json"), []byte(`{"port": 8799}`), 0o600)
	os.WriteFile(filepath.Join(dir, devices.FileName), []byte("[]\n"), 0o600)
	var out bytes.Buffer
	if err := uninstallCmd(dir, nil, strings.NewReader("n\n"), &out); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, devices.FileName)); err != nil {
		t.Error("declined uninstall deleted the device file")
	}
	if !strings.Contains(out.String(), "아무것도 바꾸지 않았습니다") {
		t.Errorf("output = %q", out.String())
	}
	// R-90 (#37): how to answer is a whole line before the [y/N].
	if !strings.Contains(out.String(), "Just Enter changes nothing.\n진행하려면 y를 누르고 Enter, 그만두려면 그냥 Enter를 누르세요.\n[y/N]: ") {
		t.Errorf("no answer line before [y/N]: %q", out.String())
	}
}

// Saying no keeps the judge schema file and cleans no judge folder.
func TestUninstallDeclinedKeepsJudgeFiles(t *testing.T) {
	judgeCleanups.reset(nil)
	dir, _ := uninstallDir(t)
	var out bytes.Buffer
	if err := uninstallCmd(dir, nil, strings.NewReader("n\n"), &out); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, oldJudgeSchemaFile)); err != nil {
		t.Error("declined uninstall deleted the judge schema file")
	}
	if calls := judgeCleanups.calls(); len(calls) != 0 {
		t.Errorf("declined uninstall cleaned %v", calls)
	}
	for _, want := range []string{
		"delete OpenCode's leftover judge conversations in the judge folder / 판정 폴더에 남은 OpenCode 판정 대화를 지움",
		"delete " + filepath.Join(dir, oldJudgeSchemaFile) + " / " + filepath.Join(dir, oldJudgeSchemaFile) + " 삭제",
	} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("the plan lacks %q:\n%s", want, out.String())
		}
	}
}

// Uninstall without -all removes the old judge schema file and cleans the
// judge folder of the configured ask folder once, after the agent is
// stopped.
func TestUninstallRemovesJudgeFiles(t *testing.T) {
	// The name codex's judges wrote until C6d (#24), on PCs that had them.
	if oldJudgeSchemaFile != "judge-schema-v1.json" {
		t.Fatalf("oldJudgeSchemaFile = %q", oldJudgeSchemaFile)
	}
	judgeCleanups.reset(nil)
	dir, ask := uninstallDir(t)
	var out bytes.Buffer
	if err := uninstallCmd(dir, []string{"-yes"}, strings.NewReader(""), &out); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, oldJudgeSchemaFile)); !os.IsNotExist(err) {
		t.Errorf("the judge schema file stayed: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, config.FileName)); err != nil {
		t.Error("uninstall without -all deleted the settings")
	}
	if calls := judgeCleanups.calls(); !slices.Equal(calls, []string{core.JudgeFolder(ask)}) {
		t.Errorf("cleanups = %v, want [%s]", calls, core.JudgeFolder(ask))
	}
	if strings.Contains(out.String(), "could not delete every OpenCode") {
		t.Errorf("output = %s", out.String())
	}
}

// A failed cleanup is a note with its code only, never its detail, and
// uninstall goes on.
func TestUninstallJudgeCleanupFails(t *testing.T) {
	judgeCleanups.reset(core.Fail(core.ErrCodeJudgeFailed, "SECRET-DETAIL"))
	t.Cleanup(func() { judgeCleanups.reset(nil) })
	dir, ask := uninstallDir(t)
	var out bytes.Buffer
	if err := uninstallCmd(dir, []string{"-yes"}, strings.NewReader(""), &out); err != nil {
		t.Fatal(err)
	}
	got := out.String()
	if !strings.Contains(got, "could not delete every OpenCode judge conversation (judge_failed); they stay in "+core.JudgeFolder(ask)) ||
		!strings.Contains(got, "OpenCode 판정 대화를 다 지우지 못했습니다 (judge_failed)") || strings.Contains(got, "SECRET") {
		t.Errorf("output = %s", got)
	}
	if !strings.Contains(got, "uninstalled") {
		t.Errorf("uninstall stopped: %s", got)
	}
	if _, err := os.Stat(filepath.Join(dir, oldJudgeSchemaFile)); !os.IsNotExist(err) {
		t.Errorf("the judge schema file stayed: %v", err)
	}
}

// The default cleanup runs nothing for a missing judge folder or a
// missing opencode (the path below is no file). It never runs the real
// opencode in a test: the path is set.
func TestCleanupJudgeFolderWithoutOpenCode(t *testing.T) {
	cfg := config.Default()
	cfg.OpenCodePath = filepath.Join(t.TempDir(), "no-opencode.exe")
	folder := filepath.Join(t.TempDir(), "judge")
	if err := cleanupJudgeFolder(context.Background(), cfg, folder); err != nil {
		t.Errorf("missing folder: %v", err)
	}
	if err := os.MkdirAll(folder, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := cleanupJudgeFolder(context.Background(), cfg, folder); err != nil {
		t.Errorf("missing opencode: %v", err)
	}
}

func TestErrCode(t *testing.T) {
	if errCode(core.Fail(core.ErrCodeJudgeFailed, "x")) != core.ErrCodeJudgeFailed || errCode(os.ErrNotExist) != core.ErrCodeInternal {
		t.Error("errCode")
	}
}

func TestUninstallAllInTempDir(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "agent")
	os.MkdirAll(dir, 0o700)
	os.WriteFile(filepath.Join(dir, "agent.json"), []byte(`{"port": 8799}`), 0o600)
	os.WriteFile(filepath.Join(dir, devices.FileName), []byte("[]\n"), 0o600)
	var out bytes.Buffer
	if err := uninstallCmd(dir, []string{"-yes", "-all"}, strings.NewReader(""), &out); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Error("-all left the agent folder")
	}
}

func TestCheckerCounts(t *testing.T) {
	var out bytes.Buffer
	c := &checker{out: &out}
	c.line("ok", m("a", "가"))
	c.line("warn", m("b", "나"))
	c.line("bad", m("c", "다"))
	if c.bad != 1 || c.warn != 1 || !strings.Contains(out.String(), "[X]") || !strings.Contains(out.String(), "다") {
		t.Errorf("checker = %+v %q", c, out.String())
	}
}

// check tells how to trust the ask folder, with its path, so an ask can go
// on in the Claude app (P2-2: it needs a trusted folder).
func TestSetupTipsTrustTheAskFolder(t *testing.T) {
	cfg := config.Default()
	cfg.Ask.Folder = filepath.Join(t.TempDir(), "my asks")
	tips := setupTips(cfg)
	var trust msg
	for _, tip := range tips {
		if strings.Contains(tip.en, "trust the ask folder") {
			trust = tip
		}
	}
	want := `cd "` + cfg.Ask.Folder + `"`
	if !strings.Contains(trust.en, want) || !strings.Contains(trust.ko, want) || !strings.Contains(trust.ko, "신뢰") {
		t.Errorf("trust tip = %+v, want %s in both languages", trust, want)
	}
	if len(tips) != 3 || !strings.Contains(tips[0].en, "claude --rc") {
		t.Errorf("tips = %+v", tips)
	}
}

// Uninstall deletes the combined-answer schema file next to the old judge one,
// and says so in its plan; saying no keeps it.
func TestUninstallRemovesSynthSchema(t *testing.T) {
	judgeCleanups.reset(nil)
	dir, _ := uninstallDir(t)
	synth := filepath.Join(dir, core.SynthSchemaFile)
	if err := os.WriteFile(synth, []byte(core.SynthSchemaV1), 0o600); err != nil {
		t.Fatal(err)
	}
	// Milestone C: the refine schema file goes too.
	if err := os.WriteFile(filepath.Join(dir, core.RefineSchemaFile), []byte(core.RefineSchemaV1), 0o600); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if err := uninstallCmd(dir, nil, strings.NewReader("n\n"), &out); err != nil {
		t.Fatal(err)
	}
	if want := "delete " + synth + " / " + synth + " 삭제"; !strings.Contains(out.String(), want) {
		t.Errorf("the plan lacks %q:\n%s", want, out.String())
	}
	if _, err := os.Stat(synth); err != nil {
		t.Error("declined uninstall deleted the synth schema file")
	}
	out.Reset()
	if err := uninstallCmd(dir, []string{"-yes"}, strings.NewReader(""), &out); err != nil {
		t.Fatal(err)
	}
	for _, f := range []string{synth, filepath.Join(dir, oldJudgeSchemaFile), filepath.Join(dir, core.RefineSchemaFile)} {
		if _, err := os.Stat(f); !os.IsNotExist(err) {
			t.Errorf("%s stayed: %v", f, err)
		}
	}
	// A missing file is no error.
	if err := uninstallCmd(dir, []string{"-yes"}, strings.NewReader(""), &bytes.Buffer{}); err != nil {
		t.Errorf("again: %v", err)
	}
}
