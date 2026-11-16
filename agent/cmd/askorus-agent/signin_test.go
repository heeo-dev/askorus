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
