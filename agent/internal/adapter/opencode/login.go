package opencode

// The sign-in of OpenCode (#60). OpenCode signs in per provider (OpenCode
// Go, OpenCode Zen, a person's own API key, ...), and some models need no
// sign-in at all (OpenCode's free models, a local model set up in its
// config). So the agent does not ask whether OpenCode is signed in but
// whether the providers of its tier models can be used now: `opencode
// models` lists the models OpenCode can use, without any secret, and the
// agent keeps only the provider part of each line. It never reads
// OpenCode's login files and never runs `opencode auth export`, which
// prints the secrets. Signing in is the person's, in their own terminal.
//
// 2026-11-18, OpenCode 2.2.7 with empty data folders (never signed in):
// models listed only the free models of the provider "opencode"; an ask
// with the default model said provider.no-route "Model unavailable", the
// same as a model that does not exist, which the app showed as an unknown
// model. With a key OpenCode refused: provider.auth, status 401, "Invalid
// credential".
//
// opencode models takes about 3 seconds, so after the first check it runs
// in the background: Info shows what the last check found, and one older
// than loginTTL starts a new one. Only a first check (or one after an ask
// failed for the sign-in) is waited for.

import (
	"context"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/heeo-dev/askorus/agent/internal/core"
	"github.com/heeo-dev/askorus/agent/internal/proc"
)

// NoticeSignedOut: the provider of a tier model is not signed in, so
// OpenCode cannot answer with it.
const NoticeSignedOut = "opencode_signed_out"

// LoginCommand signs OpenCode in to a provider; it is the Detail of
// core.ErrCodeSignedOut.
const LoginCommand = "opencode auth login"

const (
	loginTTL     = time.Minute
	loginTimeout = 20 * time.Second
)

// modelsArgs is the only command the check runs.
var modelsArgs = []string{"models", "--standalone"}

// loginState: known is false when opencode could not tell (it failed or
// listed nothing); then nothing is shown, as before. providers are the
// providers it can use now.
type loginState struct {
	known     bool
	providers map[string]bool
}

var providerRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*$`)

// providerOf is the provider part of a model, "" when it has none.
func providerOf(model string) string {
	p, rest, ok := strings.Cut(model, "/")
	if !ok || rest == "" || !providerRe.MatchString(p) {
		return ""
	}
	return p
}

// parseModels reads `opencode models`: one "provider/model" a line.
func parseModels(out []byte) loginState {
	st := loginState{providers: map[string]bool{}}
	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimSpace(line) // lines end with \r\n on Windows
		if strings.ContainsAny(line, " \t") {
			continue
		}
		if p := providerOf(line); p != "" {
			st.providers[p] = true
		}
	}
	st.known = len(st.providers) > 0
	return st
}

func (a *Adapter) runLoginCheck(ctx context.Context) loginState {
	if a.exeErr != nil {
		return loginState{}
	}
	ctx, cancel := context.WithTimeout(ctx, loginTimeout)
	defer cancel()
	dir := filepath.Dir(a.exe)
	res, err := proc.Run(ctx, proc.Cmd{Exe: a.exe, Dir: dir, Args: modelsArgs, Env: map[string]string{"PWD": dir}})
	if err != nil || res.ExitCode != 0 {
		return loginState{}
	}
	return parseModels(res.Stdout)
}

// startLogin starts a check unless one is running, and returns the channel
// that is closed when it ends. The caller holds loginMu.
func (a *Adapter) startLogin() chan struct{} {
	if a.loginDone != nil {
		return a.loginDone
	}
	done := make(chan struct{})
	a.loginDone = done
	check := a.runLoginCheck
	if a.loginCheck != nil {
		check = a.loginCheck
	}
	go func() {
		st := check(context.Background())
		a.loginMu.Lock()
		a.login, a.loginAt, a.loginDone = st, time.Now(), nil
		a.loginMu.Unlock()
		close(done)
	}()
	return done
}

// cachedLogin returns the sign-in state. One older than loginTTL is
// returned as it is while a new check runs; when there is none yet, it
// waits for the check, or until ctx ends (a phone request that went away),
// and then the check goes on for the next call.
func (a *Adapter) cachedLogin(ctx context.Context) loginState {
	a.loginMu.Lock()
	if !a.loginAt.IsZero() && time.Since(a.loginAt) < loginTTL {
		defer a.loginMu.Unlock()
		return a.login
	}
	done, first := a.startLogin(), a.loginAt.IsZero()
	st := a.login
	a.loginMu.Unlock()
	if !first {
		return st
	}
	select {
	case <-done:
	case <-ctx.Done():
		return loginState{}
	}
	a.loginMu.Lock()
	defer a.loginMu.Unlock()
	return a.login
}

// forgetLogin drops the kept state, after an ask failed for the sign-in:
// the next call waits for a new check.
func (a *Adapter) forgetLogin() {
	a.loginMu.Lock()
	a.loginAt = time.Time{}
	a.loginMu.Unlock()
}

// loginNotices adds NoticeSignedOut when the provider of a tier model is
// not among the ones opencode can use.
func (a *Adapter) loginNotices(ctx context.Context, info *core.AdapterInfo) {
	st := a.cachedLogin(ctx)
	if !st.known {
		return
	}
	for _, m := range a.AskTiers() {
		if p := providerOf(m.Model); p != "" && !st.providers[p] {
			info.Notices = append(info.Notices, NoticeSignedOut)
			return
		}
	}
}

// signInError turns an ask's error into core.ErrCodeSignedOut when it came
// from the sign-in: OpenCode refused the provider's key (401), or it does
// not know the model because its provider is not signed in, which a new
// check tells apart from a model that does not exist.
func (a *Adapter) signInError(ctx context.Context, err error, model string) error {
	switch core.ErrorCode(err) {
	case core.ErrCodeSignedOut:
		a.forgetLogin() // the next info checks again
	case core.ErrCodeBadModel:
		a.forgetLogin()
		st := a.cachedLogin(ctx) // a new check, kept for the next info
		if p := providerOf(model); st.known && p != "" && !st.providers[p] {
			return core.Fail(core.ErrCodeSignedOut, LoginCommand)
		}
	}
	return err
}
