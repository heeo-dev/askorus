//go:build syntheval

package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/heeo-dev/askorus/agent/internal/adapter/claude"
	"github.com/heeo-dev/askorus/agent/internal/adapter/codex"
	"github.com/heeo-dev/askorus/agent/internal/adapter/opencode"
	"github.com/heeo-dev/askorus/agent/internal/compares"
	"github.com/heeo-dev/askorus/agent/internal/config"
	"github.com/heeo-dev/askorus/agent/internal/core"
	"github.com/heeo-dev/askorus/agent/internal/judge"
	"github.com/heeo-dev/askorus/agent/internal/runs"
)

// agentPipeline asks in this process the way POST /v1/runs does with a
// comparison and a combined answer: the PC's AIs answer in the ask folder
// (as for the phone, so their conversations stay in each AI's history like
// the phone's), the comparison is attached to the run, and the writer
// writes once the answers are in. The running agent is not used.
type agentPipeline struct {
	folder   string
	adapters []core.Adapter
	askers   map[string]core.Asker
	order    []string
	runs     *runs.Manager
	cmp      *compares.Manager
	// judgeFolder is where the writers run (the agent's judge folder);
	// names are the AIs' names as the writer gets them (Claude, Codex,
	// OpenCode).
	judgeFolder string
	names       map[string]string
}

const owner = "synth-eval"

// questionLimit bounds one question: the answers and the combined answer.
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
// writing as settings say (tests give fakes).
func pipelineFrom(ctx context.Context, adapters []core.Adapter, folder string, settings config.JudgeConfig) (*agentPipeline, error) {
	p := &agentPipeline{folder: folder, adapters: adapters, askers: map[string]core.Asker{}, runs: runs.NewManager(), names: map[string]string{}}
	for _, a := range adapters {
		if n, ok := a.(core.Named); ok && strings.TrimSpace(n.DisplayName()) != "" {
			p.names[a.Agent()] = strings.TrimSpace(n.DisplayName())
		}
		info := a.Info(ctx)
		if asker, ok := a.(core.Asker); ok && info.Available && slices.Contains(info.Capabilities, core.CapAsk) {
			p.askers[a.Agent()] = asker
			p.order = append(p.order, a.Agent())
		}
	}
	if len(p.order) < 2 {
		return nil, errors.New("fewer than two AIs can answer on this PC (askorus-agent info)")
	}
	if settings.Off {
		return nil, errors.New("combined answers are off (agent.json judge.off)")
	}
	gate := compares.Gate{
		Settings:    settings,
		MinVersions: map[string]string{claude.Name: claude.JudgeMinVersion, codex.Name: codex.JudgeMinVersion, opencode.Name: opencode.JudgeMinVersion},
		Folder:      core.JudgeFolder(folder),
	}
	if err := os.MkdirAll(folder, 0o700); err != nil {
		return nil, err
	}
	p.judgeFolder = gate.Folder
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
	// Refining (C2), as POST /v1/runs with refine {agent: writer}: the
	// first AI to start asks the moderator, all wait for the spec.
	var refiner *compares.RunRefiner
	runID := make(chan string, 1)
	if run.Refine == "on" {
		var id string
		var once sync.Once
		refiner = p.cmp.NewRunRefiner(compares.RefineSpec{Question: q.Text, Asked: p.order, Agent: run.Writer}, func(x compares.Refined) {
			once.Do(func() { id = <-runID })
			p.runs.SetRefine(owner, id, x)
		})
	}
	// Refining with a light or a live format (C4 E3) is done here, before
	// any AI is asked: the moderator at run.ModTier, then every AI gets the
	// question with that format under it, as the agent would send it.
	asked := q.Text
	var own *RefineRec
	if run.Refine == refineLight || run.Refine == refineLive {
		form := formFixed
		if run.Refine == refineLive {
			form = formLive
		}
		own = refineOnce(ctx, p, q.Text, RunInfo{Writer: run.Writer, Tier: run.ModTier, Form: form})
		own.Before = true
		if own.Status == "done" {
			if run.Refine == refineLight {
				own.Template = lightTemplate(specOf(own))
			}
			if own.Template != "" {
				asked = strings.TrimRight(q.Text, " \t\r\n") + "\n\n" + own.Template
			}
		}
	}
	var tasks []runs.Task
	for _, name := range p.order {
		asker := p.askers[name]
		req := core.AskRequest{Folder: p.folder, Prompt: core.AskPrompt(asked), Tier: run.Tier}
		tasks = append(tasks, runs.Task{Agent: name, Ask: func(ctx context.Context) (core.AskResult, error) {
			if refiner != nil {
				text, err := refiner.Question(ctx)
				if err != nil {
					return core.AskResult{}, err
				}
				req.Prompt = core.AskPrompt(text)
			}
			return asker.Ask(ctx, req)
		}})
	}
	r, err := p.runs.Start(runs.Spec{Owner: owner, Mode: core.ModeAsk, Tier: run.Tier, Tasks: tasks})
	runID <- r.ID
	if err != nil {
		return Record{}, err
	}
	if own != nil && own.Status == "done" {
		sp := specOf(own)
		p.runs.SetRefine(owner, r.ID, compares.Refined{Status: compares.RefineDone, Agent: own.Agent, Model: own.Model, Ms: own.Ms, Spec: &sp, Template: own.Template})
	}
	synth := &compares.SynthSpec{Agent: run.Writer, Tier: run.Tier}
	if run.Synth == synthOff {
		synth = nil // written later with resynth
	}
	cid, err := p.cmp.Attach(compares.RunSpec{
		Owner: owner, RunID: r.ID, Question: q.Text, Judges: p.order, Asked: p.order,
		Synth: synth,
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
	if rf := r.Refine; rf != nil {
		rr := &RefineRec{Status: rf.Status, Agent: rf.Agent, Model: rf.Model, Ms: rf.Ms, Template: rf.Template}
		if rf.Spec != nil {
			rr.Kind, rr.Depth, rr.Language, rr.Intent = rf.Spec.Kind, rf.Spec.Depth, rf.Spec.Language, rf.Spec.Intent
		}
		if rf.Error != nil {
			rr.Error, rr.Detail = rf.Error.Code, clip(rf.Error.Message, 300)
		}
		rec.Refine = rr
	}
	if own != nil {
		rec.Refine = own // its form lines, Before, or its failure
	}
	for _, part := range r.Parts {
		a := AnswerRec{Agent: part.Agent, Model: part.Model, Status: string(part.Status), Ms: part.Ms, In: part.InputTokens, Out: part.OutputTokens, Text: part.Answer}
		if part.Error != nil {
			a.Error, a.Detail = part.Error.Code, clip(part.Error.Message, 300)
		}
		rec.Answers = append(rec.Answers, a)
	}
	// The comparison, which ends once it has the answers, then the
	// combined answer.
	var c compares.Comparison
	var endedAt time.Time
	for {
		c, _ = p.cmp.Get(owner, cid)
		if endedAt.IsZero() && c.Status != compares.Waiting && c.Status != compares.Running {
			endedAt = time.Now()
		}
		s := c.Synthesis
		if !endedAt.IsZero() && (s == nil || s.Status == compares.SynthOff || (s.Status != compares.SynthWaiting && s.Status != compares.SynthRunning)) {
			break
		}
		if err := sleep(ctx, 250*time.Millisecond); err != nil {
			return rec, err
		}
	}
	cr := &CompareRec{Status: c.Status, Ms: endedAt.Sub(answersAt).Milliseconds()}
	if c.Error != nil {
		cr.Error, cr.Detail = c.Error.Code, clip(c.Error.Message, 300)
	}
	rec.Compare = cr
	rec.Task = p.synthTask(q.Text, c, rec.Answers, r.Refine)
	if s := c.Synthesis; s != nil && s.Status != compares.SynthOff {
		sr := &SynthRec{Status: s.Status, Agent: s.Agent, Model: s.Model, Ms: s.Ms, In: s.InputTokens, Out: s.OutputTokens, Text: s.Text}
		if s.Error != nil {
			sr.Error, sr.Detail = s.Error.Code, clip(s.Error.Message, 300)
		}
		rec.Synth = sr
	}
	return rec, nil
}

// Refining of a run (RunInfo.Refine) besides off and on (the agent's).
const (
	refineLight = "light" // the agent's spec, lightTemplates (C4 E3)
	refineLive  = "live"  // the moderator writes the format (C4 E3)
)

// synthOff in RunInfo.Synth: the run writes no combined answer.
const synthOff = "off"

// specOf is the spec a refine record holds.
func specOf(r *RefineRec) judge.QuestionSpec {
	return judge.QuestionSpec{Kind: r.Kind, Intent: r.Intent, Depth: r.Depth, Language: r.Language}
}

// synthTask is the task the writer got, made again from the comparison's
// snapshot (its answers, in the turn's order) and the answers' texts the
// way compares makes it, so resynth can write once more from the same
// answers (C4, E0). It is nil without a comparison.
func (p *agentPipeline) synthTask(question string, c compares.Comparison, answers []AnswerRec, rf *runs.Refine) *judge.SynthTask {
	if len(c.Answers) == 0 {
		return nil
	}
	text := map[string]string{}
	for _, a := range answers {
		text[a.Agent] = a.Text
	}
	spec := judge.Spec{Question: question}
	agentOf := map[string]string{}
	for _, a := range c.Answers {
		a.Text = text[a.Agent]
		agentOf[a.ID] = a.Agent
		spec.Answers = append(spec.Answers, a)
	}
	t := judge.BuildSynthTask(spec, func(id string) string {
		if n := p.names[agentOf[id]]; n != "" {
			return n
		}
		return agentOf[id]
	})
	if rf != nil && rf.Status == compares.RefineDone && rf.Spec != nil {
		sp := *rf.Spec
		t.Spec = &sp
	}
	return &t
}

// Write makes one writer call the way the agent does: the AI's writer at
// the tier (compares.Writer: its model and effort), the company check on
// the model that wrote, and what it left behind removed (OpenCode's
// conversation).
func (p *agentPipeline) Write(ctx context.Context, r writeReq) (writeRes, error) {
	ws := p.cmp.Writers(ctx, r.Tier)
	i := slices.IndexFunc(ws, func(w compares.Writer) bool { return w.Agent == r.Agent })
	if i < 0 {
		return writeRes{Agent: r.Agent}, fmt.Errorf("%s does not write at tier %s on this PC (askorus-agent check)", r.Agent, r.Tier)
	}
	w := ws[i]
	if err := os.MkdirAll(p.judgeFolder, 0o700); err != nil {
		return writeRes{Agent: r.Agent}, err
	}
	req := core.SynthRequest{Folder: p.judgeFolder, Prompt: r.Prompt, Model: w.Model, Effort: w.Effort, Refine: r.Refine}
	if r.Refine {
		req.Effort = "" // as compares.Refine: the adapter's refine effort
	}
	start := time.Now()
	res, err := w.Synth.Synthesize(ctx, req)
	out := writeRes{Agent: w.Agent, Model: w.Model, Ms: time.Since(start).Milliseconds(), In: res.InputTokens, Out: res.OutputTokens, Output: res.Output}
	if res.Model != "" {
		out.Model = res.Model
	}
	if res.Cleanup != "" && w.Cleaner != nil {
		cctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 2*core.JudgeCleanupTimeout)
		_ = w.Cleaner.CleanupJudge(cctx, p.judgeFolder, []string{res.Cleanup})
		cancel()
	}
	if err != nil {
		return out, err
	}
	if family := w.Judger.Family(out.Model); family == "" || !strings.EqualFold(family, w.Family) {
		return out, fmt.Errorf("the model that wrote, %q, is not of %s's company", out.Model, w.Agent)
	}
	return out, nil
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
