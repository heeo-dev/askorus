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
		case strings.HasSuffix(n, "_signed_out") && info.SignInHint == core.SignInHintMacKeychain:
			c.line("warn", offScreenSignIn(name))
			return false
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

// offScreenSignIn is the sign-in line of check and setup in a terminal on
// a Mac outside the screen's session (#62): what claude says there is not
// the sign-in the person made on the Mac's screen.
func offScreenSignIn(name string) msg {
	return m("%s shows as not signed in, but this terminal is not on the Mac's own screen (SSH, for example) and cannot read the sign-in macOS keeps in the keychain: check again from Terminal on the Mac itself",
		"%s가 로그인되어 있지 않다고 나오지만, 이 터미널은 Mac 화면에서 연 터미널이 아니어서(SSH 등) macOS가 키체인에 둔 로그인을 읽지 못합니다. Mac 화면에서 연 터미널에서 다시 확인하세요", name)
}

// offScreenStart is what start says on a Mac outside the screen's session
// (#62), when Claude Code is installed: the agent it started stays in this
// session and cannot read Claude Code's sign-in.
func offScreenStart() msg {
	return m("this terminal is not on the Mac's own screen (SSH, for example): the agent started from it cannot read Claude Code's sign-in, which macOS keeps in the keychain, so Claude may show as not signed in even when you are. On the Mac itself, open Terminal and run askorus-agent shutdown, then askorus-agent start",
		"이 터미널은 Mac 화면에서 연 터미널이 아닙니다(SSH 등). 여기서 켠 에이전트는 macOS가 키체인에 둔 Claude Code 로그인을 읽지 못해, 로그인돼 있어도 Claude가 로그인 안 됨으로 보일 수 있습니다. Mac 화면에서 터미널을 열고 askorus-agent shutdown, askorus-agent start를 차례로 실행하세요")
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
