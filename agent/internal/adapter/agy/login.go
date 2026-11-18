package agy

// The sign-in state and the usage of Antigravity CLI (#20, as #60 for the
// other AIs). agy has no sign-in status command; its /usage command, run
// with -p, answers without starting a turn, keeping a conversation or
// spending quota (2026-11-18): the weekly share left of each model group
// and when it refreshes. It fails when agy is not signed in. The agent
// never reads agy's sign-in, which Windows keeps in the credential manager.
// Signing in again is the person's, in agy on the PC.

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"time"

	"github.com/heeo-dev/askorus/agent/internal/core"
	"github.com/heeo-dev/askorus/agent/internal/proc"
)

// NoticeSignedOut: agy is installed but not signed in.
const NoticeSignedOut = "agy_signed_out"

// LoginCommand is what signs agy in again: agy itself, opened in a
// terminal on the PC. It is the Detail of core.ErrCodeSignedOut.
const LoginCommand = "agy"

const (
	// usageTTL: /usage asks Google and takes a few seconds, so the state
	// is kept longer than the other AIs' sign-in checks.
	usageTTL     = 5 * time.Minute
	usageTimeout = 20 * time.Second
)

// UsageGroup is one group of models that share a weekly limit, as /usage
// names it ("Gemini Models").
type UsageGroup struct {
	Name string `json:"name"`
	// Left is the share of the limit left, 0 to 1.
	Left float64 `json:"left"`
	// ResetAt is when it refreshes (RFC 3339, UTC), "" when agy does not say.
	ResetAt string `json:"resetAt,omitempty"`
}

// loginState: known is false when agy could not tell; then nothing is
// shown.
type loginState struct {
	known    bool
	signedIn bool
	groups   []UsageGroup
}

// usageArgs runs /usage. --settings are the ask's locks with slash
// commands on: a person who turned slash commands off would otherwise
// send "/usage" to the model, in their own mode and with every tool.
func usageArgs() []string {
	return []string{"-p", "/usage", "--output-format", "json", "--settings", lockSettings(true, false)}
}

// readUsage reads /usage's JSON. A run that started a turn (a
// conversation id) or answered another command is unknown.
func readUsage(res proc.Result) loginState {
	var v struct {
		ConversationID string `json:"conversation_id"`
		Status         string `json:"status"`
		Error          string `json:"error"`
		Command        *struct {
			Name string `json:"name"`
			Data struct {
				Groups []struct {
					Name    string `json:"name"`
					Buckets []struct {
						Window    string  `json:"window"`
						Remaining float64 `json:"remaining_fraction"`
						ResetTime string  `json:"reset_time"`
					} `json:"buckets"`
				} `json:"groups"`
			} `json:"data"`
		} `json:"command"`
	}
	ae, _, _, rest := stderrInfo(res.Stderr)
	if json.Unmarshal(res.Stdout, &v) != nil {
		if signedOut(ae.Status, strings.ToLower(ae.ShortError+" "+rest)) {
			return loginState{known: true}
		}
		return loginState{}
	}
	switch {
	case v.ConversationID != "":
		return loginState{}
	case v.Status == "SUCCESS" && v.Command != nil && v.Command.Name == "usage":
		st := loginState{known: true, signedIn: true}
		for _, g := range v.Command.Data.Groups {
			for _, b := range g.Buckets {
				if b.Window != "weekly" {
					continue
				}
				left := min(max(b.Remaining, 0), 1)
				reset := ""
				if t, err := time.Parse(time.RFC3339, b.ResetTime); err == nil {
					reset = t.UTC().Format(time.RFC3339)
				}
				st.groups = append(st.groups, UsageGroup{Name: clip(g.Name, 60), Left: left, ResetAt: reset})
			}
		}
		return st
	case signedOut(ae.Status, strings.ToLower(v.Error+" "+ae.ShortError)):
		return loginState{known: true}
	}
	return loginState{}
}

func (a *Adapter) runUsageCheck(ctx context.Context) loginState {
	if a.exeErr != nil {
		return loginState{}
	}
	ctx, cancel := context.WithTimeout(ctx, usageTimeout)
	defer cancel()
	res, err := proc.Run(ctx, proc.Cmd{Exe: a.exe, Dir: filepath.Dir(a.exe), Args: usageArgs()})
	if err != nil {
		return loginState{}
	}
	return readUsage(res)
}

// cachedLogin returns the sign-in state, at most usageTTL old.
func (a *Adapter) cachedLogin(ctx context.Context) loginState {
	a.loginMu.Lock()
	defer a.loginMu.Unlock()
	if !a.loginAt.IsZero() && time.Since(a.loginAt) < usageTTL {
		return a.login
	}
	check := a.runUsageCheck
	if a.loginCheck != nil {
		check = a.loginCheck
	}
	a.login = check(context.WithoutCancel(ctx))
	a.loginAt = time.Now()
	return a.login
}

// forgetLogin drops the kept state, after an ask failed for the sign-in
// or the usage limit.
func (a *Adapter) forgetLogin() {
	a.loginMu.Lock()
	a.loginAt = time.Time{}
	a.loginMu.Unlock()
}

func (a *Adapter) loginNotices(ctx context.Context, info *core.AdapterInfo) {
	if st := a.cachedLogin(ctx); st.known && !st.signedIn {
		info.Notices = append(info.Notices, NoticeSignedOut)
	}
}

// Usage returns the weekly share left of each model group, nil when agy
// could not tell (not signed in, or /usage failed).
func (a *Adapter) Usage(ctx context.Context) []UsageGroup {
	st := a.cachedLogin(ctx)
	if !st.signedIn {
		return nil
	}
	return append([]UsageGroup(nil), st.groups...)
}

// resetAt is when the Gemini group's limit refreshes, "" when unknown:
// the Detail of core.ErrCodeUsageLimit.
func (a *Adapter) resetAt(ctx context.Context) string {
	for _, g := range a.Usage(ctx) {
		if strings.HasPrefix(strings.ToLower(g.Name), "gemini") {
			return g.ResetAt
		}
	}
	return ""
}

// signedOut tells from an AGY_ERROR status and agy's message (lower case)
// whether a run failed for the sign-in. agy's words for it were not seen
// yet (signing out is the person's); its error status is gRPC's.
func signedOut(status, msg string) bool {
	if status == "UNAUTHENTICATED" {
		return true
	}
	for _, s := range []string{"unauthenticated", "not signed in", "not logged in", "sign in again", "sign-in update needed", "authentication required", "/login"} {
		if strings.Contains(msg, s) {
			return true
		}
	}
	return false
}

// usageLimit tells whether a run failed for the plan's usage limit: the
// weekly limit, a spend cap or a prepaid balance, which agy stops at at
// once (its changelog). Out of capacity (503, UNAVAILABLE) is not a limit.
func usageLimit(status, msg string) bool {
	if strings.Contains(msg, "no capacity available") {
		return false
	}
	if status == "RESOURCE_EXHAUSTED" || strings.Contains(msg, "resource_exhausted") {
		return true
	}
	for _, s := range []string{"weekly limit", "quota", "spend cap", "prepaid", "credits"} {
		if strings.Contains(msg, s) {
			return true
		}
	}
	return false
}
