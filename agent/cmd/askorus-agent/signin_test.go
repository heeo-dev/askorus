package main

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/heeo-dev/askorus/agent/internal/adapter/claude"
	"github.com/heeo-dev/askorus/agent/internal/adapter/codex"
	"github.com/heeo-dev/askorus/agent/internal/core"
)

// #60: setup and check say when an AI is installed but not signed in.
func TestSignInLine(t *testing.T) {
	var out bytes.Buffer
	c := &checker{out: &out}
	if ok := signInLine(c, "Claude Code", core.AdapterInfo{Agent: claude.Name, Available: true}); !ok || out.Len() != 0 || c.warn != 0 {
		t.Errorf("signed in: ok %v, printed %q", ok, out.String())
	}
	if ok := signInLine(c, "Codex", core.AdapterInfo{Agent: codex.Name, Available: true, Notices: []string{codex.NoticeSignedOut}}); ok || !strings.Contains(out.String(), "codex login") || c.warn != 1 {
		t.Errorf("signed out: ok %v, printed %q", ok, out.String())
	}
	out.Reset()
	end := time.Date(2026, 11, 27, 3, 2, 52, 0, time.UTC)
	info := core.AdapterInfo{Agent: claude.Name, Available: true, Notices: []string{claude.NoticeLoginExpiring}, LoginExpiresAt: end.Format(time.RFC3339)}
	if ok := signInLine(c, "Claude Code", info); !ok || !strings.Contains(out.String(), end.Local().Format("2006-01-02")) || !strings.Contains(out.String(), "claude auth login") {
		t.Errorf("ends soon: ok %v, printed %q", ok, out.String())
	}
	if got := loginDay("not a time"); got != "not a time" {
		t.Errorf("loginDay = %q", got)
	}
}

// R-118 (#62): on a Mac outside the screen's session (SSH), check and setup
// do not send the person to sign in again, and start says where to start
// the agent from instead.
func TestOffScreenSignIn(t *testing.T) {
	var out bytes.Buffer
	c := &checker{out: &out}
	info := core.AdapterInfo{Agent: claude.Name, Available: true, Notices: []string{claude.NoticeSignedOut}, SignInHint: core.SignInHintMacKeychain}
	if ok := signInLine(c, "Claude Code", info); ok || c.warn != 1 || strings.Contains(out.String(), "claude auth login") || !strings.Contains(out.String(), "Mac") || !strings.Contains(out.String(), "키체인") {
		t.Errorf("off screen: ok %v, printed %q", ok, out.String())
	}

	off := true
	old := startOffScreen
	startOffScreen = func() bool { return off }
	t.Cleanup(func() { startOffScreen = old })
	note, ok := startSignInNote("claude-for-the-test")
	if !ok || !strings.Contains(note.String(), "askorus-agent shutdown") || !strings.Contains(note.String(), "askorus-agent start") || !strings.Contains(note.String(), "키체인") {
		t.Errorf("start off screen: %v %q", ok, note.String())
	}
	if strings.Contains(note.String(), "—") {
		t.Errorf("start note has a long dash: %q", note.String())
	}
	off = false
	if _, ok := startSignInNote("claude-for-the-test"); ok {
		t.Error("start on the screen added the note")
	}
	// No Claude Code on this PC: nothing to say about its sign-in.
	off = true
	t.Setenv("PATH", t.TempDir())
	t.Setenv("HOME", t.TempDir())
	if _, ok := startSignInNote(""); ok {
		t.Error("start without Claude Code added the note")
	}
}
