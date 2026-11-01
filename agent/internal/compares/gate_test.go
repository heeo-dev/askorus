package compares

import (
	"context"
	"encoding/json"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/asksemble/asksemble/agent/internal/config"
	"github.com/asksemble/asksemble/agent/internal/core"
	"github.com/asksemble/asksemble/agent/internal/judge"
)

func testGate(t *testing.T, ais ...core.Adapter) Gate {
	min := map[string]string{}
	for _, a := range ais {
		min[a.Agent()] = "1.0.0"
	}
	return Gate{MinVersions: min, Folder: filepath.Join(t.TempDir(), "ask", core.JudgeFolderName)}
}

func TestGateReasons(t *testing.T) {
	claude := newFake("claude", "anthropic")
	claude.fams = map[string]string{"gpt-x": ""} // a model it cannot place
	g := testGate(t, claude)
	if e, why := g.Engine(claude, claude.Info(context.Background())); why != "" || e.Judger == nil || e.Model != "claude-judge" || e.Effort != "medium" || e.Family != "anthropic" || e.Cleaner != nil {
		t.Fatalf("judging AI: %+v %q", e, why)
	}
	oc := cleanerAI{newFake("opencode", "deepseek")}
	if e, why := testGate(t, oc).Engine(oc, oc.Info(context.Background())); why != "" || e.Cleaner == nil {
		t.Errorf("a JudgeCleaner gets its Cleaner: %+v %q", e, why)
	}

	cases := []struct {
		name string
		a    core.Adapter
		gate func(*Gate)
		info func(*core.AdapterInfo)
		want string
	}{
		{"off", claude, func(g *Gate) { g.Settings.Off = true }, nil, WhyOff},
		{"off before unavailable", claude, func(g *Gate) { g.Settings.Off = true }, func(i *core.AdapterInfo) { i.Available = false }, WhyOff},
		{"no folder", claude, func(g *Gate) { g.Folder = "" }, nil, WhyNoFolder},
		{"relative folder", claude, func(g *Gate) { g.Folder = filepath.Join("ask", "judge") }, nil, WhyNoFolder},
		{"not a judger", &plainAI{name: "claude", version: "9.9.9"}, nil, nil, WhyCannot},
		{"unavailable", claude, nil, func(i *core.AdapterInfo) { i.Available = false }, WhyUnavailable},
		{"unavailable before old", claude, nil, func(i *core.AdapterInfo) { i.Available, i.Version = false, "0.1.0" }, WhyUnavailable},
		{"older", claude, func(g *Gate) { g.MinVersions["claude"] = "1.0.1" }, nil, WhyOld},
		{"unreadable version", claude, nil, func(i *core.AdapterInfo) { i.Version = "v1.0.0" }, WhyOld},
		{"no version", claude, nil, func(i *core.AdapterInfo) { i.Version = "" }, WhyOld},
		{"no min", claude, func(g *Gate) { delete(g.MinVersions, "claude") }, nil, WhyOld},
		{"unreadable min", claude, func(g *Gate) { g.MinVersions["claude"] = "latest" }, nil, WhyOld},
		{"another company's model", claude, func(g *Gate) {
			g.Settings.Models = map[string]core.AskModel{"claude": {Model: "gpt-x"}}
		}, nil, WhyNoFamily},
	}
	for _, c := range cases {
		g := testGate(t, claude)
		if c.gate != nil {
			c.gate(&g)
		}
		info := c.a.Info(context.Background())
		if c.info != nil {
			c.info(&info)
		}
		e, why := g.Engine(c.a, info)
		if why != c.want {
			t.Errorf("%s: reason %q, want %q", c.name, why, c.want)
		}
		if e.Judger != nil || e.Cleaner != nil || e.Family != "" || e.Agent != "claude" {
			t.Errorf("%s: an engine that does not judge must not carry a Judger: %+v", c.name, e)
		}
	}

	// The settings' model and effort; an effort alone keeps the default model.
	g = testGate(t, claude)
	g.Settings.Models = map[string]core.AskModel{"claude": {Model: "claude-big", Effort: "high"}}
	if e, why := g.Engine(claude, claude.Info(context.Background())); why != "" || e.Model != "claude-big" || e.Effort != "high" {
		t.Errorf("override: %+v %q", e, why)
	}
	g.Settings.Models = map[string]core.AskModel{"claude": {Effort: "low"}}
	if e, _ := g.Engine(claude, claude.Info(context.Background())); e.Model != "claude-judge" || e.Effort != "low" {
		t.Errorf("effort override: %+v", e)
	}
	g.Settings.Models = map[string]core.AskModel{"claude": {Model: "gpt-x"}}
	if e, why := g.Engine(claude, claude.Info(context.Background())); why != WhyNoFamily || e.Model != "gpt-x" {
		t.Errorf("unknown company keeps the model for check: %+v %q", e, why)
	}
	// Info of a judging engine.
	if ji := (Engine{Model: "m", Family: "f"}).Info(); ji != (core.JudgeInfo{Model: "m", Family: "f"}) {
		t.Errorf("Info = %+v", ji)
	}
}

func TestOverrideReachesJudge(t *testing.T) {
	ais := threeAIs()
	m, _ := newTestManager(t, ais, func(o *Options) {
		o.Gate.Settings.Models = map[string]core.AskModel{"opencode": {Model: "oc-big", Effort: "max"}}
	})
	c := startDirect(t, m, threeAnswers("phone", ""))
	c = waitIdle(t, m, "phone", c.ID)
	if c.Status != Done {
		t.Fatalf("status %s, %+v", c.Status, c.Error)
	}
	oc := fakeOf(ais[2])
	calls := oc.judgeCalls()
	if len(calls) != 2 {
		t.Fatalf("opencode judged %d times, want 2", len(calls))
	}
	for _, r := range calls {
		if r.Model != "oc-big" || r.Effort != "max" || r.Folder != m.o.Gate.Folder {
			t.Errorf("judge request model %q effort %q folder %q", r.Model, r.Effort, r.Folder)
		}
	}
	for _, r := range c.Requests {
		if r.Judge == "opencode" && r.Model != "oc-big" {
			t.Errorf("request %s model %q", r.ID, r.Model)
		}
	}
}

func TestJudgingInfo(t *testing.T) {
	g := Gate{}
	ji := g.Judging()
	if ji.Engine != "pc" || ji.Shape != judge.ShapeDirected || ji.Labels != judge.LabelsV1 || ji.Rules != judge.Rules || !slices.Equal(ji.Checks, []string{judge.CheckUnsure}) || ji.Verified != nil || ji.TypicalMs != 0 {
		t.Errorf("Judging = %+v", ji)
	}
	b, _ := json.Marshal(ji)
	if string(b) != `{"engine":"pc","shape":"directed","labels":"v1","rules":"judge-v1","checks":["unsure"]}` {
		t.Errorf("JSON = %s", b)
	}
	g.Settings = config.JudgeConfig{Checks: []string{judge.CheckNoEvidence, judge.CheckNumberMismatch}}
	if ji := g.Judging(); !slices.Equal(ji.Checks, g.Settings.Checks) {
		t.Errorf("checks = %v", ji.Checks)
	} else {
		ji.Checks[0] = "x"
		if g.Settings.Checks[0] != judge.CheckNoEvidence {
			t.Error("Judging shares the settings' checks")
		}
	}
}

func TestAnnounce(t *testing.T) {
	claude := newFake("claude", "anthropic")
	// The adapter's own slice has room, so an append in place would show.
	own := make([]core.Capability, 1, 8)
	own[0] = core.CapList
	claude.caps = own
	codex := askerAI{fakeAI: newFake("codex", "openai"), tiers: map[string]core.AskModel{core.TierStandard: {Model: "gpt-std"}}}
	codex.caps = []core.Capability{core.CapAsk}
	old := newFake("opencode", "deepseek")
	old.version = "0.9.0"
	old.caps = []core.Capability{core.CapAsk, core.CapJudge} // it names judge itself
	plain := &plainAI{name: "gemini", version: "1.0.0"}
	noAsk := askerAI{fakeAI: newFake("kimi", "moonshot"), tiers: map[string]core.AskModel{core.TierStandard: {Model: "k"}}}

	ais := []core.Adapter{claude, codex, old, plain, noAsk}
	g := testGate(t, ais...)
	infos, engines := Announce(context.Background(), ais, g)
	if len(infos) != len(ais) {
		t.Fatalf("%d infos", len(infos))
	}
	for i, info := range infos {
		if info.Agent != ais[i].Agent() {
			t.Errorf("infos[%d] is %s, want %s", i, info.Agent, ais[i].Agent())
		}
	}
	if got := own[:cap(own)][1]; got != "" {
		t.Errorf("Announce wrote into the adapter's own capability slice: %q", got)
	}
	if len(claude.caps) != 1 {
		t.Errorf("the adapter's capabilities changed: %v", claude.caps)
	}
	if !slices.Equal(infos[0].Capabilities, []core.Capability{core.CapList, core.CapJudge}) || infos[0].Judge == nil ||
		*infos[0].Judge != (core.JudgeInfo{Model: "claude-judge", Effort: "medium", Family: "anthropic"}) || infos[0].Ask != nil {
		t.Errorf("claude = %+v judge %+v", infos[0], infos[0].Judge)
	}
	if infos[1].Ask == nil || infos[1].Ask.Tiers[core.TierStandard].Model != "gpt-std" || !slices.Contains(infos[1].Capabilities, core.CapJudge) {
		t.Errorf("codex = %+v", infos[1])
	}
	if slices.Contains(infos[2].Capabilities, core.CapJudge) || infos[2].Judge != nil || !slices.Contains(infos[2].Capabilities, core.CapAsk) {
		t.Errorf("an old AI that names judge itself: %+v", infos[2])
	}
	if len(old.caps) != 2 {
		t.Errorf("the old adapter's capabilities changed: %v", old.caps)
	}
	if infos[3].Judge != nil || infos[3].Capabilities == nil || len(infos[3].Capabilities) != 0 {
		t.Errorf("a plain adapter: %+v", infos[3])
	}
	if infos[4].Ask != nil {
		t.Errorf("an Asker without the capability ask gets tiers: %+v", infos[4])
	}
	var names []string
	for _, e := range engines {
		names = append(names, e.Agent)
	}
	if !slices.Equal(names, []string{"claude", "codex", "kimi"}) {
		t.Errorf("engines = %v", names)
	}
	b, _ := json.Marshal(infos[0])
	if !strings.Contains(string(b), `"capabilities":["list","judge"]`) || !strings.Contains(string(b), `"judge":{"model":"claude-judge","effort":"medium","family":"anthropic"}`) {
		t.Errorf("JSON = %s", b)
	}

	// Off: nobody judges, and the engines are an empty list.
	g.Settings.Off = true
	infos, engines = Announce(context.Background(), ais, g)
	if len(engines) != 0 || engines == nil {
		t.Errorf("off: engines %v", engines)
	}
	for _, info := range infos {
		if info.Judge != nil || slices.Contains(info.Capabilities, core.CapJudge) {
			t.Errorf("off: %+v", info)
		}
	}
	// The Manager's Engines agree with Announce.
	m := New(Options{Adapters: ais, Gate: testGate(t, ais...)})
	defer m.Close()
	var got []string
	for _, e := range m.Engines(context.Background()) {
		got = append(got, e.Agent)
	}
	if !slices.Equal(got, names) {
		t.Errorf("Engines = %v, Announce %v", got, names)
	}
}
