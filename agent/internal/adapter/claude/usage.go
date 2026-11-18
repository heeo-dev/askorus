package claude

// What is left of the person's Claude plan (#20, decision 10). Claude Code
// reports it on its event stream: every one-off call the agent makes
// (ask, follow-up, debate turn, combined answer, question spec) runs with
// --output-format stream-json, and the stream carries one rate_limit_event
// with the 5-hour and the weekly window. The agent keeps the last one in
// memory with the time it came, and /v1/info shows it as of then. It never
// asks Claude only to read it: that would spend the plan.

import (
	"bytes"
	"encoding/json"
	"math"
	"time"

	"github.com/heeo-dev/askorus/agent/internal/core"
)

type rateWindow struct {
	Utilization *float64 `json:"utilization"` // used, 0 to 1
	ResetsAt    *float64 `json:"resetsAt"`    // seconds since 1970
	Status      string   `json:"status"`      // allowed, allowed_warning, rejected
}

// rateWindows are the windows of unifiedWindows the agent shows, in order.
var rateWindows = []struct{ key, window, name string }{
	{"five_hour", core.Window5h, "5 hours"},
	{"seven_day", core.WindowWeek, "Weekly"},
}

// readRateLimits reads the last rate_limit_event of a stream: its
// unifiedWindows, or without them the one window rateLimitType names. nil
// when there is none (an API key sign-in, an older Claude Code, the json
// output).
type rateEvent struct {
	Type string `json:"type"`
	Info *struct {
		rateWindow
		Type    string                 `json:"rateLimitType"`
		Windows map[string]*rateWindow `json:"unifiedWindows"`
	} `json:"rate_limit_info"`
}

func readRateLimits(out []byte) []core.UsageGroup {
	var ev rateEvent
	found := false
	for _, line := range bytes.Split(out, []byte("\n")) {
		line = bytes.TrimSpace(line)
		if !bytes.Contains(line, []byte("rate_limit_event")) {
			continue
		}
		// An answer may hold the words too: only a line that is the event counts.
		var e rateEvent
		if json.Unmarshal(line, &e) == nil && e.Type == "rate_limit_event" && e.Info != nil {
			ev, found = e, true
		}
	}
	if !found {
		return nil
	}
	windows := ev.Info.Windows
	if windows == nil && ev.Info.Type != "" {
		w := ev.Info.rateWindow
		windows = map[string]*rateWindow{ev.Info.Type: &w}
	}
	var groups []core.UsageGroup
	for _, rw := range rateWindows {
		w := windows[rw.key]
		if w == nil || w.Utilization == nil || math.IsNaN(*w.Utilization) {
			continue
		}
		used := math.Min(1, math.Max(0, *w.Utilization))
		g := core.UsageGroup{Name: rw.name, Left: 1 - used, Window: rw.window, Limited: w.Status == "rejected" || used >= 1}
		if w.ResetsAt != nil && *w.ResetsAt > 0 && *w.ResetsAt < 1e11 {
			g.ResetAt = time.Unix(int64(*w.ResetsAt), 0).UTC().Format(time.RFC3339)
		}
		groups = append(groups, g)
	}
	return groups
}

// noteUsage keeps the usage a run's stream reported, if it did.
func (a *Adapter) noteUsage(out []byte) {
	g := readRateLimits(out)
	if g == nil {
		return
	}
	a.usageMu.Lock()
	a.usage, a.usageAt = g, time.Now()
	a.usageMu.Unlock()
}

// keptUsage is the last usage with the time it came (UsageGroup.At).
func (a *Adapter) keptUsage() []core.UsageGroup {
	a.usageMu.Lock()
	defer a.usageMu.Unlock()
	if a.usageAt.IsZero() {
		return nil
	}
	at := a.usageAt.UTC().Format(time.RFC3339)
	out := make([]core.UsageGroup, len(a.usage))
	for i, g := range a.usage {
		g.At = at
		out[i] = g
	}
	return out
}
