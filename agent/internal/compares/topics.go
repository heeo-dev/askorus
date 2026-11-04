package compares

import (
	"strings"
	"time"

	"github.com/asksemble/asksemble/agent/internal/core"
	"github.com/asksemble/asksemble/agent/internal/judge"
)

// Topics of the 다른 말 items (C5, #11, 2026-11-04): the phone folds the
// places where the answers differ into one line under the combined answer
// and shows each item's topic and a few words per side (judge/topics.go).
// When Options.Topics is on and the combined answer starts with 다른 말
// items, the AI that writes it also names them at the quick tier, beside
// it: it takes a process slot of its own (two per AI by default), so it
// does not wait for the combined answer. It ends on its own; the combined
// answer never waits for it, and a failed one leaves the combined answer
// as it is (the phone then shows the line without topics). It is part of
// the combined answer's state: a retry or a rewrite starts it again, and
// the comparison is busy while it runs. Logs carry codes and counts only.

// Topics is the topics of a combined answer as the snapshot shows it.
// Fields may be added inside /v1, never removed (principle 4).
type Topics struct {
	Status       string              `json:"status"` // running, done or failed
	Agent        string              `json:"agent,omitempty"`
	Model        string              `json:"model,omitempty"`
	Attempt      int                 `json:"attempt,omitempty"`
	Ms           int64               `json:"ms,omitempty"`
	InputTokens  int                 `json:"inputTokens,omitempty"`
	OutputTokens int                 `json:"outputTokens,omitempty"`
	Items        []judge.DifferTopic `json:"items,omitempty"` // done: by the items' keys
	Error        *judge.Error        `json:"error,omitempty"`
}

// Topics statuses.
const (
	TopicsRunning = "running"
	TopicsDone    = "done"
	TopicsFailed  = "failed"
)

// topicsState is a combined answer's topics. Manager.mu guards it. The
// goroutine holds the pointer it started with, so after a reset its writes
// land on a state no snapshot reads.
type topicsState struct {
	status  string
	writer  string
	model   string
	attempt int
	ms      int64
	in, out int
	items   []judge.DifferTopic
	err     *judge.Error
}

func snapshotTopics(t *topicsState) *Topics {
	if t == nil {
		return nil
	}
	out := &Topics{Status: t.status, Agent: t.writer, Model: t.model, Attempt: t.attempt, Ms: t.ms,
		InputTokens: t.in, OutputTokens: t.out}
	if t.status == TopicsDone {
		out.Items = append([]judge.DifferTopic{}, t.items...)
	}
	if t.err != nil {
		e := *t.err
		out.Error = &e
	}
	return out
}

// topicsBusy reports whether c's topics are being named.
func topicsBusy(c *comparison) bool {
	return c.synth.topics != nil && c.synth.topics.status == TopicsRunning
}

// topicsStart names the 다른 말 items of in beside the combined answer that
// w writes, when Options.Topics is on and there is an item to name: with
// w's AI at the quick tier, or the first AI that may write at it. It is
// called from the combined answer's goroutine, without mu.
func (m *Manager) topicsStart(c *comparison, w Writer, in synthInput, names map[string]string) {
	if !m.o.Topics || !in.judged {
		return
	}
	task := judge.BuildTopicTask(in.task, in.items, func(id string) string { return names[id] })
	if len(task.Items) == 0 {
		return
	}
	quick := m.Writers(m.base, core.TierQuick)
	tw, ok := Writer{}, false
	for _, x := range quick {
		if x.Agent == w.Agent {
			tw, ok = x, true
			break
		}
	}
	if !ok {
		if tw, ok = pickWriter(quick, in); !ok {
			return
		}
	}
	m.locked(func() {
		ts := &topicsState{status: TopicsRunning, writer: tw.Agent, model: tw.Model}
		c.synth.topics = ts
		c.active++
		started := m.goLocked(func() { m.topicsRun(c, ts, tw, task, in.cancel) })
		if !started {
			c.active--
			ts.status, ts.err = TopicsFailed, synthFailure(core.ErrCodeCancelled, msgSynthClosed)
		}
		m.touchLocked(c)
	})
}

// topicsRun asks tw to name task's items, once more when the reply does
// not fit, and ends ts whatever happens, a panic too.
func (m *Manager) topicsRun(c *comparison, ts *topicsState, tw Writer, task judge.TopicTask, cancel <-chan struct{}) {
	err := synthFailure(core.ErrCodeInternal, "")
	var items []judge.DifferTopic
	defer func() {
		if v := recover(); v != nil {
			err = synthFailure(core.ErrCodeInternal, "")
		}
		m.mu.Lock()
		defer m.mu.Unlock()
		if err == nil {
			ts.status, ts.items, ts.err = TopicsDone, items, nil
		} else {
			ts.status, ts.items, ts.err = TopicsFailed, nil, err
		}
		if c.status == Done || c.status == Failed {
			c.finishedAt = m.now()
		}
		m.log.Info("topics ended", "comparison", c.id, "writer", ts.writer, "status", ts.status, "code", codeOf(err),
			"attempt", ts.attempt, "ms", ts.ms, "items", len(task.Items), "named", len(items), "leftOut", task.LeftOut)
		c.active--
		m.touchLocked(c)
	}()
	items, err = m.topicsCall(c, ts, tw, task, cancel)
}

// msgTopicsBad is a topics reply that did not fit, twice.
const msgTopicsBad = "the writing AI's reply did not name each 다른 말 item, also when asked again"

func codeOf(err *judge.Error) string {
	if err == nil {
		return ""
	}
	return err.Code
}

func (m *Manager) topicsCall(c *comparison, ts *topicsState, tw Writer, task judge.TopicTask, cancel <-chan struct{}) ([]judge.DifferTopic, *judge.Error) {
	m.log.Info("topics started", "comparison", c.id, "writer", tw.Agent, "rules", judge.TopicRules, "items", len(task.Items), "leftOut", task.LeftOut)
	rd := &round{start: time.Now(), cancel: cancel}
	release, failed := m.acquire(rd, tw.Agent)
	if failed != nil {
		switch {
		case failed.Code == core.ErrCodeJudgeTimeout:
			return nil, synthFailure(core.ErrCodeJudgeTimeout, msgSynthQueue)
		case m.base.Err() != nil:
			return nil, synthFailure(core.ErrCodeCancelled, msgSynthClosed)
		}
		return nil, synthFailure(core.ErrCodeCancelled, "")
	}
	called := false
	var refs []string
	defer func() {
		release()
		if called && tw.Cleaner != nil {
			m.synthCleanup(c, tw, refs)
		}
	}()
	var firstErr *judge.Error
	for attempt := 1; attempt <= 2; attempt++ {
		if attempt == 2 && rd.stopped(m.base) {
			return nil, firstErr
		}
		prompt := judge.TopicPrompt(task, judge.TopicNonce(task))
		if prompt == "" {
			return nil, synthFailure(core.ErrCodeInternal, "")
		}
		m.locked(func() { ts.attempt = attempt })
		called = true
		start := time.Now()
		res, err := m.callSynth(tw, core.SynthRequest{Folder: m.o.Gate.Folder, Prompt: prompt, Model: tw.Model, Effort: tw.Effort})
		if res.Cleanup != "" {
			refs = append(refs, res.Cleanup)
		}
		m.locked(func() {
			ts.ms += time.Since(start).Milliseconds()
			ts.in += res.InputTokens
			ts.out += res.OutputTokens
			if res.Model != "" {
				ts.model = res.Model
			}
			m.touchLocked(c)
		})
		if m.base.Err() != nil {
			return nil, synthFailure(core.ErrCodeCancelled, msgSynthClosed)
		}
		if err != nil {
			f := synthCallFailure(err)
			if f.Code == core.ErrCodeJudgeBadOutput && attempt == 1 {
				firstErr = f
				continue
			}
			return nil, f
		}
		if family := tw.Judger.Family(res.Model); family == "" || !strings.EqualFold(family, tw.Family) {
			return nil, synthFailure(core.ErrCodeBadModel, msgSynthCompany)
		}
		text, verr := judge.ValidateSynth(res.Output)
		var items []judge.DifferTopic
		if verr == nil {
			items, verr = judge.ParseTopics(text, task)
		}
		if verr != nil {
			f := synthFailure(core.ErrCodeJudgeBadOutput, msgTopicsBad)
			if attempt == 1 {
				firstErr = f
				continue
			}
			return nil, f
		}
		return items, nil
	}
	return nil, firstErr
}
