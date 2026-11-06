package api

import (
	"strings"
	"testing"

	"github.com/asksemble/asksemble/agent/internal/core"
	"github.com/asksemble/asksemble/agent/internal/judge"
)

// Asking the moderator (#25): POST /v1/runs with moderator, and /v1/info's
// moderator.

// A question to the moderator goes to that AI alone, as a one-off in the
// judge folder, with the record and the previous answers in its prompt;
// agents and compare are not read, and logs carry counts only.
func TestRunModerator(t *testing.T) {
	cl, cx := newWriterAdapter("claude", false), newWriterAdapter("codex", false)
	e, ask := judgeEnv(t, nil, cl, cx)
	st := startRun(t, e, map[string]any{
		"mode": "ask", "prompt": "QUESTION-SECRET AI들이 뭐가 달랐지?", "tier": "quick",
		"agents":  []map[string]string{{"agent": "claude", "session": "claude:s-1"}, {"agent": "codex"}},
		"compare": map[string]any{"judges": []string{"claude", "codex"}, "synthesis": map[string]string{"agent": "claude"}},
		"context": contextBody(),
		"moderator": map[string]any{
			"agent": "codex", "model": "codex-std", "effort": "high",
			"answers": []map[string]string{{"agent": "claude", "text": "ANSWER-SECRET 7시"}, {"agent": "codex", "text": "ANSWER-SECRET 8시"}, {"agent": "chatgpt", "text": "ANSWER-SECRET 9시"}},
		},
	})
	if st.ComparisonID != "" || st.CompareError != nil {
		t.Errorf("a moderator ask was compared: %+v", st)
	}
	run := e.waitRun(t, st.ID)
	if len(run.Parts) != 1 || run.Parts[0].Agent != "codex" || run.Parts[0].Session != "" {
		t.Fatalf("parts: %+v", run.Parts)
	}
	if reqs := cl.requests(); len(reqs) != 0 {
		t.Errorf("claude was asked: %+v", reqs)
	}
	reqs := cx.requests()
	if len(reqs) != 1 {
		t.Fatalf("codex asks: %d", len(reqs))
	}
	r := reqs[0]
	if r.Folder != core.JudgeFolder(ask) || !r.OneOff || r.Resume != "" || r.Model != "codex-std" || r.Effort != "high" || r.Tier != "quick" {
		t.Errorf("request: folder %q oneOff %v resume %q model %q effort %q tier %q", r.Folder, r.OneOff, r.Resume, r.Model, r.Effort, r.Tier)
	}
	for _, part := range []string{
		core.AskPreamble, "[Asksemble moderator-ask v1]",
		`EARLIER QUESTIONS (context only, do not answer them): ["EARLIER-SECRET 첫 질문"]`,
		`SETTLED: {"topic":"TOPIC-SECRET"`, `PREVIOUS ANSWER: "LAST-SECRET`,
		`ANSWER: {"ai":"Claude","text":"ANSWER-SECRET 7시"}`, `ANSWER: {"ai":"Codex","text":"ANSWER-SECRET 8시"}`, `ANSWER: {"ai":"ChatGPT","text":"ANSWER-SECRET 9시"}`,
	} {
		if !strings.Contains(r.Prompt, part) {
			t.Errorf("prompt lacks %q", part)
		}
	}
	if !strings.HasSuffix(r.Prompt, "The new question:\nQUESTION-SECRET AI들이 뭐가 달랐지?") {
		t.Errorf("prompt ends %q", r.Prompt[max(0, len(r.Prompt)-80):])
	}
	logs := e.logs.String()
	if strings.Contains(logs, "SECRET") || !strings.Contains(logs, `"moderator":true`) || !strings.Contains(logs, `"modItems":3`) {
		t.Errorf("logs: %s", logs)
	}
}

// A moderator over a limit, or an AI that cannot answer, is refused, and
// nothing is asked.
func TestRunModeratorRefused(t *testing.T) {
	cl, cx := newWriterAdapter("claude", false), newWriterAdapter("codex", false)
	e, _ := judgeEnv(t, nil, cl, cx)
	answers := func(n int, text string) []map[string]string {
		var out []map[string]string
		for range n {
			out = append(out, map[string]string{"agent": "claude", "text": text})
		}
		return out
	}
	for name, c := range map[string]struct {
		mod  map[string]any
		code string
	}{
		"no agent":            {map[string]any{"answers": answers(1, "a")}, CodeBadRequest},
		"seven answers":       {map[string]any{"agent": "claude", "answers": answers(7, "a")}, CodeBadRequest},
		"long answer":         {map[string]any{"agent": "claude", "answers": answers(1, strings.Repeat("a", judge.MaxModeratorAnswerBytes+1))}, CodeBadRequest},
		"NUL":                 {map[string]any{"agent": "claude", "answers": answers(1, "a\x00b")}, CodeBadRequest},
		"effort, no model":    {map[string]any{"agent": "claude", "effort": "high"}, CodeBadRequest},
		"bad answer agent":    {map[string]any{"agent": "claude", "answers": []map[string]string{{"agent": "Bad Name", "text": "a"}}}, CodeBadRequest},
		"unknown moderator":   {map[string]any{"agent": "gemini"}, CodeAgentNoAsk},
		"not an object field": {map[string]any{"agent": "claude", "answers": "x"}, CodeBadRequest},
	} {
		w := e.post("/v1/runs", map[string]any{"mode": "ask", "prompt": "q", "moderator": c.mod})
		if w.Code != 400 || !strings.Contains(w.Body.String(), c.code) {
			t.Errorf("%s = %d %s", name, w.Code, w.Body)
		}
	}
	if len(cl.requests())+len(cx.requests()) != 0 {
		t.Error("a refused moderator ask asked an AI")
	}
}

func TestInfoModerator(t *testing.T) {
	cl, cx := newWriterAdapter("claude", false), newWriterAdapter("codex", false)
	e, _ := judgeEnv(t, nil, cl, cx)
	resp, body := infoOf(t, e)
	if m := resp.Moderator; m == nil || m.MaxAnswers != 6 || m.MaxAnswerBytes != 8192 || m.Rules != "moderator-ask-v1" {
		t.Errorf("info = %s", body)
	}
	e2 := newEnv(t, &askAdapter{name: "claude", available: true}, nil)
	if resp, body := infoOf(t, e2); resp.Moderator != nil || strings.Contains(body, `"moderator"`) {
		t.Errorf("no ask folder: %s", body)
	}
}
