package agy

// The newest model of the tiers' line (#64). `agy models` lists the models
// agy can use, one a line: the name with its thinking level, a tab and a
// label ("gemini-3.8-flash-low\tGemini 3.8 Flash (Low)", 1.5.11 on
// 2026-11-20). The agent keeps the Gemini names and their levels.

import (
	"context"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/heeo-dev/askorus/agent/internal/core"
	"github.com/heeo-dev/askorus/agent/internal/proc"
)

// modelsArgs is the only command the list runs.
var modelsArgs = []string{"models"}

const modelsTimeout = 30 * time.Second

// lines are the lines the tiers follow: Gemini Flash.
var lines = []core.Line{
	{Base: "gemini-3.8-flash", Re: regexp.MustCompile(`^gemini-([0-9]+(?:\.[0-9]+)*)-flash$`)},
}

// listedModels reads `agy models`: each Gemini model with the levels it is
// listed with. A name without a level is a model that takes none.
func listedModels(out []byte) []core.ListedModel {
	levels := map[string][]string{}
	var order []string
	for _, line := range strings.Split(string(out), "\n") {
		name, _, _ := strings.Cut(strings.TrimSpace(line), "\t")
		name = strings.TrimSpace(name)
		if !modelRe.MatchString(name) || !core.ValidAskModel(name) {
			continue // "Fetching available models...", other companies' models
		}
		base, level := name, ""
		for e := range efforts {
			if strings.HasSuffix(name, "-"+e) {
				base, level = strings.TrimSuffix(name, "-"+e), e
				break
			}
		}
		if _, seen := levels[base]; !seen {
			levels[base] = []string{}
			order = append(order, base)
		}
		if level != "" {
			levels[base] = append(levels[base], level)
		}
	}
	list := make([]core.ListedModel, 0, len(order))
	for _, b := range order {
		list = append(list, core.ListedModel{Name: b, Efforts: core.SortEfforts(levels[b])})
	}
	return list
}

// listModels runs the list in agy's own folder.
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
	return listedModels(res.Stdout), nil
}

// askCheck is the agent's own check of a model and effort, before agy
// runs. A request it refuses never leaves a newer model out (2ho's 0.5.1
// review); only agy's own refusal does.
func askCheck(model, effort string) error {
	switch {
	case !modelRe.MatchString(model):
		return core.Fail(core.ErrCodeBadModel, model)
	case effort != "" && !efforts[effort]:
		return core.Fail(core.ErrCodeBadModel, "effort "+effort)
	}
	return nil
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
