//go:build judgeeval

package main

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
)

// Reports over several runs, and runs that did not finish (S4 review
// COR-1, COR-2, COR-3). The runs are gold runs recast as if engine claude
// had judged them, so that rules 1a-g apply.

// goldRuns runs the gold engine into a new folder: every P0-6 question as
// run "g", and with perturb the perturb set as run "gp".
func goldRuns(t *testing.T, perturb bool) (data, out string) {
	t.Helper()
	data, out = committedData(t), t.TempDir()
	runs := [][]string{{"run", "-split", "all", "-engines", "gold", "-run", "g", "-data", data, "-out", out}}
	if perturb {
		runs = append(runs, []string{"run", "-split", "all", "-engines", "gold", "-set", "perturb", "-run", "gp", "-data", data, "-out", out})
	}
	for _, args := range runs {
		if code, stdout, stderr := cli(t, context.Background(), args...); code != exitOK {
			t.Fatalf("exit %d: %s %s", code, stdout, stderr)
		}
	}
	return data, out
}

// recast copies gold run src into run dst as if engine claude (haiku,
// medium) had judged it. keep picks the records (nil: all); editM changes
// the manifest, editR record i (in file order).
func recast(t *testing.T, out, src, dst string, keep func(record) bool, editM func(*manifest), editR func(i int, r *record)) {
	t.Helper()
	m, err := readManifest(filepath.Join(out, src))
	if err != nil {
		t.Fatal(err)
	}
	m.Run, m.Engines = dst, []string{"claude"}
	m.EngineInfo = map[string]engineInfo{"claude": {Model: "haiku", Effort: "medium", Family: "anthropic", Version: "2.1.316"}}
	if editM != nil {
		editM(&m)
	}
	dir := filepath.Join(out, dst)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := writeJSON(filepath.Join(dir, fileManifest), m); err != nil {
		t.Fatal(err)
	}
	recs, order, err := loadRecords(filepath.Join(out, src, fileRequests), false)
	if err != nil {
		t.Fatal(err)
	}
	var b bytes.Buffer
	n := 0
	for _, k := range order {
		r := recs[k]
		if keep != nil && !keep(r) {
			continue
		}
		r.Engine = "claude"
		r.Key = jobKey(r.Set, r.Q, r.Item, "claude", r.From, r.Against)
		if editR != nil {
			editR(n, &r)
		}
		n++
		line, err := marshal(r, false)
		if err != nil {
			t.Fatal(err)
		}
		b.Write(line)
	}
	if err := os.WriteFile(filepath.Join(dir, fileRequests), b.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}
}

// claudeView is the report of engine claude's view over runs.
func claudeView(t *testing.T, data, out string, runs ...string) viewReport {
	t.Helper()
	u, err := loadUniverse(out, data, runs)
	if err != nil {
		t.Fatal(err)
	}
	cov := u.coverageOf()
	for _, vs := range u.views() {
		if vs.engine == "claude" {
			return makeViewReport(u, u.view(vs, handChecks), cov, 0, 1)
		}
	}
	t.Fatalf("runs %v have no claude view", runs)
	return viewReport{}
}

// ruleStates are a view's rules as "1a pass 1b n/a …".
func ruleStates(vr viewReport) string {
	var parts []string
	for _, r := range vr.Rules {
		state := "n/a"
		if r.Pass != nil {
			state = map[bool]string{true: "pass", false: "FAIL"}[*r.Pass]
		}
		parts = append(parts, r.Rule+" "+state)
	}
	return strings.Join(parts, " ")
}

// COR-1: a run stopped by a usage limit (or made for part of the set)
// lists every question in its manifest but has records for some only.
// Rules 1a-g gave pass or FAIL on it, a missing request counting as
// judged "none". A rule now needs every request of its set.
func TestRulesNeedEveryRequest(t *testing.T) {
	data, out := goldRuns(t, true)
	withOpposites := []int{2, 5, 8, 12, 15, 16, 27}
	recast(t, out, "g", "all", nil, nil, nil)
	recast(t, out, "g", "part", func(r record) bool { return slices.Contains(withOpposites, r.Q) }, nil, nil)
	recast(t, out, "gp", "syn", nil, nil, nil)
	firstQ := 0
	recast(t, out, "gp", "synpart", func(r record) bool {
		if firstQ == 0 {
			firstQ = r.Q
		}
		return r.Q != firstQ
	}, nil, nil)
	const allP06 = "1a pass 1b n/a 1c pass 1d pass 1e pass 1f pass 1g pass"
	for _, c := range []struct {
		runs    []string
		missing int
		want    string
	}{
		{[]string{"all"}, 0, allP06},
		{[]string{"part"}, 180 - 6*len(withOpposites), "1a n/a 1b n/a 1c n/a 1d n/a 1e n/a 1f n/a 1g n/a"},
		{[]string{"all", "syn"}, 0, "1a pass 1b pass 1c pass 1d pass 1e pass 1f pass 1g pass"},
		{[]string{"all", "synpart"}, -1, allP06},
		{[]string{"part", "syn"}, 180 - 6*len(withOpposites), "1a n/a 1b pass 1c n/a 1d n/a 1e n/a 1f n/a 1g n/a"},
	} {
		vr := claudeView(t, data, out, c.runs...)
		if got := ruleStates(vr); got != c.want {
			t.Errorf("runs %v (%d requests missing): %s, want %s", c.runs, vr.MissingRequests, got, c.want)
		}
		if (c.missing >= 0 && vr.MissingRequests != c.missing) || (c.missing < 0 && vr.MissingRequests == 0) {
			t.Errorf("runs %v: %d requests missing, want %d", c.runs, vr.MissingRequests, c.missing)
		}
	}
	// report.md says why the rules are n/a.
	if code, stdout, stderr := cli(t, context.Background(), "report", "-run", "part", "-boot", "0", "-data", data, "-out", out); code != exitOK {
		t.Fatalf("report: exit %d %s %s", code, stdout, stderr)
	}
	md, err := os.ReadFile(filepath.Join(out, "part", fileReportMD))
	if err != nil {
		t.Fatal(err)
	}
	if want := fmt.Sprintf("- claude (requests without a record: p06 %d):", 180-6*len(withOpposites)); !strings.Contains(string(md), want) {
		t.Errorf("report.md has no %q", want)
	}
}

// COR-3: rules 1d and 1f are about the P0-6 requests; merging the perturb
// run (for rule 1b) counted its shorter requests in them too.
func TestRulesCountP06RequestsOnly(t *testing.T) {
	data, out := goldRuns(t, true)
	// P0-6: 80 requests of 15 s, 100 of 25 s, none invalid: 1f fails.
	recast(t, out, "g", "slow", nil, nil, func(i int, r *record) {
		ms := int64(25000)
		if i < 80 {
			ms = 15000
		}
		for k := range r.Attempts {
			r.Attempts[k].Ms = ms
		}
	})
	// perturb: 60 requests of 1 s whose first output missed an id.
	recast(t, out, "gp", "fastbad", nil, nil, func(i int, r *record) {
		for k := range r.Attempts {
			r.Attempts[k].Ms = 1000
		}
		r.Attempts[0].Missing = 1
	})
	rule := func(vr viewReport, name string) ruleResult {
		for _, r := range vr.Rules {
			if r.Rule == name {
				return r
			}
		}
		t.Fatalf("no rule %s", name)
		return ruleResult{}
	}
	for _, runs := range [][]string{{"slow"}, {"slow", "fastbad"}} {
		vr := claudeView(t, data, out, runs...)
		if r := rule(vr, "1f"); r.Value != 25000 || r.Pass == nil || *r.Pass {
			t.Errorf("runs %v: 1f %v %s, want 25000 FAIL", runs, r.Value, ruleStates(vr))
		}
		if r := rule(vr, "1d"); r.Value != 0 || r.Pass == nil || !*r.Pass {
			t.Errorf("runs %v: 1d %v %s, want 0 pass", runs, r.Value, ruleStates(vr))
		}
	}
	// The request table still covers every request of the view.
	q := claudeView(t, data, out, "slow", "fastbad").Requests
	if q.Requests != 240 || q.MsP50 != 15000 || q.FirstInvalid != 60 {
		t.Errorf("merged requests %d, p50 %v, invalid first %d", q.Requests, q.MsP50, q.FirstInvalid)
	}
}

// COR-2: a job key has no tier or model in it, so two runs of the same
// requests (a quick run of every question and a standard run of Q2) merged
// silently, the later run's records and engine info winning. Such runs
// are refused now, and so is one engine judging with two models.
func TestReportRefusesMixedRuns(t *testing.T) {
	data, out := goldRuns(t, false)
	only := func(q int) (func(record) bool, func(*manifest)) {
		return func(r record) bool { return r.Q == q }, func(m *manifest) { m.Questions = []int{q} }
	}
	recast(t, out, "g", "quick", nil, nil, nil)
	k2, m2 := only(2)
	recast(t, out, "g", "q2", k2, func(m *manifest) { m2(m); m.Tier = "standard" }, nil)
	k5, m5 := only(5)
	recast(t, out, "g", "q5sonnet", k5, func(m *manifest) {
		m5(m)
		m.EngineInfo["claude"] = engineInfo{Model: "sonnet", Effort: "medium", Family: "anthropic", Version: "2.1.316"}
	}, nil)
	recast(t, out, "g", "q5high", k5, func(m *manifest) {
		m5(m)
		m.EngineInfo["claude"] = engineInfo{Model: "haiku", Effort: "high", Family: "anthropic", Version: "2.1.316"}
	}, nil)
	recast(t, out, "g", "q5new", k5, func(m *manifest) {
		m5(m)
		m.EngineInfo["claude"] = engineInfo{Model: "haiku", Effort: "medium", Family: "anthropic", Version: "2.1.317"}
	}, nil)
	// The gold run writes its records in the order its requests end.
	if _, err := loadUniverse(out, data, []string{"quick", "q2"}); err == nil ||
		!regexp.MustCompile(`^run q2: request p06\|q2\|-\|claude\|[a-z]+>[a-z]+ is in an earlier run too; report such runs apart$`).MatchString(err.Error()) {
		t.Errorf("runs quick, q2: %v", err)
	}
	for _, c := range []struct {
		runs []string
		want string
	}{
		{[]string{"q2", "q5sonnet"}, "run q5sonnet: claude judged with sonnet/medium, an earlier run with haiku/medium"},
		{[]string{"q2", "q5high"}, "run q5high: claude judged with haiku/high, an earlier run with haiku/medium"},
	} {
		_, err := loadUniverse(out, data, c.runs)
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("runs %v: %v, want %q", c.runs, err, c.want)
		}
	}
	code, stdout, stderr := cli(t, context.Background(), "report", "-run", "quick,q2", "-data", data, "-out", out)
	if code != exitError || !strings.Contains(stderr, "report such runs apart") || stdout != "" {
		t.Errorf("report -run quick,q2: exit %d, %q %q", code, stdout, stderr)
	}
	// Another CLI version of the same model merges, and both are named.
	u, err := loadUniverse(out, data, []string{"q2", "q5new"})
	if err != nil {
		t.Fatal(err)
	}
	if v := u.info["claude"].Version; v != "2.1.316, 2.1.317" {
		t.Errorf("version %q", v)
	}
	if n := len(u.records); n != 12 {
		t.Errorf("%d records", n)
	}
}

// COR-9: what each rule reads, on numbers picked so that the other
// reading gives the other verdict: shown is differ + check (1a, 1b), core
// recall is OR (1e), the median not the p90 (1f), the unsure-only share
// not the check rate (1g), and 1d and 1f read the P0-6 requests.
func TestRulesOf(t *testing.T) {
	vr := viewReport{
		Natural:    pairStats{Opposites: 21, Differ: 18, Check: 1},
		Synthetic:  pairStats{Opposites: 10, Differ: 8, Check: 1},
		Breakdowns: map[string]map[string]pairStats{"group": {groupClear: {FalseOppositeRate: 0.03}}},
		Cards:      cardStats{CoreRecallOR: 0.9, CoreRecallAND: 0.5, UnsureOnlyShare: 0.15, CheckRate: 0.4},
		Requests:   requestStats{InvalidRate: 0.5, MsP50: 30000},
	}
	p06 := requestStats{InvalidRate: 0.02, MsP50: 20000, MsP90: 40000}
	full := coverage{P06All: true, Perturb: true}
	want := []struct {
		rule  string
		value float64
	}{{"1a", 19}, {"1b", 0.9}, {"1c", 0.03}, {"1d", 0.02}, {"1e", 0.9}, {"1f", 20000}, {"1g", 0.15}}
	got := rulesOf(vr, p06, full, nil)
	if len(got) != len(want) {
		t.Fatalf("rules %+v", got)
	}
	for i, w := range want {
		if r := got[i]; r.Rule != w.rule || !near(r.Value, w.value) || r.Pass == nil || !*r.Pass {
			t.Errorf("rule %s: %v %v, want %v pass", r.Rule, r.Value, ruleStates(viewReport{Rules: got}), w.value)
		}
	}
	// One past each limit fails it.
	worse := vr
	worse.Natural.Check = 0
	worse.Synthetic.Check = 0
	worse.Breakdowns = map[string]map[string]pairStats{"group": {groupClear: {FalseOppositeRate: 0.031}}}
	worse.Cards = cardStats{CoreRecallOR: 0.89, UnsureOnlyShare: 0.16}
	if s := ruleStates(viewReport{Rules: rulesOf(worse, requestStats{InvalidRate: 0.021, MsP50: 20001}, full, nil)}); s != "1a FAIL 1b FAIL 1c FAIL 1d FAIL 1e FAIL 1f FAIL 1g FAIL" {
		t.Errorf("past the limits: %s", s)
	}
	// 1a needs all 21 natural opposites.
	short := vr
	short.Natural = pairStats{Opposites: 20, Differ: 20}
	if r := rulesOf(short, p06, full, nil)[0]; r.Pass == nil || *r.Pass {
		t.Errorf("1a on 20 opposites: %+v", r)
	}
	// Coverage: p06 not all, perturb not run, or requests without a record.
	for _, c := range []struct {
		cov     coverage
		missing map[string]int
		want    string
	}{
		{coverage{Perturb: true}, nil, "1a n/a 1b pass 1c n/a 1d n/a 1e n/a 1f n/a 1g n/a"},
		{coverage{P06All: true}, nil, "1a pass 1b n/a 1c pass 1d pass 1e pass 1f pass 1g pass"},
		{full, map[string]int{setP06: 1}, "1a n/a 1b pass 1c n/a 1d n/a 1e n/a 1f n/a 1g n/a"},
		{full, map[string]int{setPerturb: 1}, "1a pass 1b n/a 1c pass 1d pass 1e pass 1f pass 1g pass"},
	} {
		if s := ruleStates(viewReport{Rules: rulesOf(vr, p06, c.cov, c.missing)}); s != c.want {
			t.Errorf("coverage %+v missing %v: %s, want %s", c.cov, c.missing, s, c.want)
		}
	}
}
