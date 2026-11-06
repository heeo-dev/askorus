//go:build syntheval

package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"slices"
	"strings"
	"time"

	"github.com/asksemble/asksemble/agent/internal/core"
	"github.com/asksemble/asksemble/agent/internal/judge"
)

// rewrite (C9, 2026-11-05, user OK) writes the answers after a debate again
// from debate runs' records, the debate itself kept: the same points and
// turns, a new prompt. C8's answers after the debate were better but ran
// long (median 1,018 characters against the combined answer's 675) and lost
// on length. Two styles:
//
//   - short (debate-v3): r2's rule 5, kept as short as a combined answer.
//   - edit (debate-e1): the combined answer from before the debate, changed
//     only where the debate changed something.
//
// The new run keeps each record's answers, so score can compare it with the
// runs the debates came from.

var rewriteStyles = []string{"short", "short2", "short3", "plus", "edit"}

func rewriteCmd(ctx context.Context, args []string, out, errOut io.Writer) int {
	fs := flag.NewFlagSet("rewrite", flag.ContinueOnError)
	fs.SetOutput(errOut)
	from := fs.String("from", "", "the debate runs whose answers after the debate to write again, comma-separated")
	base := fs.String("base", "", "for each -from run, the run its debates came from (the combined answers before them), comma-separated")
	name := fs.String("run", "", "the new run's name (a-z, 0-9, -)")
	style := fs.String("style", "", "short, short2, short3, plus or edit")
	only := fs.String("only", "", "question IDs to keep, comma-separated")
	tier := fs.String("tier", core.TierStandard, "the writer's tier")
	mod := fs.String("moderator", "claude", "the AI that writes")
	expect := fs.Int("expect", -1, "the number of answers this will write")
	if err := fs.Parse(args); err != nil {
		return exitFlags
	}
	froms, bases, keep := splitList(*from), splitList(*base), splitList(*only)
	switch {
	case !core.ValidTier(*tier):
		fmt.Fprintln(errOut, "-tier must be quick, standard or deep")
		return exitFlags
	case !validWriter(*mod):
		fmt.Fprintln(errOut, "-moderator must be claude, codex or opencode")
		return exitFlags
	case !slices.Contains(rewriteStyles, *style):
		fmt.Fprintln(errOut, "-style must be short, short2, short3, plus or edit")
		return exitFlags
	case len(froms) == 0 || len(froms) != len(bases):
		fmt.Fprintln(errOut, "give -from and -base, one base run for each debate run")
		return exitFlags
	}
	dir, err := runDir(*name)
	if err != nil {
		fmt.Fprintln(errOut, err)
		return exitFlags
	}
	if _, err := os.Stat(dir); err == nil {
		fmt.Fprintf(errOut, "run %s exists: pick another name\n", *name)
		return exitFlags
	}
	type job struct {
		rec    Record
		before string
	}
	var todo []job
	seen := map[string]bool{}
	for i, f := range froms {
		recs, err := loadRun(f)
		if err != nil {
			fmt.Fprintln(errOut, err)
			return exitFlags
		}
		brecs, err := loadRun(bases[i])
		if err != nil {
			fmt.Fprintln(errOut, err)
			return exitFlags
		}
		before := map[string]string{}
		for _, b := range brecs {
			if b.Synth != nil && b.Synth.Status == "done" {
				before[b.Question.ID] = b.Synth.Text
			}
		}
		for _, r := range recs {
			id := r.Question.ID
			switch {
			case len(keep) > 0 && !slices.Contains(keep, id):
			case seen[id]:
				fmt.Fprintf(out, "%-12s skipped: already taken from an earlier -from run\n", id)
			case r.Debate == nil || !slices.ContainsFunc(r.Debate.Points, func(p DebatePoint) bool { return p.Debate && len(p.Turns) > 0 }):
				fmt.Fprintf(out, "%-12s skipped: no debate in %s\n", id, f)
			case before[id] == "":
				fmt.Fprintf(out, "%-12s skipped: no combined answer in %s\n", id, bases[i])
			default:
				seen[id] = true
				todo = append(todo, job{r, before[id]})
			}
		}
	}
	if *expect != len(todo) {
		fmt.Fprintf(errOut, "this would write %d answers (up to 2 AI calls each): give -expect %d\n", len(todo), len(todo))
		return exitFlags
	}
	if selfElevated() {
		fmt.Fprintln(errOut, "synth-eval runs with normal rights: use a terminal that is not run as administrator")
		return exitError
	}
	d, closeD, err := newDebater(ctx)
	if err != nil {
		fmt.Fprintln(errOut, err)
		return exitError
	}
	defer closeD()
	fmt.Fprintf(out, "rewrite %s: %d answers after the debate, style %s, writer %s at %s\n", *name, len(todo), *style, *mod, *tier)
	code := exitOK
	for i, j := range todo {
		if ctx.Err() != nil {
			return exitInterrupted
		}
		rec := j.rec
		rec.Synth = rewriteAfterDebate(ctx, d, j.rec, j.before, *style, *mod, *tier)
		rec.Run = RunInfo{Name: *name, Tier: j.rec.Run.Tier, Writer: *mod, From: j.rec.Run.Name, SynthTier: *tier, Synth: "debate-" + *style}
		rec.Seq, rec.At = i+1, time.Now().UTC().Format(time.RFC3339)
		if err := saveRecord(dir, rec); err != nil {
			fmt.Fprintf(out, "cannot save %s: %v\n", rec.Question.ID, err)
			code = exitError
		}
		s := rec.Synth
		fmt.Fprintf(out, "%-12s %s %.1fs %d chars (before %d)%s\n", rec.Question.ID, s.Status, float64(s.Ms)/1000, len([]rune(s.Text)), len([]rune(j.before)),
			map[bool]string{true: " " + s.Error, false: ""}[s.Error != ""])
		if limitedText(s.Detail) {
			return exitUsageLimit
		}
	}
	return code
}

func loadRun(name string) ([]Record, error) {
	d, err := runDir(name)
	if err != nil {
		return nil, err
	}
	return loadRecords(d)
}

// rewriteAfterDebate writes r's answer after its debate again in style.
func rewriteAfterDebate(ctx context.Context, w writer, r Record, before, style, mod, tier string) *SynthRec {
	task := judge.SynthTask{Question: r.Question.Text}
	for _, a := range doneAnswers(r) {
		task.Answers = append(task.Answers, judge.SynthAnswer{AI: aiName(a.Agent), Text: a.Text})
	}
	if r.Task != nil {
		task.Question, task.Earlier, task.Answers = r.Task.Question, r.Task.Earlier, r.Task.Answers
	}
	points := r.Debate.Points
	return writeSynth(ctx, w, task, mod, tier, func(t judge.SynthTask, nonce string) string {
		return rewritePrompt(t, points, before, style, nonce)
	})
}

func rewritePrompt(task judge.SynthTask, points []DebatePoint, before, style, nonce string) string {
	switch style {
	case "edit":
		return afterDebatePromptFor(task, points, nonce, "debate-e1", debateEditRule5, strings.TrimSpace(before))
	case "plus":
		return afterDebatePromptFor(task, points, nonce, "debate-v3p", debatePlusRule5, "")
	case "short3":
		return afterDebatePromptFor(task, points, nonce, "debate-v4b", debateShort3Rule5, "")
	case "short2":
		return afterDebatePromptFor(task, points, nonce, "debate-v4", debateShort2Rule5, "")
	}
	return afterDebatePromptFor(task, points, nonce, "debate-v3", debateShortRule5, "")
}
