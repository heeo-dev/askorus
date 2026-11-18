package scenario

import (
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/heeo-dev/askorus/agent/internal/adapter/claude"
	"github.com/heeo-dev/askorus/agent/internal/adapter/codex"
	"github.com/heeo-dev/askorus/agent/internal/adapter/opencode"
	"github.com/heeo-dev/askorus/agent/internal/core"
	"github.com/heeo-dev/askorus/agent/internal/fakecli"
)

// IT-22 (#60, 2026-11-16): an AI that is installed but not signed in. The
// 11-16 macOS test showed it as ready in the connection check and only an
// ask failed, with the CLI's English message. Now /v1/info says so, and an
// ask that fails for the sign-in has its own code with the command that
// signs it in again. OpenCode (11-18) signs in per provider: one that is
// not signed in lists only its free models and does not know the default
// tier model, which the app used to show as an unknown model.
func TestIT22SignIn(t *testing.T) {
	t.Run("signed out", func(t *testing.T) {
		r := newRig(t, func(o *rigOptions) {
			o.env = map[string]string{fakecli.EnvSignIn: "claude=out,codex=out,opencode=out", fakecli.EnvAsk: "claude=signedout,codex=signedout"}
		})
		r.addDevice()
		info := r.info()
		cl, cx, oc := agentInfo(t, info, fakecli.RoleClaude), agentInfo(t, info, fakecli.RoleCodex), agentInfo(t, info, fakecli.RoleOpenCode)
		if !cl.Available || !slices.Contains(cl.Notices, claude.NoticeSignedOut) {
			t.Errorf("claude %+v", cl)
		}
		if !cx.Available || !slices.Contains(cx.Notices, codex.NoticeSignedOut) {
			t.Errorf("codex %+v", cx)
		}
		if !oc.Available || !slices.Equal(oc.Notices, []string{opencode.NoticeSignedOut}) {
			t.Errorf("opencode %+v", oc)
		}
		st := r.waitRun(r.ask(nil, nil).ID)
		want := map[string]string{fakecli.RoleClaude: claude.LoginCommand, fakecli.RoleCodex: codex.LoginCommand, fakecli.RoleOpenCode: opencode.LoginCommand}
		for _, p := range st.Parts {
			if p.Error == nil || p.Error.Code != core.ErrCodeSignedOut || p.Error.Message != want[p.Agent] || p.Error.Hint != "" {
				t.Errorf("%s part %+v, want %s with %q and no hint", p.Agent, p.Error, core.ErrCodeSignedOut, want[p.Agent])
			}
		}
		if len(st.Parts) != 3 {
			t.Errorf("%d parts", len(st.Parts))
		}
	})

	// OpenCode signed in, but its provider refuses the key (401,
	// "Invalid credential"): the models are listed, so the info says
	// nothing, and the ask's part says to sign in again.
	t.Run("OpenCode's key refused", func(t *testing.T) {
		r := newRig(t, func(o *rigOptions) { o.env = map[string]string{fakecli.EnvAsk: "opencode=signedout"} })
		r.addDevice()
		if oc := agentInfo(t, r.info(), fakecli.RoleOpenCode); len(oc.Notices) != 0 {
			t.Errorf("opencode %+v", oc)
		}
		st := r.waitRun(r.ask(nil, nil).ID)
		for _, p := range st.Parts {
			if p.Agent == fakecli.RoleOpenCode && (p.Error == nil || p.Error.Code != core.ErrCodeSignedOut || p.Error.Message != opencode.LoginCommand) {
				t.Errorf("opencode part %+v, want %s with %q", p.Error, core.ErrCodeSignedOut, opencode.LoginCommand)
			}
		}
	})

	// 11-18: OpenCode's free models need no sign-in but share a daily
	// limit; the part says when they work again instead of OpenCode's
	// English message.
	t.Run("OpenCode's free models at their limit", func(t *testing.T) {
		r := newRig(t, func(o *rigOptions) { o.env = map[string]string{fakecli.EnvAsk: "opencode=freelimit"} })
		r.addDevice()
		st := r.waitRun(r.ask(nil, nil).ID)
		for _, p := range st.Parts {
			if p.Agent != fakecli.RoleOpenCode {
				continue
			}
			if p.Error == nil || p.Error.Code != core.ErrCodeFreeLimit {
				t.Fatalf("opencode part %+v, want %s", p.Error, core.ErrCodeFreeLimit)
			}
			if at, err := time.Parse(time.RFC3339, p.Error.Message); err != nil || time.Until(at) < fakecli.FreeLimitWait-time.Minute || time.Until(at) > fakecli.FreeLimitWait+time.Minute {
				t.Errorf("message %q (%v), want the time it works again", p.Error.Message, err)
			}
		}
	})

	// R-118 (#62, 2026-11-17): on a Mac, an agent started over SSH stays
	// outside the screen's session, where claude cannot read its sign-in
	// in the keychain. Claude shows as signed out though the person is
	// signed in, and signing in again does not help: the info and the
	// failed part say why, so the app can tell them to start the agent
	// again from the Mac's screen. Codex keeps its sign-in in a file.
	t.Run("Mac outside the screen's session", func(t *testing.T) {
		r := newRig(t, func(o *rigOptions) {
			o.offScreen = true
			o.env = map[string]string{fakecli.EnvSignIn: "claude=out,codex=out", fakecli.EnvAsk: "claude=signedout,codex=signedout"}
		})
		r.addDevice()
		info := r.info()
		cl, cx := agentInfo(t, info, fakecli.RoleClaude), agentInfo(t, info, fakecli.RoleCodex)
		if !slices.Contains(cl.Notices, claude.NoticeSignedOut) || cl.SignInHint != core.SignInHintMacKeychain {
			t.Errorf("claude %+v, want %s with hint %s", cl, claude.NoticeSignedOut, core.SignInHintMacKeychain)
		}
		if cx.SignInHint != "" {
			t.Errorf("codex %+v has a hint", cx)
		}
		st := r.waitRun(r.ask(nil, nil).ID)
		for _, p := range st.Parts {
			switch p.Agent {
			case fakecli.RoleClaude:
				if p.Error == nil || p.Error.Code != core.ErrCodeSignedOut || p.Error.Message != claude.LoginCommand || p.Error.Hint != core.SignInHintMacKeychain {
					t.Errorf("claude part %+v, want %s with hint %s", p.Error, core.ErrCodeSignedOut, core.SignInHintMacKeychain)
				}
			case fakecli.RoleCodex:
				if p.Error == nil || p.Error.Code != core.ErrCodeSignedOut || p.Error.Hint != "" {
					t.Errorf("codex part %+v, want %s and no hint", p.Error, core.ErrCodeSignedOut)
				}
			}
		}
	})

	t.Run("Mac outside the screen's session, signed in", func(t *testing.T) {
		r := newRig(t, func(o *rigOptions) { o.offScreen = true })
		r.addDevice()
		if cl := agentInfo(t, r.info(), fakecli.RoleClaude); len(cl.Notices) != 0 || cl.SignInHint != "" {
			t.Errorf("claude %+v: a signed-in claude needs no hint", cl)
		}
	})

	t.Run("ends soon", func(t *testing.T) {
		r := newRig(t, func(o *rigOptions) { o.env = map[string]string{fakecli.EnvSignIn: "claude=soon"} })
		r.addDevice()
		cl := agentInfo(t, r.info(), fakecli.RoleClaude)
		end, err := time.Parse(time.RFC3339, cl.LoginExpiresAt)
		if !slices.Equal(cl.Notices, []string{claude.NoticeLoginExpiring}) || err != nil || time.Until(end) > 24*time.Hour || time.Until(end) < 0 {
			t.Errorf("claude %+v (%v)", cl, err)
		}
	})

	t.Run("signed in, and nothing but the sign-in leaves the CLI", func(t *testing.T) {
		r := newRig(t, nil)
		r.addDevice()
		info := r.info()
		for _, a := range info.Agents {
			if len(a.Notices) != 0 || a.LoginExpiresAt != "" {
				t.Errorf("%s %+v", a.Agent, a)
			}
		}
		// The email the fake's auth status prints is never passed on.
		if b := r.callJSON("GET", "/v1/info", nil, 200, nil); strings.Contains(string(b), "example.com") {
			t.Errorf("/v1/info carries the email: %s", b)
		}
	})
}
