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

// firstWait is how long Info waits for the first check after the agent
// started, while nothing is known yet. Later checks never hold Info up: a
// check took 17 to 25 seconds on a real PC (2026-11-25), and every
// /v1/info and question waited for it once its state was older than
// usageTTL, so the phone's first request after a pause ran past its time
// limit ("PC에 닿지 않습니다"). A variable so a test need not wait.
var firstWait = 2 * time.Second

// UsageGroup is core's.
type UsageGroup = core.UsageGroup

// loginState: known is false when agy could not tell; then nothing is
// shown.
type loginState struct {
	known    bool
	signedIn bool
	groups   []UsageGroup
}

// usageArgs runs /usage. --settings are the ask's locks with slash
// commands on (R-124: with them off, as the ask's settings have them,
// "/usage" went to the model as a question every few minutes, in a new
// conversation and on the quota). A person who turned slash commands off
// in agy is covered too; and were it a turn after all, it is locked and
// readUsage throws it away (it has a conversation id).
func usageArgs() []string {
	return []string{"-p", "/usage", "--output-format", "json", "--settings", lockSettings(true, true)}
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
			// Askorus runs Gemini models only (decision 4): the other
			// groups (agy's Claude and GPT models) would only confuse.
			if !strings.HasPrefix(strings.ToLower(strings.TrimSpace(g.Name)), "gemini") {
				continue
			}
			for _, b := range g.Buckets {
				if b.Window != "weekly" {
					continue
				}
				left := min(max(b.Remaining, 0), 1)
				reset := ""
				if t, err := time.Parse(time.RFC3339, b.ResetTime); err == nil {
					reset = t.UTC().Format(time.RFC3339)
				}
				st.groups = append(st.groups, UsageGroup{Name: clip(g.Name, 60), Left: left, ResetAt: reset, Window: core.WindowWeek, Limited: left <= 0})
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

// startLogin starts a check in the background unless one runs, and
// returns the channel that is closed when it ends (at once when the agent
// is stopping and no check can start). The caller holds loginMu. The
// check runs under the usage reads' context, so stopping the agent ends it.
func (a *Adapter) startLogin() chan struct{} {
	if a.loginDone != nil {
		return a.loginDone
	}
	done := make(chan struct{})
	check := a.runUsageCheck
	if a.loginCheck != nil {
		check = a.loginCheck
	}
	gen := a.loginGen
	if !core.GoRead(func(ctx context.Context) {
		st := check(ctx)
		a.loginMu.Lock()
		a.login, a.checked, a.loginDone = st, true, nil
		// A check that began before forgetLogin may say what the failed ask
		// just disproved: kept, but not as fresh, so the next call checks again.
		if gen == a.loginGen {
			a.loginAt = time.Now()
		}
		a.loginMu.Unlock()
		close(done)
	}) {
		close(done)
		return done
	}
	a.loginDone = done
	return done
}

// fresh reports whether the kept state is younger than usageTTL. The
// caller holds loginMu.
func (a *Adapter) fresh() bool {
	return !a.loginAt.IsZero() && time.Since(a.loginAt) < usageTTL
}

// keptLogin returns the kept sign-in state at once and starts a new check
// when it is older than usageTTL; the next call has it. Only while nothing
// was ever checked does it wait for the check, at most firstWait.
func (a *Adapter) keptLogin(ctx context.Context) loginState {
	a.loginMu.Lock()
	if a.fresh() {
		defer a.loginMu.Unlock()
		return a.login
	}
	done, first := a.startLogin(), !a.checked
	st := a.login
	a.loginMu.Unlock()
	if !first {
		return st
	}
	t := time.NewTimer(firstWait)
	defer t.Stop()
	select {
	case <-done:
	case <-t.C:
	case <-ctx.Done():
	}
	a.loginMu.Lock()
	defer a.loginMu.Unlock()
	return a.login
}

// cachedLogin returns a sign-in state at most usageTTL old, waiting for a
// check when needed, or until ctx ends (then what is kept). For an ask
// that failed for the usage limit, which wants the time it ends.
func (a *Adapter) cachedLogin(ctx context.Context) loginState {
	// Twice at most: a check that was running when forgetLogin came ends
	// without counting as fresh, and the one after it is the answer.
	for range 2 {
		a.loginMu.Lock()
		if a.fresh() {
			defer a.loginMu.Unlock()
			return a.login
		}
		done := a.startLogin()
		a.loginMu.Unlock()
		select {
		case <-done:
		case <-ctx.Done():
			a.loginMu.Lock()
			defer a.loginMu.Unlock()
			return a.login
		}
	}
	a.loginMu.Lock()
	defer a.loginMu.Unlock()
	return a.login
}

// forgetLogin drops the kept state's freshness, after an ask failed for
// the sign-in or the usage limit, and starts a new check at once, so the
// next /v1/info is likely to have it. The state itself stays until then.
func (a *Adapter) forgetLogin() {
	a.loginMu.Lock()
	a.loginAt = time.Time{}
	a.loginGen++
	a.startLogin()
	a.loginMu.Unlock()
}

// loginNotices adds the sign-in notice, or the usage when agy is signed in.
func (a *Adapter) loginNotices(ctx context.Context, info *core.AdapterInfo) {
	st := a.keptLogin(ctx)
	switch {
	case st.known && !st.signedIn:
		info.Notices = append(info.Notices, NoticeSignedOut)
	case st.signedIn && len(st.groups) > 0:
		info.Usage = append([]UsageGroup(nil), st.groups...)
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
