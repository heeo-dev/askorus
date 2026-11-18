package opencode

import (
	"context"
	"errors"
	"maps"
	"slices"
	"sync/atomic"
	"testing"
	"time"

	"github.com/heeo-dev/askorus/agent/internal/core"
	"github.com/heeo-dev/askorus/agent/internal/proc"
)

// #60 (11-18): `opencode models` lists the models OpenCode can use now;
// the agent keeps only their providers.
func TestParseModels(t *testing.T) {
	cases := []struct {
		name string
		out  string
		want []string
	}{
		{"signed in", "opencode-go/deepseek-v4-pro\r\nopencode-go/glm-5.3\r\nopencode/big-pickle\r\n", []string{"opencode", "opencode-go"}},
		{"never signed in", "opencode/big-pickle\nopencode/deepseek-v4.1-flash-free\n", []string{"opencode"}},
		{"own and local providers", "openrouter/anthropic/claude-x\nlocaltest/m1\n", []string{"localtest", "openrouter"}},
		{"not a list", "Error: database is locked\n", nil},
		{"nothing", "", nil},
	}
	for _, c := range cases {
		st := parseModels([]byte(c.out))
		got := slices.Sorted(maps.Keys(st.providers))
		if st.known != (len(c.want) > 0) || !slices.Equal(got, c.want) {
			t.Errorf("%s: known %v providers %v, want %v", c.name, st.known, got, c.want)
		}
	}
}

func TestProviderOf(t *testing.T) {
	for model, want := range map[string]string{
		"opencode-go/deepseek-v4.1-flash#low": "opencode-go",
		"openrouter/anthropic/claude-x":       "openrouter",
		"deepseek-v4-pro":                     "",
		"/m":                                  "",
		"p/":                                  "",
		"a b/m":                               "",
	} {
		if got := providerOf(model); got != want {
			t.Errorf("providerOf(%q) = %q, want %q", model, got, want)
		}
	}
}

func signedIn(providers ...string) loginState {
	st := loginState{known: true, providers: map[string]bool{}}
	for _, p := range providers {
		st.providers[p] = true
	}
	return st
}

// The notice is about the tier models: OpenCode's free and local models
// need no sign-in, so a person whose tiers use them sees none.
func TestLoginNotices(t *testing.T) {
	free := map[string]core.AskModel{
		core.TierQuick: {Model: "opencode/big-pickle"}, core.TierStandard: {Model: "opencode/big-pickle"}, core.TierDeep: {Model: "localtest/m1"},
	}
	for _, c := range []struct {
		name    string
		tiers   map[string]core.AskModel
		st      loginState
		notices []string
	}{
		{"unknown", nil, loginState{}, nil},
		{"signed in", nil, signedIn("opencode-go", "opencode"), nil},
		{"never signed in", nil, signedIn("opencode"), []string{NoticeSignedOut}},
		{"free and local tiers, never signed in", free, signedIn("opencode", "localtest"), nil},
		{"one tier's provider missing", map[string]core.AskModel{core.TierDeep: {Model: "anthropic/claude-x"}}, signedIn("opencode-go", "opencode"), []string{NoticeSignedOut}},
	} {
		a, _, _ := fakeAdapter(t, c.tiers)
		st := c.st
		a.loginCheck = func(context.Context) loginState { return st }
		if info := a.Info(context.Background()); !info.Available || !slices.Equal(info.Notices, c.notices) {
			t.Errorf("%s: %+v, want notices %v", c.name, info, c.notices)
		}
	}
}

// The check runs the fake's `opencode models --standalone`; one that
// fails shows nothing, as before.
func TestLoginCheckRunsModels(t *testing.T) {
	a, argsFile, _ := fakeAdapter(t, nil)
	if info := a.Info(context.Background()); len(info.Notices) != 0 {
		t.Errorf("signed in: %+v", info)
	}
	a.loginMu.Lock()
	st := a.login
	a.loginMu.Unlock()
	if !st.known || !st.providers["opencode-go"] {
		t.Errorf("state %+v", st)
	}
	t.Setenv("ASKORUS_FAKE_MODE", "signedout")
	a.forgetLogin()
	if info := a.Info(context.Background()); !slices.Equal(info.Notices, []string{NoticeSignedOut}) {
		t.Errorf("signed out: %+v", info)
	}
	t.Setenv("ASKORUS_FAKE_MODE", "modelsfail")
	a.forgetLogin()
	if info := a.Info(context.Background()); len(info.Notices) != 0 {
		t.Errorf("models failed: %+v", info)
	}
	if cs := calls(t, argsFile); len(cs) != 0 {
		t.Errorf("the check ran more than models: %+v", cs)
	}
}

// A check takes about 3 seconds: only the first one is waited for. After
// loginTTL, Info answers with what the last one found while a new one
// runs; a phone request that goes away does not wait at all.
func TestLoginCheckInBackground(t *testing.T) {
	a, _, _ := fakeAdapter(t, nil)
	var runs atomic.Int32
	release := make(chan struct{})
	a.loginCheck = func(context.Context) loginState {
		if runs.Add(1) > 1 {
			<-release
			return signedIn("opencode") // signed out meanwhile
		}
		return signedIn("opencode-go", "opencode")
	}
	if info := a.Info(context.Background()); len(info.Notices) != 0 || runs.Load() != 1 {
		t.Fatalf("first: %+v after %d checks", info, runs.Load())
	}
	a.loginMu.Lock()
	a.loginAt = time.Now().Add(-2 * loginTTL)
	a.loginMu.Unlock()
	running := func() chan struct{} {
		a.loginMu.Lock()
		defer a.loginMu.Unlock()
		return a.loginDone
	}
	start := time.Now()
	if info := a.Info(context.Background()); len(info.Notices) != 0 || time.Since(start) > 2*time.Second {
		t.Errorf("stale: %+v after %v, want the old state at once", info, time.Since(start))
	}
	first := running()
	if info := a.Info(context.Background()); len(info.Notices) != 0 || first == nil || running() != first {
		t.Errorf("one check at a time: %+v", info)
	}
	close(release)
	select {
	case <-first:
	case <-time.After(5 * time.Second):
		t.Fatal("the new check did not end")
	}
	if info := a.Info(context.Background()); !slices.Equal(info.Notices, []string{NoticeSignedOut}) || runs.Load() != 2 {
		t.Errorf("after the new check: %+v, %d checks", info, runs.Load())
	}

	b, _, _ := fakeAdapter(t, nil)
	hold := make(chan struct{})
	defer close(hold)
	b.loginCheck = func(context.Context) loginState { <-hold; return signedIn("opencode") }
	gone, cancel := context.WithCancel(context.Background())
	cancel()
	if info := b.Info(gone); !info.Available || len(info.Notices) != 0 {
		t.Errorf("gone: %+v", info)
	}
}

// 11-18: never signed in, every free model said this at once, with the
// same time to wait (up to midnight UTC).
func TestAskFreeLimit(t *testing.T) {
	line := `{"type":"error","timestamp":1,"sessionID":"ses_e773ef84effedagCyC2bkvl1W9","error":{"type":"provider.free-limit","message":"Free usage limit reached. Connect a provider or subscribe to OpenCode Go to keep going, or switch models.","status":429,"retryAfterMs":24959000}}`
	before := time.Now()
	_, err := parseOutput(proc.Result{ExitCode: 1, Stdout: []byte(line + "\r\n")}, "opencode/big-pickle")
	var ae *core.ActionError
	if !errors.As(err, &ae) || ae.Code != core.ErrCodeFreeLimit {
		t.Fatalf("%v, want %s", err, core.ErrCodeFreeLimit)
	}
	at, perr := time.Parse(time.RFC3339, ae.Detail)
	want := before.Add(24959 * time.Second)
	if perr != nil || at.Location() != time.UTC || at.Second() != 0 || at.Sub(want).Abs() > 2*time.Minute {
		t.Errorf("detail %q (%v), want about %v", ae.Detail, perr, want.UTC())
	}
	if err := freeLimit(&eventError{Type: "provider.free-limit"}, before); core.ErrorCode(err) != core.ErrCodeFreeLimit || err.(*core.ActionError).Detail != "" {
		t.Errorf("no time to wait: %v", err)
	}
}

// An ask fails for the sign-in in two ways (11-18): the provider refuses
// the key (401), or OpenCode does not know the model because its provider
// is not signed in. A model that does not exist says the same as the
// second, so a new check tells them apart.
func TestAskSignedOut(t *testing.T) {
	cases := []struct {
		mode, model string
		code        string
	}{
		{"signedout", "", core.ErrCodeSignedOut},
		{"badkey", "", core.ErrCodeSignedOut},
		{"badmodel", "opencode-go/nope", core.ErrCodeBadModel},
		{"signedout", "opencode/nope", core.ErrCodeBadModel}, // its provider is signed in (the free models)
	}
	for _, c := range cases {
		a, _, folder := fakeAdapter(t, nil)
		t.Setenv("ASKORUS_FAKE_MODE", c.mode)
		_, err := a.Ask(context.Background(), core.AskRequest{Folder: folder, Prompt: "q", Tier: core.TierStandard, Model: c.model})
		var ae *core.ActionError
		if !errors.As(err, &ae) || ae.Code != c.code {
			t.Errorf("%s %q: %v, want %s", c.mode, c.model, err, c.code)
			continue
		}
		if c.code == core.ErrCodeSignedOut && ae.Detail != LoginCommand {
			t.Errorf("%s: detail %q", c.mode, ae.Detail)
		}
		// The check of a signed-out ask is kept: the next info shows it at once.
		if c.mode == "signedout" && c.model == "" {
			if info := a.Info(context.Background()); !slices.Equal(info.Notices, []string{NoticeSignedOut}) {
				t.Errorf("info after the ask: %+v", info)
			}
		}
	}
}
