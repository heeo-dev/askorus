package api

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/asksemble/asksemble/agent/internal/compares"
	"github.com/asksemble/asksemble/agent/internal/core"
	"github.com/asksemble/asksemble/agent/internal/judge"
)

// /v1/debates (P2-5, #26) and /v1/info's debate.

// debateWriter moderates by the prompt: a plan with one debated point
// (Claude, then Codex), DONE | agreed for every check and AFTER-TEXT after
// the debate.
type debateWriter struct{ *writerAdapter }

const apiPlan = "POINT | p1 | fact | yes | 시간\nSIDE | p1 | Claude | 7시\nSIDE | p1 | Codex | 8시\nTURN | p1 | Claude | 근거는?\nTURN | p1 | Codex | 출처는?"

func (w debateWriter) Synthesize(ctx context.Context, req core.SynthRequest) (core.SynthResult, error) {
	res, err := w.writerAdapter.Synthesize(ctx, req)
	if err != nil {
		return res, err
	}
	answer := "AFTER-TEXT"
	switch {
	case strings.HasPrefix(req.Prompt, "[Asksemble debate-plan v2]"):
		answer = apiPlan
	case strings.HasPrefix(req.Prompt, "[Asksemble debate-check v1]"):
		answer = "DONE | agreed | 8시입니다."
	}
	res.Output, _ = json.Marshal(map[string]string{"answer": answer})
	return res, nil
}

func debateBody(key string) map[string]any {
	return map[string]any{
		"clientDebateId": key, "question": "QUESTION-SECRET 첫 배는?", "synthesis": "COMBINED-SECRET AI마다 다릅니다.",
		"answers": []map[string]string{
			{"agent": "claude", "via": "pc", "model": "claude-std", "text": "ANSWER-SECRET 7시"},
			{"agent": "codex", "via": "pc", "model": "codex-std", "text": "ANSWER-SECRET 8시"},
		},
		"moderator": "claude", "tier": "standard",
	}
}

func (e env) debate(t *testing.T, id string) (DebateStatus, string) {
	t.Helper()
	w := e.do("GET", "/v1/debates/"+id, nil)
	if w.Code != 200 {
		t.Fatalf("GET debate = %d %s", w.Code, w.Body)
	}
	return decodeBody[DebateStatus](t, w), w.Body.String()
}

// waitDebate polls like the phone until the debate has ended.
func (e env) waitDebate(t *testing.T, id string) (DebateStatus, string) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for {
		st, body := e.debate(t, id)
		switch st.Status {
		case compares.DebateDone, compares.DebateStopped, compares.DebateFailed:
			if st.PollAfterMs != 0 {
				t.Errorf("ended debate with pollAfterMs %d", st.PollAfterMs)
			}
			return st, body
		}
		if st.PollAfterMs != compares.PollMs {
			t.Fatalf("%s debate with pollAfterMs %d", st.Status, st.PollAfterMs)
		}
		if time.Now().After(deadline) {
			t.Fatalf("debate still %s", st.Status)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func debateAdapters() (debateWriter, debateWriter) {
	return debateWriter{newWriterAdapter("claude", false)}, debateWriter{newWriterAdapter("codex", false)}
}

func TestDebateAPI(t *testing.T) {
	cl, cx := debateAdapters()
	e, ask := judgeEnv(t, nil, cl, cx)
	w := e.post("/v1/debates", debateBody("d1"))
	if w.Code != 202 {
		t.Fatalf("POST = %d %s", w.Code, w.Body)
	}
	start := decodeBody[DebateStatus](t, w)
	if !comparisonIDRe.MatchString(start.ID) || start.PollAfterMs != compares.PollMs || start.Status != compares.DebatePlanning {
		t.Fatalf("start = %s", w.Body)
	}
	st, body := e.waitDebate(t, start.ID)
	if st.Status != compares.DebateDone || st.After == nil || st.After.Text != "AFTER-TEXT" || st.Moderator != "claude" || st.Calls != 5 {
		t.Fatalf("debate = %s", body)
	}
	for _, field := range []string{`"debateId":`, `"points":[{"id":"p1"`, `"sides":[{"ais":["claude"],"says":"7시"}`, `"turns":[{"ai":"claude"`, `"status":"done"`, `"result":"agreed"`, `"after":{"text":"AFTER-TEXT","agent":"claude"`, `"tier":"standard"`} {
		if !strings.Contains(body, field) {
			t.Errorf("body lacks %s: %s", field, body)
		}
	}
	// The debaters asked in the judge folder.
	for _, a := range []debateWriter{cl, cx} {
		reqs := a.requests()
		if len(reqs) != 1 || reqs[0].Folder != core.JudgeFolder(ask) || !strings.Contains(reqs[0].Prompt, "[Asksemble debate-turn v1]") {
			t.Errorf("%s asks: %+v", a.name, reqs)
		}
	}
	// The same clientDebateId gives the same debate; another device none.
	if again := decodeBody[DebateStatus](t, e.post("/v1/debates", debateBody("d1"))); again.ID != start.ID {
		t.Errorf("again = %s", again.ID)
	}
	if w := e.do("GET", "/v1/debates/"+start.ID, map[string]string{"Authorization": "Bearer " + e.addDevice(t, "tablet")}); w.Code != 404 || !strings.Contains(w.Body.String(), CodeDebateNotFound) {
		t.Errorf("another device = %d %s", w.Code, w.Body)
	}
	// Stopping an ended debate gives it as it is.
	if w := e.post("/v1/debates/"+start.ID+"/stop", nil); w.Code != 200 || decodeBody[DebateStatus](t, w).Status != compares.DebateDone {
		t.Errorf("stop after = %d %s", w.Code, w.Body)
	}
	if w := e.do("GET", "/v1/debates/nope", nil); w.Code != 404 {
		t.Errorf("unknown = %d", w.Code)
	}
	if w := e.do("GET", "/v1/debates", nil); w.Code != 405 {
		t.Errorf("GET /v1/debates = %d", w.Code)
	}
	// The log has no text of the debate.
	logs := e.logs.String()
	for _, secret := range []string{"QUESTION-SECRET", "ANSWER-SECRET", "COMBINED-SECRET", "AFTER-TEXT", "says SECRET"} {
		if strings.Contains(logs, secret) {
			t.Errorf("log has %s", secret)
		}
	}
}

func TestDebateAPIStopAndBusy(t *testing.T) {
	cl, cx := debateAdapters()
	cl.askGate = make(chan struct{})
	t.Cleanup(func() { close(cl.askGate) })
	e, _ := judgeEnv(t, nil, cl, cx)
	start := decodeBody[DebateStatus](t, e.post("/v1/debates", debateBody("d1")))
	for deadline := time.Now().Add(10 * time.Second); ; time.Sleep(5 * time.Millisecond) {
		st, _ := e.debate(t, start.ID)
		if len(st.Points) == 1 && len(st.Points[0].Turns) == 1 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("no turn: %+v", st)
		}
	}
	// One debate at a time.
	if w := e.post("/v1/debates", debateBody("d2")); w.Code != 429 || !strings.Contains(w.Body.String(), CodeTooManyDebates) {
		t.Errorf("a second debate = %d %s", w.Code, w.Body)
	}
	w := e.post("/v1/debates/"+start.ID+"/stop", nil)
	st := decodeBody[DebateStatus](t, w)
	if w.Code != 200 || st.Status != compares.DebateStopped || st.PollAfterMs != 0 || st.Points[0].Turns[0].Status != compares.TurnFailed {
		t.Errorf("stop = %d %s", w.Code, w.Body)
	}
	if got, _ := e.waitDebate(t, start.ID); got.Status != compares.DebateStopped || got.After != nil {
		t.Errorf("after stop = %+v", got)
	}
	if w := e.post("/v1/debates", debateBody("d2")); w.Code != 202 {
		t.Errorf("after the stop = %d %s", w.Code, w.Body)
	}
}

func TestDebateAPIRefused(t *testing.T) {
	cl, cx := debateAdapters()
	e, _ := judgeEnv(t, nil, cl, cx)
	bad := func(name string, mutate func(map[string]any), code int, want string) {
		t.Helper()
		b := debateBody("")
		mutate(b)
		w := e.post("/v1/debates", b)
		if w.Code != code || !strings.Contains(w.Body.String(), want) {
			t.Errorf("%s = %d %s", name, w.Code, w.Body)
		}
	}
	bad("one answer", func(b map[string]any) { b["answers"] = b["answers"].([]map[string]string)[:1] }, 400, core.ErrCodeNeedTwoAnswers)
	bad("no combined", func(b map[string]any) { b["synthesis"] = "" }, 400, CodeBadRequest)
	bad("bad tier", func(b map[string]any) { b["tier"] = "max" }, 400, CodeBadRequest)
	bad("unknown moderator", func(b map[string]any) { b["moderator"] = "gemini" }, 400, CodeBadRequest)
	// 11-06: the moderator model goes with the moderator it was set for.
	bad("model without moderator", func(b map[string]any) { delete(b, "moderator"); b["moderatorModel"] = "claude-deep" }, 400, "moderatorModel")
	bad("bad effort", func(b map[string]any) { b["moderatorModel"] = "claude-deep"; b["moderatorEffort"] = "HIGH" }, 400, "moderatorModel")
	bad("big combined", func(b map[string]any) { b["synthesis"] = strings.Repeat("a", compares.MaxCombinedBytes+1) }, 413, CodeBadRequest)
	bad("long id", func(b map[string]any) { b["clientDebateId"] = strings.Repeat("k", maxClientID+1) }, 400, CodeBadRequest)
	if w := e.postRaw(e.token, "/v1/debates", "{"); w.Code != 400 {
		t.Errorf("bad JSON = %d", w.Code)
	}
	if w := e.postRaw(e.token, "/v1/debates", `{"synthesis":"`+strings.Repeat("a", maxDebateBody)+`"}`); w.Code != 413 {
		t.Errorf("huge body = %d", w.Code)
	}
	// Without an ask folder there is no debate.
	e2 := newEnv(t, cl, func(o *Options) { o.Adapters = []core.Adapter{cl, cx}; o.JudgeMinVersions = judgeMins })
	if w := e2.post("/v1/debates", debateBody("")); w.Code != 503 || !strings.Contains(w.Body.String(), CodeAskFolder) {
		t.Errorf("no ask folder = %d %s", w.Code, w.Body)
	}
}

func TestInfoDebate(t *testing.T) {
	cl, cx := debateAdapters()
	e, _ := judgeEnv(t, nil, cl, cx)
	resp, body := infoOf(t, e)
	if d := resp.Debate; d == nil || d.MaxPoints != judge.MaxDebated || d.MaxTurns != 4 || d.Rules != judge.DebateRules {
		t.Errorf("info = %s", body)
	}
	e2 := newEnv(t, cl, nil)
	if resp, body := infoOf(t, e2); resp.Debate != nil || strings.Contains(body, `"debate"`) {
		t.Errorf("no ask folder: %s", body)
	}
}
