package compares

import (
	"context"
	"maps"
	"path/filepath"
	"slices"

	"github.com/asksemble/asksemble/agent/internal/config"
	"github.com/asksemble/asksemble/agent/internal/core"
	"github.com/asksemble/asksemble/agent/internal/judge"
)

// Why an AI does not judge, as Gate.Engine gives it, in the order Engine
// checks them. The CLI's check turns them into text.
const (
	WhyOff         = "off"             // agent.json judge.off
	WhyNoFolder    = "no_folder"       // no ask folder, so no judge folder
	WhyCannot      = "cannot_judge"    // the adapter is no core.Judger
	WhyUnavailable = "unavailable"     // the AI program is not found or does not run
	WhyOld         = "old_version"     // older than the version the judge command was tested with, or unreadable
	WhyNoFamily    = "unknown_company" // the company of its judge model is unknown
	// WhyCannotWrite: the AI judges but cannot write combined answers: its
	// adapter is no core.Synthesizer or core.Asker, or its ask tier has no
	// model (Gate.Writer only).
	WhyCannotWrite = "cannot_synthesize"
)

// EngineName is InfoResponse.judging.engine and Comparison.engine: the AI
// programs on this PC judge, as an answer's via "pc" says they answered. A
// later API engine gets a name of its own (S3 decision 1).
const EngineName = "pc"

// Gate decides which AIs judge. The API's /v1/info, the CLI's info and
// check and the comparisons all ask it, so they cannot disagree.
type Gate struct {
	Settings config.JudgeConfig
	// MinVersions maps an AI to the oldest version of its CLI the judge
	// command was tested with (its adapter's JudgeMinVersion). An AI not
	// listed never judges.
	MinVersions map[string]string
	// Folder is the judge folder, core.JudgeFolder(the ask folder): ""
	// when there is no ask folder, and then no AI judges.
	Folder string
}

// Engine is an AI that judges, with the model and effort it judges with
// and that model's company.
type Engine struct {
	Agent   string
	Judger  core.Judger
	Cleaner core.JudgeCleaner // nil when judging leaves nothing behind
	Model   string
	Effort  string // "" when the AI decides
	Family  string
}

// Info is the engine as AdapterInfo.judge shows it.
func (e Engine) Info() core.JudgeInfo {
	return core.JudgeInfo{Model: e.Model, Effort: e.Effort, Family: e.Family}
}

// Engine reports whether adapter a, whose Info is info, judges. The
// reason is "" when it does, else the first of WhyOff, WhyNoFolder,
// WhyCannot, WhyUnavailable, WhyOld and WhyNoFamily that holds. The model
// is the settings' (judge.models) or the adapter's JudgeDefault, and its
// company must be one the adapter can tell: a settings override that names
// another company's model judges nothing (fail closed).
//
// With a reason, Engine has no Judger and no Cleaner, so it cannot judge
// by mistake: it holds only the Agent and, once a is a Judger, Model and
// Effort, for the CLI's check lines.
func (g Gate) Engine(a core.Adapter, info core.AdapterInfo) (Engine, string) {
	e := Engine{Agent: a.Agent()}
	if g.Settings.Off {
		return e, WhyOff
	}
	if g.Folder == "" || !filepath.IsAbs(g.Folder) {
		return e, WhyNoFolder
	}
	j, ok := a.(core.Judger)
	if !ok {
		return e, WhyCannot
	}
	m := g.Settings.ModelFor(e.Agent, j.JudgeDefault())
	e.Model, e.Effort = m.Model, m.Effort
	if !info.Available {
		return e, WhyUnavailable
	}
	if min, ok := g.MinVersions[e.Agent]; !ok || !core.VersionAtLeast(info.Version, min) {
		return e, WhyOld
	}
	family := ""
	if e.Model != "" {
		family = j.Family(e.Model)
	}
	if family == "" {
		return e, WhyNoFamily
	}
	e.Judger, e.Family = j, family
	e.Cleaner, _ = a.(core.JudgeCleaner)
	return e, ""
}

// Writer is an AI that writes combined answers (종합 답변, S5b), with the
// model and effort it writes with, its ask tier's, and that model's
// company.
type Writer struct {
	Agent   string
	Synth   core.Synthesizer
	Judger  core.Judger       // names the company of the model that wrote
	Cleaner core.JudgeCleaner // nil when writing leaves nothing behind
	Model   string
	Effort  string // "" when the tier names none; the adapter picks
	Family  string
}

// Writer reports whether adapter a, whose Info is info, writes combined
// answers at tier. The reason is "" when it does, else Engine's reason
// (the gate of judging: off, folder, version, company), WhyCannotWrite
// when a is no core.Synthesizer or core.Asker or tier's model is empty, or
// WhyNoFamily when the company of tier's model is unknown. The model is
// a's AskTiers()[tier], so agent.json ask.tiers counts; a tier that is
// not valid is standard. With a reason, Writer has no Synth, Judger or
// Cleaner.
func (g Gate) Writer(a core.Adapter, info core.AdapterInfo, tier string) (Writer, string) {
	w := Writer{Agent: a.Agent()}
	e, why := g.Engine(a, info)
	if why != "" {
		return w, why
	}
	s, ok := a.(core.Synthesizer)
	asker, ok2 := a.(core.Asker)
	if !ok || !ok2 {
		return w, WhyCannotWrite
	}
	if !core.ValidTier(tier) {
		tier = core.TierStandard
	}
	m := asker.AskTiers()[tier]
	if m.Model == "" {
		return w, WhyCannotWrite
	}
	w.Model, w.Effort = m.Model, m.Effort
	family := e.Judger.Family(m.Model)
	if family == "" {
		return w, WhyNoFamily
	}
	w.Synth, w.Judger, w.Cleaner, w.Family = s, e.Judger, e.Cleaner, family
	return w, ""
}

// Verified is how the judge rules were verified (판정 검증): the day and
// each AI's CLI version. InfoResponse.judging carries JudgingVerified.
type Verified struct {
	Date     string            `json:"date"`               // "2026-11-01"
	Versions map[string]string `json:"versions,omitempty"` // AI → the CLI version it was verified with
}

// JudgingVerified is stage 1 of 판정 검증 (S6, 11-01 user decision 4): the
// day the judge rules and the quick judge models were measured, and the
// CLI version of each AI (by its adapter name) they were measured with.
// Judging gives a copy; the CLI's check names it when this PC's CLI is
// another version. Only change it after measuring again.
var JudgingVerified = Verified{Date: "2026-11-01", Versions: map[string]string{
	"claude":   "2.1.316",
	"codex":    "0.164.0",
	"opencode": "2.2.1",
}}

// TypicalMs is how long a comparison of three answers usually takes, in ms:
// the median wall time of the S6 product-view comparisons at the quick tier
// (30 questions, the slowest of their six requests). The phone shows it
// while comparing ('보통 {s}초쯤 걸립니다').
const TypicalMs = 40444

// JudgingInfo is InfoResponse.judging: how comparisons are judged on this
// PC. Fields may be added inside /v1, never removed (principle 4).
type JudgingInfo struct {
	Engine    string    `json:"engine"` // EngineName
	Shape     string    `json:"shape"`  // judge.ShapeDirected
	Labels    string    `json:"labels"` // judge.LabelsV1
	Rules     string    `json:"rules"`  // judge.Rules
	Checks    []string  `json:"checks"` // the enabled checks
	Verified  *Verified `json:"verified,omitempty"`
	TypicalMs int       `json:"typicalMs,omitempty"`
}

// Judging is the judging info of these settings. The verification (a copy
// of JudgingVerified) and TypicalMs come only with what S6 measured: the
// adapters' own judge models (no judge.models in agent.json) and the
// default checks, in any order (S6R-3). With other models or checks the
// phone keeps its 'not measured yet' footer and shows no time. judge.off,
// perAI and maxUnits (at most the 40 measured) keep them.
func (g Gate) Judging() JudgingInfo {
	ji := JudgingInfo{
		Engine: EngineName, Shape: judge.ShapeDirected, Labels: judge.LabelsV1, Rules: judge.Rules,
		Checks: g.Settings.ChecksOrDefault(),
	}
	if len(g.Settings.Models) == 0 && slices.Equal(slices.Sorted(slices.Values(ji.Checks)), slices.Sorted(slices.Values(judge.DefaultChecks))) {
		ji.Verified = &Verified{Date: JudgingVerified.Date, Versions: maps.Clone(JudgingVerified.Versions)}
		ji.TypicalMs = TypicalMs
	}
	return ji
}

// Announce returns every adapter's Info, in the adapters' order, as
// /v1/info and the CLI's info show it, and the engines of the AIs that
// judge. An AI with the capability ask gets its ask tiers. An AI that
// judges gets the capability judge, on a copy of its capability list (the
// adapter's own slice stays as it is), and its JudgeInfo; any other has
// neither, even when its adapter named judge itself: only the gate
// announces it. An AI that judges and whose adapter is a core.Synthesizer
// and a core.Asker also gets the capability synthesize (S5b): the same
// gate, so judge.off turns both off; an adapter's own synthesize is
// dropped like its judge. infos[i] is adapters[i]'s, so the CLI's check
// can ask Gate.Engine for the reasons.
func Announce(ctx context.Context, adapters []core.Adapter, g Gate) ([]core.AdapterInfo, []Engine) {
	infos := make([]core.AdapterInfo, 0, len(adapters))
	engines := []Engine{}
	for _, a := range adapters {
		info := a.Info(ctx)
		if asker, ok := a.(core.Asker); ok && slices.Contains(info.Capabilities, core.CapAsk) {
			info.Ask = &core.AskInfo{Tiers: asker.AskTiers()}
		}
		caps := slices.DeleteFunc(slices.Clone(info.Capabilities), func(c core.Capability) bool {
			return c == core.CapJudge || c == core.CapSynthesize
		})
		if caps == nil {
			caps = []core.Capability{}
		}
		info.Judge = nil
		if e, why := g.Engine(a, info); why == "" {
			caps = append(caps, core.CapJudge)
			_, writes := a.(core.Synthesizer)
			if _, asks := a.(core.Asker); writes && asks {
				caps = append(caps, core.CapSynthesize)
			}
			ji := e.Info()
			info.Judge = &ji
			engines = append(engines, e)
		}
		info.Capabilities = caps
		infos = append(infos, info)
	}
	return infos, engines
}
