//go:build syntheval

package main

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/asksemble/asksemble/agent/internal/core"
	"github.com/asksemble/asksemble/agent/internal/judge"
)

// The quality experiments (C4, E0): resynth, refine, score, pairs.

// fakeWriter replies from a function of the call; it counts the calls.
type fakeWriter struct {
	mu    sync.Mutex
	calls []writeReq
	reply func(n int, r writeReq) (writeRes, error)
}

func (f *fakeWriter) Write(_ context.Context, r writeReq) (writeRes, error) {
	f.mu.Lock()
	f.calls = append(f.calls, r)
	n := len(f.calls)
	f.mu.Unlock()
	return f.reply(n, r)
}

// useWriter puts f in newWriter for a command and allows normal rights.
func useWriter(t *testing.T, f writer) {
	t.Helper()
	old := newWriter
	newWriter = func(context.Context) (writer, func(), error) { return f, func() {}, nil }
	t.Cleanup(func() { newWriter = old })
	oldE := selfElevated
	selfElevated = func() bool { return false }
	t.Cleanup(func() { selfElevated = oldE })
}

func synthJSON(text string) []byte {
	b := new(bytes.Buffer)
	b.WriteString(`{"answer":`)
	b.WriteString(quoteJSON(text))
	b.WriteString(`}`)
	return b.Bytes()
}

func quoteJSON(s string) string {
	var b strings.Builder
	b.WriteByte('"')
	for _, r := range s {
		switch r {
		case '"':
			b.WriteString(`\"`)
		case '\\':
			b.WriteString(`\\`)
		case '\n':
			b.WriteString(`\n`)
		default:
			b.WriteRune(r)
		}
	}
	b.WriteByte('"')
	return b.String()
}

// taskRecord is a record with a task, as the pipeline saves it since E0.
func taskRecord(id string, seq int) Record {
	task := judge.SynthTask{
		Question: "상자의 가격은?",
		Answers:  []judge.SynthAnswer{{AI: "Claude", Text: "가격은 1원입니다."}, {AI: "Codex", Text: "가격은 2원입니다."}},
		Differ:   []judge.SynthDiffer{{Sides: []judge.SynthSide{{AIs: []string{"Claude"}, Says: []string{"가격은 1원입니다."}}, {AIs: []string{"Codex"}, Says: []string{"가격은 2원입니다."}}}, NumbersDiffer: true}},
		Compared: true,
	}
	return Record{
		V: 1, Seq: seq, Question: Question{ID: id, File: "general.txt", Text: task.Question},
		Run:     RunInfo{Name: "base", Tier: core.TierQuick, Writer: "claude", Refine: "off"},
		Answers: []AnswerRec{{Agent: "claude", Status: "done", Ms: 1000, Text: task.Answers[0].Text}, {Agent: "codex", Status: "done", Ms: 2000, Text: task.Answers[1].Text}},
		Compare: &CompareRec{Status: "done", Ms: 3000, Counts: judge.Counts{Differ: 1}},
		Synth:   &SynthRec{Status: "done", Agent: "claude", Ms: 4000, Judged: true, Text: "old"},
		Task:    &task,
	}
}

func TestResynthWritesFromTheSameAnswersAsksAgainOnceAndStopsOnALimit(t *testing.T) {
	useRoot(t)
	base, _ := runDir("base")
	r1, r2, r3 := taskRecord("g-1", 1), taskRecord("g-2", 2), taskRecord("g-3", 3)
	old := taskRecord("g-4", 4)
	old.Task = nil // a run before E0
	for _, r := range []Record{r1, r2, r3, old} {
		if err := saveRecord(base, r); err != nil {
			t.Fatal(err)
		}
	}
	f := &fakeWriter{reply: func(n int, r writeReq) (writeRes, error) {
		switch n {
		case 1:
			return writeRes{Agent: r.Agent, Model: "claude-sonnet", Ms: 5000, Output: []byte("not json")}, nil // asked again
		case 2:
			return writeRes{Agent: r.Agent, Model: "claude-sonnet", Ms: 6000, Output: synthJSON("{{Claude}}은 1원, {{Codex}}는 2원이라고 했습니다.")}, nil
		case 3:
			return writeRes{Agent: r.Agent}, &core.ActionError{Code: core.ErrCodeJudgeFailed, Detail: "You've hit your limit"}
		}
		return writeRes{Agent: r.Agent, Model: "claude-sonnet", Ms: 7000, Output: synthJSON("{{Claude}} 1원, {{Codex}} 2원.")}, nil
	}}
	useWriter(t, f)
	var out, errOut bytes.Buffer
	ctx := context.Background()
	if c := runMain(ctx, []string{"resynth", "-from", "base", "-run", "std", "-tier", "standard", "-expect", "2"}, &out, &errOut); c != exitFlags || !strings.Contains(errOut.String(), "-expect 3") {
		t.Fatalf("expect: %d %s", c, errOut.String())
	}
	if c := runMain(ctx, []string{"resynth", "-from", "base", "-run", "std", "-tier", "standard", "-synth", "v9", "-expect", "3"}, &out, &errOut); c != exitFlags {
		t.Fatalf("an unknown prompt ran: %d", c)
	}
	out.Reset()
	if c := runMain(ctx, []string{"resynth", "-from", "base", "-run", "std", "-tier", "standard", "-expect", "3"}, &out, &errOut); c != exitUsageLimit {
		t.Fatalf("exit %d, out %s, err %s", c, out.String(), errOut.String())
	}
	if !strings.Contains(out.String(), "skipped: base keeps no task") {
		t.Fatalf("out %s", out.String())
	}
	for _, c := range f.calls {
		if c.Agent != "claude" || c.Tier != core.TierStandard || c.Refine || !strings.HasPrefix(c.Prompt, "[Asksemble synthesis v2]") {
			t.Fatalf("call %+v", c)
		}
	}
	dir, _ := runDir("std")
	rs, err := loadRecords(dir)
	if err != nil || len(rs) != 2 {
		t.Fatalf("records %v %v", rs, err)
	}
	got := rs[0]
	if got.Synth.Status != "done" || got.Synth.Ms != 11000 || got.Synth.Model != "claude-sonnet" || got.Synth.Unnamed != 0 || !strings.Contains(got.Synth.Text, "{{Codex}}") {
		t.Fatalf("synth %+v", got.Synth)
	}
	if got.Run.From != "base" || got.Run.SynthTier != core.TierStandard || got.Run.Synth != "product" || got.Run.Tier != core.TierQuick || got.Run.Name != "std" {
		t.Fatalf("run %+v", got.Run)
	}
	if len(got.Answers) != 2 || got.Compare.Ms != 3000 || got.Task == nil || got.Question.ID != "g-1" {
		t.Fatalf("not the source's answers: %+v", got)
	}
	if rs[1].Stop != "usage_limit" || rs[1].Synth.Status != "failed" {
		t.Fatalf("limit %+v %+v", rs[1].Stop, rs[1].Synth)
	}
	// Resume writes g-2 and g-3 only.
	out.Reset()
	if c := runMain(ctx, []string{"resynth", "-from", "base", "-run", "std", "-tier", "standard", "-resume", "-expect", "2"}, &out, &errOut); c != exitOK {
		t.Fatalf("resume %d %s %s", c, out.String(), errOut.String())
	}
	rs, _ = loadRecords(dir)
	if len(rs) != 3 || rs[1].Stop != "" || rs[2].Synth.Status != "done" {
		t.Fatalf("after resume %+v", rs)
	}
	// The report names who wrote.
	out.Reset()
	if c := runMain(ctx, []string{"report", "-run", "base,std"}, &out, &errOut); c != exitOK {
		t.Fatalf("report %d %s", c, errOut.String())
	}
	if !strings.Contains(out.String(), "claude · quick · product") || !strings.Contains(out.String(), "claude · standard · product") {
		t.Fatalf("report %s", out.String())
	}
}

func TestResynthNeedsANewRunAndAWriter(t *testing.T) {
	useRoot(t)
	useWriter(t, &fakeWriter{reply: func(int, writeReq) (writeRes, error) { return writeRes{}, errors.New("no call") }})
	var out, errOut bytes.Buffer
	for _, args := range [][]string{
		{"resynth", "-from", "a", "-run", "a", "-expect", "0"},
		{"resynth", "-from", "a", "-run", "b", "-writer", "gemini", "-expect", "0"},
		{"resynth", "-from", "a", "-run", "b", "-tier", "max", "-expect", "0"},
	} {
		if c := runMain(context.Background(), args, &out, &errOut); c != exitFlags {
			t.Fatalf("%v: %d", args, c)
		}
	}
}
