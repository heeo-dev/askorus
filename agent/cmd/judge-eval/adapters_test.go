//go:build judgeeval

package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/asksemble/asksemble/agent/internal/core"
	"github.com/asksemble/asksemble/agent/internal/fakecli"
)

// The real adapters against internal/fakecli (plan part 5): judge-eval
// builds them from agent.json and the gate as the agent does, and every
// call is recorded with its folder, arguments, environment and stdin.

// fakeRig points agent.json at the fake claude, codex and opencode and
// returns the mini set, the out folder and the eval folder.
func fakeRig(t *testing.T) (data, out, eval string) {
	t.Helper()
	exes := fakecli.Install(t)
	agentDir, askDir := t.TempDir(), t.TempDir()
	cfg := map[string]any{"port": 8790, "folders": []any{}, "claudePath": exes[fakecli.RoleClaude],
		"codexPath": exes[fakecli.RoleCodex], "opencodePath": exes[fakecli.RoleOpenCode], "ask": map[string]any{"folder": askDir}}
	b, _ := json.Marshal(cfg)
	if err := os.WriteFile(filepath.Join(agentDir, "agent.json"), b, 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("ASKSEMBLE_AGENT_DIR", agentDir)
	t.Setenv("CLAUDE_CONFIG_DIR", t.TempDir())
	return writeMini(t, nil), t.TempDir(), filepath.Join(core.JudgeFolder(askDir), evalFolderName)
}

func sameDir(a, b string) bool {
	return strings.EqualFold(filepath.Clean(a), filepath.Clean(b))
}

func TestRealAdapters(t *testing.T) {
	data, out, eval := fakeRig(t)
	now := time.Now().UnixMilli()
	const (
		keep  = "ses_keepNotAJudge0000000000000"
		other = "ses_judgeInAnotherFolder000000"
		fresh = "ses_freshJudgeNoRef00000000000"
		stale = "ses_staleJudgeLeftBehind000000"
	)
	if err := os.MkdirAll(eval, 0o700); err != nil {
		t.Fatal(err)
	}
	fakecli.SetSessions(t, []fakecli.Session{
		{ID: keep, Title: "my chat", Updated: now, Created: now, ProjectID: fakecli.ProjectID, Directory: eval},
		{ID: other, Title: fakecli.JudgeTitle, Updated: now - 3600_000, Created: now, ProjectID: fakecli.ProjectID, Directory: t.TempDir()},
		{ID: fresh, Title: fakecli.JudgeTitle, Updated: now, Created: now, ProjectID: fakecli.ProjectID, Directory: eval},
		{ID: stale, Title: fakecli.JudgeTitle, Updated: now - 3600_000, Created: now, ProjectID: fakecli.ProjectID, Directory: eval},
	})
	code, stdout, stderr := cli(t, context.Background(), "run", "-engines", "claude,codex,opencode", "-run", "f", "-expect", "18", "-data", data, "-out", out)
	if code != exitOK {
		t.Fatalf("exit %d: %s %s", code, stdout, stderr)
	}
	recs, _, _ := loadRecords(filepath.Join(out, "f", fileRequests), false)
	if len(recs) != 18 {
		t.Fatalf("%d records", len(recs))
	}
	for k, r := range recs {
		if r.Status != statusDone || len(r.Attempts) != 1 {
			t.Errorf("%s: %s %s, %d attempts", k, r.Status, r.Code, len(r.Attempts))
		}
	}
	m, err := readManifest(filepath.Join(out, "f"))
	if err != nil {
		t.Fatal(err)
	}
	wantModels := map[string]string{"claude": "haiku", "codex": "gpt-6-luna", "opencode": "opencode-go/deepseek-v4.1-flash"}
	for e, model := range wantModels {
		if m.EngineInfo[e].Model != model || m.EngineInfo[e].Version != fakecli.Versions[e] {
			t.Errorf("%s: %+v", e, m.EngineInfo[e])
		}
	}
	schemaDir := filepath.Join(out, "f", codexSchemaDir)
	if !sameDir(m.EvalFolder, eval) || !sameDir(m.CodexSchema, schemaDir) || !m.SettingsIgnored {
		t.Errorf("manifest folders %q %q", m.EvalFolder, m.CodexSchema)
	}

	content := []string{"서울", "한강", "940만", "700㎢"}
	judges := map[string]int{}
	var deleted []string
	for _, c := range fakecli.Calls(t) {
		if !sameDir(c.Dir, eval) {
			t.Errorf("%s %q ran in %q, not the eval folder", c.Role, c.Args, c.Dir)
		}
		for _, a := range c.Args {
			for _, s := range content {
				if strings.Contains(a, s) {
					t.Errorf("%s: argument %q holds content", c.Role, a)
				}
			}
		}
		switch {
		case c.Role == fakecli.RoleClaude:
			judges[c.Role]++
			want := []string{"-p", "--output-format", "json", "--model", "haiku", "--effort", "medium", "--tools", "",
				"--strict-mcp-config", "--no-session-persistence", "--safe-mode", "--json-schema", core.JudgeSchemaV1}
			if !slices.Equal(c.Args, want) {
				t.Errorf("claude args %q", c.Args)
			}
		case c.Role == fakecli.RoleCodex:
			judges[c.Role]++
			i := slices.Index(c.Args, "--output-schema")
			if c.Args[0] != "exec" || i < 0 || c.Args[len(c.Args)-1] != "-" || !slices.Contains(c.Args, "--ephemeral") {
				t.Fatalf("codex args %q", c.Args)
			}
			if p := c.Args[i+1]; !sameDir(filepath.Dir(p), schemaDir) || filepath.Base(p) != core.JudgeSchemaFile || c.Schema != core.JudgeSchemaV1 {
				t.Errorf("codex schema %q", p)
			}
		case c.Role == fakecli.RoleOpenCode && c.Args[0] == "run":
			judges[c.Role]++
			want := []string{"run", "--standalone", "--format", "json", "--agent", fakecli.JudgeAgent, "-m", "opencode-go/deepseek-v4.1-flash#high", "--title", fakecli.JudgeTitle}
			if !slices.Equal(c.Args, want) || !sameDir(c.Env["PWD"], eval) {
				t.Errorf("opencode args %q, PWD %q", c.Args, c.Env["PWD"])
			}
		case c.Role == fakecli.RoleOpenCode && c.Args[1] == "delete":
			deleted = append(deleted, c.Args[2])
			continue
		default:
			continue
		}
		if !strings.Contains(c.Stdin, "서울 인구는 약 940만 명입니다.") || !strings.Contains(c.Stdin, "서울의 인구와 면적은?") {
			t.Errorf("%s: the question and answers are not on stdin", c.Role)
		}
	}
	if judges[fakecli.RoleClaude] != 6 || judges[fakecli.RoleCodex] != 6 || judges[fakecli.RoleOpenCode] != 6 {
		t.Errorf("judge calls %v", judges)
	}
	if b, err := os.ReadFile(filepath.Join(schemaDir, core.JudgeSchemaFile)); err != nil || string(b) != core.JudgeSchemaV1 {
		t.Errorf("schema file: %v", err)
	}
	// OpenCode: the six judge conversations and the stale one are deleted,
	// nothing else.
	var left []string
	for _, s := range fakecli.Sessions(t) {
		left = append(left, s.ID)
	}
	slices.Sort(left)
	if want := []string{fresh, other, keep}; !slices.Equal(left, slices.Sorted(slices.Values(want))) {
		t.Errorf("sessions left %v", left)
	}
	if len(deleted) != 7 || !slices.Contains(deleted, stale) || slices.ContainsFunc(deleted, func(id string) bool { return id == keep || id == other || id == fresh }) {
		t.Errorf("deleted %v", deleted)
	}
	events, _ := os.ReadFile(filepath.Join(out, "f", fileEvents))
	if !strings.Contains(string(events), "cleanup engine=opencode refs=0 code=ok") || strings.Contains(string(events), "서울") {
		t.Errorf("events.log:\n%s", events)
	}
}

func TestRealAdaptersUsageLimit(t *testing.T) {
	data, out, _ := fakeRig(t)
	t.Setenv(fakecli.EnvJudge, "codex=usage_limit")
	code, stdout, stderr := cli(t, context.Background(), "run", "-engines", "claude,codex,opencode", "-run", "u", "-expect", "18", "-data", data, "-out", out)
	if code != exitUsageLimit {
		t.Fatalf("exit %d: %s %s", code, stdout, stderr)
	}
	if s := readStop(t, filepath.Join(out, "u")); s.Code != stopUsage || s.Engine != "codex" {
		t.Errorf("stop.json %+v", s)
	}
	recs, _, _ := loadRecords(filepath.Join(out, "u", fileRequests), false)
	for k := range recs {
		if strings.Contains(k, "|codex|") {
			t.Errorf("codex recorded %s", k)
		}
	}
}

func TestRealAdaptersProseOnce(t *testing.T) {
	data, out, _ := fakeRig(t)
	t.Setenv(fakecli.EnvJudge, "codex=prose_once")
	code, stdout, stderr := cli(t, context.Background(), "run", "-engines", "codex", "-run", "p", "-expect", "6", "-parallel", "1", "-data", data, "-out", out)
	if code != exitOK {
		t.Fatalf("exit %d: %s %s", code, stdout, stderr)
	}
	recs, _, _ := loadRecords(filepath.Join(out, "p", fileRequests), false)
	twice := 0
	for _, r := range recs {
		if r.Status != statusDone {
			t.Errorf("%s: %s %s", r.Key, r.Status, r.Code)
		}
		if len(r.Attempts) == 2 {
			twice++
			if a := r.Attempts[0]; a.Validate != core.ErrCodeJudgeBadOutput || a.Output != fakecli.ProseText {
				t.Errorf("first attempt %+v", a)
			}
		}
	}
	if twice != 1 || len(recs) != 6 {
		t.Errorf("%d records, %d with two attempts", len(recs), twice)
	}
}
