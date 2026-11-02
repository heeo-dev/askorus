//go:build judgeeval

package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"

	"github.com/asksemble/asksemble/agent/internal/adapter/claude"
	"github.com/asksemble/asksemble/agent/internal/adapter/codex"
	"github.com/asksemble/asksemble/agent/internal/adapter/opencode"
	"github.com/asksemble/asksemble/agent/internal/compares"
	"github.com/asksemble/asksemble/agent/internal/config"
	"github.com/asksemble/asksemble/agent/internal/core"
)

// Engines (plan part 2). The three adapters are built as the agent builds
// them, with no tier overrides, and the product's gate (compares.Gate and
// Announce) decides whether each judges: version, family and model. agent.json
// is read only for the exe paths and the ask folder; its judge.models and
// ask.tiers are ignored, so a run is the same on every PC (deviation 11).
// No login file is ever read.

// Engine names.
const (
	engineGold = "gold" // in-process: answers from the gold labels, no model call
)

// realEngines are the engines that call an AI's CLI.
var realEngines = []string{claude.Name, codex.Name, opencode.Name}

// engineNames are every engine -engines may name.
var engineNames = append(slices.Clone(realEngines), engineGold)

// Eval folder and Codex schema folder names.
const (
	evalFolderName = "eval"         // <ask>\judge\eval
	codexSchemaDir = "codex-schema" // <run>\codex-schema
)

// engine is one judging engine of a run.
type engine struct {
	name    string
	judger  core.Judger // nil for gold, which makes one per job
	cleaner core.JudgeCleaner
	model   string
	effort  string
	family  string
	version string
}

func (e *engine) gold() bool { return e.name == engineGold }

func (e *engine) info() engineInfo {
	return engineInfo{Model: e.model, Effort: e.effort, Family: e.family, Version: e.version}
}

// answerFamily is the company of the model that wrote an answer, as its
// adapter names it. Family reads only the model name, so a zero adapter
// does: nothing is looked up or run.
func answerFamily(ai, model string) string {
	if model == "" {
		return ""
	}
	switch ai {
	case claude.Name:
		return new(claude.Adapter).Family(model)
	case codex.Name:
		return new(codex.Adapter).Family(model)
	case opencode.Name:
		return new(opencode.Adapter).Family(model)
	}
	return ""
}

// judgeFamilies are the companies the product plans its judges with when
// no engine of the run says: each adapter's JudgeDefault model.
func judgeFamilies() map[string]string {
	return map[string]string{
		claude.Name:   answerFamily(claude.Name, new(claude.Adapter).JudgeDefault().Model),
		codex.Name:    answerFamily(codex.Name, new(codex.Adapter).JudgeDefault().Model),
		opencode.Name: answerFamily(opencode.Name, new(opencode.Adapter).JudgeDefault().Model),
	}
}

// engineSetup is what setupEngines made.
type engineSetup struct {
	engines     []*engine
	evalFolder  string // "" for a gold-only run
	codexSchema string // "" without codex
}

// buildEngines makes the engines of a run; tests replace it.
var buildEngines = setupEngines

// setupEngines builds the named engines for a run in runDir with tier.
// A real engine the gate does not let judge is an error, before any call.
func setupEngines(ctx context.Context, names []string, tier, runDir string) (engineSetup, error) {
	var st engineSetup
	var adapters []core.Adapter
	var cfg config.Config
	needReal := slices.ContainsFunc(names, func(n string) bool { return slices.Contains(realEngines, n) })
	if needReal {
		dir, err := config.Dir()
		if err != nil {
			return st, err
		}
		if cfg, err = config.Load(dir); err != nil {
			return st, err
		}
		ask, err := cfg.AskFolder()
		if err != nil {
			return st, err
		}
		st.evalFolder = filepath.Join(core.JudgeFolder(ask), evalFolderName)
		if err := os.MkdirAll(st.evalFolder, 0o700); err != nil {
			return st, fmt.Errorf("eval folder: %w", err)
		}
	}
	for _, n := range names {
		switch n {
		case claude.Name:
			claudeDir, err := claude.DefaultClaudeDir()
			if err != nil {
				return st, err
			}
			adapters = append(adapters, claude.New(claude.Options{ClaudeDir: claudeDir, ClaudePath: cfg.ClaudePath}))
		case codex.Name:
			st.codexSchema = filepath.Join(runDir, codexSchemaDir)
			if err := os.MkdirAll(st.codexSchema, 0o700); err != nil {
				return st, fmt.Errorf("codex schema folder: %w", err)
			}
			adapters = append(adapters, codex.New(codex.Options{Path: cfg.CodexPath, SchemaDir: st.codexSchema}))
		case opencode.Name:
			adapters = append(adapters, opencode.New(opencode.Options{Path: cfg.OpenCodePath}))
		}
	}
	if len(adapters) > 0 {
		models, err := tierModels(adapters, tier)
		if err != nil {
			return st, err
		}
		gate := compares.Gate{
			Settings: config.JudgeConfig{Models: models},
			MinVersions: map[string]string{
				claude.Name: claude.JudgeMinVersion, codex.Name: codex.JudgeMinVersion, opencode.Name: opencode.JudgeMinVersion,
			},
			Folder: st.evalFolder,
		}
		infos, engines := compares.Announce(ctx, adapters, gate)
		for i, a := range adapters {
			k := slices.IndexFunc(engines, func(e compares.Engine) bool { return e.Agent == a.Agent() })
			if k < 0 {
				_, why := gate.Engine(a, infos[i])
				return st, fmt.Errorf("%s does not judge: %s (version %q)", a.Agent(), why, infos[i].Version)
			}
			e := engines[k]
			st.engines = append(st.engines, &engine{name: e.Agent, judger: e.Judger, cleaner: e.Cleaner,
				model: e.Model, effort: e.Effort, family: e.Family, version: infos[i].Version})
		}
	}
	if slices.Contains(names, engineGold) {
		st.engines = append(st.engines, &engine{name: engineGold, model: goldModel, family: goldModel, version: "-"})
	}
	// In the order -engines named them.
	slices.SortStableFunc(st.engines, func(a, b *engine) int { return slices.Index(names, a.name) - slices.Index(names, b.name) })
	return st, nil
}

// tierModels are the judge models of a tier: none for quick (each adapter's
// JudgeDefault), each adapter's standard ask tier for standard.
func tierModels(adapters []core.Adapter, tier string) (map[string]core.AskModel, error) {
	switch tier {
	case core.TierQuick:
		return nil, nil
	case core.TierStandard:
		out := map[string]core.AskModel{}
		for _, a := range adapters {
			asker, ok := a.(core.Asker)
			if !ok {
				return nil, fmt.Errorf("%s has no ask tiers", a.Agent())
			}
			m, ok := asker.AskTiers()[core.TierStandard]
			if !ok || m.Model == "" {
				return nil, fmt.Errorf("%s has no standard tier", a.Agent())
			}
			out[a.Agent()] = m
		}
		return out, nil
	}
	return nil, errors.New("tier must be quick or standard")
}
