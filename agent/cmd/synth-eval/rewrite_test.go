//go:build syntheval

package main

import (
	"bytes"
	"context"
	"strings"
	"sync"
	"testing"

	"github.com/asksemble/asksemble/agent/internal/judge"
)

// C9: the answer after a debate written again, short or as an edit of the
// combined answer before the debate.

func TestRewritePrompts(t *testing.T) {
	task := judge.SynthTask{Question: "이름은?", Answers: []judge.SynthAnswer{{AI: "Claude", Text: "Panelist"}, {AI: "Codex", Text: "Asksemble"}}}
	points := []DebatePoint{{ID: "p1", Topic: "이름", Nature: "taste", Debate: true, Turns: []DebateTurn{{AI: "Claude", Ask: "?", Text: "AskPanel", Stance: "partly"}}, Result: "kept", Line: "AskPanel"}}
	const n = "0123456789abcdef"
	short := rewritePrompt(task, points, "이전 글", "short", n)
	if !strings.HasPrefix(short, "[Asksemble synthesis debate-v3] ") || !strings.Contains(short, debateShortRule5) || !strings.Contains(short, "Keep it short") || strings.Contains(short, "COMBINED BEFORE") {
		t.Fatalf("short:\n%s", short)
	}
	edit := rewritePrompt(task, points, " 이전 글 ", "edit", n)
	if !strings.HasPrefix(edit, "[Asksemble synthesis debate-e1] ") || !strings.Contains(edit, debateEditRule5) ||
		!strings.Contains(edit, "COMBINED BEFORE: \"이전 글\"\n</data-"+n+">") || strings.Contains(edit, "Keep it short") {
		t.Fatalf("edit:\n%s", edit)
	}
	plus := rewritePrompt(task, points, "이전 글", "plus", n)
	if !strings.HasPrefix(plus, "[Asksemble synthesis debate-v3p] ") || !strings.Contains(plus, debatePlusRule5) || !strings.Contains(plus, "({{Codex}} 안)") || !strings.Contains(plus, "at most 3 lines") {
		t.Fatalf("plus:\n%s", plus)
	}
	// The short rule is r2's with the length sentences before its last one.
	if !strings.HasSuffix(debateShortRule5, "and not the moderator.") || !strings.HasPrefix(debateShortRule5, "5. The AIs debated") {
		t.Errorf("short rule: %s", debateShortRule5)
	}
}

// rewriteWriter answers with the prompt's version, so a test sees which
// prompt each answer came from.
type rewriteWriter struct {
	mu      sync.Mutex
	prompts []string
}

func (w *rewriteWriter) Write(_ context.Context, r writeReq) (writeRes, error) {
	w.mu.Lock()
	w.prompts = append(w.prompts, r.Prompt)
	w.mu.Unlock()
	v := strings.SplitN(strings.TrimPrefix(r.Prompt, "[Asksemble synthesis "), "]", 2)[0]
	return writeRes{Agent: r.Agent, Output: synthJSON("짧은 글 " + v)}, nil
}

func (w *rewriteWriter) AskOne(context.Context, string, string, string) (string, int64, error) {
	return "", 0, nil
}

func TestRewriteCmd(t *testing.T) {
	useRoot(t)
	selfElevated = func() bool { return false }
	w := &rewriteWriter{}
	old := newDebater
	newDebater = func(context.Context) (debater, func(), error) { return w, func() {}, nil }
	t.Cleanup(func() { newDebater = old })
	save := func(run string, r Record) {
		d, _ := runDir(run)
		if err := saveRecord(d, r); err != nil {
			t.Fatal(err)
		}
	}
	debated := func(id string) Record {
		r := taskRecord(id, 1)
		r.Synth.Text = "긴 글"
		r.Debate = &DebateRec{Method: "r2", Points: []DebatePoint{{ID: "p1", Topic: "가격", Nature: "fact", Debate: true, Turns: []DebateTurn{{AI: "Claude", Ask: "?", Text: "2원", Stance: "change"}}}}}
		return r
	}
	for _, id := range []string{"g-1", "g-2", "g-3"} {
		b := taskRecord(id, 1)
		b.Synth.Text = "전 글 " + id
		save("base", b)
	}
	save("deb", debated("g-1"))
	save("deb", debated("g-2"))
	none := taskRecord("g-3", 1)
	none.Debate = &DebateRec{Method: "r2", Points: []DebatePoint{}}
	save("deb", none)
	save("deb2", debated("g-2")) // taken from deb already
	var out, errOut bytes.Buffer
	run := func(args ...string) int {
		out.Reset()
		errOut.Reset()
		return runMain(context.Background(), append([]string{"rewrite"}, args...), &out, &errOut)
	}
	for _, args := range [][]string{
		{"-from", "deb", "-base", "base", "-run", "x", "-style", "long", "-expect", "2"},
		{"-from", "deb,deb2", "-base", "base", "-run", "x", "-style", "short", "-expect", "2"},
		{"-from", "deb,deb2", "-base", "base,base", "-run", "x", "-style", "short", "-expect", "3"},
	} {
		if c := run(args...); c != exitFlags {
			t.Errorf("%v: %d %s", args, c, errOut.String())
		}
	}
	if c := run("-from", "deb,deb2", "-base", "base,base", "-run", "x", "-style", "edit", "-expect", "2"); c != exitOK {
		t.Fatalf("edit: %d %s %s", c, out.String(), errOut.String())
	}
	if !strings.Contains(out.String(), "g-3          skipped: no debate in deb") || !strings.Contains(out.String(), "g-2          skipped: already taken") {
		t.Errorf("out:\n%s", out.String())
	}
	d, _ := runDir("x")
	recs, err := loadRecords(d)
	if err != nil || len(recs) != 2 {
		t.Fatalf("records %v %v", recs, err)
	}
	for _, r := range recs {
		if r.Synth.Text != "짧은 글 debate-e1" || r.Run.Synth != "debate-edit" || r.Run.From != "base" || r.Debate == nil || len(r.Answers) != 2 {
			t.Errorf("%s: %+v %+v", r.Question.ID, r.Synth, r.Run)
		}
	}
	if len(w.prompts) != 2 || !strings.Contains(w.prompts[0], "COMBINED BEFORE: \"전 글 g-1\"") {
		t.Errorf("prompts: %d\n%s", len(w.prompts), w.prompts[0])
	}
	if c := run("-from", "deb", "-base", "base", "-run", "x", "-style", "short", "-expect", "2"); c != exitFlags || !strings.Contains(errOut.String(), "exists") {
		t.Errorf("existing run: %d %s", c, errOut.String())
	}
}
