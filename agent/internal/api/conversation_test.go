package api

import (
	"strings"
	"testing"

	"github.com/heeo-dev/askorus/agent/internal/compares"
	"github.com/heeo-dev/askorus/agent/internal/core"
	"github.com/heeo-dev/askorus/agent/internal/judge"
)

// The conversation record (#29): context on /v1/runs, /v1/comparisons and
// /v1/debates, and /v1/info's context.

func contextBody() map[string]any {
	return map[string]any{
		"earlier": []string{"EARLIER-SECRET 첫 질문"},
		"settled": []map[string]string{{"topic": "TOPIC-SECRET", "result": "agreed", "line": "LINE-SECRET 8시입니다."}},
		"last":    "LAST-SECRET 첫 배는 8시입니다.",
	}
}

// A follow-up's record goes before the question of every AI asked, with
// the earlier questions for an AI without a session; its combined answer
// is synth-v6. Logs carry counts only.
func TestRunContext(t *testing.T) {
	cl, cx := newWriterAdapter("claude", false), newWriterAdapter("codex", false)
	e, _ := judgeEnv(t, nil, cl, cx)
	st := startRun(t, e, map[string]any{
		"mode": "ask", "prompt": "QUESTION-SECRET 마지막 배는?",
		"agents":  []map[string]string{{"agent": "claude", "session": "claude:s-1"}, {"agent": "codex"}},
		"compare": map[string]any{"judges": []string{"claude", "codex"}, "synthesis": map[string]string{"agent": "claude"}},
		"context": contextBody(),
	})
	e.waitRun(t, st.ID)
	c := e.waitSynthesis(t, st.ComparisonID)
	if c.Synthesis == nil || c.Synthesis.Status != compares.SynthDone {
		t.Fatalf("comparison = %+v", c.Comparison)
	}
	known, fresh := cl.requests(), cx.requests()
	if len(known) != 1 || len(fresh) != 1 {
		t.Fatalf("asks: %d, %d", len(known), len(fresh))
	}
	for _, p := range []string{known[0].Prompt, fresh[0].Prompt} {
		if !strings.HasPrefix(p, core.AskPreamble) || !strings.HasSuffix(p, "The new question:\nQUESTION-SECRET 마지막 배는?") ||
			!strings.Contains(p, `SETTLED: {"topic":"TOPIC-SECRET","result":"agreed","line":"LINE-SECRET 8시입니다."}`) ||
			!strings.Contains(p, `PREVIOUS QUESTION: "EARLIER-SECRET 첫 질문"`) || !strings.Contains(p, `PREVIOUS ANSWER: "LAST-SECRET 첫 배는 8시입니다."`) {
			t.Errorf("ask prompt:\n%s", p)
		}
	}
	if strings.Contains(known[0].Prompt, "EARLIER QUESTIONS") || !strings.Contains(fresh[0].Prompt, `EARLIER QUESTIONS (context only, do not answer them): ["EARLIER-SECRET 첫 질문"]`) {
		t.Errorf("earlier questions: claude (a session) %v, codex (none) %v", strings.Contains(known[0].Prompt, "EARLIER"), strings.Contains(fresh[0].Prompt, "EARLIER"))
	}
	synths := cl.synthCalls()
	if len(synths) != 1 || !strings.HasPrefix(synths[0].Prompt, "[Askorus synthesis v6]") || !strings.Contains(synths[0].Prompt, "LAST-SECRET") {
		t.Fatalf("synthesis: %+v", synths)
	}
	e.idle(t, st.ComparisonID)
	logs := e.logs.String()
	for _, leak := range []string{"SECRET", "마지막"} {
		if strings.Contains(logs, leak) {
			t.Errorf("log has %q: %s", leak, logs)
		}
	}
	for _, line := range []string{`"settled":1`, `"lastBytes":`, `"rules":"synth-v6"`} {
		if !strings.Contains(logs, line) {
			t.Errorf("log lacks %s: %s", line, logs)
		}
	}
}

// Without a record, or with earlier questions alone, every prompt is as
// before.
func TestRunContextNone(t *testing.T) {
	for name, ctx := range map[string]any{
		"none":          nil,
		"earlier alone": map[string]any{"earlier": []string{"q0"}},
		"blank":         map[string]any{"settled": []map[string]string{{"topic": " ", "result": "agreed", "line": "x"}}, "last": "  "},
	} {
		cl := newJudge("claude")
		e, _ := judgeEnv(t, nil, cl)
		body := map[string]any{"mode": "ask", "prompt": "q", "agents": []map[string]string{{"agent": "claude"}}}
		if ctx != nil {
			body["context"] = ctx
		}
		w := e.post("/v1/runs", body)
		if w.Code != 202 {
			t.Fatalf("%s: POST = %d %s", name, w.Code, w.Body)
		}
		e.waitRun(t, decodeBody[RunStatus](t, w).ID)
		if reqs := cl.requests(); len(reqs) != 1 || reqs[0].Prompt != core.AskPrompt("q") {
			t.Errorf("%s: prompt %+v", name, reqs)
		}
	}
}

// A context over a limit, or not text, is a 400 on each route, and
// nothing is asked.
func TestContextRefused(t *testing.T) {
	long := func(n int) string { return strings.Repeat("가", n/3+1) }
	settled := func(topic, result, line string) []map[string]string {
		return []map[string]string{{"topic": topic, "result": result, "line": line}}
	}
	var many []map[string]string
	for range judge.MaxSettled + 1 {
		many = append(many, settled("t", "agreed", "l")[0])
	}
	bad := map[string]map[string]any{
		"13 settled":     {"settled": many},
		"bad result":     {"settled": settled("t", "won", "l")},
		"long topic":     {"settled": settled(long(judge.MaxSettledTopic), "agreed", "l")},
		"long line":      {"settled": settled("t", "agreed", long(judge.MaxSettledLine))},
		"long last":      {"last": long(judge.MaxLastAnswerBytes)},
		"NUL":            {"last": "a\x00b"},
		"4 earlier":      {"earlier": []string{"a", "b", "c", "d"}, "last": "x"},
		"long earlier":   {"earlier": []string{long(judge.MaxEarlierBytes)}, "last": "x"},
		"not an object":  nil,
		"settled string": {"settled": "x"},
	}
	cl, cx := debateAdapters()
	e, _ := judgeEnv(t, nil, cl, cx)
	for name, ctx := range bad {
		var c any = ctx
		if name == "not an object" {
			c = "x"
		}
		run := map[string]any{"mode": "ask", "prompt": "q", "context": c}
		cmp := map[string]any{"question": "q", "answers": []map[string]string{{"id": "a", "agent": "claude", "via": "pc", "text": "x"}, {"id": "b", "agent": "codex", "via": "pc", "text": "y"}}, "context": c}
		deb := debateBody("d-" + name)
		deb["context"] = c
		for route, body := range map[string]map[string]any{"/v1/runs": run, "/v1/comparisons": cmp, "/v1/debates": deb} {
			if w := e.post(route, body); w.Code != 400 || !strings.Contains(w.Body.String(), CodeBadRequest) {
				t.Errorf("%s %s = %d %s", name, route, w.Code, w.Body)
			}
		}
	}
	if len(cl.requests())+len(cx.requests())+len(cl.synthCalls())+len(cx.synthCalls()) != 0 {
		t.Error("a refused request asked an AI")
	}
	// The longest context fits every body.
	var full []map[string]string
	for range judge.MaxSettled {
		full = append(full, settled(strings.Repeat("\n", judge.MaxSettledTopic), "agreed", strings.Repeat("\n", judge.MaxSettledLine))[0])
	}
	longest := map[string]any{
		"earlier": []string{strings.Repeat("\n", judge.MaxEarlierBytes), strings.Repeat("\n", judge.MaxEarlierBytes), strings.Repeat("\n", judge.MaxEarlierBytes)},
		"settled": full, "last": strings.Repeat("\n", judge.MaxLastAnswerBytes),
	}
	if w := e.post("/v1/runs", map[string]any{"mode": "ask", "prompt": strings.Repeat("\n", maxPrompt-1) + "q", "context": longest}); w.Code != 202 {
		t.Errorf("the longest context: %d %s", w.Code, w.Body)
	}
}

// A comparison made again and a debate take the record: synth-v6, and
// debate-plan v3 with debate-v5.
func TestComparisonAndDebateContext(t *testing.T) {
	cl, cx := debateAdapters()
	e, _ := judgeEnv(t, nil, cl, cx)
	w := e.post("/v1/comparisons", map[string]any{
		"clientComparisonId": "c1", "question": "q",
		"answers":   []map[string]string{{"id": "a", "agent": "claude", "via": "pc", "model": "claude-std", "text": "x"}, {"id": "b", "agent": "codex", "via": "pc", "model": "codex-std", "text": "y"}},
		"synthesis": map[string]string{"agent": "codex"}, "context": contextBody(),
	})
	if w.Code != 202 {
		t.Fatalf("POST comparison = %d %s", w.Code, w.Body)
	}
	c := decodeBody[ComparisonStatus](t, w)
	if got := e.waitSynthesis(t, c.ID); got.Synthesis == nil || got.Synthesis.Status != compares.SynthDone {
		t.Fatalf("comparison = %+v", got.Comparison)
	}
	e.idle(t, c.ID)
	if s := cx.synthCalls(); len(s) != 1 || !strings.HasPrefix(s[0].Prompt, "[Askorus synthesis v6]") {
		t.Errorf("combined answer: %+v", s)
	}
	body := debateBody("d1")
	body["context"] = contextBody()
	w = e.post("/v1/debates", body)
	if w.Code != 202 {
		t.Fatalf("POST debate = %d %s", w.Code, w.Body)
	}
	if st, b := e.waitDebate(t, decodeBody[DebateStatus](t, w).ID); st.Status != compares.DebateDone {
		t.Fatalf("debate = %s", b)
	}
	var plan, after bool
	for _, s := range cl.synthCalls() {
		plan = plan || (strings.HasPrefix(s.Prompt, "[Askorus debate-plan v3]") && strings.Contains(s.Prompt, "TOPIC-SECRET") && !strings.Contains(s.Prompt, "LAST-SECRET"))
		after = after || (strings.HasPrefix(s.Prompt, "[Askorus synthesis debate-v5]") && strings.Contains(s.Prompt, "LAST-SECRET"))
	}
	if !plan || !after {
		t.Errorf("plan v3 %v, after v5 %v", plan, after)
	}
	if logs := e.logs.String(); strings.Contains(logs, "SECRET") || !strings.Contains(logs, `"rules":"debate-v5"`) {
		t.Errorf("logs: %s", logs)
	}
}

func TestInfoContext(t *testing.T) {
	e := newEnv(t, &askAdapter{name: "claude", available: true}, nil)
	resp, body := infoOf(t, e)
	if c := resp.Context; c == nil || c.MaxSettled != 12 || c.MaxLastBytes != 8192 || !strings.Contains(body, `"context":{"maxSettled":12,"maxLastBytes":8192}`) {
		t.Errorf("info = %s", body)
	}
}
