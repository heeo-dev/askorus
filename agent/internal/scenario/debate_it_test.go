package scenario

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/asksemble/asksemble/agent/internal/api"
	"github.com/asksemble/asksemble/agent/internal/compares"
	"github.com/asksemble/asksemble/agent/internal/core"
	"github.com/asksemble/asksemble/agent/internal/fakecli"
	"github.com/asksemble/asksemble/agent/internal/judge"
)

// IT-19 (P2-5 토론, #26): a debate through the whole agent and the three
// real adapters around the fake CLIs. The moderator's calls are the
// combined answer's locked-down command with the debate prompts; each
// turn is the ask command in the judge folder, the prompt on stdin.

func debateRequest(key string) map[string]any {
	return map[string]any{
		"clientDebateId": key, "question": "첫 배는 몇 시에 떠?", "synthesis": "첫 배 시간은 AI마다 다릅니다.",
		"answers": []map[string]string{
			{"agent": fakecli.RoleClaude, "via": "pc", "text": "첫 배는 7시입니다."},
			{"agent": fakecli.RoleCodex, "via": "pc", "text": "First ferry at 8 a.m."},
			{"agent": fakecli.RoleOpenCode, "via": "pc", "text": "첫 배는 8시입니다."},
		},
		"moderator": fakecli.RoleClaude, "tier": "standard",
	}
}

func (r *rig) waitDebate(id string) (api.DebateStatus, []byte) {
	r.t.Helper()
	deadline := time.Now().Add(30 * time.Second)
	for {
		var st api.DebateStatus
		b := r.callJSON("GET", "/v1/debates/"+id, nil, 200, &st)
		switch st.Status {
		case compares.DebateDone, compares.DebateStopped, compares.DebateFailed:
			return st, b
		}
		if time.Now().After(deadline) {
			r.t.Fatalf("debate still %s", st.Status)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func TestIT19Debate(t *testing.T) {
	t.Run("a point, one more turn", func(t *testing.T) {
		r := newRig(t, func(o *rigOptions) { o.env = map[string]string{fakecli.EnvDebate: fakecli.DebateNext} })
		r.addDevice()
		if d := r.info().Debate; d == nil || d.MaxPoints != 2 || d.MaxTurns != 4 || d.Rules != judge.DebateRules {
			t.Fatalf("info debate = %+v", d)
		}
		var start api.DebateStatus
		r.callJSON("POST", "/v1/debates", debateRequest("d1"), 202, &start)
		st, b := r.waitDebate(start.ID)
		if st.Status != compares.DebateDone || st.After == nil || st.After.Text != fakecli.DebateAfterText || st.Moderator != fakecli.RoleClaude {
			t.Fatalf("debate: %s", b)
		}
		p := st.Points[0]
		var who []string
		for _, tn := range p.Turns {
			who = append(who, tn.AI)
			if tn.Status != compares.TurnDone || tn.Stance != judge.StanceKeep || !strings.HasPrefix(tn.Text, fakecli.DebateTurnText) {
				t.Errorf("turn %+v", tn)
			}
		}
		// Claude and Codex as planned, then Claude once more (mode next).
		if strings.Join(who, ",") != "claude,codex,claude" || p.Turns[2].Ask != fakecli.DebateNextAsk || p.Result != judge.OutcomeAgreed || p.Line != fakecli.DebateLine {
			t.Errorf("point %+v", p)
		}
		// Moderator: plan, check (next), check (the last: done), after.
		synths := calls(t, 0, kindSynth, "")
		var heads []string
		for _, c := range synths {
			if c.Role != fakecli.RoleClaude {
				t.Errorf("a %s writer call", c.Role)
			}
			heads = append(heads, strings.SplitN(c.Stdin, "]", 2)[0]+"]")
		}
		if strings.Join(heads, ",") != "[Asksemble debate-plan v2],[Asksemble debate-check v1],[Asksemble debate-check v1],[Asksemble synthesis debate-v4]" {
			t.Errorf("moderator calls %v", heads)
		}
		// The turns: the ask command, in the judge folder, the prompt on
		// stdin and never in an argument.
		asks := calls(t, 0, kindAsk, "")
		if len(asks) != 3 {
			t.Fatalf("%d asks", len(asks))
		}
		for _, c := range asks {
			if !strings.Contains(c.Stdin, "[Asksemble debate-turn v1]") || !strings.EqualFold(filepath.Clean(c.Dir), filepath.Clean(r.judge)) {
				t.Errorf("turn %s in %q: %.120q", c.Role, c.Dir, c.Stdin)
			}
			for _, a := range c.Args {
				if strings.Contains(a, "debate-turn") || strings.Contains(a, "첫 배") {
					t.Errorf("the prompt in an argument: %q", c.Args)
				}
			}
		}
		onlyAsksAndWriters(t)
		logs := r.logs.String()
		for _, secret := range []string{"첫 배", "First ferry", fakecli.DebateTurnText, fakecli.DebateAfterText, fakecli.DebateLine} {
			if strings.Contains(logs, secret) {
				t.Errorf("the agent log holds %q", secret)
			}
		}
		if st.Calls != 7 {
			t.Errorf("calls %d", st.Calls)
		}
	})

	t.Run("nothing to debate", func(t *testing.T) {
		r := newRig(t, func(o *rigOptions) { o.env = map[string]string{fakecli.EnvDebate: fakecli.DebateNone} })
		r.addDevice()
		var start api.DebateStatus
		r.callJSON("POST", "/v1/debates", debateRequest("d1"), 202, &start)
		st, b := r.waitDebate(start.ID)
		if st.Status != compares.DebateDone || st.After != nil || len(st.Points) != 0 || st.Calls != 1 || !strings.Contains(string(b), `"points":[]`) {
			t.Errorf("debate: %s", b)
		}
	})

	t.Run("two points, a turn fails", func(t *testing.T) {
		r := newRig(t, func(o *rigOptions) {
			o.env = map[string]string{fakecli.EnvDebate: fakecli.DebateTwo, fakecli.EnvAsk: "codex=fail"}
		})
		r.addDevice()
		var start api.DebateStatus
		r.callJSON("POST", "/v1/debates", debateRequest("d1"), 202, &start)
		st, b := r.waitDebate(start.ID)
		if st.Status != compares.DebateDone || len(st.Points) != 2 || st.After == nil {
			t.Fatalf("debate: %s", b)
		}
		failed := st.Points[0].Turns[1]
		if failed.AI != fakecli.RoleCodex || failed.Status != compares.TurnFailed || failed.Error == nil || strings.Contains(failed.Error.Message, "usage limit") {
			t.Errorf("codex's turn: %+v", failed)
		}
		if p2 := st.Points[1]; p2.Nature != judge.NatureTaste || len(p2.Turns) != 1 || p2.Turns[0].AI != fakecli.RoleOpenCode || p2.Turns[0].Status != compares.TurnDone {
			t.Errorf("p2: %+v", p2)
		}
		// OpenCode's turn is an ask: nothing of it is deleted. Only a
		// writer's conversation would be.
		if del := calls(t, 0, kindDelete, ""); len(del) != 0 {
			t.Errorf("deleted %d", len(del))
		}
	})

	t.Run("stop", func(t *testing.T) {
		r := newRig(t, func(o *rigOptions) {
			o.env = map[string]string{fakecli.EnvSynth: "claude=slow", fakecli.EnvSleep: "20s"}
		})
		r.addDevice()
		var start api.DebateStatus
		r.callJSON("POST", "/v1/debates", debateRequest("d1"), 202, &start)
		var stopped api.DebateStatus
		r.callJSON("POST", "/v1/debates/"+start.ID+"/stop", nil, 200, &stopped)
		if stopped.Status != compares.DebateStopped || stopped.PollAfterMs != 0 {
			t.Errorf("stop: %+v", stopped)
		}
		st, _ := r.waitDebate(start.ID)
		if st.Status != compares.DebateStopped || st.After != nil {
			t.Errorf("after stop: %+v", st)
		}
		// One at a time, but a stopped debate is no longer running.
		r.callJSON("POST", "/v1/debates", debateRequest("d2"), 202, nil)
	})

	t.Run("refused", func(t *testing.T) {
		r := newRig(t, nil)
		r.addDevice()
		one := debateRequest("")
		one["answers"] = one["answers"].([]map[string]string)[:1]
		if st, b := r.call("POST", "/v1/debates", one); st != 400 || !strings.Contains(string(b), core.ErrCodeNeedTwoAnswers) {
			t.Errorf("one answer = %d %s", st, b)
		}
	})
}
