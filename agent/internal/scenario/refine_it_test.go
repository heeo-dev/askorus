package scenario

// IT-16 (docs/testing.md): question refining (질문 정제, milestone C)
// through the whole agent: the moderator fills in the question's spec,
// every AI is asked the question with its answer template, the writer gets
// the question as written, and the combined answer is synth-v5. Refining is
// off in the agent since 2026-11-04 (C4); these tests turn it on.

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/heeo-dev/askorus/agent/internal/compares"
	"github.com/heeo-dev/askorus/agent/internal/core"
	"github.com/heeo-dev/askorus/agent/internal/fakecli"
	"github.com/heeo-dev/askorus/agent/internal/judge"
)

// templateMark starts the answer template in a Korean question.
const templateMark = "답 형식 (Askorus이 붙임"

func callsOfKind(t *testing.T, kind string) []fakecli.Call {
	t.Helper()
	var out []fakecli.Call
	for _, c := range fakecli.Calls(t) {
		if kindOf(c) == kind {
			out = append(out, c)
		}
	}
	return out
}

// refineOn turns 질문 정제 on in the rig: it is off in the agent since
// 2026-11-04 (C4), and these tests keep its code working.
func refineOn(o *rigOptions) { o.refine = true }

func TestRefineAsksWithTheTemplateAndWritesV5(t *testing.T) {
	r := newRig(t, refineOn)
	r.addDevice()
	st := r.ask(nil, map[string]any{
		"refine":  map[string]any{"agent": "claude"},
		"compare": map[string]any{"synthesis": map[string]any{"agent": "claude"}},
	})
	if st.ComparisonID == "" || st.CompareError != nil {
		t.Fatalf("compare: %q %+v", st.ComparisonID, st.CompareError)
	}
	run := r.waitRun(st.ID)
	c, _ := r.waitComparison(st.ComparisonID)

	rf := run.Refine
	var want judge.QuestionSpec
	mustJSON(t, fakecli.RefineSpec, &want)
	if rf == nil || rf.Status != compares.RefineDone || rf.Agent != "claude" || rf.Spec == nil || *rf.Spec != want || rf.Template != judge.AnswerTemplate(want) {
		t.Fatalf("refine %+v", rf)
	}
	// One refine call, by the moderator, with the refine prompt and nothing of the question in its arguments.
	refines := callsOfKind(t, kindRefine)
	if len(refines) != 1 || refines[0].Role != fakecli.RoleClaude || !strings.HasPrefix(refines[0].Stdin, "[Askorus refine v1]") {
		t.Fatalf("refine calls %+v", refines)
	}
	noContentIn(t, "refine arguments", strings.Join(refines[0].Args, "\x00"))
	// Every AI got the question as written with the template under it.
	asks := callsOfKind(t, kindAsk)
	if len(asks) != 3 {
		t.Fatalf("%d asks", len(asks))
	}
	for _, a := range asks {
		if !strings.Contains(a.Stdin, question+"\n\n"+templateMark) || !a.At.After(refines[0].At) {
			t.Errorf("%s ask stdin lacks the template (or came before refining): %q", a.Role, a.Stdin)
		}
	}
	// The combined answer is synth-v5, which reads the question itself as
	// written, never the template: no SPEC line, no FORM.
	s := synthOf(t, c)
	writers := callsOfKind(t, kindSynth)
	if s.Status != compares.SynthDone || len(writers) != 1 {
		t.Fatalf("synth %+v, %d writer calls", s, len(writers))
	}
	if w := writers[0].Stdin; !strings.HasPrefix(w, "[Askorus synthesis v5.1]") || strings.Contains(w, "\nSPEC: ") || strings.Contains(w, "\nFORM. ") ||
		strings.Contains(w, templateMark) {
		t.Errorf("writer prompt is not synth-v5 with the question as written: %q", w[:min(len(w), 200)])
	}
}

func TestRefineThatFailsAsksTheQuestionAsWritten(t *testing.T) {
	r := newRig(t, refineOn)
	r.addDevice()
	t.Setenv(fakecli.EnvSynth, "claude="+fakecli.SynthProse)
	st := r.ask(nil, map[string]any{"refine": map[string]any{"agent": "claude"}})
	run := r.waitRun(st.ID)
	if rf := run.Refine; rf == nil || rf.Status != compares.RefineFailed || rf.Error == nil || rf.Error.Code != core.ErrCodeJudgeBadOutput || rf.Spec != nil {
		t.Fatalf("refine %+v", run.Refine)
	}
	if n := len(callsOfKind(t, kindRefine)); n != 2 {
		t.Fatalf("%d refine calls, want 2 (asked again once)", n)
	}
	for _, a := range callsOfKind(t, kindAsk) {
		if strings.Contains(a.Stdin, templateMark) || !strings.Contains(a.Stdin, question) {
			t.Errorf("%s was not asked the question as written", a.Role)
		}
	}
	for _, p := range run.Parts {
		if p.Status != "done" {
			t.Errorf("%s: %s", p.Agent, p.Status)
		}
	}
}

// The agent's default since 2026-11-04: a run that asks for refining (an
// older app does by default) is asked as written, with no moderator call,
// no refine in its snapshot and synth-v5 for the combined answer.
func TestRefineIsOffInTheAgent(t *testing.T) {
	r := newRig(t, nil)
	r.addDevice()
	st := r.ask(nil, map[string]any{
		"refine":  map[string]any{"agent": "claude"},
		"compare": map[string]any{"synthesis": map[string]any{"agent": "claude"}},
	})
	run := r.waitRun(st.ID)
	if run.Refine != nil {
		t.Fatalf("refine %+v", run.Refine)
	}
	if n := len(callsOfKind(t, kindRefine)); n != 0 {
		t.Fatalf("%d refine calls", n)
	}
	asks := callsOfKind(t, kindAsk)
	if len(asks) != 3 {
		t.Fatalf("%d asks", len(asks))
	}
	for _, a := range asks {
		if strings.Contains(a.Stdin, templateMark) || !strings.Contains(a.Stdin, question) {
			t.Errorf("%s was not asked the question as written", a.Role)
		}
	}
	c, _ := r.waitComparison(st.ComparisonID)
	s := synthOf(t, c)
	writers := callsOfKind(t, kindSynth)
	if s.Status != compares.SynthDone || len(writers) != 1 || !strings.HasPrefix(writers[0].Stdin, "[Askorus synthesis v5.1]") {
		t.Fatalf("synth %+v, %d writer calls", s, len(writers))
	}
	// A moderator name that is no AI's is not refused either: the field is ignored.
	r.waitRun(r.ask(nil, map[string]any{"refine": map[string]any{"agent": "no such AI!"}}).ID)
	if n := len(callsOfKind(t, kindRefine)); n != 0 {
		t.Fatalf("%d refine calls", n)
	}
}

func TestRefinePicksAnAskedModeratorAndIsOffUnlessAsked(t *testing.T) {
	r := newRig(t, refineOn)
	r.addDevice()
	// Codex is not asked: the first asked AI that may moderate does it.
	st := r.ask([]map[string]string{{"agent": "opencode"}, {"agent": "claude"}}, map[string]any{"refine": map[string]any{"agent": "codex"}})
	run := r.waitRun(st.ID)
	if rf := run.Refine; rf == nil || rf.Status != compares.RefineDone || rf.Agent != "opencode" {
		t.Fatalf("refine %+v", run.Refine)
	}
	if rs := callsOfKind(t, kindRefine); len(rs) != 1 || rs[0].Role != fakecli.RoleOpenCode {
		t.Fatalf("refine calls %+v", rs)
	}
	// No refine field, or off: no refining, the question as written.
	for _, extra := range []map[string]any{nil, {"refine": map[string]any{"off": true}}} {
		before := len(callsOfKind(t, kindRefine))
		st := r.ask(nil, extra)
		run := r.waitRun(st.ID)
		if run.Refine != nil || len(callsOfKind(t, kindRefine)) != before {
			t.Fatalf("%v: refine %+v", extra, run.Refine)
		}
	}
	// A moderator name that is no AI name is refused.
	if code, _ := r.call("POST", "/v1/runs", map[string]any{"mode": "ask", "prompt": question, "refine": map[string]any{"agent": "Claude Code"}}); code != 400 {
		t.Fatalf("bad moderator name: %d", code)
	}
}

func mustJSON(t *testing.T, s string, v any) {
	t.Helper()
	if err := json.Unmarshal([]byte(s), v); err != nil {
		t.Fatal(err)
	}
}
