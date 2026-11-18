package scenario

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/heeo-dev/askorus/agent/internal/adapter/agy"
	"github.com/heeo-dev/askorus/agent/internal/compares"
	"github.com/heeo-dev/askorus/agent/internal/core"
	"github.com/heeo-dev/askorus/agent/internal/fakecli"
)

// IT-24 (#20, 2026-11-18): Antigravity through the agent, as the phone
// asks it. /v1/info shows agy with its tier models and levels; an ask runs
// agy in the ask folder's agy folder with the ask's agent file, the
// question on stdin as one stream-json message and never in an argument;
// a follow-up carries the conversation on; a sign-in that ended, the
// weekly limit and a run that slipped its lock each have their own error.
func TestIT24Agy(t *testing.T) {
	t.Run("info and ask", func(t *testing.T) {
		r := newRig(t, func(o *rigOptions) { o.agy = true })
		r.addDevice()
		ai := agentInfo(t, r.info(), fakecli.RoleAgy)
		if !ai.Available || ai.Version != "1.5.10" || !slices.Contains(ai.Capabilities, core.CapAsk) || len(ai.Notices) != 0 {
			t.Errorf("agy info %+v", ai)
		}
		if ai.Ask == nil || ai.Ask.Tiers[core.TierQuick] != (core.AskModel{Model: "gemini-3.8-flash", Effort: "low"}) ||
			!slices.Equal(ai.Ask.Efforts["gemini-3.8-flash"], []string{"low", "medium", "high"}) || ai.Ask.Images == nil || !ai.Ask.Images.All {
			t.Errorf("agy ask info %+v", ai.Ask)
		}
		if len(ai.Usage) != 2 || ai.Usage[0].Name != "Gemini Models" || ai.Usage[0].ResetAt != fakecli.AgyResetTime {
			t.Errorf("agy usage %+v", ai.Usage)
		}

		st := r.waitRun(r.ask([]map[string]string{{"agent": "agy"}, {"agent": "claude"}}, nil).ID)
		var part *struct{ agent, session, answer, model string }
		for _, p := range st.Parts {
			if p.Agent == fakecli.RoleAgy {
				part = &struct{ agent, session, answer, model string }{p.Agent, p.Session, p.Answer, p.Model}
				if p.Error != nil {
					t.Fatalf("agy part error %+v", p.Error)
				}
			}
		}
		if part == nil || !strings.HasPrefix(part.answer, "fake agy: ") || !strings.HasPrefix(part.session, "agy:") || part.model != "gemini-3.8-flash-low" {
			t.Fatalf("agy part %+v", part)
		}
		c := lastCall(t, fakecli.RoleAgy)
		if c.Dir != filepath.Join(r.askDir, agy.FolderName) {
			t.Errorf("agy ran in %q", c.Dir)
		}
		if !slices.Contains(c.Args, "--agent") || !slices.Contains(c.Args, "--settings") || slices.Contains(c.Args, "--dangerously-skip-permissions") {
			t.Errorf("agy args %q", c.Args)
		}
		for _, a := range c.Args {
			if strings.Contains(a, question) {
				t.Errorf("the question is in an argument: %q", a)
			}
		}
		var msg struct {
			Message struct {
				Content []struct{ Text string } `json:"content"`
			} `json:"message"`
		}
		if err := json.Unmarshal([]byte(c.Stdin), &msg); err != nil || len(msg.Message.Content) != 1 || !strings.Contains(msg.Message.Content[0].Text, question) {
			t.Errorf("stdin %q %v", c.Stdin, err)
		}

		// The follow-up names the part's session and carries it on.
		st2 := r.waitRun(r.ask([]map[string]string{{"agent": "agy", "session": part.session}}, nil).ID)
		if len(st2.Parts) != 1 || st2.Parts[0].Error != nil || st2.Parts[0].Session != part.session {
			t.Errorf("follow-up %+v", st2.Parts)
		}
		native := strings.TrimPrefix(part.session, "agy:")
		if c := lastCall(t, fakecli.RoleAgy); !slices.Contains(c.Args, native) {
			t.Errorf("follow-up args %q", c.Args)
		}
	})

	t.Run("signed out", func(t *testing.T) {
		r := newRig(t, func(o *rigOptions) {
			o.agy = true
			o.env = map[string]string{fakecli.EnvSignIn: "agy=out", fakecli.EnvAsk: "agy=signedout"}
		})
		r.addDevice()
		if ai := agentInfo(t, r.info(), fakecli.RoleAgy); !ai.Available || !slices.Equal(ai.Notices, []string{agy.NoticeSignedOut}) {
			t.Errorf("agy info %+v", ai)
		}
		st := r.waitRun(r.ask([]map[string]string{{"agent": "agy"}}, nil).ID)
		if p := st.Parts[0]; p.Error == nil || p.Error.Code != core.ErrCodeSignedOut || p.Error.Message != agy.LoginCommand {
			t.Errorf("part %+v", p.Error)
		}
	})

	t.Run("weekly limit", func(t *testing.T) {
		r := newRig(t, func(o *rigOptions) {
			o.agy = true
			o.env = map[string]string{fakecli.EnvAsk: "agy=fail"}
		})
		r.addDevice()
		st := r.waitRun(r.ask([]map[string]string{{"agent": "agy"}}, nil).ID)
		if p := st.Parts[0]; p.Error == nil || p.Error.Code != core.ErrCodeUsageLimit || p.Error.Message != fakecli.AgyResetTime {
			t.Errorf("part %+v", p.Error)
		}
	})

	t.Run("lock not held", func(t *testing.T) {
		r := newRig(t, func(o *rigOptions) {
			o.agy = true
			o.env = map[string]string{fakecli.EnvAgy: fakecli.AgyOpenMode}
		})
		r.addDevice()
		st := r.waitRun(r.ask([]map[string]string{{"agent": "agy"}}, nil).ID)
		if p := st.Parts[0]; p.Error == nil || p.Error.Code != core.ErrCodeInternal || p.Answer != "" {
			t.Errorf("part %+v %q", p.Error, p.Answer)
		}
	})

	t.Run("photos in a folder of their own", func(t *testing.T) {
		r := newRig(t, func(o *rigOptions) { o.agy = true })
		r.addDevice()
		if ai := agentInfo(t, r.info(), fakecli.RoleAgy); ai.Ask == nil || ai.Ask.Images == nil || !ai.Ask.Images.All {
			t.Errorf("agy ask info %+v", ai.Ask)
		}
		up := r.upload(itJPEG)
		st := r.waitRun(r.ask([]map[string]string{{"agent": "agy"}, {"agent": "claude"}}, map[string]any{"attachments": []string{up.ID}}).ID)
		for _, p := range st.Parts {
			if p.Agent == fakecli.RoleAgy && (p.Error != nil || !strings.HasSuffix(p.Answer, " [photos: 1]")) {
				t.Errorf("agy part %+v %q", p.Error, p.Answer)
			}
		}
		c := lastCall(t, fakecli.RoleAgy)
		if filepath.Dir(c.Dir) != filepath.Join(r.askDir, agy.PhotoFolderName) || c.Photos != 1 || !slices.Contains(c.Args, "askorus-photo") {
			t.Errorf("agy call in %q with %d photos: %q", c.Dir, c.Photos, c.Args)
		}
		for _, a := range c.Args {
			if strings.Contains(a, up.ID) || strings.Contains(a, question) {
				t.Errorf("a photo or the question is in an argument: %q", a)
			}
		}
		if entries, err := os.ReadDir(filepath.Join(r.askDir, agy.PhotoFolderName)); err != nil || len(entries) != 0 {
			t.Errorf("photo folders left: %d %v", len(entries), err)
		}
	})

	t.Run("moderator", func(t *testing.T) {
		r := newRig(t, func(o *rigOptions) { o.agy = true })
		r.addDevice()
		two := []map[string]string{{"agent": fakecli.RoleClaude}, {"agent": fakecli.RoleAgy}}
		st := r.ask(two, map[string]any{"tier": "standard", "compare": map[string]any{
			"judges": []string{fakecli.RoleClaude, fakecli.RoleAgy}, "synthesis": map[string]any{"agent": fakecli.RoleAgy}}})
		if st.ComparisonID == "" || st.CompareError != nil {
			t.Fatalf("POST /v1/runs: comparisonId %q, compareError %+v", st.ComparisonID, st.CompareError)
		}
		r.waitRun(st.ID)
		c, _ := r.waitComparison(st.ComparisonID)
		if s := synthOf(t, c); c.Status != compares.Done || s.Status != compares.SynthDone || s.Agent != fakecli.RoleAgy || s.Text != fakecli.SynthLead {
			t.Fatalf("comparison %s, synthesis %+v", c.Status, s)
		}
		w := lastCall(t, fakecli.RoleAgy)
		if w.Dir != filepath.Join(r.judge, agy.FolderName) || !slices.Contains(w.Args, "askorus-synth") || !slices.Contains(w.Args, core.SynthSchemaV1) {
			t.Errorf("writer call in %q: %q", w.Dir, w.Args)
		}
		if !strings.Contains(w.Stdin, "[Askorus synthesis ") {
			t.Errorf("writer stdin %q", w.Stdin)
		}
		r.waitIdle(c.ID)
	})
}

// lastCall is role's last recorded run.
func lastCall(t *testing.T, role string) fakecli.Call {
	t.Helper()
	calls := fakecli.Calls(t)
	for i := len(calls) - 1; i >= 0; i-- {
		if calls[i].Role == role {
			return calls[i]
		}
	}
	t.Fatalf("%s did not run", role)
	return fakecli.Call{}
}
