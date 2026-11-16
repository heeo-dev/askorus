package claude

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/heeo-dev/askorus/agent/internal/core"
	"github.com/heeo-dev/askorus/agent/internal/proc"
)

// #60: only loggedIn and loginExpiresAt are read; the rest of claude's
// answer (email, organization) is never kept.
func TestParseAuthStatus(t *testing.T) {
	end := time.Date(2026, 11, 27, 3, 2, 52, 90e6, time.UTC)
	cases := []struct {
		name string
		out  string
		want loginState
	}{
		{"signed in", `{"loggedIn":true,"authMethod":"claude.ai","email":"someone@example.com","orgName":"X","loginExpiresAt":"2026-11-27T03:02:52.090Z"}`, loginState{known: true, signedIn: true, expires: end}},
		{"signed out", `{"loggedIn":false}`, loginState{known: true}},
		{"no expiry", "  {\"loggedIn\":true}\n", loginState{known: true, signedIn: true}},
		{"bad expiry", `{"loggedIn":true,"loginExpiresAt":"soon"}`, loginState{known: true, signedIn: true}},
		{"no loggedIn", `{"email":"someone@example.com"}`, loginState{}},
		{"not json", "error: unknown command 'auth'", loginState{}},
		{"empty", "", loginState{}},
	}
	for _, c := range cases {
		if got := parseAuthStatus([]byte(c.out)); got != c.want {
			t.Errorf("%s: %+v, want %+v", c.name, got, c.want)
		}
	}
}

func TestLoginNotices(t *testing.T) {
	a := testAdapter(t)
	t.Setenv("CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC", "")
	t.Setenv("DISABLE_GROWTHBOOK", "")
	soon := time.Now().Add(24 * time.Hour).UTC().Truncate(time.Second)
	cases := []struct {
		name    string
		st      loginState
		notices []string
		expires string
	}{
		{"unknown", loginState{}, nil, ""},
		{"signed in, far", loginState{known: true, signedIn: true, expires: time.Now().Add(30 * 24 * time.Hour)}, nil, ""},
		{"signed in, no end", loginState{known: true, signedIn: true}, nil, ""},
		{"signed out", loginState{known: true}, []string{NoticeSignedOut}, ""},
		{"ended", loginState{known: true, signedIn: true, expires: time.Now().Add(-time.Hour)}, []string{NoticeSignedOut}, ""},
		{"ends soon", loginState{known: true, signedIn: true, expires: soon}, []string{NoticeLoginExpiring}, soon.Format(time.RFC3339)},
	}
	for _, c := range cases {
		st := c.st
		a.loginCheck = func(context.Context) loginState { return st }
		a.forgetLogin()
		info := a.Info(context.Background())
		if !info.Available || !slices.Equal(info.Notices, c.notices) || info.LoginExpiresAt != c.expires {
			t.Errorf("%s: available %v notices %v expires %q, want %v %q", c.name, info.Available, info.Notices, info.LoginExpiresAt, c.notices, c.expires)
		}
	}
}

// The state is kept for loginTTL, so the phone's info calls do not run
// claude each time; a failed ask drops it.
func TestLoginKept(t *testing.T) {
	a := testAdapter(t)
	calls := 0
	a.loginCheck = func(context.Context) loginState { calls++; return loginState{known: true, signedIn: true} }
	a.Info(context.Background())
	a.Info(context.Background())
	if calls != 1 {
		t.Errorf("checked %d times, want 1", calls)
	}
	a.forgetLogin()
	a.Info(context.Background())
	if calls != 2 {
		t.Errorf("checked %d times after forgetLogin, want 2", calls)
	}
}

// The agent may run `claude auth status --json` and nothing else under auth:
// signing in and out is the person's.
func TestAuthArgs(t *testing.T) {
	if err := checkArgs([]string{"auth", "status", "--json"}); err != nil {
		t.Errorf("auth status --json: %v", err)
	}
	for _, args := range [][]string{
		{"auth"}, {"auth", "login"}, {"auth", "logout"}, {"auth", "status", "status"},
		{"auth", "status", "login"}, {"auth", "login", "--json"}, {"status"}, {"auth", "status", "--text"},
	} {
		if err := checkArgs(args); !errors.Is(err, ErrArgRejected) {
			t.Errorf("checkArgs(%q) = %v, want rejected", args, err)
		}
	}
}

func TestAskSignedOut(t *testing.T) {
	status401 := `{"type":"result","is_error":true,"api_error_status":401,"result":"Authentication failed","session_id":"0b6f2a8e-1111-4222-8333-444455556666"}`
	cases := []struct {
		name string
		res  proc.Result
		code string
	}{
		{"no result, stderr", proc.Result{ExitCode: 1, Stderr: []byte("Failed to authenticate: this login reached its time limit and cannot be renewed. Run `claude auth login` to sign in again")}, core.ErrCodeSignedOut},
		{"result says so", proc.Result{ExitCode: 1, Stdout: []byte(`{"type":"result","is_error":true,"result":"Invalid API key · Please run /login"}`)}, core.ErrCodeSignedOut},
		{"401", proc.Result{ExitCode: 1, Stdout: []byte(status401)}, core.ErrCodeSignedOut},
		{"other error", proc.Result{ExitCode: 1, Stdout: []byte(`{"type":"result","is_error":true,"result":"Overloaded"}`)}, core.ErrCodeAskFailed},
		{"other stderr", proc.Result{ExitCode: 1, Stderr: []byte("something broke")}, core.ErrCodeAskFailed},
	}
	for _, c := range cases {
		_, err := parseAskOutput(c.res, "sonnet")
		var ae *core.ActionError
		if !errors.As(err, &ae) || ae.Code != c.code {
			t.Errorf("%s: %v, want %s", c.name, err, c.code)
			continue
		}
		if c.code == core.ErrCodeSignedOut && ae.Detail != LoginCommand {
			t.Errorf("%s: detail %q, want the login command", c.name, ae.Detail)
		}
	}
}
