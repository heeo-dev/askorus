package claude

// The sign-in state of Claude Code (#60). An installed but signed-out
// claude answers --version, so the phone used to show it as ready and only
// an ask found out, with the CLI's English message. The agent now asks
// claude itself with `claude auth status --json` and keeps two of its
// fields: whether it is signed in and when that sign-in ends. Everything
// else in that answer (the email, the organization, the plan) is dropped
// here and never logged or sent to the phone, and the credentials file is
// never read. Signing in again is the person's, in their own terminal.

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"time"

	"github.com/heeo-dev/askorus/agent/internal/core"
)

// Notice codes for the sign-in (the app turns them into text).
const (
	NoticeSignedOut     = "claude_signed_out"
	NoticeLoginExpiring = "claude_login_expiring" // with AdapterInfo.LoginExpiresAt
)

// LoginCommand signs Claude Code in again; it is the Detail of
// core.ErrCodeSignedOut.
const LoginCommand = "claude auth login"

const (
	// loginTTL keeps a state this long: someone who just signed in sees it
	// within a minute, and the phone's info calls do not run claude each time.
	loginTTL = time.Minute
	// loginWarnBefore: a sign-in that ends sooner than this is shown as
	// ending soon. claude's own text gives a window of about three days.
	loginWarnBefore = 72 * time.Hour
	loginTimeout    = 10 * time.Second
)

// loginState is what the agent knows of the sign-in. known is false when
// claude could not tell (an older claude without auth status, a failure):
// then nothing is shown, as before.
type loginState struct {
	known    bool
	signedIn bool
	expires  time.Time // zero when claude does not say
}

// parseAuthStatus reads `claude auth status --json`, keeping only
// loggedIn and loginExpiresAt.
func parseAuthStatus(out []byte) loginState {
	var s struct {
		LoggedIn       *bool  `json:"loggedIn"`
		LoginExpiresAt string `json:"loginExpiresAt"`
	}
	if json.Unmarshal(bytes.TrimSpace(out), &s) != nil || s.LoggedIn == nil {
		return loginState{}
	}
	st := loginState{known: true, signedIn: *s.LoggedIn}
	if t, err := time.Parse(time.RFC3339Nano, s.LoginExpiresAt); err == nil {
		st.expires = t.UTC()
	}
	return st
}

// runLoginCheck asks claude. A signed-out claude may end with a non-zero
// exit code and still print the JSON, so the output is read either way.
func (a *Adapter) runLoginCheck(ctx context.Context) loginState {
	if a.runner == nil {
		return loginState{}
	}
	res, _ := a.runner.RunFor(ctx, loginTimeout, "", "auth", "status", "--json")
	return parseAuthStatus(res.Stdout)
}

// cachedLogin returns the sign-in state, at most loginTTL old. The phone's
// request may go away; its cancel must not be kept as a failed check.
func (a *Adapter) cachedLogin(ctx context.Context) loginState {
	a.loginMu.Lock()
	defer a.loginMu.Unlock()
	if !a.loginAt.IsZero() && time.Since(a.loginAt) < loginTTL {
		return a.login
	}
	check := a.runLoginCheck
	if a.loginCheck != nil {
		check = a.loginCheck
	}
	a.login = check(context.WithoutCancel(ctx))
	a.loginAt = time.Now()
	return a.login
}

// forgetLogin drops the kept state, after an ask failed for the sign-in.
func (a *Adapter) forgetLogin() {
	a.loginMu.Lock()
	a.loginAt = time.Time{}
	a.loginMu.Unlock()
}

// loginNotices adds the sign-in notices to info. A sign-in whose end has
// passed counts as signed out.
func (a *Adapter) loginNotices(ctx context.Context, info *core.AdapterInfo) {
	st := a.cachedLogin(ctx)
	if !st.known {
		return
	}
	left := time.Until(st.expires)
	switch {
	case !st.signedIn, !st.expires.IsZero() && left <= 0:
		info.Notices = append(info.Notices, NoticeSignedOut)
	case !st.expires.IsZero() && left < loginWarnBefore:
		info.Notices = append(info.Notices, NoticeLoginExpiring)
		info.LoginExpiresAt = st.expires.Format(time.RFC3339)
	}
}

// signedOutMarks are pieces of what claude prints when it is not signed in
// or its sign-in ended (lower case). The 2026-11-16 one read: "Failed to
// authenticate: this login reached its time limit and cannot be renewed.
// Run `claude auth login` to sign in again, ...".
var signedOutMarks = []string{
	"failed to authenticate",
	"claude auth login",
	"not logged in",
	"please run /login",
	"oauth token has expired",
	"invalid api key",
}

// signedOut tells whether an ask failed for the sign-in.
func signedOut(msg string, status *int) bool {
	if status != nil && *status == 401 {
		return true
	}
	m := strings.ToLower(msg)
	for _, s := range signedOutMarks {
		if strings.Contains(m, s) {
			return true
		}
	}
	return false
}
