package main

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"

	"github.com/heeo-dev/askorus/agent/internal/adapter/claude"
	"github.com/heeo-dev/askorus/agent/internal/config"
	"github.com/heeo-dev/askorus/agent/internal/core"
	"github.com/heeo-dev/askorus/agent/internal/devices"
)

// setup walks a new PC through what the agent needs, in order (#37). It
// only strings together what the other commands do (path on, the needs
// check reports, start, autostart on, login, pair) and stops at the first
// step the person has to fix, saying how; running it again picks up from
// there. It changes nothing outside the agent: Tailscale is inside the
// agent (#57), and signing in is the person's own step in a browser.
func setupCmd(dir string, args []string, in io.Reader, out io.Writer) error {
	if len(args) != 0 {
		return fail("usage: askorus-agent setup", "사용법: askorus-agent setup")
	}
	cfg, err := config.Load(dir)
	if err != nil {
		return err
	}
	adapter, err := newAdapter(cfg)
	if err != nil {
		return err
	}
	env := setupEnv{
		elevated: claude.SelfElevated,
		exe:      os.Executable,
		exeDir:   exeDir,
		pathHas:  userPathHas,
		pathAdd:  userPathAdd,
		claude:   adapter.Info,
		others: func(ctx context.Context) []namedInfo {
			var out []namedInfo
			for _, a := range askOnlyAdapters(cfg) {
				out = append(out, namedInfo{name: displayName(a.Agent()), info: a.Info(ctx)})
			}
			return out
		},
		running: func(port int) bool { return agentRunning(port) },
		start:   func(out io.Writer) error { return startCmd(dir, nil, out) },
		autoGet: autostartGet,
		autoSet: autostartSet,
		node:    func() (nodeInfo, bool) { return readNodeInfo(dir) },
		login: func(ctx context.Context, out io.Writer) error {
			ctx, stop := signal.NotifyContext(ctx, os.Interrupt)
			defer stop()
			return waitLogin(ctx, dir, out, loginWait)
		},
		paired: func() int {
			store, err := devices.Open(dir)
			if err != nil {
				return 0
			}
			list, _ := store.List()
			return len(list)
		},
		pair: func(in io.Reader, out io.Writer) error { return pair(dir, nil, in, out) },
	}
	return runSetup(context.Background(), cfg, env, in, out)
}

// setupEnv is what setup looks at and changes; tests replace it so they
// never touch the real PATH, logon entry, Tailscale or agent.
type setupEnv struct {
	elevated func() bool
	exe      func() (string, error)
	exeDir   func() (string, error)
	pathHas  func(dir string) bool
	pathAdd  func(dir string) (bool, error)
	claude   func(ctx context.Context) core.AdapterInfo
	others   func(ctx context.Context) []namedInfo // the AIs that only answer asks (Codex, OpenCode)
	running  func(port int) bool                   // an agent with that port runs
	start    func(out io.Writer) error
	autoGet  func() string
	autoSet  func(exe string) error
	node     func() (nodeInfo, bool)                        // the running agent's Tailscale node
	login    func(ctx context.Context, out io.Writer) error // shows the login link and waits
	paired   func() int
	pair     func(in io.Reader, out io.Writer) error
}

// namedInfo is one AI program as setup shows it.
type namedInfo struct {
	name string // displayName
	info core.AdapterInfo
}

const setupSteps = 5

func runSetup(ctx context.Context, cfg config.Config, env setupEnv, in io.Reader, out io.Writer) error {
	br := bufio.NewReader(in) // one reader, so pair gets what setup did not read
	c := &checker{out: out}
	step := func(n int, x msg) {
		title := x.en
		if x.ko != x.en {
			title += " / " + x.ko
		}
		fmt.Fprintf(out, "\n[%d/%d] %s\n", n, setupSteps, title)
	}
	stop := func(x msg) error {
		c.line("bad", x)
		return fail("setup stopped at this step: fix it, then run askorus-agent setup again",
			"이 단계에서 멈췄습니다. 고친 뒤 askorus-agent setup을 다시 실행하세요")
	}

	say(out, m("Setting up the Askorus PC agent. Each step says what it does; run this again any time.",
		"Askorus PC 에이전트를 설정합니다. 단계마다 무엇을 하는지 보여 줍니다. 언제든 다시 실행해도 됩니다."))

	step(1, m("This PC", "이 PC"))
	if env.elevated() {
		return stop(m("this terminal runs as administrator; open a normal PowerShell window (not \"Run as administrator\")",
			"이 터미널은 관리자 권한입니다. 일반 PowerShell 창을 여세요 (\"관리자 권한으로 실행\"이 아닌 창)"))
	}
	c.line("ok", m("normal user rights", "일반 사용자 권한"))
	dir, err := env.exeDir()
	if err != nil {
		return err
	}
	if looksTemporary(dir) {
		return stop(m("%s is in %s, which looks temporary; move it to a folder you keep (the install script uses %s)",
			"%s가 임시 폴더로 보이는 %s에 있습니다. 계속 둘 폴더로 옮기세요 (설치 스크립트는 %s에 둡니다)", exeName, dir, installFolderHint))
	}
	if env.pathHas(dir) {
		c.line("ok", m("on this user's PATH: %s", "이 사용자의 PATH에 있음: %s", dir))
	} else {
		if _, err := env.pathAdd(dir); err != nil {
			return err
		}
		c.line("ok", m("put %s on this user's PATH: new terminals find askorus-agent", "%s를 이 사용자의 PATH에 넣었습니다. 새 터미널에서 askorus-agent로 쓸 수 있습니다", dir))
	}

	// Any of the AI programs will do (2026-11-10 decision): Claude Code is
	// needed only for the PC's Claude Code sessions, and two or more AIs for
	// combined answers and debates (a comparison needs two answers).
	step(2, m("AI programs", "AI 프로그램"))
	info := env.claude(ctx)
	others := env.others(ctx)
	found := 0
	if info.Available {
		found++
	}
	for _, o := range others {
		if o.info.Available {
			found++
		}
	}
	if found == 0 {
		return stop(m("no AI program was found: install at least one of Claude Code, Codex or OpenCode and sign in to it (two or more give combined answers and debates)",
			"AI 프로그램을 찾지 못했습니다. Claude Code, Codex, OpenCode 중 하나 이상을 설치하고 로그인하세요 (둘 이상이면 종합 답변과 토론이 나옵니다)"))
	}
	if info.Available {
		for _, n := range info.Notices {
			switch n {
			case claude.NoticeServiceElevated:
				return stop(m("the Claude background service runs as administrator; in an administrator terminal run: claude daemon stop --any",
					"Claude 백그라운드 서비스가 관리자 권한입니다. 관리자 터미널에서 실행하세요: claude daemon stop --any"))
			case claude.NoticeRCDisabledEnv:
				return stop(m("CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC or DISABLE_GROWTHBOOK is set, so Remote Control cannot work; remove it",
					"CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC 또는 DISABLE_GROWTHBOOK이 켜져 있어 Remote Control을 쓸 수 없습니다. 지우세요"))
			}
		}
		c.line("ok", m("Claude Code %s found", "Claude Code %s 찾음", info.Version))
	} else {
		c.line("warn", m("Claude Code was not found: asks go to the other AIs, but My PCs (the PC's Claude Code sessions on the phone) needs it. To add it, install it (https://code.claude.com/docs/en/setup) and sign in (run claude once in a terminal)",
			"Claude Code를 찾지 못했습니다. 묻기는 다른 AI로 하지만, '내 PC'(이 PC의 Claude Code 세션을 폰에서 쓰기)에는 Claude Code가 있어야 합니다. 쓰려면 설치하고(https://code.claude.com/docs/en/setup) 로그인하세요 (터미널에서 claude를 한 번 실행)"))
	}
	for _, o := range others {
		if o.info.Available {
			c.line("ok", m("%s %s found (asks)", "%s %s 찾음 (묻기)", o.name, o.info.Version))
		} else {
			c.line("info", m("%s not found (optional: asks use it when it is installed and signed in)", "%s 없음 (없어도 됩니다. 설치하고 로그인하면 묻기에 같이 씁니다)", o.name))
		}
	}
	if found == 1 {
		c.line("warn", m("only one AI program: asks work, but combined answers and debates need two or more",
			"AI 프로그램이 하나뿐입니다. 묻기는 되지만 종합 답변과 토론은 둘 이상일 때 나옵니다"))
	}

	// The agent carries Tailscale inside (#57), so there is nothing of
	// Tailscale to install or set on the PC: the agent is started first,
	// then signed in with a link.
	step(3, m("Run the agent", "에이전트 켜기"))
	if env.running(cfg.Port) {
		c.line("ok", m("the agent runs", "에이전트가 돌고 있습니다"))
	} else if err := env.start(out); err != nil {
		return err
	}
	exe, err := env.exe()
	if err != nil {
		return err
	}
	// The logon entry must start this exe: one left by an older copy in
	// another folder would start that one, or nothing.
	switch entry := env.autoGet(); {
	case strings.EqualFold(entry, autostartCommand(exe)):
		c.line("ok", m("starts at logon", "로그인할 때 켜짐"))
	case entry != "":
		if err := env.autoSet(exe); err != nil {
			return err
		}
		c.line("ok", m("the logon entry started another file (%s); it now starts this one", "로그인할 때 켜기가 다른 파일(%s)을 켜고 있어서 이 파일로 바꿨습니다", entry))
	default:
		if err := env.autoSet(exe); err != nil {
			return err
		}
		c.line("ok", m("the agent now starts when you sign in to Windows, with normal rights (turn it off: askorus-agent autostart off)",
			"이제 Windows에 로그인하면 에이전트가 일반 권한으로 켜집니다 (끄려면: askorus-agent autostart off)"))
	}

	step(4, m("Tailscale sign-in (how the phone reaches this PC)", "Tailscale 로그인 (폰이 이 PC에 닿는 길)"))
	if n, ok := env.node(); ok && n.signedIn() {
		c.line("ok", m("signed in to Tailscale as %s; phones reach this PC as %s, inside your Tailscale network only",
			"Tailscale에 %s 계정으로 로그인됨. 폰은 내 Tailscale 망 안에서만 %s로 이 PC에 닿습니다", n.Owner, n.Name))
	} else {
		say(out, m("The phone reaches this PC through Tailscale, a private network between your own devices. The agent has it built in: you only sign in, with a Tailscale account (free; you can make one with a Google, Microsoft, GitHub or Apple account on the sign-in page).",
			"폰은 내 기기끼리만 통하는 사설망인 Tailscale로 이 PC에 닿습니다. 에이전트에 들어 있어서 로그인만 하면 됩니다. Tailscale 계정이 필요합니다 (무료. 로그인 화면에서 Google, Microsoft, GitHub, Apple 계정으로 만들 수 있습니다)."))
		if err := env.login(ctx, out); err != nil {
			return err
		}
		if n, ok := env.node(); !ok || !n.signedIn() {
			return stop(m("this PC is not signed in to Tailscale yet", "이 PC가 아직 Tailscale에 로그인되지 않았습니다"))
		}
	}

	step(5, m("Pair the phone", "폰과 짝 맺기"))
	if n := env.paired(); n > 0 {
		c.line("ok", m("%d paired devices; to add a phone: askorus-agent pair", "짝 맺은 기기 %d개. 폰을 더 하려면: askorus-agent pair", n))
	} else {
		say(out, m("On the phone, install the Askorus app and open it. It asks you to sign in to Tailscale: use the same account as on this PC. Nothing else needs installing on the phone.",
			"폰에 Askorus 앱을 설치하고 여세요. 앱이 Tailscale 로그인을 안내합니다. 이 PC와 같은 계정으로 로그인하세요. 폰에 따로 설치할 것은 없습니다."))
		if err := env.pair(br, out); err != nil {
			return err
		}
	}

	fmt.Fprintln(out)
	if info.Available { // the tips are all Claude Code's
		for _, tip := range setupTips(cfg) {
			c.line("info", tip)
		}
		fmt.Fprintln(out)
	}
	say(out, m("Set up. Check everything any time with: askorus-agent check",
		"설정을 마쳤습니다. 언제든 점검하려면: askorus-agent check"))
	return nil
}
