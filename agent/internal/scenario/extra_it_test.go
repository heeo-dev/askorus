package scenario

import (
	"slices"
	"testing"

	"github.com/heeo-dev/askorus/agent/internal/core"
	"github.com/heeo-dev/askorus/agent/internal/fakecli"
)

// IT-27 (#65, 2026-11-20): models outside the tiers through the whole
// agent. /v1/info offers Codex's Astra and Claude's Fable in ask.extra,
// both heavy, with their efforts in ask.efforts and in no tier; an own
// pick of Astra runs codex with it; a moderator pick of Fable writes the
// combined answer with claude --model fable.
func TestIT27ExtraModels(t *testing.T) {
	r := newRig(t, func(o *rigOptions) { o.follow = true })
	r.addDevice()
	info := r.info()
	cx, cl := agentInfo(t, info, fakecli.RoleCodex), agentInfo(t, info, fakecli.RoleClaude)
	if cx.Ask == nil || !slices.Equal(cx.Ask.Extra, []core.ExtraModel{{Model: "gpt-6.2-astra", Heavy: true}}) || len(cx.Ask.Efforts["gpt-6.2-astra"]) != 6 {
		t.Fatalf("codex ask info %+v", cx.Ask)
	}
	if cl.Ask == nil || !slices.Equal(cl.Ask.Extra, []core.ExtraModel{{Model: "fable", Heavy: true}}) || len(cl.Ask.Efforts["fable"]) != 5 {
		t.Fatalf("claude ask info %+v", cl.Ask)
	}
	for _, ai := range []core.AdapterInfo{cx, cl} {
		for tier, m := range ai.Ask.Tiers {
			if m.Model == "gpt-6.2-astra" || m.Model == "fable" {
				t.Errorf("%s %s runs an extra model", ai.Agent, tier)
			}
		}
	}
	if oc := agentInfo(t, info, fakecli.RoleOpenCode); oc.Ask.Extra != nil {
		t.Errorf("opencode offers extra models: %+v", oc.Ask.Extra)
	}

	from := len(fakecli.Calls(t))
	st := r.waitRun(r.ask([]map[string]string{{"agent": "codex", "model": "gpt-6.2-astra", "effort": "low"}}, nil).ID)
	if len(st.Parts) != 1 || st.Parts[0].Error != nil || st.Parts[0].Model != "gpt-6.2-astra" {
		t.Fatalf("astra pick: %+v", st.Parts)
	}
	if asks := calls(t, from, kindAsk, fakecli.RoleCodex); len(asks) != 1 || !hasArgs(asks[0].Args, "-m", "gpt-6.2-astra") || !hasArgs(asks[0].Args, "-c", `model_reasoning_effort="low"`) {
		t.Errorf("astra ask args %+v", asks)
	}

	from = len(fakecli.Calls(t))
	_, c, _ := r.compareRun(map[string]any{"synthesis": map[string]string{"agent": "claude", "model": "fable"}})
	if c.Synthesis == nil || c.Synthesis.Agent != fakecli.RoleClaude {
		t.Fatalf("synthesis %+v", c.Synthesis)
	}
	ws := calls(t, from, kindSynth, fakecli.RoleClaude)
	if len(ws) != 1 || !hasArgs(ws[0].Args, "--model", "fable") || !hasArgs(ws[0].Args, "--effort", "medium") {
		t.Errorf("the moderator pick of Fable: %+v", ws)
	}
}
