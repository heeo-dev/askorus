package core

// Following the newest model of a line (#64, 2026-11-20). The tier models
// were names fixed in the code ("gpt-6.1-sol"), so a new version
// (gpt-6.2-sol) went unused until a new agent came out. Now an adapter
// names the lines its tiers follow and how to list its CLI's models, and
// a Follower keeps that list and answers, for a model of a line, the
// newest one listed. Claude's tiers are aliases (haiku, sonnet, opus) that
// Claude Code itself keeps on its newest models, so Claude needs none.
//
// Decided with the maintainer (#64): the tiers and the models the phone
// picked follow their line; a model a person wrote in agent.json stays as
// written; a list that cannot be read keeps the models the agent ships
// with; a new model the AI refuses is asked again with the shipped one
// and left out until the next list; the list is read when the agent
// starts, when the CLI's version changes and every FollowTTL, in the
// background.

import (
	"context"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"
)

// FollowTTL is how long a model list is kept.
const FollowTTL = 6 * time.Hour

// Following is implemented by askers whose tier models follow the newest
// model of their lines.
type Following interface {
	// Follow returns the model to run for model, one the phone named (an
	// own pick, a moderator): the newest of its line when model is not a
	// tier model now and its line has a newer one, else model.
	Follow(model string) string
	// Newer maps each model the agent ships with to the newer one its
	// tiers use now; empty while they use the shipped ones (/v1/info
	// ask.newer).
	Newer() map[string]string
}

// Line is one line of models an adapter follows: the model the agent
// ships with, and a pattern of the line's names whose first group is the
// version ("6.1" of "gpt-6.1-sol").
type Line struct {
	Base string
	Re   *regexp.Regexp
}

// ListedModel is one model a CLI lists.
type ListedModel struct {
	Name string
	// Efforts are the efforts the CLI says the model takes; nil when it
	// does not say (OpenCode).
	Efforts []string
}

// Follower keeps an AI's model list and picks the newest model of a line.
// The zero List never lists, and a nil *Follower follows nothing: both
// keep the shipped models.
type Follower struct {
	Lines []Line
	// List runs the CLI's model list; it bounds its own time. nil: the
	// adapter does not follow (agent.json ask.follow false, or a test).
	List func(context.Context) ([]ListedModel, error)
	TTL  time.Duration // 0: FollowTTL

	mu      sync.Mutex
	listed  map[string]ListedModel
	at      time.Time // when the last list ended, even a failed one
	version string    // the CLI version the last list was read under
	stale   bool      // the CLI's version changed since
	refused map[string]bool
	done    chan struct{} // set while a list runs
}

// Check starts a list in the background when there is none yet, when the
// last one is older than the TTL or when version (the CLI's, "" when not
// known) differs from the one the last list was read under. It never
// waits. Adapters call it from Info, which the app asks before questions.
func (f *Follower) Check(version string) {
	if f == nil || f.List == nil {
		return
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if version != "" && !f.at.IsZero() && version != f.version {
		f.stale = true
	}
	ttl := f.TTL
	if ttl == 0 {
		ttl = FollowTTL
	}
	if f.done != nil || (!f.at.IsZero() && !f.stale && time.Since(f.at) < ttl) || !usageReads.begin() {
		return
	}
	done := make(chan struct{})
	f.done = done
	go func() {
		defer usageReads.wg.Done()
		list, err := f.List(usageReads.ctx)
		f.mu.Lock()
		if err == nil && len(list) > 0 {
			f.listed = map[string]ListedModel{}
			for _, m := range list {
				if ValidAskModel(m.Name) {
					f.listed[m.Name] = m
				}
			}
			f.refused = nil // a new list tries the refused ones again
		}
		// A failed list keeps the last good one (or none: the shipped
		// models) until the next try, a TTL later.
		f.at, f.version, f.stale, f.done = time.Now(), version, false, nil
		f.mu.Unlock()
		close(done)
	}()
}

// Wait waits for a list that runs, or until ctx ends.
func (f *Follower) Wait(ctx context.Context) {
	if f == nil {
		return
	}
	f.mu.Lock()
	done := f.done
	f.mu.Unlock()
	if done == nil {
		return
	}
	select {
	case <-done:
	case <-ctx.Done():
	}
}

// Listed reports whether a list was read.
func (f *Follower) Listed() bool {
	if f == nil {
		return false
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.listed != nil
}

// line is the line model belongs to and its version, or nil.
func (f *Follower) line(model string) (*Line, []int) {
	for i := range f.Lines {
		if m := f.Lines[i].Re.FindStringSubmatch(model); m != nil && len(m) > 1 {
			if v := parseVersion(m[1]); v != nil {
				return &f.Lines[i], v
			}
		}
	}
	return nil, nil
}

// Newest returns the newest listed model of model's line that is newer
// than model, takes effort (when the list says which efforts a model
// takes; "" takes any) and was not refused; else model. A model of no
// line, or one with no list read, is returned as it is.
func (f *Follower) Newest(model, effort string) string {
	if f == nil {
		return model
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	ln, have := f.line(model)
	if ln == nil {
		return model
	}
	best, bestV := model, have
	for name, m := range f.listed {
		if f.refused[name] || !ln.Re.MatchString(name) {
			continue
		}
		if effort != "" && m.Efforts != nil && !slices.Contains(m.Efforts, effort) {
			continue
		}
		sub := ln.Re.FindStringSubmatch(name)
		if v := parseVersion(sub[1]); v != nil && compareVersions(v, bestV) > 0 {
			best, bestV = name, v
		}
	}
	return best
}

// Base returns the model the agent ships with for model's line, "" when
// model is of no line.
func (f *Follower) Base(model string) string {
	if f == nil {
		return ""
	}
	if ln, _ := f.line(model); ln != nil {
		return ln.Base
	}
	return ""
}

// Refuse leaves model out until the next list: the AI refused it (an
// unknown model, an effort it does not take). It reports whether model
// was a newer model of a line, so that the caller asks again with
// Base(model).
func (f *Follower) Refuse(model string) bool {
	if f == nil {
		return false
	}
	base := f.Base(model)
	if base == "" || base == model {
		return false
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.refused == nil {
		f.refused = map[string]bool{}
	}
	f.refused[model] = true
	return true
}

// Efforts returns the efforts the list says model takes, nil when it
// does not say.
func (f *Follower) Efforts(model string) []string {
	if f == nil {
		return nil
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.listed[model].Efforts)
}

// FollowPick is Following.Follow for an asker whose tiers follow f:
// model as it is when a tier runs it, else the newest of its line.
func FollowPick(tiers map[string]AskModel, f *Follower, model string) string {
	for _, m := range tiers {
		if m.Model == model {
			return model
		}
	}
	return f.Newest(model, "")
}

// NewerTiers is Following.Newer for an asker whose tiers follow f: each
// tier model of a line that is not the line's shipped model, under the
// shipped one.
func NewerTiers(tiers map[string]AskModel, f *Follower) map[string]string {
	out := map[string]string{}
	for _, m := range tiers {
		if base := f.Base(m.Model); base != "" && base != m.Model {
			out[base] = m.Model
		}
	}
	return out
}

// parseVersion reads "6", "6.1" or "6.10.2", nil for anything else.
func parseVersion(s string) []int {
	if s == "" {
		return nil
	}
	parts := strings.Split(s, ".")
	v := make([]int, len(parts))
	for i, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil || n < 0 || p != strconv.Itoa(n) {
			return nil
		}
		v[i] = n
	}
	return v
}

// compareVersions orders versions part by part, a missing part as 0, so
// that 6 and 6.0 are equal and 6.10 is after 6.9.
func compareVersions(a, b []int) int {
	for i := 0; i < len(a) || i < len(b); i++ {
		x, y := 0, 0
		if i < len(a) {
			x = a[i]
		}
		if i < len(b) {
			y = b[i]
		}
		if x != y {
			return x - y
		}
	}
	return 0
}
