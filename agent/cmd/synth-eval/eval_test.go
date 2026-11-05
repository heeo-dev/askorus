//go:build syntheval

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/asksemble/asksemble/agent/internal/judge"
)

func writeFile(t *testing.T, dir, name, body string) string {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

// useRoot points the runs at a temporary folder.
func useRoot(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	old := rootDir
	rootDir = func() (string, error) { return root, nil }
	t.Cleanup(func() { rootDir = old })
	return root
}

func TestQuestionsFileLinesCommentsAndRepeats(t *testing.T) {
	dir := t.TempDir()
	a := writeFile(t, dir, "general.txt", "\uFEFF# a set\n\nWhat is TLS?\n  How tall is Everest?  \n# skipped\nWhat is TLS?\n")
	b := writeFile(t, dir, "mine.txt", "커피는 하루에 몇 잔?\nHow tall is Everest?\n")
	qs, err := readQuestions([]string{a, b})
	if err != nil {
		t.Fatal(err)
	}
	got := []string{}
	for _, q := range qs {
		got = append(got, q.ID+"="+q.Text)
	}
	want := []string{"general-1=What is TLS?", "general-2=How tall is Everest?", "mine-1=커피는 하루에 몇 잔?"}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("got %v, want %v", got, want)
	}
	// IDs keep their numbers when a repeat is skipped (general-3 was a repeat).
	p, err := pick(qs, []string{"mine-1", "general-1"})
	if err != nil || len(p) != 2 || p[0].ID != "general-1" || p[1].ID != "mine-1" {
		t.Fatalf("pick = %v, %v", p, err)
	}
	if _, err := pick(qs, []string{"general-9"}); err == nil {
		t.Fatal("an unknown ID was taken")
	}
	empty := writeFile(t, dir, "empty.txt", "# nothing\n\n")
	if _, err := readQuestions([]string{empty}); err == nil {
		t.Fatal("a file without questions was taken")
	}
	long := writeFile(t, dir, "long.txt", strings.Repeat("가", maxQuestion+1)+"\n")
	if _, err := readQuestions([]string{long}); err == nil {
		t.Fatal("a question over the limit was taken")
	}
}

// fakePipeline answers from a table; stop makes a question hit a usage
// limit, cancel ends the context while a question is asked.
type fakePipeline struct {
	asked   []string
	stop    string
	cancel  string
	cancelF context.CancelFunc
}

func (f *fakePipeline) Versions(context.Context) map[string]string {
	return map[string]string{"claude": "2.1.316"}
}

func (f *fakePipeline) Ask(_ context.Context, q Question, _ RunInfo) (Record, error) {
	f.asked = append(f.asked, q.ID)
	if q.ID == f.cancel {
		f.cancelF()
		return Record{}, context.Canceled
	}
	rec := Record{
		Answers: []AnswerRec{{Agent: "claude", Status: "done", Ms: 4000, Text: "답"}, {Agent: "codex", Status: "done", Ms: 6000, Text: "답"}},
		Compare: &CompareRec{Status: "done", Ms: 20000},
		Synth:   &SynthRec{Status: "done", Agent: "claude", Ms: 9000, Text: "서울은 수도입니다. 인구는 약 940만 명입니다."},
	}
	if q.ID == f.stop {
		rec.Answers[1] = AnswerRec{Agent: "codex", Status: "failed", Error: "ask_failed", Detail: "You've hit your usage limit. Try again at 5pm."}
	}
	return rec, nil
}

func TestRunSavesEachQuestionStopsOnAUsageLimitAndResumes(t *testing.T) {
	useRoot(t)
	dir, _ := runDir("base")
	qs := []Question{{ID: "g-1", Text: "q1"}, {ID: "g-2", Text: "q2"}, {ID: "g-3", Text: "q3"}}
	run := RunInfo{Name: "base", Tier: "quick", Writer: "claude", Refine: "off"}
	now := func() time.Time { return time.Date(2026, 11, 3, 12, 0, 0, 0, time.UTC) }
	f := &fakePipeline{stop: "g-2"}
	var out bytes.Buffer
	if code := runQuestions(context.Background(), f, qs, run, dir, false, now, &out); code != exitUsageLimit {
		t.Fatalf("exit %d, want usage limit; %s", code, out.String())
	}
	if strings.Join(f.asked, ",") != "g-1,g-2" || !strings.Contains(out.String(), "-resume") {
		t.Fatalf("asked %v; %s", f.asked, out.String())
	}
	// Resume: g-1 is done, g-2 (stopped) and g-3 are asked.
	f2 := &fakePipeline{}
	out.Reset()
	if code := runQuestions(context.Background(), f2, qs, run, dir, true, now, &out); code != exitOK {
		t.Fatalf("exit %d; %s", code, out.String())
	}
	if strings.Join(f2.asked, ",") != "g-2,g-3" {
		t.Fatalf("resume asked %v", f2.asked)
	}
	rs, err := loadRecords(dir)
	if err != nil || len(rs) != 3 {
		t.Fatalf("records %d, %v", len(rs), err)
	}
	r := rs[1]
	if r.Seq != 2 || r.Question.ID != "g-2" || r.Stop != "" || r.Run != run || r.At != "2026-11-03T12:00:00Z" || r.Versions["claude"] != "2.1.316" {
		t.Fatalf("record %+v", r)
	}
}

func TestAnInterruptedQuestionIsNotSaved(t *testing.T) {
	useRoot(t)
	dir, _ := runDir("cut")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	f := &fakePipeline{cancel: "g-2", cancelF: cancel}
	qs := []Question{{ID: "g-1", Text: "q1"}, {ID: "g-2", Text: "q2"}}
	var out bytes.Buffer
	if code := runQuestions(ctx, f, qs, RunInfo{Name: "cut"}, dir, false, time.Now, &out); code != exitInterrupted {
		t.Fatalf("exit %d", code)
	}
	if done(dir, "g-2") || !done(dir, "g-1") {
		t.Fatal("the interrupted question was saved, or the first was not")
	}
}

// A record of a run from before C6 holds the judging's fields and the C5
// drafts' (written as the old code wrote them): it still loads, counts as
// done, its task still makes the product prompt, and a record written
// from it again leaves them out.
func TestARecordFromBeforeC6StillLoads(t *testing.T) {
	useRoot(t)
	dir, _ := runDir("e2-old")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	writeFile(t, dir, "q-g-1.json", `{
  "v": 1, "seq": 1,
  "question": {"id": "g-1", "file": "general.txt", "question": "상자의 가격은?"},
  "run": {"name": "e2-old", "tier": "quick", "writer": "claude", "refine": "off"},
  "at": "2026-11-04T12:00:00Z",
  "answers": [{"agent": "claude", "status": "done", "ms": 1000, "text": "가격은 1원입니다."}, {"agent": "codex", "status": "done", "ms": 2000, "text": "가격은 2원입니다."}],
  "compare": {"status": "done", "ms": 3000, "counts": {"differ": 1, "check": 1, "agree": 0, "onlyOne": 0, "notCompared": 0, "notJudged": 0}, "requests": 2, "failed": 0},
  "synth": {"status": "done", "agent": "claude", "ms": 4000, "judged": true, "unnamedSides": 1, "text": "old"},
  "synthTask": {"Question": "상자의 가격은?", "Earlier": null,
    "Answers": [{"AI": "Claude", "Text": "가격은 1원입니다."}, {"AI": "Codex", "Text": "가격은 2원입니다."}],
    "Differ": [{"Sides": [{"AIs": ["Claude"], "Says": ["가격은 1원입니다."]}, {"AIs": ["Codex"], "Says": ["가격은 2원입니다."]}], "NumbersDiffer": true}],
    "Check": [{"AI": "Codex", "Says": ["가격은 2원입니다."], "Why": ["unsure"]}],
    "Compared": true, "LeftOut": 0, "Spec": null},
  "label": {"status": "done", "sections": [{"text": "old"}], "labels": []},
  "topic": {"status": "done", "items": [{"key": "d1", "topic": "가격", "gists": ["1원", "2원"]}]},
  "differs": {"status": "done", "places": [{"topic": "가격", "sides": [{"ais": ["Claude"], "gist": "1원", "quote": "가격은 1원입니다.", "found": true}]}]}
}`)
	rs, err := loadRecords(dir)
	if err != nil || len(rs) != 1 || !done(dir, "g-1") {
		t.Fatalf("records %+v, %v", rs, err)
	}
	r := rs[0]
	if r.Compare.Ms != 3000 || r.Synth.Text != "old" || r.Task == nil || r.Task.Question != "상자의 가격은?" || len(r.Task.Answers) != 2 || r.Task.Answers[1].AI != "Codex" {
		t.Fatalf("record %+v, task %+v", r, r.Task)
	}
	if p := judge.SynthPrompt(*r.Task, "0123456789abcdef"); !strings.HasPrefix(p, "[Asksemble synthesis v5.1]") || strings.Contains(p, "DIFFER") {
		t.Fatalf("prompt:\n%s", p)
	}
	again, _ := runDir("e2-again")
	if err := saveRecord(again, r); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(recordFile(again, "g-1"))
	if err != nil {
		t.Fatal(err)
	}
	for _, old := range []string{"counts", "requests", "judged", "unnamedSides", "Differ", "Check", "Compared", "LeftOut", "label", "topic", "differs"} {
		if strings.Contains(string(b), `"`+old+`"`) {
			t.Errorf("the record written again keeps %q:\n%s", old, b)
		}
	}
}

func TestMetricsShapeLatinExtrasAndTimes(t *testing.T) {
	text := "## 결론\n\n서울 인구는 약 940만 명입니다. Claude와 Codex가 다르게 말합니다.\n\n| AI | 인구 |\n| --- | --- |\n| {{Claude}} | 940만 |\n\n- 출처는 `API` 문서 https://example.com/x\n\n다음에 쓸 문장:\n- 부산 인구는?\n"
	r := Record{
		Question: Question{ID: "k-1", Text: "서울 인구는?"},
		Answers:  []AnswerRec{{Status: "done", Ms: 5000, Text: "답"}, {Status: "failed"}},
		Compare:  &CompareRec{Ms: 21000},
		Synth:    &SynthRec{Status: "done", Ms: 8000, Text: text},
	}
	m := metricsOf(r)
	if m.Answered != 1 || m.Asked != 2 || m.AskMs != 5000 || m.CompareMs != 21000 || m.TotalMs != 34000 || !m.SynthOK {
		t.Fatalf("times %+v", m)
	}
	if !m.Table || m.Headings != 1 || !m.StartsHeading || m.Bullets != 2 || !m.Korean {
		t.Fatalf("shape %+v", m)
	}
	if m.FirstSentence != len([]rune("서울 인구는 약 940만 명입니다.")) {
		t.Fatalf("first sentence %d", m.FirstSentence)
	}
	// Names, code and links are not English mixed in; nothing else is Latin here.
	if m.LatinShare != 0 {
		t.Fatalf("latin %v", m.LatinShare)
	}
	if !m.Extras {
		t.Fatal("the closing 'next' block was not seen")
	}
	if got := latinShare("서울 latency 지연"); got <= 0.3 || got >= 0.7 {
		t.Fatalf("latin share %v", got)
	}
	// Refined: the parts waited for the spec, so their time holds it; it is counted once.
	r.Refine = &RefineRec{Status: "done", Ms: 3000, Kind: "number", Depth: "short"}
	if m := metricsOf(r); m.AskMs != 2000 || m.RefineMs != 3000 || m.TotalMs != 34000 || m.Kind != "number" {
		t.Fatalf("refined times %+v", m)
	}
	r.Refine = nil
	// No combined answer: only the times.
	r.Synth = &SynthRec{Status: "failed", Error: "judge_timeout"}
	if m := metricsOf(r); m.SynthOK || m.Chars != 0 || m.SynthStatus != "failed" {
		t.Fatalf("failed synth %+v", m)
	}
}

func TestReportSideBySideAndJSON(t *testing.T) {
	mk := func(refine string, chars int) []Record {
		return []Record{{
			V: 1, Seq: 1, Question: Question{ID: "g-1", Text: "q"}, Run: RunInfo{Refine: refine, Tier: "quick"},
			Answers: []AnswerRec{{Status: "done", Ms: 3000}},
			Compare: &CompareRec{Ms: 10000},
			Synth:   &SynthRec{Status: "done", Ms: 5000, Text: strings.Repeat("a", chars)},
		}, {V: 1, Seq: 2, Question: Question{ID: "g-2", Text: "q2"}, Stop: "usage_limit"}}
	}
	var out bytes.Buffer
	writeReport(&out, []string{"base", "refined"}, [][]Record{mk("off", 100), mk("on", 60)})
	s := out.String()
	for _, want := range []string{"base", "refined", "off · quick", "questions (stopped)", "2 (1)", "median length (chars)", "== base", "g-2          stopped: usage_limit"} {
		if !strings.Contains(s, want) {
			t.Fatalf("report lacks %q:\n%s", want, s)
		}
	}
	out.Reset()
	if err := writeReportJSON(&out, []string{"base"}, [][]Record{mk("off", 100)}); err != nil {
		t.Fatal(err)
	}
	var v []struct {
		Summary   Summary   `json:"summary"`
		Questions []Metrics `json:"questions"`
	}
	if err := json.Unmarshal(out.Bytes(), &v); err != nil || v[0].Summary.MedianChars != 100 || v[0].Summary.MedianTotal != 18 || len(v[0].Questions) != 2 {
		t.Fatalf("json %v %+v", err, v)
	}
}

func TestRunFlagsGuardTheCalls(t *testing.T) {
	useRoot(t)
	dir := t.TempDir()
	q := writeFile(t, dir, "general.txt", "q1\nq2\n")
	made := false
	old := newPipeline
	newPipeline = func(context.Context) (pipeline, func(), error) { made = true; return nil, nil, errors.New("no") }
	t.Cleanup(func() { newPipeline = old })
	oldE := selfElevated
	selfElevated = func() bool { return false }
	t.Cleanup(func() { selfElevated = oldE })
	run := func(args ...string) (int, string) {
		var out, errOut bytes.Buffer
		code := runMain(context.Background(), args, &out, &errOut)
		return code, out.String() + errOut.String()
	}
	for _, c := range []struct {
		args []string
		want string
	}{
		{[]string{"run", "-q", q, "-run", "base"}, "give -expect 2"},
		{[]string{"run", "-q", q, "-run", "base", "-expect", "3"}, "give -expect 2"},
		{[]string{"run", "-q", q, "-run", "base", "-expect", "2", "-refine", "maybe"}, "-refine must be off, on, light or live"},
		{[]string{"run", "-q", q, "-run", "base", "-expect", "2", "-mod-tier", "max"}, "-mod-tier must be quick, standard or deep"},
		{[]string{"run", "-q", q, "-run", "base", "-expect", "2", "-synth", "v4"}, "-synth must be product or off"},
		{[]string{"run", "-q", q, "-run", "Base", "-expect", "2"}, "run name"},
		{[]string{"run", "-q", q, "-run", "base", "-expect", "2", "-tier", "fast"}, "-tier"},
		{[]string{"run", "-q", q, "-run", "base", "-expect", "2", "-writer", "gemini"}, "-writer"},
	} {
		code, s := run(c.args...)
		if code != exitFlags || !strings.Contains(s, c.want) || made {
			t.Fatalf("%v: exit %d %q (pipeline made %v)", c.args, code, s, made)
		}
	}
	selfElevated = func() bool { return true }
	if code, s := run("run", "-q", q, "-run", "base", "-expect", "2"); code != exitError || !strings.Contains(s, "normal rights") || made {
		t.Fatalf("elevated: exit %d %q", code, s)
	}
	selfElevated = func() bool { return false }
	if code, _ := run("run", "-q", q, "-run", "base", "-expect", "2"); code != exitError || !made {
		t.Fatalf("a run that passes the checks did not make the pipeline (exit %d)", code)
	}
	code, s := run("questions", "-q", q)
	if code != exitOK || !strings.Contains(s, "general-2    q2") || !strings.Contains(s, "up to 8 times") {
		t.Fatalf("questions: %d %q", code, s)
	}
}
