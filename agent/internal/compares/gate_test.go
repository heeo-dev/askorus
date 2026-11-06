package compares

import (
	"context"
	"encoding/json"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/asksemble/asksemble/agent/internal/core"
)

func testGate(t *testing.T, ais ...core.Adapter) Gate {
	min := map[string]string{}
	for _, a := range ais {
		min[a.Agent()] = "1.0.0"
	}
	return Gate{MinVersions: min, Folder: filepath.Join(t.TempDir(), "ask", core.JudgeFolderName)}
}

// synthOnlyAI is a fakeAI that writes combined answers but does not ask.
type synthOnlyAI struct{ *fakeAI }

func (s synthOnlyAI) Synthesize(context.Context, core.SynthRequest) (core.SynthResult, error) {
	return core.SynthResult{}, nil
}

// Gate.Engine: the reasons, in the order it checks them (off, no_folder,
// cannot_judge, unavailable, old_version), and no Judger or Cleaner with
// a reason.
func TestGateReasons(t *testing.T) {
	ctx := context.Background()
	claude := newFake("claude", "anthropic")
	g := testGate(t, claude)
	if e, why := g.Engine(claude, claude.Info(ctx)); why != "" || e.Agent != "claude" || e.Judger == nil || e.Cleaner != nil {
		t.Fatalf("an AI that passes: %+v %q", e, why)
	}
	oc := cleanerAI{newFake("opencode", "deepseek")}
	if e, why := testGate(t, oc).Engine(oc, oc.Info(ctx)); why != "" || e.Cleaner == nil {
		t.Errorf("a JudgeCleaner gets its Cleaner: %+v %q", e, why)
	}

	plain := &plainAI{name: "claude", version: "9.9.9"}
	cases := []struct {
		name string
		a    core.Adapter
		gate func(*Gate)
		info func(*core.AdapterInfo)
		want string
	}{
		{"off", claude, func(g *Gate) { g.Settings.Off = true }, nil, WhyOff},
		{"off before no folder", claude, func(g *Gate) { g.Settings.Off, g.Folder = true, "" }, nil, WhyOff},
		{"off before unavailable", claude, func(g *Gate) { g.Settings.Off = true }, func(i *core.AdapterInfo) { i.Available = false }, WhyOff},
		{"no folder", claude, func(g *Gate) { g.Folder = "" }, nil, WhyNoFolder},
		{"relative folder", claude, func(g *Gate) { g.Folder = filepath.Join("ask", "judge") }, nil, WhyNoFolder},
		{"no folder before not a judger", plain, func(g *Gate) { g.Folder = "" }, nil, WhyNoFolder},
		{"not a judger", plain, nil, nil, WhyCannot},
		{"not a judger before unavailable", plain, nil, func(i *core.AdapterInfo) { i.Available = false }, WhyCannot},
		{"unavailable", claude, nil, func(i *core.AdapterInfo) { i.Available = false }, WhyUnavailable},
		{"unavailable before old", claude, nil, func(i *core.AdapterInfo) { i.Available, i.Version = false, "0.1.0" }, WhyUnavailable},
		{"a cleaner unavailable", oc, nil, func(i *core.AdapterInfo) { i.Available = false }, WhyUnavailable},
		{"older", claude, func(g *Gate) { g.MinVersions["claude"] = "1.0.1" }, nil, WhyOld},
		{"unreadable version", claude, nil, func(i *core.AdapterInfo) { i.Version = "v1.0.0" }, WhyOld},
		{"no version", claude, nil, func(i *core.AdapterInfo) { i.Version = "" }, WhyOld},
		{"no min", claude, func(g *Gate) { delete(g.MinVersions, "claude") }, nil, WhyOld},
		{"unreadable min", claude, func(g *Gate) { g.MinVersions["claude"] = "latest" }, nil, WhyOld},
	}
	for _, c := range cases {
		g := testGate(t, claude)
		if c.gate != nil {
			c.gate(&g)
		}
		info := c.a.Info(ctx)
		if c.info != nil {
			c.info(&info)
		}
		e, why := g.Engine(c.a, info)
		if why != c.want {
			t.Errorf("%s: reason %q, want %q", c.name, why, c.want)
		}
		if e.Judger != nil || e.Cleaner != nil || e.Agent != c.a.Agent() {
			t.Errorf("%s: an AI that does not pass must carry no Judger or Cleaner: %+v", c.name, e)
		}
	}
}

// effortAI is an asker that lists its efforts (core.EffortLister).
type effortAI struct {
	askerAI
	efforts map[string][]string
}

func (a effortAI) AskEfforts() map[string][]string { return a.efforts }

// 11-06: /v1/info ask.efforts comes from askers that list their efforts.
func TestAnnounceEfforts(t *testing.T) {
	with := effortAI{askerAI{fakeAI: newFake("codex", "openai"), tiers: map[string]core.AskModel{core.TierStandard: {Model: "gpt-std", Effort: "medium"}}}, map[string][]string{"gpt-std": {"low", "medium"}}}
	with.caps = []core.Capability{core.CapAsk}
	without := askerAI{fakeAI: newFake("kimi", "moonshot"), tiers: map[string]core.AskModel{core.TierStandard: {Model: "k"}}}
	without.caps = []core.Capability{core.CapAsk}
	ais := []core.Adapter{with, without}
	infos, _ := Announce(context.Background(), ais, testGate(t, ais...))
	if infos[0].Ask == nil || !slices.Equal(infos[0].Ask.Efforts["gpt-std"], []string{"low", "medium"}) {
		t.Errorf("with = %+v", infos[0].Ask)
	}
	if infos[1].Ask == nil || infos[1].Ask.Efforts != nil {
		t.Errorf("without = %+v", infos[1].Ask)
	}
	if b, _ := json.Marshal(infos[1].Ask); strings.Contains(string(b), "efforts") {
		t.Errorf("an AI without a list sends efforts: %s", b)
	}
}

func TestAnnounce(t *testing.T) {
	claude := newWriter("claude", "anthropic", "Claude")
	// The adapter's own slice has room, so an append in place would show.
	own := make([]core.Capability, 1, 8)
	own[0] = core.CapList
	claude.caps = own
	codex := askerAI{fakeAI: newFake("codex", "openai"), tiers: map[string]core.AskModel{core.TierStandard: {Model: "gpt-std"}}}
	codex.caps = []core.Capability{core.CapAsk}
	old := newWriter("opencode", "deepseek", "OpenCode")
	old.version = "0.9.0"
	old.caps = []core.Capability{core.CapAsk, core.CapSynthesize} // it names synthesize itself
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
		// No AI announces judge since C6d (#24), nor any judge info.
		if b, _ := json.Marshal(info); strings.Contains(string(b), "judge") {
			t.Errorf("infos[%d] JSON = %s", i, b)
		}
	}
	if got := own[:cap(own)][1]; got != "" {
		t.Errorf("Announce wrote into the adapter's own capability slice: %q", got)
	}
	if len(claude.caps) != 1 {
		t.Errorf("the adapter's capabilities changed: %v", claude.caps)
	}
	if !slices.Equal(infos[0].Capabilities, []core.Capability{core.CapList, core.CapSynthesize}) || infos[0].Ask != nil {
		t.Errorf("claude = %+v", infos[0])
	}
	if infos[1].Ask == nil || infos[1].Ask.Tiers[core.TierStandard].Model != "gpt-std" || !slices.Equal(infos[1].Capabilities, []core.Capability{core.CapAsk}) {
		t.Errorf("codex = %+v", infos[1])
	}
	if !slices.Equal(infos[2].Capabilities, []core.Capability{core.CapAsk}) || infos[2].Ask == nil {
		t.Errorf("an old AI that names synthesize itself: %+v", infos[2])
	}
	if len(old.caps) != 2 {
		t.Errorf("the old adapter's capabilities changed: %v", old.caps)
	}
	if infos[3].Capabilities == nil || len(infos[3].Capabilities) != 0 {
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
	if b, _ := json.Marshal(infos[0]); !strings.Contains(string(b), `"capabilities":["list","synthesize"]`) {
		t.Errorf("JSON = %s", b)
	}

	// Off: no AI passes, and the engines are an empty list.
	g.Settings.Off = true
	infos, engines = Announce(context.Background(), ais, g)
	if len(engines) != 0 || engines == nil {
		t.Errorf("off: engines %v", engines)
	}
	if !slices.Equal(infos[0].Capabilities, []core.Capability{core.CapList}) {
		t.Errorf("off: claude %v", infos[0].Capabilities)
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

// Gate.Writer: the gate first, then Synthesizer and Asker, the tier model
// and its company.
func TestGateWriter(t *testing.T) {
	ctx := context.Background()
	cl := newWriter("claude", "anthropic", "Claude")
	cl.fams = map[string]string{"claude-deep": ""}
	g := testGate(t, cl)
	w, why := g.Writer(cl, cl.Info(ctx), core.TierQuick)
	if why != "" || w.Synth == nil || w.Judger == nil || w.Cleaner != nil || w.Model != "claude-quick" || w.Effort != "low" || w.Family != "anthropic" || w.Agent != "claude" {
		t.Fatalf("writer: %+v %q", w, why)
	}
	for _, tier := range []string{"", "fast", core.TierStandard} {
		if w, why := g.Writer(cl, cl.Info(ctx), tier); why != "" || w.Model != "claude-standard" {
			t.Errorf("tier %q: %+v %q", tier, w, why)
		}
	}
	if w, why := g.Writer(cl, cl.Info(ctx), core.TierDeep); why != WhyNoFamily || w.Synth != nil || w.Judger != nil {
		t.Errorf("unknown company: %+v %q", w, why)
	}
	// The gate's reasons come first.
	off := g
	off.Settings.Off = true
	if w, why := off.Writer(cl, cl.Info(ctx), core.TierQuick); why != WhyOff || w.Synth != nil {
		t.Errorf("off: %+v %q", w, why)
	}
	cl.setAvailable(false)
	if _, why := g.Writer(cl, cl.Info(ctx), core.TierQuick); why != WhyUnavailable {
		t.Errorf("unavailable: %q", why)
	}
	cl.setAvailable(true)
	// An AI that passes the gate but cannot write, or has no tier model.
	neither := newFake("codex", "openai")
	if _, why := testGate(t, neither).Writer(neither, neither.Info(ctx), core.TierQuick); why != WhyCannotWrite {
		t.Errorf("neither asks nor writes: %q", why)
	}
	asker := askerAI{fakeAI: newFake("codex", "openai"), tiers: map[string]core.AskModel{core.TierStandard: {Model: "gpt"}}}
	if _, why := testGate(t, asker).Writer(asker, asker.Info(ctx), core.TierQuick); why != WhyCannotWrite {
		t.Errorf("asks but does not write: %q", why)
	}
	writeOnly := synthOnlyAI{newFake("codex", "openai")}
	if _, why := testGate(t, writeOnly).Writer(writeOnly, writeOnly.Info(ctx), core.TierQuick); why != WhyCannotWrite {
		t.Errorf("writes but does not ask: %q", why)
	}
	cl.tiers = map[string]core.AskModel{core.TierStandard: {Effort: "medium"}}
	if _, why := g.Writer(cl, cl.Info(ctx), core.TierStandard); why != WhyCannotWrite {
		t.Errorf("no tier model: %q", why)
	}
	// The cleaner comes with the engine.
	oc := cleanerWriter{newWriter("opencode", "deepseek", "OpenCode")}
	if w, why := testGate(t, oc).Writer(oc, oc.Info(ctx), core.TierQuick); why != "" || w.Cleaner == nil {
		t.Errorf("opencode: %+v %q", w, why)
	}
}

// Announce gives synthesize to an AI that passes the gate and can ask and
// write, never on its own say, and judge.off takes it away.
func TestAnnounceSynthesize(t *testing.T) {
	cl := newWriter("claude", "anthropic", "Claude")
	cl.caps = []core.Capability{core.CapAsk, core.CapSynthesize} // it names synthesize itself
	old := newWriter("codex", "openai", "Codex")
	old.version = "0.9.0"
	old.caps = []core.Capability{core.CapAsk, core.CapSynthesize}
	asker := askerAI{fakeAI: newFake("opencode", "deepseek"), tiers: map[string]core.AskModel{core.TierStandard: {Model: "x"}}}
	asker.caps = []core.Capability{core.CapAsk}
	writeOnly := synthOnlyAI{newFake("kimi", "moonshot")}
	writeOnly.caps = []core.Capability{core.CapSynthesize}
	ais := []core.Adapter{cl, old, asker, writeOnly}
	g := testGate(t, ais...)
	infos, engines := Announce(context.Background(), ais, g)
	if !slices.Equal(infos[0].Capabilities, []core.Capability{core.CapAsk, core.CapSynthesize}) {
		t.Errorf("claude: %v", infos[0].Capabilities)
	}
	if slices.Contains(infos[1].Capabilities, core.CapSynthesize) {
		t.Errorf("an old AI that names synthesize itself: %v", infos[1].Capabilities)
	}
	if !slices.Equal(infos[2].Capabilities, []core.Capability{core.CapAsk}) {
		t.Errorf("an AI that asks but cannot write: %v", infos[2].Capabilities)
	}
	if len(infos[3].Capabilities) != 0 || len(engines) != 3 {
		t.Errorf("an AI that writes but does not ask: %v; %d engines", infos[3].Capabilities, len(engines))
	}
	if len(cl.caps) != 2 || len(old.caps) != 2 || len(writeOnly.caps) != 1 {
		t.Errorf("the adapters' own lists changed: %v %v %v", cl.caps, old.caps, writeOnly.caps)
	}
	g.Settings.Off = true
	infos, _ = Announce(context.Background(), ais, g)
	for _, info := range infos {
		if slices.Contains(info.Capabilities, core.CapSynthesize) {
			t.Errorf("off: %v", info.Capabilities)
		}
	}
}
