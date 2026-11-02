//go:build judgeeval

package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/asksemble/asksemble/agent/internal/core"
	"github.com/asksemble/asksemble/agent/internal/judge"
)

// The run (plan part 2, jobs and request loop). Each engine has a pool of
// -parallel workers that takes its jobs in list order (question by
// question), so a run that stops covers whole questions evenly; the
// engines run side by side. Console and events.log carry codes, ids,
// counts and ms only.

// job is one request of a run.
type job struct {
	key    string
	set    string
	q      int
	item   string // a perturb item's id, "" for p06
	engine string
	req    judge.Request
	spec   judge.Spec
	task   core.JudgeTask
	sha    string
}

// jobKey is "<set>|q<q>|<item or ->|<engine>|<from>><against>".
func jobKey(set string, q int, item, engine, from, against string) string {
	if item == "" {
		item = "-"
	}
	return set + "|q" + strconv.Itoa(q) + "|" + item + "|" + engine + "|" + from + ">" + against
}

// makeJobs lists the jobs of each engine in order: p06 question by
// question, the six directions in Plan order (r1–r6); perturb item by
// item, one request X→Y'.
func (p *prepared) makeJobs(set string, questions []int, items []*perturbItem, engines []string) []*job {
	var out []*job
	for _, e := range engines {
		if set == setPerturb {
			for _, it := range items {
				spec := p.itemSpec(it)
				req := judge.Request{ID: "r1", From: it.From, Against: it.Against, Judge: e}
				out = append(out, newJob(set, it.Q, it.ID, e, req, spec))
			}
			continue
		}
		for _, q := range questions {
			spec := p.spec(q)
			for i, d := range directions() {
				req := judge.Request{ID: "r" + strconv.Itoa(i+1), From: d[0], Against: d[1], Judge: e}
				out = append(out, newJob(set, q, "", e, req, spec))
			}
		}
	}
	return out
}

func newJob(set string, q int, item, engine string, req judge.Request, spec judge.Spec) *job {
	task := judge.BuildTask(spec, req)
	return &job{key: jobKey(set, q, item, engine, req.From, req.Against), set: set, q: q, item: item, engine: engine,
		req: req, spec: spec, task: task, sha: taskSHA(task)}
}

// Stop codes in stop.json.
const (
	stopUsage     = "usage_limit"
	stopBudget    = "call_budget"
	stopInterrupt = "interrupted"
	stopSuspect   = "suspect" // judge_used_tool or bad_model: look before going on
)

// stopFile is stop.json.
type stopFile struct {
	Code      string `json:"code"`
	Engine    string `json:"engine,omitempty"`
	At        string `json:"at"`
	Remaining int    `json:"remaining"`
}

// runner runs the jobs of one invocation.
type runner struct {
	o          runOptions
	p          *prepared
	items      map[string]*perturbItem
	engines    map[string]*engine
	evalFolder string
	st         *store

	mu          sync.Mutex
	stop        string
	stopEngine  string
	stopAt      time.Time
	calls       int
	finished    int                       // jobs recorded as finished this invocation
	counts      map[string]map[string]int // engine → "done", a failure code or "abandoned:<why>" → n
	refs        map[string]map[int][]string
	left        map[string]map[int]int
	cleanupErrs int
}

// setStop stops the run; the first reason wins.
func (r *runner) setStop(code, engine string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.setStopLocked(code, engine)
}

func (r *runner) setStopLocked(code, engine string) {
	if r.stop == "" {
		r.stop, r.stopEngine, r.stopAt = code, engine, time.Now().UTC()
	}
}

func (r *runner) stopped(ctx context.Context) bool {
	if ctx.Err() != nil {
		return true
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.stop != ""
}

// before is checked before every attempt: Ctrl+C, a stop, the budget.
func (r *runner) before(ctx context.Context) func() error {
	return func() error {
		if ctx.Err() != nil {
			return errStop(abandonInterrupt)
		}
		r.mu.Lock()
		defer r.mu.Unlock()
		if r.stop != "" {
			return errStop(abandonStopped)
		}
		if r.o.maxCalls > 0 && r.calls >= r.o.maxCalls {
			r.setStopLocked(stopBudget, "")
			return errStop(abandonBudget)
		}
		r.calls++
		return nil
	}
}

// execute runs the jobs and the OpenCode cleanups.
func (r *runner) execute(ctx context.Context, jobs []*job) {
	r.counts, r.refs, r.left = map[string]map[string]int{}, map[string]map[int][]string{}, map[string]map[int]int{}
	byEngine := map[string][]*job{}
	for _, j := range jobs {
		byEngine[j.engine] = append(byEngine[j.engine], j)
		if r.left[j.engine] == nil {
			r.left[j.engine], r.refs[j.engine], r.counts[j.engine] = map[int]int{}, map[int][]string{}, map[string]int{}
		}
		r.left[j.engine][j.q]++
	}
	for _, name := range r.o.engines {
		if e := r.engines[name]; e != nil && e.cleaner != nil && len(byEngine[name]) > 0 {
			r.cleanup(e, nil) // judge conversations an earlier run left behind
		}
	}
	var wg sync.WaitGroup
	for _, name := range r.o.engines {
		list := byEngine[name]
		if len(list) == 0 {
			continue
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			r.pool(ctx, r.engines[name], list)
		}()
	}
	wg.Wait()
	// A final call per engine: the refs of questions a stop left
	// unfinished, and stale conversations (refs or none).
	for _, name := range r.o.engines {
		e := r.engines[name]
		if e == nil || e.cleaner == nil || len(byEngine[name]) == 0 {
			continue
		}
		var refs []string
		r.mu.Lock()
		qs := make([]int, 0, len(r.refs[name]))
		for q := range r.refs[name] {
			qs = append(qs, q)
		}
		sort.Ints(qs)
		for _, q := range qs {
			refs = append(refs, r.refs[name][q]...)
			delete(r.refs[name], q)
		}
		r.mu.Unlock()
		r.cleanup(e, refs)
	}
}

// pool runs one engine's jobs with -parallel workers, in list order.
func (r *runner) pool(ctx context.Context, e *engine, list []*job) {
	ch := make(chan *job)
	var wg sync.WaitGroup
	for range r.o.parallel {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := range ch {
				r.runJob(ctx, e, j)
			}
		}()
	}
	for _, j := range list {
		if r.stopped(ctx) {
			break
		}
		select {
		case ch <- j:
		case <-ctx.Done():
		}
	}
	close(ch)
	wg.Wait()
}

// runJob runs one job and records it, unless it was left unfinished.
func (r *runner) runJob(ctx context.Context, e *engine, j *job) {
	judger := e.judger
	if e.gold() {
		judger = r.p.goldFor(j, r.o.goldEvidence, r.items)
	}
	l := attemptLoop{judger: judger, family: e.family, folder: r.evalFolder, model: e.model, effort: e.effort,
		before: r.before(ctx),
		after: func(a attemptRec) {
			code := a.Code
			if code == "" {
				code = a.Validate
			}
			if code == "" {
				code = "ok"
			}
			r.st.event("attempt", "key", j.key, "n", a.N, "code", code, "ms", a.Ms, "in", a.In, "out", a.Out, "missing", a.Missing)
		}}
	out := l.run(ctx, j.task)
	if out.abandon == abandonUsage {
		r.setStop(stopUsage, e.name)
	}
	if out.recorded() && !out.done && (out.code == core.ErrCodeJudgeUsedTool || out.code == core.ErrCodeBadModel) {
		r.setStop(stopSuspect, e.name) // recorded below; no new attempt starts
	}
	if out.recorded() {
		rec := makeRecord(j, e, out)
		if err := r.st.add(rec); err != nil {
			r.st.event("write_failed", "key", j.key)
			r.setStop(stopInterrupt, e.name)
		} else {
			r.mu.Lock()
			if rec.finished(false) {
				r.finished++
			}
			if out.done {
				r.counts[e.name][statusDone]++
			} else {
				r.counts[e.name][out.code]++
			}
			r.mu.Unlock()
		}
	} else {
		r.st.event("abandoned", "key", j.key, "why", out.abandon)
		r.mu.Lock()
		r.counts[e.name]["abandoned:"+out.abandon]++
		r.mu.Unlock()
	}
	if e.cleaner == nil {
		return
	}
	r.mu.Lock()
	r.refs[e.name][j.q] = append(r.refs[e.name][j.q], out.refs...)
	r.left[e.name][j.q]--
	var refs []string
	if r.left[e.name][j.q] == 0 {
		refs = r.refs[e.name][j.q]
		delete(r.refs[e.name], j.q)
	}
	r.mu.Unlock()
	if len(refs) > 0 {
		r.cleanup(e, refs)
	}
}

// cleanup deletes the judge conversations refs name from the eval folder
// (nil refs: only stale ones), with a context of its own, so it runs after
// Ctrl+C too. Only the code and the count are logged.
func (r *runner) cleanup(e *engine, refs []string) {
	ctx, cancel := context.WithTimeout(context.Background(), core.JudgeCleanupTimeout*time.Duration(2+len(refs)))
	defer cancel()
	code := "ok"
	func() {
		defer func() {
			if v := recover(); v != nil {
				code = core.ErrCodeInternal
			}
		}()
		if err := e.cleaner.CleanupJudge(ctx, r.evalFolder, refs); err != nil {
			if code = core.ErrorCode(err); code == "" {
				code = core.ErrCodeInternal
			}
		}
	}()
	if code != "ok" {
		r.mu.Lock()
		r.cleanupErrs++
		r.mu.Unlock()
	}
	r.st.event("cleanup", "engine", e.name, "refs", len(refs), "code", code)
}

// makeRecord is the line of a recorded job.
func makeRecord(j *job, e *engine, out outcome) record {
	rec := record{Key: j.key, Set: j.set, Q: j.q, Item: j.item, Engine: e.name, From: j.req.From, Against: j.req.Against,
		Model: out.model, Effort: e.effort, CLI: e.version, Task: j.sha, Attempts: out.attempts,
		Verdicts: []verdictRec{}, Missing: []string{}}
	if rec.Attempts == nil {
		rec.Attempts = []attemptRec{}
	}
	if out.done {
		rec.Status = statusDone
		for _, v := range out.v.Verdicts {
			ev := v.Evidence
			if ev == nil {
				ev = []string{}
			}
			rec.Verdicts = append(rec.Verdicts, verdictRec{ID: v.ID, Core: v.Core, Evidence: ev, Reason: v.Reason, Label: v.Label, Unsure: v.Unsure})
		}
		if out.v.Missing != nil {
			rec.Missing = out.v.Missing
		}
	} else {
		rec.Status, rec.Code = statusFailed, out.code
	}
	return rec
}

// validated is a record's verdicts as judge.Validated.
func (rec record) validated() judge.Validated {
	v := judge.Validated{Verdicts: []judge.Verdict{}, Missing: rec.Missing}
	for _, vr := range rec.Verdicts {
		v.Verdicts = append(v.Verdicts, judge.Verdict{ID: vr.ID, Core: vr.Core, Evidence: vr.Evidence, Label: vr.Label, Unsure: vr.Unsure, Reason: vr.Reason})
	}
	for _, a := range rec.Attempts {
		v.Dropped += a.Dropped
	}
	return v
}

// runNameRe is a run name: a folder name with no path in it.
var runNameRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,79}$`)

// resolveQuestions are the run's questions: the split's, or those of
// -questions, each of which must be in the split.
func resolveQuestions(d *dataSet, split string, want []int) ([]int, error) {
	all := d.questionsOf(split)
	if len(want) == 0 {
		return all, nil
	}
	var out []int
	for _, q := range want {
		if !slices.Contains(all, q) {
			return nil, fmt.Errorf("-questions: question %d is not in split %s", q, split)
		}
		if !slices.Contains(out, q) {
			out = append(out, q)
		}
	}
	slices.Sort(out)
	return out, nil
}

// cmdRun is the run subcommand.
func cmdRun(ctx context.Context, o runOptions, stdout, stderr io.Writer) int {
	d, err := loadData(o.data)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return exitError
	}
	p := prepare(d, o.maxUnits)
	questions, err := resolveQuestions(d, o.split, o.questions)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return exitFlags
	}
	var set *perturbSet
	var items []*perturbItem
	itemByID := map[string]*perturbItem{}
	if o.set == setPerturb {
		set = makePerturbSet(d, o.n)
		items = set.selectItems(questions)
		p.prepareItems(items)
		for _, it := range items {
			itemByID[it.ID] = it
		}
	}
	if o.run == "" {
		o.run = o.set + "-" + o.split + "-" + o.tier + "-" + time.Now().Format("20060102-150405")
	}
	if !runNameRe.MatchString(o.run) {
		fmt.Fprintln(stderr, "-run: a run name is letters, digits, '.', '_' and '-'")
		return exitFlags
	}
	runDir := filepath.Join(o.out, o.run)
	_, statErr := os.Stat(runDir)
	exists := statErr == nil
	switch {
	case o.resume && !exists:
		fmt.Fprintf(stderr, "-resume: there is no run %s\n", o.run)
		return exitFlags
	case !o.resume && exists:
		fmt.Fprintf(stderr, "run %s exists; add -resume to go on with it\n", o.run)
		return exitFlags
	}

	jobs := p.makeJobs(o.set, questions, items, o.engines)
	var old manifest
	records := map[string]record{}
	if o.resume {
		if old, err = readManifest(runDir); err != nil {
			fmt.Fprintln(stderr, err)
			return exitError
		}
		if code, msg := resumeCheck(old, o, questions, d, set); code != exitOK {
			fmt.Fprintln(stderr, msg)
			return code
		}
		if records, _, err = loadRecords(filepath.Join(runDir, fileRequests), true); err != nil {
			fmt.Fprintln(stderr, err)
			return exitError
		}
		for _, j := range jobs {
			if rec, ok := records[j.key]; ok && rec.Task != j.sha {
				fmt.Fprintf(stderr, "resume: the task of %s changed since it ran; start a new run\n", j.key)
				return exitError
			}
		}
	}
	var toRun []*job
	for _, j := range jobs {
		if rec, ok := records[j.key]; ok && rec.finished(o.retryFailed) {
			continue
		}
		toRun = append(toRun, j)
	}
	anyReal := slices.ContainsFunc(o.engines, func(n string) bool { return slices.Contains(realEngines, n) })
	if (anyReal && o.expect < 0) || (o.expect >= 0 && o.expect != len(toRun)) {
		fmt.Fprintf(stderr, "-expect: this run would start %d requests (%d skipped as finished); give -expect %d to start them\n",
			len(toRun), len(jobs)-len(toRun), len(toRun))
		return exitFlags
	}

	made := false
	if !exists {
		if err := os.MkdirAll(runDir, 0o700); err != nil {
			fmt.Fprintln(stderr, err)
			return exitError
		}
		made = true
	}
	setup, err := buildEngines(ctx, o.engines, o.tier, runDir)
	if err != nil {
		if made {
			os.RemoveAll(runDir)
		}
		fmt.Fprintln(stderr, "engines:", err)
		return exitError
	}
	engines := map[string]*engine{}
	versions := map[string]string{}
	for _, e := range setup.engines {
		engines[e.name] = e
		versions[e.name] = e.version
	}
	m := old
	now := time.Now().UTC().Format(time.RFC3339)
	if o.resume {
		for _, e := range setup.engines {
			was, ok := old.EngineInfo[e.name]
			if !ok {
				continue
			}
			if was.Model != e.model || was.Effort != e.effort || was.Family != e.family {
				fmt.Fprintf(stderr, "resume: %s now judges with %s/%s, the run with %s/%s\n", e.name, e.model, e.effort, was.Model, was.Effort)
				return exitError
			}
			if was.Version != e.version {
				fmt.Fprintf(stdout, "version: %s %s -> %s (recorded in the manifest)\n", e.name, was.Version, e.version)
			}
		}
	} else {
		m = manifest{Tool: "judge-eval", Format: manifestFormat, Run: o.run, Created: now, Set: o.set, Split: o.split,
			Questions: questions, Engines: o.engines, Tier: o.tier, Shape: o.shape, Labels: o.labels, Rules: judge.Rules,
			MaxUnits: o.maxUnits, Parallel: o.parallel, Checks: judge.DefaultChecks, GoldEvidence: o.goldEvidence,
			EngineInfo: map[string]engineInfo{}, SettingsIgnored: true,
			Data: dataHashes{Files: d.fileHashes()}, EvalFolder: setup.evalFolder, CodexSchema: setup.codexSchema, Commit: o.commit}
		for _, e := range setup.engines {
			m.EngineInfo[e.name] = e.info()
		}
		if set != nil {
			m.PerturbN, m.Data.Perturb = o.n, set.hash()
			if err := os.WriteFile(filepath.Join(runDir, fileSet), set.canonical(), 0o600); err != nil {
				fmt.Fprintln(stderr, err)
				return exitError
			}
		}
	}
	if setup.evalFolder != "" && m.EvalFolder == "" {
		m.EvalFolder = setup.evalFolder
	}
	if setup.codexSchema != "" && m.CodexSchema == "" {
		m.CodexSchema = setup.codexSchema
	}
	m.Sessions = append(m.Sessions, session{Started: now, Versions: versions, Resume: o.resume})
	if err := writeJSON(filepath.Join(runDir, fileManifest), m); err != nil {
		fmt.Fprintln(stderr, err)
		return exitError
	}
	fmt.Fprintf(stdout, "run %s: %d requests (%d already finished), engines %s, tier %s\n",
		o.run, len(toRun), len(jobs)-len(toRun), strings.Join(o.engines, ","), o.tier)
	for _, e := range setup.engines {
		fmt.Fprintf(stdout, "  %s %s model %s effort %s family %s\n", e.name, e.version, e.model, e.effort, e.family)
	}

	st, err := openStore(runDir)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return exitError
	}
	defer st.close()
	r := &runner{o: o, p: p, items: itemByID, engines: engines, evalFolder: setup.evalFolder, st: st}
	st.event("start", "run", o.run, "requests", len(toRun), "resume", o.resume)
	r.execute(ctx, toRun)

	remaining := len(toRun) - r.finished
	for _, name := range o.engines {
		var parts []string
		keys := make([]string, 0, len(r.counts[name]))
		for k := range r.counts[name] {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			parts = append(parts, fmt.Sprintf("%s %d", k, r.counts[name][k]))
		}
		if len(parts) > 0 {
			fmt.Fprintf(stdout, "  %s: %s\n", name, strings.Join(parts, ", "))
		}
	}
	if r.cleanupErrs > 0 {
		fmt.Fprintf(stdout, "  opencode cleanup errors: %d (codes in events.log)\n", r.cleanupErrs)
	}
	stopPath := filepath.Join(runDir, fileStop)
	code, stop, engineName := exitOK, r.stop, r.stopEngine
	switch {
	case ctx.Err() != nil:
		code, stop = exitInterrupted, stopInterrupt
	case stop == stopUsage:
		code = exitUsageLimit
	case stop == stopBudget:
		code = exitBudget
	case stop == stopSuspect:
		code = exitSuspect
	case stop == stopInterrupt:
		code = exitError // a record could not be written
	}
	if code == exitOK {
		if err := os.Remove(stopPath); err != nil && !errors.Is(err, os.ErrNotExist) {
			fmt.Fprintln(stderr, err)
		}
		st.event("end", "remaining", remaining)
		fmt.Fprintf(stdout, "done: %d requests recorded, %d remaining\n", r.finished, remaining)
		return exitOK
	}
	at := r.stopAt
	if at.IsZero() {
		at = time.Now().UTC()
	}
	if err := writeJSON(stopPath, stopFile{Code: stop, Engine: engineName, At: at.Format(time.RFC3339), Remaining: remaining}); err != nil {
		fmt.Fprintln(stderr, err)
	}
	st.event("stop", "code", stop, "engine", engineName, "remaining", remaining)
	fmt.Fprintf(stdout, "stopped: %s %s, %d requests remaining\n", stop, engineName, remaining)
	return code
}

// resumeCheck compares a resume's flags and data with the run's manifest.
func resumeCheck(m manifest, o runOptions, questions []int, d *dataSet, set *perturbSet) (int, string) {
	switch {
	case m.Set != o.set || m.Split != o.split || !slices.Equal(m.Questions, questions):
		return exitFlags, fmt.Sprintf("resume: the run is -set %s -split %s with questions %v", m.Set, m.Split, m.Questions)
	case m.Tier != o.tier || m.Shape != o.shape || m.Labels != o.labels:
		return exitFlags, fmt.Sprintf("resume: the run is -tier %s -shape %s -labels %s", m.Tier, m.Shape, m.Labels)
	case m.MaxUnits != o.maxUnits || m.GoldEvidence != o.goldEvidence || (o.set == setPerturb && m.PerturbN != o.n):
		return exitFlags, fmt.Sprintf("resume: the run is -max-units %d -n %d, gold evidence %v", m.MaxUnits, m.PerturbN, m.GoldEvidence)
	}
	for _, e := range o.engines {
		if !slices.Contains(m.Engines, e) {
			return exitFlags, fmt.Sprintf("resume: engine %s is not one of the run's %v", e, m.Engines)
		}
	}
	files := d.fileHashes()
	for name, sum := range m.Data.Files {
		if files[name] != sum {
			return exitError, fmt.Sprintf("resume: data file %s changed since the run started", name)
		}
	}
	if len(files) != len(m.Data.Files) {
		return exitError, "resume: the data files are not the run's"
	}
	if set != nil && set.hash() != m.Data.Perturb {
		return exitError, "resume: the perturb set is not the run's"
	}
	return exitOK, ""
}
