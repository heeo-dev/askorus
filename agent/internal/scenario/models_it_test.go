package scenario

import (
	"slices"
	"testing"

	"github.com/heeo-dev/askorus/agent/internal/core"
	"github.com/heeo-dev/askorus/agent/internal/fakecli"
)

// IT-26 (#64, 2026-11-20): the newest model of each tier's line through
// the whole agent. With a newer Sol in Codex's list, /v1/info shows deep
// on it and names it in ask.newer; an ask with an own pick of the old Sol
// (saved before the new one came) runs the new one, and so does a
// moderator pick of it. A newer model the AI refuses is asked again with
// the shipped one, and /v1/info goes back to the shipped one.
func TestIT26NewestModels(t *testing.T) {
	t.Run("followed", func(t *testing.T) {
		r := newRig(t, func(o *rigOptions) { o.follow, o.env = true, map[string]string{fakecli.EnvNewer: "codex=list"} })
		r.addDevice()
		ai := agentInfo(t, r.info(), fakecli.RoleCodex)
		if ai.Ask == nil || ai.Ask.Tiers[core.TierDeep].Model != fakecli.NewerCodex || ai.Ask.Newer["gpt-6.1-sol"] != fakecli.NewerCodex ||
			len(ai.Ask.Efforts[fakecli.NewerCodex]) != 6 {
			t.Fatalf("codex ask info %+v", ai.Ask)
		}
		if oc := agentInfo(t, r.info(), fakecli.RoleOpenCode); oc.Ask == nil || oc.Ask.Newer != nil || oc.Ask.Tiers[core.TierQuick].Model != "opencode-go/deepseek-v4.1-flash" {
			t.Errorf("opencode moved without a newer model: %+v", oc.Ask)
		}

		from := len(fakecli.Calls(t))
		st := r.waitRun(r.ask([]map[string]string{{"agent": "codex", "model": "gpt-6.1-sol", "effort": "high"}}, nil).ID)
		if len(st.Parts) != 1 || st.Parts[0].Error != nil || st.Parts[0].Model != fakecli.NewerCodex {
			t.Fatalf("own pick: %+v", st.Parts)
		}
		if asks := calls(t, from, kindAsk, fakecli.RoleCodex); len(asks) != 1 || !hasArgs(asks[0].Args, "-m", fakecli.NewerCodex) || !hasArgs(asks[0].Args, "-c", `model_reasoning_effort="high"`) {
			t.Errorf("ask args %+v", asks)
		}

		from = len(fakecli.Calls(t))
		_, c, _ := r.compareRun(map[string]any{"synthesis": map[string]string{"agent": "codex", "model": "gpt-6.1-sol", "effort": "high"}})
		if c.Synthesis == nil || c.Synthesis.Agent != fakecli.RoleCodex {
			t.Fatalf("synthesis %+v", c.Synthesis)
		}
		if ws := calls(t, from, kindSynth, fakecli.RoleCodex); len(ws) != 1 || !hasArgs(ws[0].Args, "-m", fakecli.NewerCodex) {
			t.Errorf("the moderator pick did not follow: %+v", ws)
		}
	})

	t.Run("refused", func(t *testing.T) {
		r := newRig(t, func(o *rigOptions) { o.follow, o.env = true, map[string]string{fakecli.EnvNewer: "codex=refuse"} })
		r.addDevice()
		if m := agentInfo(t, r.info(), fakecli.RoleCodex).Ask.Tiers[core.TierDeep].Model; m != fakecli.NewerCodex {
			t.Fatalf("deep = %q", m)
		}
		from := len(fakecli.Calls(t))
		st := r.waitRun(r.ask([]map[string]string{{"agent": "codex"}}, map[string]any{"tier": "deep"}).ID)
		if len(st.Parts) != 1 || st.Parts[0].Error != nil || st.Parts[0].Model != "gpt-6.1-sol" || st.Parts[0].Answer == "" {
			t.Fatalf("parts %+v", st.Parts)
		}
		var ms []string
		for _, a := range calls(t, from, kindAsk, fakecli.RoleCodex) {
			ms = append(ms, a.Args[slices.Index(a.Args, "-m")+1])
		}
		if !slices.Equal(ms, []string{fakecli.NewerCodex, "gpt-6.1-sol"}) {
			t.Errorf("asked with %q", ms)
		}
		ai := agentInfo(t, r.info(), fakecli.RoleCodex)
		if ai.Ask.Tiers[core.TierDeep].Model != "gpt-6.1-sol" || ai.Ask.Newer != nil {
			t.Errorf("info after the refusal %+v", ai.Ask)
		}
	})
}

// hasArgs reports whether args holds flag directly followed by value.
func hasArgs(args []string, flag, value string) bool {
	for i := 0; i+1 < len(args); i++ {
		if args[i] == flag && args[i+1] == value {
			return true
		}
	}
	return false
}
