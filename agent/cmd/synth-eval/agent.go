//go:build syntheval

package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"slices"
	"time"

	"github.com/asksemble/asksemble/agent/internal/adapter/claude"
	"github.com/asksemble/asksemble/agent/internal/adapter/codex"
	"github.com/asksemble/asksemble/agent/internal/adapter/opencode"
	"github.com/asksemble/asksemble/agent/internal/compares"
	"github.com/asksemble/asksemble/agent/internal/config"
	"github.com/asksemble/asksemble/agent/internal/core"
	"github.com/asksemble/asksemble/agent/internal/judge"
	"github.com/asksemble/asksemble/agent/internal/runs"
)

// agentPipeline asks in this process the way POST /v1/runs does with a
// comparison and a combined answer: the PC's AIs answer in the ask folder
// (as for the phone, so their conversations stay in each AI's history like
// the phone's), the comparison is attached to the run, and the writer
// writes once the judges are done. The running agent is not used.
type agentPipeline struct {
	folder   string
	adapters []core.Adapter
	askers   map[string]core.Asker
	order    []string
	runs     *runs.Manager
	cmp      *compares.Manager
}

const owner = "synth-eval"

// questionLimit bounds one question: answers, judges and the combined answer.
var questionLimit = 15 * time.Minute

func newAgentPipeline(ctx context.Context) (*agentPipeline, error) {
	dir, err := config.Dir()
	if err != nil {
		return nil, err
	}
	cfg, err := config.Load(dir)
	if err != nil {
		return nil, err
	}
	folder, err := cfg.AskFolder()
	if err != nil {
		return nil, err
	}
	claudeDir, err := claude.DefaultClaudeDir()
	if err != nil {
		return nil, err
	}
	adapters := []core.Adapter{
		claude.New(claude.Options{ClaudeDir: claudeDir, ClaudePath: cfg.ClaudePath, DefaultModel: cfg.DefaultModel, DefaultEffort: cfg.DefaultEffort, AskTiers: cfg.Ask.Tiers[claude.Name]}),
		codex.New(codex.Options{Path: cfg.CodexPath, Tiers: cfg.Ask.Tiers[codex.Name]}),
		opencode.New(opencode.Options{Path: cfg.OpenCodePath, Tiers: cfg.Ask.Tiers[opencode.Name]}),
	}
	return pipelineFrom(ctx, adapters, folder, cfg.Judge)
}

// pipelineFrom makes the pipeline on these adapters, asking in folder and
// judging as settings say (tests give fakes).
func pipelineFrom(ctx context.Context, adapters []core.Adapter, folder string, settings config.JudgeConfig) (*agentPipeline, error) {
	p := &agentPipeline{folder: folder, adapters: adapters, askers: map[string]core.Asker{}, runs: runs.NewManager()}
	for _, a := range adapters {
		info := a.Info(ctx)
		if asker, ok := a.(core.Asker); ok && info.Available && slices.Contains(info.Capabilities, core.CapAsk) {
			p.askers[a.Agent()] = asker
			p.order = append(p.order, a.Agent())
		}
	}
	if len(p.order) < 2 {
		return nil, errors.New("fewer than two AIs can answer on this PC (asksemble-agent info)")
	}
	if settings.Off {
		return nil, errors.New("comparison judging is off (agent.json judge.off)")
	}
	gate := compares.Gate{
		Settings:    settings,
		MinVersions: map[string]string{claude.Name: claude.JudgeMinVersion, codex.Name: codex.JudgeMinVersion, opencode.Name: opencode.JudgeMinVersion},
		Folder:      core.JudgeFolder(folder),
	}
	if err := os.MkdirAll(folder, 0o700); err != nil {
		return nil, err
	}
	p.cmp = compares.New(compares.Options{Adapters: adapters, Gate: gate, Runs: p.runs, Logger: slog.New(slog.DiscardHandler)})
	p.runs.OnEnd = p.cmp.RunEnded // as the API: an ended run starts its comparison
	return p, nil
}

func (p *agentPipeline) Close() { p.cmp.Close() }

// Versions are the AI programs' versions, for the record.
func (p *agentPipeline) Versions(ctx context.Context) map[string]string {
	out := map[string]string{}
	for _, a := range p.adapters {
		if info := a.Info(ctx); info.Version != "" {
			out[a.Agent()] = info.Version
		}
	}
	return out
}

// Ask asks every AI that can answer, at the run's tier, with the
// comparison and the combined answer attached, and waits for all three.
func (p *agentPipeline) Ask(ctx context.Context, q Question, run RunInfo) (Record, error) {
	ctx, cancel := context.WithTimeout(ctx, questionLimit)
	defer cancel()
	var tasks []runs.Task
	for _, name := range p.order {
		asker := p.askers[name]
		req := core.AskRequest{Folder: p.folder, Prompt: core.AskPrompt(q.Text), Tier: run.Tier}
		tasks = append(tasks, runs.Task{Agent: name, Ask: func(ctx context.Context) (core.AskResult, error) { return asker.Ask(ctx, req) }})
	}
	r, err := p.runs.Start(runs.Spec{Owner: owner, Mode: core.ModeAsk, Tier: run.Tier, Tasks: tasks})
	if err != nil {
		return Record{}, err
	}
	cid, err := p.cmp.Attach(compares.RunSpec{
		Owner: owner, RunID: r.ID, Question: q.Text, Judges: p.order, Asked: p.order,
		Synth: &compares.SynthSpec{Agent: run.Writer, Tier: run.Tier},
	})
	if err != nil {
		return Record{}, fmt.Errorf("attach: %w", err)
	}
	// The answers.
	for r.Status == runs.Running {
		if err := sleep(ctx, 250*time.Millisecond); err != nil {
			return Record{}, err
		}
		r, _ = p.runs.Get(owner, r.ID)
	}
	answersAt := time.Now()
	var rec Record
	for _, part := range r.Parts {
		a := AnswerRec{Agent: part.Agent, Model: part.Model, Status: string(part.Status), Ms: part.Ms, In: part.InputTokens, Out: part.OutputTokens, Text: part.Answer}
		if part.Error != nil {
			a.Error, a.Detail = part.Error.Code, clip(part.Error.Message, 300)
		}
		rec.Answers = append(rec.Answers, a)
	}
	// The judges, then the combined answer.
	var c compares.Comparison
	var judgedAt time.Time
	for {
		c, _ = p.cmp.Get(owner, cid)
		if judgedAt.IsZero() && c.Status != compares.Waiting && c.Status != compares.Running {
			judgedAt = time.Now()
		}
		s := c.Synthesis
		if !judgedAt.IsZero() && (s == nil || (s.Status != compares.SynthWaiting && s.Status != compares.SynthRunning)) {
			break
		}
		if err := sleep(ctx, 250*time.Millisecond); err != nil {
			return rec, err
		}
	}
	cr := &CompareRec{Status: c.Status, Ms: judgedAt.Sub(answersAt).Milliseconds(), Counts: c.Counts, Requests: len(c.Requests)}
	for _, req := range c.Requests {
		if req.Status == judge.RequestFailed {
			cr.Failed++
			if req.Error != nil && cr.Detail == "" {
				cr.Detail = clip(req.Error.Message, 300)
			}
		}
	}
	if c.Error != nil {
		cr.Error, cr.Detail = c.Error.Code, clip(c.Error.Message, 300)
	}
	rec.Compare = cr
	if s := c.Synthesis; s != nil {
		sr := &SynthRec{Status: s.Status, Agent: s.Agent, Model: s.Model, Ms: s.Ms, In: s.InputTokens, Out: s.OutputTokens, Judged: s.Judged, Unnamed: s.UnnamedSides, Text: s.Text}
		if s.Error != nil {
			sr.Error, sr.Detail = s.Error.Code, clip(s.Error.Message, 300)
		}
		rec.Synth = sr
	}
	return rec, nil
}

func sleep(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}
