package main

// The sign-in lines of setup and check (#60): an AI program that is
// installed but not signed in, or whose sign-in ends soon. The adapters
// find it out with the CLI's own status command; this only words it.

import (
	"net/url"
	"strings"
	"time"

	"github.com/heeo-dev/askorus/agent/internal/adapter/claude"
	"github.com/heeo-dev/askorus/agent/internal/adapter/codex"
	"github.com/heeo-dev/askorus/agent/internal/core"
)

// loginCommandFor is the command that signs the AI in on this PC.
func loginCommandFor(agent string) string {
	switch agent {
	case claude.Name:
		return claude.LoginCommand
	case codex.Name:
		return codex.LoginCommand
	}
	return ""
}

// signInLine prints a line when the AI is not signed in, or its sign-in
// ends soon. It returns false when the AI cannot answer for that.
func signInLine(c *checker, name string, info core.AdapterInfo) bool {
	cmd := loginCommandFor(info.Agent)
	for _, n := range info.Notices {
		switch {
		case strings.HasSuffix(n, "_signed_out"):
			c.line("warn", m("%s is installed but not signed in, so it cannot answer: sign in with %s",
				"%s가 설치돼 있지만 로그인되어 있지 않아 답하지 못합니다. 로그인하세요: %s", name, cmd))
			return false
		case strings.HasSuffix(n, "_login_expiring"):
			c.line("warn", m("%s's sign-in ends on %s: sign in again before then with %s",
				"%s 로그인이 %s에 끝납니다. 그 전에 다시 로그인하세요: %s", name, loginDay(info.LoginExpiresAt), cmd))
		}
	}
	return true
}

// loginDay is the day a sign-in ends, in this PC's time.
func loginDay(at string) string {
	t, err := time.Parse(time.RFC3339, at)
	if err != nil {
		return at
	}
	return t.Local().Format("2006-01-02")
}

// aiGuide is the README's section on installing and signing in to the AI
// programs (#60): the English README, or the Korean one.
func aiGuide(en bool) string {
	if en {
		return "https://github.com/heeo-dev/askorus#installing-the-ai-programs"
	}
	return "https://github.com/heeo-dev/askorus/blob/main/README.ko.md#" + url.PathEscape("ai-프로그램-설치")
}
