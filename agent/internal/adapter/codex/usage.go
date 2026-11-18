package codex

// What is left of the person's Codex plan (#20, decision 9). The agent asks
// `codex app-server`, Codex's own JSON-RPC server on stdin and stdout, the
// one request account/rateLimits/read: no model is asked and nothing is
// spent. It writes three constant lines and nothing from a phone, never
// another method (the same server can spend the plan's free reset). Of the
// answer it keeps the two windows only; the account id, the plan and the
// credits are dropped. The server is marked experimental: anything that is
// not the expected answer leaves the usage out.

import (
	"bytes"
	"context"
	"encoding/json"
	"math"
	"os"
	"strconv"
	"time"

	"github.com/heeo-dev/askorus/agent/internal/core"
	"github.com/heeo-dev/askorus/agent/internal/proc"
)

const (
	usageTTL     = 5 * time.Minute
	usageTimeout = 10 * time.Second
)

// usageArgs is the only command the read runs.
func usageArgs() []string { return []string{"app-server"} }

// usageLines is everything the agent writes to the server: the handshake
// and the read, then stdin closes once the read is answered.
const usageLines = `{"id":1,"method":"initialize","params":{"clientInfo":{"name":"askorus","title":"Askorus","version":"1"}}}` + "\n" +
	`{"method":"initialized"}` + "\n" +
	`{"id":2,"method":"account/rateLimits/read"}` + "\n"

// usageAnswered reports whether line is the answer to the read (id 2),
// a result or an error.
func usageAnswered(line []byte) bool {
	var m struct {
		ID json.RawMessage `json:"id"`
	}
	return json.Unmarshal(line, &m) == nil && string(m.ID) == "2"
}

// Usage reads what is left now, without the kept value (the live test).
func (a *Adapter) Usage(ctx context.Context) []core.UsageGroup { return a.readUsage(ctx) }

// readUsage runs the server in an empty folder of its own.
func (a *Adapter) readUsage(ctx context.Context) []core.UsageGroup {
	if a.exeErr != nil {
		return nil
	}
	if st := a.cachedLogin(ctx); !st.signedIn {
		return nil
	}
	dir, err := os.MkdirTemp("", "askorus-codex-usage-")
	if err != nil {
		return nil
	}
	defer os.RemoveAll(dir)
	ctx, cancel := context.WithTimeout(ctx, usageTimeout)
	defer cancel()
	res, err := proc.RunUntil(ctx, proc.Cmd{Exe: a.exe, Dir: dir, Args: usageArgs(), Stdin: usageLines}, usageAnswered)
	if err != nil {
		return nil
	}
	return parseRateLimits(res.Stdout)
}

type rateWindow struct {
	UsedPercent *float64        `json:"usedPercent"`
	ResetsAt    json.RawMessage `json:"resetsAt"`
	Minutes     *float64        `json:"windowDurationMins"`
}

// parseRateLimits reads the answer to the read: the primary and secondary
// windows of rateLimits, as UsageGroups. A window is used up when the AI
// says it used all of it, or says ordinary use is not allowed and it is
// the fullest window.
func parseRateLimits(out []byte) []core.UsageGroup {
	var answer struct {
		ID     json.RawMessage `json:"id"`
		Result *struct {
			RateLimits *struct {
				Primary     *rateWindow `json:"primary"`
				Secondary   *rateWindow `json:"secondary"`
				ReachedType *string     `json:"rateLimitReachedType"`
			} `json:"rateLimits"`
			Allowed *bool `json:"ordinaryUsageAllowed"`
		} `json:"result"`
	}
	found := false
	for _, line := range bytes.Split(out, []byte("\n")) {
		answer.Result = nil
		if json.Unmarshal(bytes.TrimSpace(line), &answer) == nil && string(answer.ID) == "2" {
			found = true
			break
		}
	}
	if !found || answer.Result == nil || answer.Result.RateLimits == nil {
		return nil
	}
	rl := answer.Result.RateLimits
	var groups []core.UsageGroup
	fullest, fullestUsed := -1, -1.0
	for i, w := range []*rateWindow{rl.Primary, rl.Secondary} {
		if w == nil || w.UsedPercent == nil || math.IsNaN(*w.UsedPercent) {
			continue
		}
		window := windowOf(w.Minutes, i)
		if window == "" {
			continue
		}
		used := math.Min(100, math.Max(0, *w.UsedPercent))
		g := core.UsageGroup{Name: windowNames[window], Left: (100 - used) / 100, Window: window, ResetAt: resetTime(w.ResetsAt), Limited: used >= 100}
		if used > fullestUsed {
			fullest, fullestUsed = len(groups), used
		}
		groups = append(groups, g)
	}
	reached := rl.ReachedType != nil && *rl.ReachedType != ""
	notAllowed := answer.Result.Allowed != nil && !*answer.Result.Allowed
	if (reached || notAllowed) && fullest >= 0 {
		groups[fullest].Limited = true
	}
	return groups
}

// windowNames are the windows' names in English; the app names them by Window.
var windowNames = map[string]string{core.Window5h: "5 hours", core.WindowWeek: "Weekly", core.WindowMonth: "Monthly"}

// windowOf names a window by its length; without one, the primary window
// is the 5-hour one and the secondary the weekly one (as on 2026-11-18).
func windowOf(minutes *float64, i int) string {
	if minutes == nil {
		return []string{core.Window5h, core.WindowWeek}[i]
	}
	switch m := *minutes; {
	case m == 300:
		return core.Window5h
	case m == 7*24*60:
		return core.WindowWeek
	case m >= 28*24*60 && m <= 31*24*60:
		return core.WindowMonth
	}
	return ""
}

// resetTime reads resetsAt, seconds since 1970 (or a time some versions
// send as text), as RFC 3339 in UTC; "" when it is neither.
func resetTime(raw json.RawMessage) string {
	if len(raw) == 0 || string(raw) == "null" {
		return ""
	}
	var s string
	if json.Unmarshal(raw, &s) == nil {
		if t, err := time.Parse(time.RFC3339, s); err == nil {
			return t.UTC().Format(time.RFC3339)
		}
		if n, err := strconv.ParseFloat(s, 64); err == nil {
			return unixTime(n)
		}
		return ""
	}
	var n float64
	if json.Unmarshal(raw, &n) == nil {
		return unixTime(n)
	}
	return ""
}

func unixTime(sec float64) string {
	if sec <= 0 || sec > 1e11 { // not seconds since 1970
		return ""
	}
	return time.Unix(int64(sec), 0).UTC().Format(time.RFC3339)
}
