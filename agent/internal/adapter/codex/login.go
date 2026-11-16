package codex

// The sign-in state of Codex CLI (#60), as in the claude adapter: the agent
// asks `codex login status` and keeps only whether it is signed in. It never
// reads ~/.codex/auth.json. Signing in again (`codex login`) is the
// person's, in their own terminal.

import (
	"context"
	"path/filepath"
	"strings"
	"time"

	"github.com/heeo-dev/askorus/agent/internal/core"
	"github.com/heeo-dev/askorus/agent/internal/proc"
)

// NoticeSignedOut: codex is installed but not signed in.
const NoticeSignedOut = "codex_signed_out"

// LoginCommand signs Codex in again; it is the Detail of
// core.ErrCodeSignedOut.
const LoginCommand = "codex login"

const (
	loginTTL     = time.Minute
	loginTimeout = 10 * time.Second
)

// loginState: known is false when codex could not tell; then nothing is
// shown, as before.
type loginState struct {
	known    bool
	signedIn bool
}

// readLoginStatus reads `codex login status`: exit code 0 when signed in
// ("Logged in using ChatGPT"), "Not logged in" and a failing exit code when
// not. Anything else is unknown.
func readLoginStatus(res proc.Result) loginState {
	if res.ExitCode == 0 {
		return loginState{known: true, signedIn: true}
	}
	out := strings.ToLower(string(res.Stdout) + "\n" + string(res.Stderr))
	if strings.Contains(out, "not logged in") {
		return loginState{known: true}
	}
	return loginState{}
}

func (a *Adapter) runLoginCheck(ctx context.Context) loginState {
	if a.exeErr != nil {
		return loginState{}
	}
	ctx, cancel := context.WithTimeout(ctx, loginTimeout)
	defer cancel()
	res, err := proc.Run(ctx, proc.Cmd{Exe: a.exe, Dir: filepath.Dir(a.exe), Args: []string{"login", "status"}})
	if err != nil {
		return loginState{}
	}
	return readLoginStatus(res)
}

// cachedLogin returns the sign-in state, at most loginTTL old.
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

func (a *Adapter) loginNotices(ctx context.Context, info *core.AdapterInfo) {
	if st := a.cachedLogin(ctx); st.known && !st.signedIn {
		info.Notices = append(info.Notices, NoticeSignedOut)
	}
}

// signedOutMarks are pieces of what codex prints when it is not signed in
// or its sign-in cannot be refreshed (lower case).
var signedOutMarks = []string{
	"not logged in",
	"codex login",
	"401 unauthorized",
	"token_expired",
	"refresh token",
}

// signedOut tells whether an ask failed for the sign-in.
func signedOut(msg string) bool {
	m := strings.ToLower(msg)
	for _, s := range signedOutMarks {
		if strings.Contains(m, s) {
			return true
		}
	}
	return false
}
