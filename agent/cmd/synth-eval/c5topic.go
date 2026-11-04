//go:build syntheval

package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/asksemble/asksemble/agent/internal/core"
	"github.com/asksemble/asksemble/agent/internal/judge"
)

// topics measures the topics of the 다른 말 items (C5, #11, 11-04): the
// writer names each item of a run with the agent's own prompt
// (judge.TopicPrompt, topics-v1) as the agent does beside the combined
// answer, at the quick tier. The draft it started from (run c5-d) is that
// prompt with another head. A record's task holds no item keys, so the
// items are keyed d1, d2, … in the task's order. A question without a
// 다른 말 item needs no call.

// TopicRec is what topics made of a record's 다른 말 items.
type TopicRec struct {
	Status string              `json:"status"` // done, none (no item, no call), failed
	Agent  string              `json:"agent,omitempty"`
	Model  string              `json:"model,omitempty"`
	Ms     int64               `json:"ms,omitempty"`
	In     int                 `json:"inputTokens,omitempty"`
	Out    int                 `json:"outputTokens,omitempty"`
	Items  []judge.DifferTopic `json:"items,omitempty"` // one per named item, keyed d1, d2, …
	Error  string              `json:"error,omitempty"`
	Detail string              `json:"detail,omitempty"`
}

// topicTaskOf is the topics task of r's 다른 말 items, as
// judge.BuildTopicTask makes it from a comparison: the sides with words,
// an item of fewer than two such sides left out.
func topicTaskOf(r Record) judge.TopicTask {
	t := judge.TopicTask{}
	if r.Task == nil {
		return t
	}
	t.Question = r.Task.Question
	for i, d := range r.Task.Differ {
		it := judge.TopicItem{Key: fmt.Sprintf("d%d", i+1), Positions: len(d.Sides)}
		for j, sd := range d.Sides {
			if len(sd.Says) > 0 {
				it.Sides = append(it.Sides, sd)
				it.Pos = append(it.Pos, j)
			}
		}
		if len(it.Sides) >= 2 {
			t.Items = append(t.Items, it)
		}
	}
	return t
}

func topicsCmd(ctx context.Context, args []string, out, errOut io.Writer) int {
	fs := flag.NewFlagSet("topics", flag.ContinueOnError)
	fs.SetOutput(errOut)
	from := fs.String("from", "", "the run whose 다른 말 items to name")
	name := fs.String("run", "", "the new run's name (a-z, 0-9, -)")
	only := fs.String("only", "", "question IDs to keep, comma-separated")
	tier := fs.String("tier", core.TierQuick, "the tier whose model names: quick, standard or deep")
	agent := fs.String("writer", "claude", "the AI that names")
	resume := fs.Bool("resume", false, "skip the questions the run already has")
	expect := fs.Int("expect", -1, "the number of calls this run will make")
	if err := fs.Parse(args); err != nil {
		return exitFlags
	}
	switch {
	case !core.ValidTier(*tier):
		fmt.Fprintln(errOut, "-tier must be quick, standard or deep")
		return exitFlags
	case !validWriter(*agent):
		fmt.Fprintln(errOut, "-writer must be claude, codex or opencode")
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
	if src, err = pickRecords(src, splitList(*only)); err != nil {
		fmt.Fprintln(errOut, err)
		return exitFlags
	}
	var todo []Record
	calls := 0
	for _, r := range src {
		switch {
		case r.Task == nil:
			fmt.Fprintf(out, "%-12s skipped: no synth task in %s\n", r.Question.ID, *from)
		case *resume && done(dir, r.Question.ID):
		default:
			todo = append(todo, r)
			if len(topicTaskOf(r).Items) > 0 {
				calls++
			}
		}
	}
	if *expect != calls {
		fmt.Fprintf(errOut, "this run would make %d topic calls (up to %d AI calls): give -expect %d\n", calls, 2*calls, calls)
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
	var w writer
	if calls > 0 {
		ww, closeW, err := newWriter(ctx)
		if err != nil {
			fmt.Fprintln(errOut, err)
			return exitError
		}
		defer closeW()
		w = ww
	}
	fmt.Fprintf(out, "run %s: topics for the 다른 말 items of %d questions from %s (%d calls), writer %s, tier %s\n", *name, len(todo), *from, calls, *agent, *tier)
	for i, r := range todo {
		rec := r
		rec.Run.Name, rec.Run.From = *name, *from
		rec.Seq, rec.Stop = i+1, ""
		rec.Topic = topicOne(ctx, w, topicTaskOf(r), *agent, *tier)
		if ctx.Err() != nil {
			fmt.Fprintf(out, "%-12s interrupted; run again with -resume\n", r.Question.ID)
			return exitInterrupted
		}
		if limitedText(rec.Topic.Detail) {
			rec.Stop = "usage_limit"
		}
		if err := saveRecord(dir, rec); err != nil {
			fmt.Fprintf(out, "cannot save %s: %v\n", r.Question.ID, err)
			return exitError
		}
		t := rec.Topic
		var names []string
		for _, it := range t.Items {
			names = append(names, it.Topic+" ("+strings.Join(it.Gists, " / ")+")")
		}
		fmt.Fprintf(out, "%-12s %s %.1fs %s %s\n", r.Question.ID, t.Status, float64(t.Ms)/1000, strings.Join(names, "; "), t.Error)
		if rec.Stop == "usage_limit" {
			fmt.Fprintln(out, "stopped on a usage limit; run again with -resume when it resets")
			return exitUsageLimit
		}
	}
	return exitOK
}

// topicOne names task's items, asking once more with a new nonce when
// the reply does not fit (judge.ParseTopics).
func topicOne(ctx context.Context, w writer, task judge.TopicTask, agent, tier string) *TopicRec {
	tr := &TopicRec{Status: "none"}
	if len(task.Items) == 0 {
		return tr
	}
	tr.Status, tr.Agent = "failed", agent
	for attempt := 1; attempt <= 2; attempt++ {
		p := judge.TopicPrompt(task, judge.TopicNonce(task))
		if p == "" {
			continue
		}
		start := time.Now()
		res, err := w.Write(ctx, writeReq{Agent: agent, Tier: tier, Prompt: p})
		if res.Ms == 0 {
			res.Ms = time.Since(start).Milliseconds()
		}
		tr.Ms += res.Ms
		tr.In += res.In
		tr.Out += res.Out
		if res.Model != "" {
			tr.Model = res.Model
		}
		if err != nil {
			tr.Error, tr.Detail = errCodeOf(err), clip(err.Error(), 300)
			if tr.Error == core.ErrCodeJudgeBadOutput && attempt == 1 && ctx.Err() == nil {
				continue
			}
			return tr
		}
		got, verr := judge.ValidateSynth(res.Output)
		var named []judge.DifferTopic
		if verr == nil {
			named, verr = judge.ParseTopics(got, task)
		}
		if verr != nil {
			tr.Error, tr.Detail = core.ErrCodeJudgeBadOutput, clip(got, 300)
			continue
		}
		tr.Status, tr.Error, tr.Detail, tr.Items = "done", "", "", named
		return tr
	}
	return tr
}
