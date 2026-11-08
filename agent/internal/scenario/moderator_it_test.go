package scenario

import (
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/heeo-dev/askorus/agent/internal/fakecli"
	"github.com/heeo-dev/askorus/agent/internal/runs"
)

// IT-21 (#25, 11-07): asking the moderator through the whole agent and the
// real adapters around the fake CLIs. A question to all three, then the
// same question to the moderator alone: the moderator AI's ask command as
// a one-off in the judge folder, the prompt (the record and the previous
// answers by name) on stdin only, no session kept or named, no comparison.

func TestIT21ModeratorAsk(t *testing.T) {
	for _, moderator := range []string{fakecli.RoleClaude, fakecli.RoleOpenCode} {
		t.Run(moderator, func(t *testing.T) {
			r := newRig(t, nil)
			r.addDevice()
			if m := r.info().Moderator; m == nil || m.MaxAnswers != 6 || m.MaxAnswerBytes != 8192 {
				t.Fatalf("info moderator = %+v", m)
			}
			first := r.waitRun(r.ask(nil, nil).ID)
			var answers []map[string]string
			for _, p := range first.Parts {
				answers = append(answers, map[string]string{"agent": p.Agent, "text": p.Answer})
			}
			from := len(fakecli.Calls(t))
			const q = "AI들이 뭐가 달랐지?"
			st := r.ask(nil, map[string]any{
				"prompt":    q,
				"moderator": map[string]any{"agent": moderator, "answers": answers},
				"context":   map[string]any{"earlier": []string{question}, "last": "Fake combined answer."},
				"compare":   map[string]any{"synthesis": map[string]string{"agent": fakecli.RoleClaude}},
			})
			if st.ComparisonID != "" {
				t.Errorf("a moderator ask was compared: %s", st.ComparisonID)
			}
			run := r.waitRun(st.ID)
			if run.Status != runs.Done || len(run.Parts) != 1 || run.Parts[0].Agent != moderator || run.Parts[0].Session != "" || run.Parts[0].Answer == "" {
				t.Fatalf("run: %+v", run)
			}
			asks := calls(t, from, kindAsk, "")
			if len(asks) != 1 || asks[0].Role != moderator {
				t.Fatalf("asks: %d", len(asks))
			}
			c := asks[0]
			if !strings.EqualFold(filepath.Clean(c.Dir), filepath.Clean(r.judge)) {
				t.Errorf("asked in %q, not the judge folder", c.Dir)
			}
			for _, part := range []string{"[Askorus moderator-ask v1]", `EARLIER QUESTIONS (context only, do not answer them): ["` + question + `"]`, `PREVIOUS ANSWER: "Fake combined answer."`, `ANSWER: {"ai":"Claude","text":`, `ANSWER: {"ai":"Codex","text":`, `ANSWER: {"ai":"OpenCode","text":`} {
				if !strings.Contains(c.Stdin, part) {
					t.Errorf("stdin lacks %q", part)
				}
			}
			if !strings.HasSuffix(strings.TrimRight(c.Stdin, "\n"), "The new question:\n"+q) {
				t.Errorf("stdin ends %q", c.Stdin[max(0, len(c.Stdin)-60):])
			}
			if nonce, opens, closes := nonceOf(c.Stdin); len(nonce) != 16 || opens != 1 || closes != 1 {
				t.Errorf("data block: %q %d %d", nonce, opens, closes)
			}
			for _, a := range c.Args {
				if strings.Contains(a, "달랐지") || strings.Contains(a, "moderator-ask") {
					t.Errorf("the prompt in an argument: %q", c.Args)
				}
			}
			// One-off: Claude keeps no session; OpenCode's conversation is deleted.
			switch moderator {
			case fakecli.RoleClaude:
				if !slices.Contains(c.Args, "--no-session-persistence") {
					t.Errorf("claude args: %q", c.Args)
				}
			case fakecli.RoleOpenCode:
				if del := calls(t, from, kindDelete, ""); len(del) != 1 {
					t.Errorf("opencode deletes: %d", len(del))
				}
			}
			if strings.Contains(r.logs.String(), "달랐지") {
				t.Error("the agent log holds the question")
			}
		})
	}
}
