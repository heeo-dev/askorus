//go:build syntheval

package main

import (
	"context"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/asksemble/asksemble/agent/internal/adapter/claude"
	"github.com/asksemble/asksemble/agent/internal/adapter/codex"
	"github.com/asksemble/asksemble/agent/internal/adapter/opencode"
	"github.com/asksemble/asksemble/agent/internal/config"
	"github.com/asksemble/asksemble/agent/internal/core"
	"github.com/asksemble/asksemble/agent/internal/fakecli"
	"github.com/asksemble/asksemble/agent/internal/judge"
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

// With -refine on (C2) the writer refines the question first: the record
// keeps the spec and the template every AI got.
func TestTheAgentPipelineRefinesFirst(t *testing.T) {
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
	rec, err := p.Ask(ctx, Question{ID: "g-1", Text: "이 상자의 가격과 높이는 얼마인가요?"}, RunInfo{Tier: core.TierQuick, Writer: "claude", Refine: "on"})
	if err != nil {
		t.Fatal(err)
	}
	rf := rec.Refine
	if rf == nil || rf.Status != "done" || rf.Agent != "claude" || rf.Kind != "compare" || rf.Depth != "short" || rf.Template == "" {
		t.Fatalf("refine %+v", rf)
	}
	for _, c := range fakecli.Calls(t) {
		if strings.HasPrefix(c.Stdin, "[Asksemble") || c.Stdin == "" {
			continue // the moderator, judges, writer
		}
		if !strings.Contains(c.Stdin, rf.Template) {
			t.Errorf("%s was asked without the template", c.Role)
		}
	}
	if m := metricsOf(rec); m.Kind != "compare" || m.TotalMs != m.RefineMs+m.AskMs+m.CompareMs+m.SynthMs || !m.SynthOK {
		t.Fatalf("metrics %+v", m)
	}
}

// The record keeps the writer's task (C4, E0): made again from it, the
// prompt is the one the writer got, byte for byte (its nonce aside), and
// a writer call through the agent's adapters writes the same combined
// answer again. With refining, the task carries the spec (synth-v3).
func TestTheRecordKeepsTheWritersTaskForResynth(t *testing.T) {
	for _, refine := range []string{"off", "on"} {
		t.Run(refine, func(t *testing.T) {
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
			rec, err := p.Ask(ctx, Question{ID: "g-1", Text: "이 상자의 가격과 높이는 얼마인가요?"}, RunInfo{Tier: core.TierQuick, Writer: "claude", Refine: refine})
			if err != nil {
				t.Fatal(err)
			}
			task := rec.Task
			if task == nil || len(task.Answers) != 3 || len(task.Differ) != 1 || !task.Compared || task.Answers[0].AI != "Claude" {
				t.Fatalf("task %+v", task)
			}
			if (task.Spec != nil) != (refine == "on") {
				t.Fatalf("spec %+v with refine %s", task.Spec, refine)
			}
			var sent string
			for _, c := range fakecli.Calls(t) {
				if strings.HasPrefix(c.Stdin, "[Asksemble synthesis ") {
					sent = c.Stdin
				}
			}
			m := regexp.MustCompile(`<data-([0-9a-f]{16})>`).FindStringSubmatch(sent)
			if m == nil {
				t.Fatalf("no synth prompt among the calls")
			}
			if again := judge.SynthPrompt(*task, m[1]); again != sent {
				t.Fatalf("the task does not make the prompt the writer got:\n%s\n----\n%s", again, sent)
			}
			// A call of the writer through the agent's adapters: the same text.
			sr := writeSynth(ctx, p, *task, "claude", core.TierQuick, judge.SynthPrompt)
			if sr.Status != "done" || sr.Text != rec.Synth.Text || sr.Model == "" {
				t.Fatalf("resynth %+v, first %q", sr, rec.Synth.Text)
			}
			// A writer the PC does not have at that tier is an error, not a call.
			if _, err := p.Write(ctx, writeReq{Agent: "gemini", Tier: core.TierQuick, Prompt: "x"}); err == nil {
				t.Fatal("an unknown writer wrote")
			}
		})
	}
}
