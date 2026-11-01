//go:build judgeeval

package main

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/asksemble/asksemble/agent/internal/core"
	"github.com/asksemble/asksemble/agent/internal/judge"
)

// The request loop (plan part 2, attempt.go). It is compares.Manager's
// judgeRequest and mergeVerdicts, copied rule for rule because they are
// unexported (deviation 2); TestAttemptParity runs the same table through
// the product's Manager. What differs is only what a measurement needs: a
// usage limit, the call budget, a stop or Ctrl+C leave the request
// unrecorded, so a resume runs it again, and every attempt's raw output is
// kept for the run folder.

// callLimit bounds one judge call, as compares.JudgeLimit does: the
// adapters apply core.JudgeTimeout themselves.
const callLimit = core.JudgeTimeout + 30*time.Second

// abandonAfter is how long a call may go on past callLimit before the
// request stops waiting for it (compares' abandonAfter).
var abandonAfter = 5 * time.Second

// usageLimitWords mark a judge_failed whose message is a usage limit
// (plan part 2): the run stops, and the request is asked again on resume.
var usageLimitWords = []string{
	"usage limit", "rate limit", "rate-limit", "limit reached", "hit your limit", "quota",
	"too many requests", "try again at", "try again later", "weekly limit", "5-hour", "credit balance",
}

// usageLimit reports whether err is a judge_failed that names a usage
// limit.
func usageLimit(err error) bool {
	var ae *core.ActionError
	if !errors.As(err, &ae) || ae.Code != core.ErrCodeJudgeFailed {
		return false
	}
	d := strings.ToLower(ae.Detail)
	for _, w := range usageLimitWords {
		if strings.Contains(d, w) {
			return true
		}
	}
	return false
}

// failureCode is compares' judgeFailure, code only: an ActionError's code,
// else internal.
func failureCode(err error) string {
	var ae *core.ActionError
	if !errors.As(err, &ae) || ae.Code == "" {
		return core.ErrCodeInternal
	}
	return ae.Code
}

// Why a request ended without being recorded.
const (
	abandonUsage     = "usage_limit"
	abandonBudget    = "call_budget"
	abandonStopped   = "stopped"     // another request stopped the run
	abandonInterrupt = "interrupted" // Ctrl+C
)

// errStop is what before gives to stop the loop before an attempt: one of
// the abandon reasons.
type errStop string

func (e errStop) Error() string { return string(e) }

// attemptRec is one judge call of a request, as requests.jsonl keeps it.
type attemptRec struct {
	N        int    `json:"n"`
	Ms       int64  `json:"ms"`
	Model    string `json:"model"`
	In       int    `json:"in"`
	Out      int    `json:"out"`
	Code     string `json:"code"`     // the adapter's error code, bad_model, or "" when it answered
	Validate string `json:"validate"` // judge_bad_output or internal when Validate refused the output
	Missing  int    `json:"missing"`
	Dropped  int    `json:"dropped"`
	Output   string `json:"output"` // the judge's raw output; content, kept only in the run folder
}

// outcome is how a request ended.
type outcome struct {
	done     bool
	code     string // the failure's code when not done
	abandon  string // when set the request is not recorded
	v        judge.Validated
	attempts []attemptRec
	model    string   // the model that judged last, else the asked one
	refs     []string // cleanup refs, whatever happened
}

// recorded reports whether the outcome is written to requests.jsonl.
func (o outcome) recorded() bool { return o.abandon == "" }

// attemptLoop runs one request.
type attemptLoop struct {
	judger core.Judger
	family string // the company the engine judges as
	folder string
	model  string
	effort string
	// before runs before every attempt; an errStop leaves the request
	// unrecorded. nil: nothing to check.
	before func() error
	// after runs after every judge call with its attempt, for events.log.
	after func(a attemptRec)
	// prompt and nonce are judge.Prompt and judge.Nonce; tests replace
	// them.
	prompt func(core.JudgeTask, string) string
	nonce  func(core.JudgeTask) string
}

// run asks the judge, once more when the reply was not the judge JSON or
// missed ids, as compares' judgeRequest does: a new nonce every time,
// attempt 1's verdicts stand and attempt 2 fills only the ids it missed.
func (l attemptLoop) run(ctx context.Context, task core.JudgeTask) outcome {
	prompt, nonce := l.prompt, l.nonce
	if prompt == nil {
		prompt = judge.Prompt
	}
	if nonce == nil {
		nonce = judge.Nonce
	}
	var out outcome
	out.model = l.model
	var first *judge.Validated // attempt 1's verdicts when it missed ids
	firstErr := ""             // attempt 1's bad output
	end := func(code string) outcome {
		if first != nil {
			out.done, out.v = true, *first // attempt 1's verdicts stand
			return out
		}
		out.code = code
		return out
	}
	abandon := func(why string) outcome {
		out.abandon = why
		return out
	}
	for attempt := 1; attempt <= 2; attempt++ {
		if l.before != nil {
			if err := l.before(); err != nil {
				var s errStop
				if errors.As(err, &s) {
					return abandon(string(s))
				}
				return abandon(abandonStopped)
			}
		}
		if ctx.Err() != nil {
			return abandon(abandonInterrupt)
		}
		p := prompt(task, nonce(task))
		if p == "" {
			return end(core.ErrCodeInternal)
		}
		start := time.Now()
		res, err := callJudge(ctx, l.judger, core.JudgeRequest{Folder: l.folder, Task: task, Prompt: p, Model: l.model, Effort: l.effort})
		rec := attemptRec{N: attempt, Ms: time.Since(start).Milliseconds(), Model: res.Model, In: res.InputTokens, Out: res.OutputTokens, Output: string(res.Output)}
		// The cleanup ref is kept whatever the error (core.Judger).
		if res.Cleanup != "" {
			out.refs = append(out.refs, res.Cleanup)
		}
		if res.Model != "" {
			out.model = res.Model
		}
		if ctx.Err() != nil {
			// Ctrl+C: what the judge gave is not counted.
			rec.Code = abandonInterrupt
			l.report(rec)
			return abandon(abandonInterrupt)
		}
		if err != nil {
			rec.Code = failureCode(err)
			out.attempts = append(out.attempts, rec)
			l.report(rec)
			if usageLimit(err) {
				return abandon(abandonUsage)
			}
			if rec.Code == core.ErrCodeJudgeBadOutput && attempt == 1 {
				firstErr = rec.Code
				continue
			}
			return end(rec.Code)
		}
		// S2 decision 3: a judge of another company than planned may be
		// judging its own company's answer. Its reply is thrown away.
		if family := l.judger.Family(res.Model); family == "" || !strings.EqualFold(family, l.family) {
			rec.Code = core.ErrCodeBadModel
			out.attempts = append(out.attempts, rec)
			l.report(rec)
			return end(core.ErrCodeBadModel)
		}
		v, verr := judge.Validate(task, res.Output)
		if verr != nil {
			var je *judge.Error
			if !errors.As(verr, &je) || je.Code == core.ErrCodeInternal {
				rec.Validate = core.ErrCodeInternal
				out.attempts = append(out.attempts, rec)
				l.report(rec)
				return end(core.ErrCodeInternal)
			}
			rec.Validate = core.ErrCodeJudgeBadOutput
			out.attempts = append(out.attempts, rec)
			l.report(rec)
			if attempt == 1 {
				firstErr = core.ErrCodeJudgeBadOutput
				continue
			}
			return end(core.ErrCodeJudgeBadOutput)
		}
		rec.Missing, rec.Dropped = len(v.Missing), v.Dropped
		out.attempts = append(out.attempts, rec)
		l.report(rec)
		switch {
		case first != nil:
			out.done, out.v = true, mergeVerdicts(task, *first, v)
			return out
		case attempt == 1 && len(v.Missing) > 0:
			first = &v
			continue
		}
		out.done, out.v = true, v
		return out
	}
	return end(firstErr)
}

func (l attemptLoop) report(a attemptRec) {
	if l.after != nil {
		l.after(a)
	}
}

// callJudge is compares' callJudge: one Judge call within callLimit. A
// Judge that panics gives internal; one that ends with the limit gives
// judge_timeout; one that keeps going abandonAfter past it is left behind
// with judge_timeout.
func callJudge(parent context.Context, j core.Judger, req core.JudgeRequest) (core.JudgeResult, error) {
	ctx, cancel := context.WithTimeout(parent, callLimit)
	defer cancel()
	type result struct {
		res core.JudgeResult
		err error
	}
	ch := make(chan result, 1)
	go func() {
		defer func() {
			if v := recover(); v != nil {
				ch <- result{err: core.Fail(core.ErrCodeInternal, "")}
			}
		}()
		res, err := j.Judge(ctx, req)
		ch <- result{res, err}
	}()
	timedOut := func(r result) (core.JudgeResult, error) {
		if r.err != nil && errors.Is(ctx.Err(), context.DeadlineExceeded) {
			r.err = core.Fail(core.ErrCodeJudgeTimeout, "")
		}
		return r.res, r.err
	}
	select {
	case r := <-ch:
		return timedOut(r)
	case <-ctx.Done():
	}
	t := time.NewTimer(abandonAfter)
	defer t.Stop()
	select {
	case r := <-ch:
		return timedOut(r)
	case <-t.C:
		return core.JudgeResult{}, core.Fail(core.ErrCodeJudgeTimeout, "")
	}
}

// mergeVerdicts is compares' mergeVerdicts: attempt 1's verdicts stand and
// attempt 2's are taken only for the ids attempt 1 missed, in the order
// the ids were sent. Ids both missed stay missing. Reason comes along.
func mergeVerdicts(task core.JudgeTask, first, second judge.Validated) judge.Validated {
	missed := map[string]bool{}
	for _, id := range first.Missing {
		missed[id] = true
	}
	byID := map[string]judge.Verdict{}
	for _, v := range first.Verdicts {
		byID[v.ID] = v
	}
	for _, v := range second.Verdicts {
		if missed[v.ID] {
			byID[v.ID] = v
		}
	}
	out := judge.Validated{Verdicts: []judge.Verdict{}, Missing: []string{}, Dropped: first.Dropped + second.Dropped}
	seen := map[string]bool{}
	for _, u := range task.Units {
		if u.ID == "" || seen[u.ID] {
			continue
		}
		seen[u.ID] = true
		if v, ok := byID[u.ID]; ok {
			out.Verdicts = append(out.Verdicts, v)
		} else if missed[u.ID] {
			out.Missing = append(out.Missing, u.ID)
		}
	}
	return out
}
