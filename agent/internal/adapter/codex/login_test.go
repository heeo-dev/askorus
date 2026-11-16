package codex

import (
	"context"
	"errors"
	"slices"
	"testing"

	"github.com/heeo-dev/askorus/agent/internal/core"
	"github.com/heeo-dev/askorus/agent/internal/proc"
)

// #60: `codex login status` says whether codex is signed in.
func TestReadLoginStatus(t *testing.T) {
	cases := []struct {
		name string
		res  proc.Result
		want loginState
	}{
		{"signed in", proc.Result{ExitCode: 0, Stdout: []byte("Logged in using ChatGPT\n")}, loginState{known: true, signedIn: true}},
		{"signed out", proc.Result{ExitCode: 1, Stderr: []byte("Not logged in\n")}, loginState{known: true}},
		{"other failure", proc.Result{ExitCode: 2, Stderr: []byte("error: unrecognized subcommand 'login'")}, loginState{}},
	}
	for _, c := range cases {
		if got := readLoginStatus(c.res); got != c.want {
			t.Errorf("%s: %+v, want %+v", c.name, got, c.want)
		}
	}
}

func TestLoginNotices(t *testing.T) {
	a, _, _ := fakeAdapter(t, nil)
	for _, c := range []struct {
		st      loginState
		notices []string
	}{
		{loginState{}, nil},
		{loginState{known: true, signedIn: true}, nil},
		{loginState{known: true}, []string{NoticeSignedOut}},
	} {
		st := c.st
		a.loginCheck = func(context.Context) loginState { return st }
		a.forgetLogin()
		if info := a.Info(context.Background()); !info.Available || !slices.Equal(info.Notices, c.notices) {
			t.Errorf("%+v: %+v, want notices %v", c.st, info, c.notices)
		}
	}
}

func TestAskSignedOut(t *testing.T) {
	cases := []struct {
		name string
		res  proc.Result
		code string
	}{
		{"not logged in", proc.Result{ExitCode: 1, Stderr: []byte("Error: Not logged in. Run `codex login` first.")}, core.ErrCodeSignedOut},
		{"401", proc.Result{ExitCode: 1, Stderr: []byte("unexpected status 401 Unauthorized: token expired")}, core.ErrCodeSignedOut},
		{"other", proc.Result{ExitCode: 1, Stderr: []byte("stream disconnected before completion")}, core.ErrCodeAskFailed},
	}
	for _, c := range cases {
		_, err := parseOutput(c.res, "gpt-6.1-terra", "")
		var ae *core.ActionError
		if !errors.As(err, &ae) || ae.Code != c.code {
			t.Errorf("%s: %v, want %s", c.name, err, c.code)
			continue
		}
		if c.code == core.ErrCodeSignedOut && ae.Detail != LoginCommand {
			t.Errorf("%s: detail %q", c.name, ae.Detail)
		}
	}
}
