//go:build judgeeval

package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"github.com/asksemble/asksemble/agent/internal/config"
	"github.com/asksemble/asksemble/agent/internal/core"
	"github.com/asksemble/asksemble/agent/internal/judge"
)

// Flags of the subcommands (plan part 2). A bad flag is exit 2.

// notBuilt is the refusal of the stage-2 variants (deviation 1).
const notBuilt = "not built until stage 2 (P2-6)"

type runOptions struct {
	set, split   string
	questions    []int
	engines      []string
	tier         string
	shape        string
	labels       string
	run          string
	out          string
	data         string
	resume       bool
	retryFailed  bool
	parallel     int
	maxCalls     int
	maxUnits     int
	n            int
	goldEvidence bool
	expect       int // -1 when not given
	commit       string
}

type reportOptions struct {
	runs        []string
	out         string
	data        string
	checks      []string
	sweepChecks bool
	boot        int
	seed        uint64
	texts       bool
	asJSON      bool
}

type perturbOptions struct {
	data string
	n    int
	file string
	show int
}

type dataOptions struct {
	data     string
	maxUnits int
}

// flagError is a bad flag: exit 2.
type flagError struct{ msg string }

func (e *flagError) Error() string { return e.msg }

func badFlag(format string, a ...any) error { return &flagError{fmt.Sprintf(format, a...)} }

// newFlags makes a flag set that reports to stderr.
func newFlags(name string, stderr io.Writer) *flag.FlagSet {
	fs := flag.NewFlagSet("judge-eval "+name, flag.ContinueOnError)
	fs.SetOutput(stderr)
	return fs
}

// parse parses args, turning flag errors into flagError.
func parse(fs *flag.FlagSet, args []string) error {
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return badFlag("help")
		}
		return badFlag("%v", err)
	}
	if fs.NArg() > 0 {
		return badFlag("unexpected argument %q", fs.Arg(0))
	}
	return nil
}

// list splits a comma list, dropping empty parts.
func list(s string) []string {
	var out []string
	for _, p := range strings.Split(s, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

// outAndData fills the default -out and -data.
func outAndData(out, data *string) error {
	if *out == "" {
		d, err := defaultOut()
		if err != nil {
			return err
		}
		*out = d
	}
	if *data == "" {
		d, err := findData()
		if err != nil {
			return badFlag("%v", err)
		}
		*data = d
	}
	return nil
}

func parseRun(args []string, stderr io.Writer) (runOptions, error) {
	o := runOptions{}
	fs := newFlags("run", stderr)
	fs.StringVar(&o.set, "set", setP06, "p06 or perturb")
	fs.StringVar(&o.split, "split", splitDev, "dev, test or all")
	questions := fs.String("questions", "", "questions, e.g. 2,12 (each in the split)")
	engines := fs.String("engines", strings.Join(realEngines, ","), "claude,codex,opencode and/or gold")
	fs.StringVar(&o.tier, "tier", core.TierQuick, "quick or standard")
	fs.StringVar(&o.shape, "shape", judge.ShapeDirected, "directed")
	fs.StringVar(&o.labels, "labels", judge.LabelsV1, "v1")
	fs.StringVar(&o.run, "run", "", "run name (default <set>-<split>-<tier>-<time>)")
	fs.StringVar(&o.out, "out", "", `base folder (default %LOCALAPPDATA%\Asksemble\judge-eval)`)
	fs.StringVar(&o.data, "data", "", "the docs/judge folder (default: found above the working folder)")
	fs.BoolVar(&o.resume, "resume", false, "go on with an existing run")
	fs.BoolVar(&o.retryFailed, "retry-failed", false, "with -resume, run failed requests again")
	fs.IntVar(&o.parallel, "parallel", config.DefaultJudgePerAI, "requests at once per engine, 1-4")
	fs.IntVar(&o.maxCalls, "max-calls", 0, "stop after this many judge calls (0: no limit)")
	fs.IntVar(&o.maxUnits, "max-units", judge.MaxUnits, "units judged per answer, 1-40")
	fs.IntVar(&o.n, "n", 60, "perturb items")
	fs.BoolVar(&o.goldEvidence, "gold-evidence", false, "the gold engine cites the evidence gold")
	fs.IntVar(&o.expect, "expect", -1, "the number of requests this run starts (required with a real engine)")
	fs.StringVar(&o.commit, "commit", "", "the commit, recorded")
	if err := parse(fs, args); err != nil {
		return o, err
	}
	switch {
	case o.set != setP06 && o.set != setPerturb:
		return o, badFlag("-set must be p06 or perturb")
	case o.split != splitDev && o.split != splitTest && o.split != splitAll:
		return o, badFlag("-split must be dev, test or all")
	case o.tier != core.TierQuick && o.tier != core.TierStandard:
		return o, badFlag("-tier must be quick or standard")
	case o.shape == judge.ShapePair || o.labels == judge.LabelsV1Four:
		return o, badFlag("-shape %s -labels %s: %s", o.shape, o.labels, notBuilt)
	case o.shape != judge.ShapeDirected:
		return o, badFlag("-shape must be directed")
	case o.labels != judge.LabelsV1:
		return o, badFlag("-labels must be v1")
	case o.parallel < 1 || o.parallel > config.MaxJudgePerAI:
		return o, badFlag("-parallel must be 1-%d", config.MaxJudgePerAI)
	case o.maxCalls < 0:
		return o, badFlag("-max-calls must be 0 or more")
	case o.maxUnits < 1 || o.maxUnits > judge.MaxUnits:
		return o, badFlag("-max-units must be 1-%d", judge.MaxUnits)
	case o.n < 1 || o.n > 1000:
		return o, badFlag("-n must be 1-1000")
	case o.retryFailed && !o.resume:
		return o, badFlag("-retry-failed needs -resume")
	case o.expect < -1:
		return o, badFlag("-expect must be 0 or more")
	}
	for _, s := range list(*questions) {
		q, err := strconv.Atoi(s)
		if err != nil || q < 1 {
			return o, badFlag("-questions: %q is not a question number", s)
		}
		o.questions = append(o.questions, q)
	}
	for _, e := range list(*engines) {
		if !slices.Contains(engineNames, e) {
			return o, badFlag("-engines: unknown engine %q", e)
		}
		if !slices.Contains(o.engines, e) {
			o.engines = append(o.engines, e)
		}
	}
	if len(o.engines) == 0 {
		return o, badFlag("-engines names no engine")
	}
	if o.commit != "" && !commitRe(o.commit) {
		return o, badFlag("-commit must be a hexadecimal commit id")
	}
	return o, outAndData(&o.out, &o.data)
}

// commitRe reports whether s is a short or full commit id.
func commitRe(s string) bool {
	if len(s) < 4 || len(s) > 64 {
		return false
	}
	for _, c := range s {
		if !strings.ContainsRune("0123456789abcdef", c) {
			return false
		}
	}
	return true
}

func parseReport(args []string, stderr io.Writer) (reportOptions, error) {
	o := reportOptions{}
	fs := newFlags("report", stderr)
	runs := fs.String("run", "", "run names, comma separated (several merge)")
	fs.StringVar(&o.out, "out", "", "base folder")
	fs.StringVar(&o.data, "data", "", "the docs/judge folder")
	checks := fs.String("checks", strings.Join(judge.DefaultChecks, ","), "enabled checks")
	fs.BoolVar(&o.sweepChecks, "sweep-checks", false, "add each other check alone and print rule 2's deltas")
	fs.IntVar(&o.boot, "boot", 2000, "bootstrap draws")
	seed := fs.Uint64("seed", 20261101, "bootstrap seed")
	fs.BoolVar(&o.texts, "texts", false, "print unit texts for reading by hand")
	fs.BoolVar(&o.asJSON, "json", false, "print report.json")
	fs.String("commit", "", "ignored")
	if err := parse(fs, args); err != nil {
		return o, err
	}
	o.seed = *seed
	o.runs = list(*runs)
	if len(o.runs) == 0 {
		return o, badFlag("-run names no run")
	}
	for _, r := range o.runs {
		if !runNameRe.MatchString(r) {
			return o, badFlag("-run: %q is not a run name", r)
		}
	}
	for _, c := range list(*checks) {
		if !slices.Contains(judge.AllChecks, c) {
			return o, badFlag("-checks: unknown check %q", c)
		}
		if !slices.Contains(o.checks, c) {
			o.checks = append(o.checks, c)
		}
	}
	if o.boot < 0 || o.boot > 100000 {
		return o, badFlag("-boot must be 0-100000")
	}
	return o, outAndData(&o.out, &o.data)
}

func parsePerturb(args []string, stderr io.Writer) (perturbOptions, error) {
	o := perturbOptions{}
	fs := newFlags("perturb", stderr)
	fs.StringVar(&o.data, "data", "", "the docs/judge folder")
	fs.IntVar(&o.n, "n", 60, "items")
	fs.StringVar(&o.file, "out", "", `the file to write (default %LOCALAPPDATA%\Asksemble\judge-eval\perturb-v1.json)`)
	fs.IntVar(&o.show, "show", 0, "print the first N items (content)")
	if err := parse(fs, args); err != nil {
		return o, err
	}
	if o.n < 1 || o.n > 1000 || o.show < 0 {
		return o, badFlag("-n must be 1-1000 and -show 0 or more")
	}
	if o.file == "" {
		d, err := defaultOut()
		if err != nil {
			return o, err
		}
		o.file = filepath.Join(d, perturbVersion+".json")
	}
	out := ""
	return o, outAndData(&out, &o.data)
}

func parseData(args []string, stderr io.Writer) (dataOptions, error) {
	o := dataOptions{}
	fs := newFlags("data", stderr)
	fs.StringVar(&o.data, "data", "", "the docs/judge folder")
	fs.IntVar(&o.maxUnits, "max-units", judge.MaxUnits, "units judged per answer")
	if err := parse(fs, args); err != nil {
		return o, err
	}
	if o.maxUnits < 1 || o.maxUnits > judge.MaxUnits {
		return o, badFlag("-max-units must be 1-%d", judge.MaxUnits)
	}
	out := ""
	return o, outAndData(&out, &o.data)
}

func parseCleanup(args []string, stderr io.Writer) error {
	fs := newFlags("cleanup", stderr)
	fs.String("data", "", "ignored")
	return parse(fs, args)
}
