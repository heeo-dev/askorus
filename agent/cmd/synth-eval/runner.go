//go:build syntheval

package main

import (
	"context"
	"fmt"
	"io"
	"strings"
	"time"
)

// pipeline asks one question the way the phone does and gives back what
// the record keeps; the agent's (agent.go), or a fake in tests.
type pipeline interface {
	Versions(ctx context.Context) map[string]string
	Ask(ctx context.Context, q Question, run RunInfo) (Record, error)
}

// usageLimitWords mark an AI's message that names a usage limit (as
// judge-eval): the run stops there, and resume asks that question again.
var usageLimitWords = []string{
	"usage limit", "rate limit", "rate-limit", "limit reached", "hit your limit", "quota",
	"too many requests", "try again at", "try again later", "weekly limit", "5-hour", "credit balance",
}

func usageLimited(r Record) bool {
	msgs := []string{}
	for _, a := range r.Answers {
		msgs = append(msgs, a.Detail)
	}
	if r.Compare != nil {
		msgs = append(msgs, r.Compare.Detail)
	}
	if r.Synth != nil {
		msgs = append(msgs, r.Synth.Detail)
	}
	for _, m := range msgs {
		m = strings.ToLower(m)
		for _, w := range usageLimitWords {
			if strings.Contains(m, w) {
				return true
			}
		}
	}
	return false
}

// callsPerQuestion is what one question costs at most on a PC with three
// AIs: three answers, two judges per answer and one combined answer (a
// bad output asked again comes on top).
const callsPerQuestion = 10

// runQuestions asks each question in turn and saves its record; with
// resume, a question the run already has is skipped. It stops on a usage
// limit (exitUsageLimit; that question is asked again on resume) and when
// ctx ends (exitInterrupted; that question is not saved).
func runQuestions(ctx context.Context, p pipeline, qs []Question, run RunInfo, dir string, resume bool, now func() time.Time, out io.Writer) int {
	versions := p.Versions(ctx)
	for i, q := range qs {
		if resume && done(dir, q.ID) {
			fmt.Fprintf(out, "%-12s already done\n", q.ID)
			continue
		}
		rec, err := p.Ask(ctx, q, run)
		if ctx.Err() != nil {
			fmt.Fprintf(out, "%-12s interrupted; run again with -resume\n", q.ID)
			return exitInterrupted
		}
		rec.V, rec.Seq, rec.Question, rec.Run, rec.Versions = 1, i+1, q, run, versions
		rec.At = now().UTC().Format(time.RFC3339)
		if err != nil {
			rec.Stop = clip(err.Error(), 300)
		}
		if usageLimited(rec) {
			rec.Stop = "usage_limit"
		}
		if err := saveRecord(dir, rec); err != nil {
			fmt.Fprintf(out, "cannot save %s: %v\n", q.ID, err)
			return exitError
		}
		fmt.Fprintln(out, progressLine(rec))
		if rec.Stop == "usage_limit" {
			fmt.Fprintln(out, "stopped on a usage limit; run again with -resume when it resets")
			return exitUsageLimit
		}
	}
	return exitOK
}

// progressLine is one question's line while a run goes.
func progressLine(r Record) string {
	ok := 0
	for _, a := range r.Answers {
		if a.Status == "done" {
			ok++
		}
	}
	line := fmt.Sprintf("%-12s answers %d/%d", r.Question.ID, ok, len(r.Answers))
	if c := r.Compare; c != nil {
		line += fmt.Sprintf(" · compare %s %.1fs (differ %d, check %d, agree %d, only one %d)",
			c.Status, float64(c.Ms)/1000, c.Counts.Differ, c.Counts.Check, c.Counts.Agree, c.Counts.OnlyOne)
	}
	if s := r.Synth; s != nil {
		line += fmt.Sprintf(" · combined %s", s.Status)
		if s.Status == "done" {
			line += fmt.Sprintf(" %.1fs %d chars", float64(s.Ms)/1000, len([]rune(s.Text)))
		} else if s.Error != "" {
			line += " " + s.Error
		}
	}
	if r.Stop != "" {
		line += " · stopped: " + r.Stop
	}
	return line
}
