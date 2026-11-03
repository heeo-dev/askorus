//go:build syntheval

package main

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/asksemble/asksemble/agent/internal/adapter/claude"
	"github.com/asksemble/asksemble/agent/internal/adapter/codex"
	"github.com/asksemble/asksemble/agent/internal/adapter/opencode"
	"github.com/asksemble/asksemble/agent/internal/config"
	"github.com/asksemble/asksemble/agent/internal/core"
	"github.com/asksemble/asksemble/agent/internal/fakecli"
)

// The test binary stands in for claude, codex and opencode (fakecli), as
// in the scenario tests.
func TestMain(m *testing.M) {
	if fakecli.Active() {
		os.Exit(fakecli.Main(os.Args[1:]))
	}
	os.Exit(m.Run())
}

// The agent's pipeline, on the three fakes: one question asked the way
// the phone asks, answered by all three, compared and written into one.
func TestTheAgentPipelineAsksComparesAndWrites(t *testing.T) {
	exes := fakecli.Install(t)
	fakecli.SetAnswers(t, map[string]string{
		fakecli.RoleClaude:   "가격은 NUM p=1입니다. 높이는 NUM h=5입니다.",
		fakecli.RoleCodex:    "가격은 NUM p=2입니다. 높이는 NUM h=5입니다.",
		fakecli.RoleOpenCode: "높이는 NUM h=5입니다. 오늘은 맑습니다.",
	})
	root := t.TempDir()
	claudeDir := filepath.Join(root, "claude")
	if err := os.MkdirAll(filepath.Join(claudeDir, "projects"), 0o700); err != nil {
		t.Fatal(err)
	}
	adapters := []core.Adapter{
		claude.New(claude.Options{ClaudeDir: claudeDir, ClaudePath: exes[fakecli.RoleClaude]}),
		codex.New(codex.Options{Path: exes[fakecli.RoleCodex], SchemaDir: filepath.Join(root, "agent")}),
		opencode.New(opencode.Options{Path: exes[fakecli.RoleOpenCode]}),
	}
	ctx := context.Background()
	p, err := pipelineFrom(ctx, adapters, filepath.Join(root, "ask"), config.JudgeConfig{})
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	if v := p.Versions(ctx); len(v) != 3 {
		t.Fatalf("versions %v", v)
	}
	rec, err := p.Ask(ctx, Question{ID: "g-1", Text: "이 상자의 가격과 높이는 얼마인가요?"}, RunInfo{Tier: core.TierQuick, Writer: "claude", Refine: "off"})
	if err != nil {
		t.Fatal(err)
	}
	if len(rec.Answers) != 3 {
		t.Fatalf("answers %+v", rec.Answers)
	}
	for _, a := range rec.Answers {
		if a.Status != "done" || a.Text == "" || a.Model == "" {
			t.Fatalf("answer %+v", a)
		}
	}
	c := rec.Compare
	if c == nil || c.Status != "done" || c.Requests == 0 || c.Counts.Differ != 1 || c.Counts.Agree == 0 {
		t.Fatalf("compare %+v", c)
	}
	s := rec.Synth
	if s == nil || s.Status != "done" || s.Agent != "claude" || s.Text == "" || !s.Judged {
		t.Fatalf("synth %+v", s)
	}
	if m := metricsOf(rec); !m.SynthOK || m.Answered != 3 || m.Differ != 1 {
		t.Fatalf("metrics %+v", m)
	}
}
