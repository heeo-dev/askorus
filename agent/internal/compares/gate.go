package compares

import (
	"context"
	"path/filepath"
	"slices"

	"github.com/asksemble/asksemble/agent/internal/config"
	"github.com/asksemble/asksemble/agent/internal/core"
)

// Why an AI does not pass the gate, as Gate.Engine gives it, in the order
// Engine checks them. The CLI's check turns them into text. The values are
// the judging days' (cannot_judge: the adapter cannot name its models'
// companies).
const (
	WhyOff         = "off"             // agent.json judge.off
	WhyNoFolder    = "no_folder"       // no ask folder, so no judge folder
	WhyCannot      = "cannot_judge"    // the adapter is no core.Judger, so it cannot name its models' companies
	WhyUnavailable = "unavailable"     // the AI program is not found or does not run
	WhyOld         = "old_version"     // older than the version its one-off command was tested with, or unreadable
	WhyNoFamily    = "unknown_company" // the company of its tier's model is unknown (Gate.Writer only)
	// WhyCannotWrite: the AI passes the gate but cannot write combined
	// answers: its adapter is no core.Synthesizer or core.Asker, or its ask
	// tier has no model (Gate.Writer only).
	WhyCannotWrite = "cannot_synthesize"
)

// EngineName is Comparison.engine: the AI programs on this PC write, as an
// answer's via "pc" says they answered. A later API engine gets a name of
// its own (S3 decision 1).
const EngineName = "pc"

// Gate decides which AIs may run the one-off commands of a comparison.
// The API's /v1/info, the CLI's info and check and the comparisons all ask
// it, so they cannot disagree.
type Gate struct {
	Settings config.JudgeConfig
	// MinVersions maps an AI to the oldest version of its CLI the one-off
	// commands were tested with (its adapter's JudgeMinVersion). An AI not
	// listed never writes.
	MinVersions map[string]string
	// Folder is the judge folder, core.JudgeFolder(the ask folder), where
	// the one-off calls run: "" when there is no ask folder, and then no AI
	// writes.
	Folder string
}

// Engine is an AI that passes the gate: it may run the one-off commands
// (the combined answer, refining). The name is the judging days'.
type Engine struct {
	Agent   string
	Judger  core.Judger       // names the company of a model
	Cleaner core.JudgeCleaner // nil when a call leaves nothing behind
}

// Engine reports whether adapter a, whose Info is info, passes the gate.
// The reason is "" when it does, else the first of WhyOff, WhyNoFolder,
// WhyCannot, WhyUnavailable and WhyOld that holds. With a reason, Engine
// has no Judger and no Cleaner, so it cannot be used by mistake.
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
	if !info.Available {
		return e, WhyUnavailable
	}
	if min, ok := g.MinVersions[e.Agent]; !ok || !core.VersionAtLeast(info.Version, min) {
		return e, WhyOld
	}
	e.Judger = j
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
// (off, folder, version), WhyCannotWrite
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

// Announce returns every adapter's Info, in the adapters' order, as
// /v1/info and the CLI's info show it, and the engines of the AIs that
// pass the gate (Gate.Engine). An AI with the capability ask gets its ask
// tiers. An AI that passes the gate and whose adapter is a
// core.Synthesizer and a core.Asker gets the capability synthesize (S5b),
// on a copy of its capability list (the adapter's own slice stays as it
// is); any other has not, even when its adapter named it itself: only the
// gate announces it, so judge.off turns it off. Since C6d (#24) no AI
// announces judge. infos[i] is adapters[i]'s, so the CLI's
// check can ask Gate.Engine for the reasons.
func Announce(ctx context.Context, adapters []core.Adapter, g Gate) ([]core.AdapterInfo, []Engine) {
	infos := make([]core.AdapterInfo, 0, len(adapters))
	engines := []Engine{}
	for _, a := range adapters {
		info := a.Info(ctx)
		if asker, ok := a.(core.Asker); ok && slices.Contains(info.Capabilities, core.CapAsk) {
			info.Ask = &core.AskInfo{Tiers: asker.AskTiers()}
		}
		caps := slices.DeleteFunc(slices.Clone(info.Capabilities), func(c core.Capability) bool {
			return c == core.CapSynthesize
		})
		if caps == nil {
			caps = []core.Capability{}
		}
		if e, why := g.Engine(a, info); why == "" {
			_, writes := a.(core.Synthesizer)
			if _, asks := a.(core.Asker); writes && asks {
				caps = append(caps, core.CapSynthesize)
			}
			engines = append(engines, e)
		}
		info.Capabilities = caps
		infos = append(infos, info)
	}
	return infos, engines
}
