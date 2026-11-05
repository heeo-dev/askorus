//go:build syntheval

package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"slices"
	"time"

	"github.com/asksemble/asksemble/agent/internal/core"
	"github.com/asksemble/asksemble/agent/internal/judge"
)

// resynth writes a run's combined answers again from the same answers and
// judgments (the task each record keeps), with another tier's model or
// another prompt, into a new run (C4, E2). Only the writer changes, so two
// such runs compare the combined answer alone, without the answers' luck.

// synthVariants are the combined-answer prompts resynth writes with, by
// name: product is the agent's own (synth-v4). A draft to try goes here,
// its text in variants.go.
var synthVariants = map[string]func(judge.SynthTask, string) string{
	"product": judge.SynthPrompt,
	"c5-b":    synthC5B,
	"c5-nj":   synthC5NoJudge,
	"c5-m":    synthC5M,
	"c5-one":  synthC5One,
	"c5-one2": synthC5One2,
	"c5-solo": synthC5Solo,
	"c5-sel":  c5SoloWith("c5-sel", "sel"),
	"c5-nat":  c5SoloWith("c5-nat", "nat"),
	"c5-sn":   c5SoloWith("c5-sn", "sel", "nat"),
	"c5-sf":   c5SoloWith("c5-sf", "sel", "few"),
	"c5-sfd":  c5SoloWith("c5-sfd", "sel", "few", "nodup"),
	"c5-si":   c5SoloWith("c5-si", "sel", "inline"),
	"c5-sq":   c5SoloWith("c5-sq", "sel", "quiet"),
}

// synthSplits take a variant's reply apart when it holds more than the
// combined answer: the answer, and the list of differences it carries.
var synthSplits = map[string]func(string, []judge.SynthAnswer) (string, *DiffRec){
	"c5-one":  splitC5One,
	"c5-one2": splitC5One,
}

func variantNames() []string {
	var out []string
	for n := range synthVariants {
		out = append(out, n)
	}
	slices.Sort(out)
	return out
}

func resynthCmd(ctx context.Context, args []string, out, errOut io.Writer) int {
	fs := flag.NewFlagSet("resynth", flag.ContinueOnError)
	fs.SetOutput(errOut)
	from := fs.String("from", "", "the run whose answers and judgments to write from")
	name := fs.String("run", "", "the new run's name (a-z, 0-9, -)")
	only := fs.String("only", "", "question IDs to keep, comma-separated")
	tier := fs.String("tier", core.TierQuick, "the tier whose model writes: quick, standard or deep")
	agent := fs.String("writer", "claude", "the AI that writes the combined answer")
	variant := fs.String("synth", "product", "the combined-answer prompt")
	resume := fs.Bool("resume", false, "skip the questions the run already has")
	expect := fs.Int("expect", -1, "the number of combined answers this run will write")
	if err := fs.Parse(args); err != nil {
		return exitFlags
	}
	prompt, ok := synthVariants[*variant]
	switch {
	case !core.ValidTier(*tier):
		fmt.Fprintln(errOut, "-tier must be quick, standard or deep")
		return exitFlags
	case !validWriter(*agent):
		fmt.Fprintln(errOut, "-writer must be claude, codex or opencode")
		return exitFlags
	case !ok:
		fmt.Fprintf(errOut, "-synth must be one of %v\n", variantNames())
		return exitFlags
	case *from == *name:
		fmt.Fprintln(errOut, "-run must be a new run, not -from")
		return exitFlags
	}
	srcDir, err := runDir(*from)
	if err != nil {
		fmt.Fprintln(errOut, err)
		return exitFlags
	}
	dir, err := runDir(*name)
	if err != nil {
		fmt.Fprintln(errOut, err)
		return exitFlags
	}
	src, err := loadRecords(srcDir)
	if err != nil {
		fmt.Fprintln(errOut, err)
		return exitError
	}
	src, err = pickRecords(src, splitList(*only))
	if err != nil {
		fmt.Fprintln(errOut, err)
		return exitFlags
	}
	var todo []Record
	for _, r := range src {
		switch {
		case r.Stop != "":
			fmt.Fprintf(out, "%-12s skipped: it stopped in %s\n", r.Question.ID, *from)
		case r.Task == nil:
			fmt.Fprintf(out, "%-12s skipped: %s keeps no task (a run before E0)\n", r.Question.ID, *from)
		case *resume && done(dir, r.Question.ID):
		default:
			todo = append(todo, r)
		}
	}
	if *expect != len(todo) {
		fmt.Fprintf(errOut, "this run would write %d combined answers (up to %d AI calls): give -expect %d\n", len(todo), 2*len(todo), len(todo))
		return exitFlags
	}
	if !*resume {
		if _, err := os.Stat(dir); err == nil {
			fmt.Fprintf(errOut, "run %s exists: pick another name, or -resume\n", *name)
			return exitFlags
		}
	}
	if selfElevated() {
		fmt.Fprintln(errOut, "synth-eval runs with normal rights: use a terminal that is not run as administrator")
		return exitError
	}
	if len(todo) == 0 {
		fmt.Fprintln(out, "nothing left to write")
		return exitOK
	}
	w, closeW, err := newWriter(ctx)
	if err != nil {
		fmt.Fprintln(errOut, err)
		return exitError
	}
	defer closeW()
	fmt.Fprintf(out, "run %s: %d combined answers from %s, writer %s, tier %s, prompt %s\n", *name, len(todo), *from, *agent, *tier, *variant)
	return resynthRecords(ctx, w, todo, *from, RunInfo{Name: *name, Writer: *agent, SynthTier: *tier, Synth: *variant}, prompt, dir, time.Now, out)
}

// pickRecords keeps the records whose IDs only names, in the run's order;
// every name must be one of them. No names keeps them all.
func pickRecords(rs []Record, only []string) ([]Record, error) {
	var qs []Question
	for _, r := range rs {
		qs = append(qs, r.Question)
	}
	if _, err := pick(qs, only); err != nil {
		return nil, err
	}
	if len(only) == 0 {
		return rs, nil
	}
	var out []Record
	for _, r := range rs {
		if slices.Contains(only, r.Question.ID) {
			out = append(out, r)
		}
	}
	return out, nil
}

// resynthRecords writes each record's combined answer again and saves the
// record under the new run, with the source's question, answers,
// comparison and refining. It stops on a usage limit and when ctx ends, as
// runQuestions does.
func resynthRecords(ctx context.Context, w writer, src []Record, from string, run RunInfo, prompt func(judge.SynthTask, string) string, dir string, now func() time.Time, out io.Writer) int {
	for i, r := range src {
		rec := r
		rec.Run = RunInfo{Name: run.Name, Tier: r.Run.Tier, Writer: run.Writer, Refine: r.Run.Refine, ModTier: r.Run.ModTier, From: from, SynthTier: run.SynthTier, Synth: run.Synth}
		rec.Seq, rec.Stop = i+1, ""
		rec.At = now().UTC().Format(time.RFC3339)
		rec.Synth = writeSynth(ctx, w, *r.Task, run.Writer, run.SynthTier, prompt)
		rec.Differs = nil
		if split := synthSplits[run.Synth]; split != nil && rec.Synth.Status == "done" {
			sr := rec.Synth
			sr.Text, rec.Differs = split(sr.Text, r.Task.Answers)
			rec.Differs.Agent, rec.Differs.Model, rec.Differs.Ms, rec.Differs.In, rec.Differs.Out = sr.Agent, sr.Model, sr.Ms, sr.In, sr.Out
		}
		if ctx.Err() != nil {
			fmt.Fprintf(out, "%-12s interrupted; run again with -resume\n", r.Question.ID)
			return exitInterrupted
		}
		if limitedText(rec.Synth.Detail) {
			rec.Stop = "usage_limit"
		}
		if err := saveRecord(dir, rec); err != nil {
			fmt.Fprintf(out, "cannot save %s: %v\n", r.Question.ID, err)
			return exitError
		}
		fmt.Fprintln(out, progressLine(rec))
		if d := rec.Differs; d != nil {
			quotes, found := 0, 0
			for _, p := range d.Places {
				for _, sd := range p.Sides {
					quotes++
					if sd.Found {
						found++
					}
				}
			}
			fmt.Fprintf(out, "%-12s list %s: %d places, quotes found %d/%d %s\n", "", d.Status, len(d.Places), found, quotes, d.Error)
		}
		if rec.Stop == "usage_limit" {
			fmt.Fprintln(out, "stopped on a usage limit; run again with -resume when it resets")
			return exitUsageLimit
		}
	}
	return exitOK
}

// writeSynth asks the writer for task's combined answer, once more with a
// new nonce when the reply is not one, as compares does.
func writeSynth(ctx context.Context, w writer, task judge.SynthTask, agent, tier string, prompt func(judge.SynthTask, string) string) *SynthRec {
	sr := &SynthRec{Status: "failed", Agent: agent, Judged: task.Compared}
	for attempt := 1; attempt <= 2; attempt++ {
		p := prompt(task, judge.SynthNonce(task))
		if p == "" {
			sr.Error = core.ErrCodeInternal
			return sr
		}
		res, err := w.Write(ctx, writeReq{Agent: agent, Tier: tier, Prompt: p})
		sr.Ms += res.Ms
		sr.In += res.In
		sr.Out += res.Out
		if res.Model != "" {
			sr.Model = res.Model
		}
		retry := attempt == 1 && ctx.Err() == nil
		if err != nil {
			sr.Error, sr.Detail = errCodeOf(err), clip(err.Error(), 300)
			if sr.Error == core.ErrCodeJudgeBadOutput && retry {
				continue
			}
			return sr
		}
		text, verr := judge.ValidateSynth(res.Output)
		if verr != nil {
			sr.Error, sr.Detail = core.ErrCodeJudgeBadOutput, ""
			if retry {
				continue
			}
			return sr
		}
		sr.Status, sr.Error, sr.Detail, sr.Text = "done", "", "", text
		sr.Unnamed = judge.UnnamedSides(task, text)
		return sr
	}
	return sr
}

// errCodeOf is a writer error's code, or "error" for one without.
func errCodeOf(err error) string {
	if c := core.ErrorCode(err); c != "" {
		return c
	}
	return "error"
}
