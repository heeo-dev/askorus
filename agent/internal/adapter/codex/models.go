package codex

// The newest model of each tier's line (#64). Codex has no aliases like
// Claude's opus: its models are names with a version ("gpt-6.1-sol"), and
// a new version is a new name. `codex debug models` prints Codex's model
// catalog as JSON (0.171.0, 2026-11-20): each model's name (slug), whether
// it is listed or hidden, and the efforts it takes. The agent keeps the
// names of the listed models and their efforts, nothing else.

import (
	"context"
	"encoding/json"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/heeo-dev/askorus/agent/internal/config"
	"github.com/heeo-dev/askorus/agent/internal/core"
	"github.com/heeo-dev/askorus/agent/internal/proc"
)

// modelsArgs is the only command the list runs.
var modelsArgs = []string{"debug", "models"}

const modelsTimeout = 30 * time.Second

// lines are the lines the tiers follow (quick Luna, standard Terra, deep
// Sol) and the line of the extra model (Astra, #65), each from the model
// the agent ships with.
var lines = []core.Line{
	{Base: "gpt-6-luna", Re: regexp.MustCompile(`^gpt-([0-9]+(?:\.[0-9]+)*)-luna$`)},
	{Base: "gpt-6.1-terra", Re: regexp.MustCompile(`^gpt-([0-9]+(?:\.[0-9]+)*)-terra$`)},
	{Base: "gpt-6.1-sol", Re: regexp.MustCompile(`^gpt-([0-9]+(?:\.[0-9]+)*)-sol$`)},
	{Base: "gpt-6.2-astra", Re: regexp.MustCompile(`^gpt-([0-9]+(?:\.[0-9]+)*)-astra$`)},
}

// extraModels are the models outside the tiers the person may pick (#65):
// Astra, which uses much more of the plan (2026-11-20 decision), from the
// shipped one of its line.
var extraModels = []core.ExtraModel{{Model: "gpt-6.2-astra", Heavy: true}}

// AskExtra lists them, each the newest of its line (core.ExtraLister).
func (a *Adapter) AskExtra() []core.ExtraModel {
	out := make([]core.ExtraModel, len(extraModels))
	for i, e := range extraModels {
		e.Model = a.follow.Newest(e.Model, "")
		out[i] = e
	}
	return out
}

// parseModels reads `codex debug models`: the listed models (visibility
// "list"; hidden ones such as the review model are left out) with their
// efforts. Anything that is not the catalog lists nothing.
func parseModels(out []byte) []core.ListedModel {
	var v struct {
		Models []struct {
			Slug       string            `json:"slug"`
			Visibility string            `json:"visibility"`
			Levels     []json.RawMessage `json:"supported_reasoning_levels"`
		} `json:"models"`
	}
	if json.Unmarshal(out, &v) != nil {
		return nil
	}
	var list []core.ListedModel
	for _, m := range v.Models {
		if m.Visibility != "list" || !core.ValidAskModel(m.Slug) {
			continue
		}
		var es []string
		for _, raw := range m.Levels {
			var l struct {
				Effort string `json:"effort"`
			}
			var s string
			switch {
			case json.Unmarshal(raw, &l) == nil && l.Effort != "":
				s = l.Effort
			case json.Unmarshal(raw, &s) == nil:
			}
			if efforts[s] {
				es = append(es, s)
			}
		}
		list = append(list, core.ListedModel{Name: m.Slug, Efforts: core.SortEfforts(es)})
	}
	return list
}

// listModels runs the list in codex's own folder.
func (a *Adapter) listModels(ctx context.Context) ([]core.ListedModel, error) {
	if a.exeErr != nil {
		return nil, a.exeErr
	}
	ctx, cancel := context.WithTimeout(ctx, modelsTimeout)
	defer cancel()
	res, err := proc.Run(ctx, proc.Cmd{Exe: a.exe, Dir: filepath.Dir(a.exe), Args: modelsArgs})
	if err != nil || res.ExitCode != 0 {
		return nil, err
	}
	return parseModels(res.Stdout), nil
}

// Follow returns the model to run for a model the phone picked
// (core.Following).
func (a *Adapter) Follow(model string) string { return core.FollowPick(a.AskTiers(), a.follow, model) }

// Newer maps each shipped tier model to the newer one a tier uses now
// (core.Following).
func (a *Adapter) Newer() map[string]string { return core.NewerTiers(a.AskTiers(), a.follow) }

// WaitModels waits for a model list that runs (Info starts one), or until
// ctx ends: for a command that runs once, and for tests.
func (a *Adapter) WaitModels(ctx context.Context) { a.follow.Wait(ctx) }

// askCheck is the agent's own check of a model and effort, before codex
// runs. A request it refuses never leaves a newer model out: a paired
// phone sending an effort codex does not have must not cost every phone
// the newer model (2ho's 0.5.1 review). Only codex's own refusal does.
func askCheck(model, effort string) error {
	switch {
	case !config.ValidModel(model):
		return core.Fail(core.ErrCodeBadModel, model)
	case effort != "" && !efforts[effort]:
		return core.Fail(core.ErrCodeBadModel, "effort "+effort)
	}
	return nil
}

// refused reports whether codex refused the model itself: it does not
// know it, or the person's plan does not offer it.
func refused(err error) bool {
	if core.ErrorCode(err) == core.ErrCodeBadModel {
		return true
	}
	d := strings.ToLower(core.ErrorDetail(err))
	return err != nil && strings.Contains(d, "model") && strings.Contains(d, "not supported")
}
