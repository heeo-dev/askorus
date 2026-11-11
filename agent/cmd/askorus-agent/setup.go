package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/heeo-dev/askorus/agent/internal/adapter/claude"
	"github.com/heeo-dev/askorus/agent/internal/config"
	"github.com/heeo-dev/askorus/agent/internal/core"
	"github.com/heeo-dev/askorus/agent/internal/devices"
	"github.com/heeo-dev/askorus/agent/internal/tailnet"
)

// setup walks a new PC through what the agent needs, in order (#37). It
// only strings together what the other commands do (path on, the needs
// check reports, start, autostart on, pair) and stops at the first step the
// person has to fix, saying how; running it again picks up from there. The
// one Tailscale setting it may change is the serve port to the agent, and
// only after the person typed y (2026-11-09 decision); Just Enter prints
// the command for the person to run, as check does.
func setupCmd(dir string, args []string, in io.Reader, out io.Writer) error {
	if len(args) != 0 {
		return fail("usage: askorus-agent setup", "사용법: askorus-agent setup")
	}
	if !controlSupported() {
		// macOS and Linux for now (#52): the steps setup would walk through, by hand.
		return fail("setup is only supported on Windows for now. Here: run `askorus-agent check` and fix what it lists, keep `askorus-agent serve` running in a terminal, then run `askorus-agent pair` in another",
			"setup은 지금 Windows에서만 됩니다. 여기서는 `askorus-agent check`가 알려 주는 것을 고치고, 터미널 하나에 `askorus-agent serve`를 켜 둔 채 다른 터미널에서 `askorus-agent pair`를 실행하세요")
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
		tailnet:   lookTailnet,
		servePort: suggestServePort,
		serveOn: func(ctx context.Context, httpsPort, localPort int) error {
			c, err := tailnet.New()
			if err != nil {
				return err
			}
			return c.ServeOn(ctx, httpsPort, localPort)
		},
		listening: agentListening,
		ours:      agentOnPort,
		settings:  filepath.Join(dir, config.FileName),
		start:     func(out io.Writer) error { return startCmd(dir, nil, out) },
		autoGet:   autostartGet,
		autoSet:   func(exe string) error { return autostartSet(exe, false) },
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
	elevated  func() bool
	exe       func() (string, error)
	exeDir    func() (string, error)
	pathHas   func(dir string) bool
	pathAdd   func(dir string) (bool, error)
	claude    func(ctx context.Context) core.AdapterInfo
	others    func(ctx context.Context) []namedInfo // the AIs that only answer asks (Codex, OpenCode)
	tailnet   func(ctx context.Context, port int) tailnetInfo
	servePort func(ctx context.Context, port int) int
	serveOn   func(ctx context.Context, httpsPort, localPort int) error
	listening func(port int) bool
	ours      func(port int) bool // the program on the port is an agent (agentOnPort)
	settings  string              // the settings file, where the port is set
	start     func(out io.Writer) error
	autoGet   func() string
	autoSet   func(exe string) error
	paired    func() int
	pair      func(in io.Reader, out io.Writer) error
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
		return stop(m("askorus-agent.exe is in %s, which looks temporary; move it to a folder you keep (the install script uses %%LOCALAPPDATA%%\\Programs\\Askorus)",
			"askorus-agent.exe가 임시 폴더로 보이는 %s에 있습니다. 계속 둘 폴더로 옮기세요 (설치 스크립트는 %%LOCALAPPDATA%%\\Programs\\Askorus에 둡니다)", dir))
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

	step(3, m("Tailscale (how the phone reaches this PC)", "Tailscale (폰이 이 PC에 닿는 길)"))
	// Tailscale passes the phone's requests to this port: another program
	// on it would be opened to the tailnet instead of the agent.
	if env.listening(cfg.Port) && !env.ours(cfg.Port) {
		return stop(m("another program already uses 127.0.0.1:%d, the agent's port: set a free \"port\" in %s",
			"다른 프로그램이 에이전트 포트 127.0.0.1:%d를 이미 씁니다. %s에서 \"port\"를 빈 포트로 바꾸세요", cfg.Port, env.settings))
	}
	serveNotYet := m("tailscale serve does not point at the agent yet", "tailscale serve가 아직 에이전트를 가리키지 않습니다")
	triedServe := false // setup turns serve on once at most; then the person does
	for {
		ti := env.tailnet(ctx, cfg.Port)
		var be biError
		if errors.As(ti.problem, &be) {
			return stop(msg{"Tailscale: " + be.en + ". Install Tailscale from https://tailscale.com/download and sign in, on this PC and on the phone with the same account",
				"Tailscale: " + be.ko + ". https://tailscale.com/download 에서 Tailscale을 설치하고, 이 PC와 폰에서 같은 계정으로 로그인하세요"})
		}
		if ti.problem != nil {
			return ti.problem
		}
		if !ti.status.HTTPS {
			return stop(m("HTTPS certificates are off for your tailnet: in the Tailscale admin console (DNS page) turn on MagicDNS and HTTPS Certificates",
				"tailnet에 HTTPS 인증서가 꺼져 있습니다. Tailscale 관리 화면(DNS 쪽)에서 MagicDNS와 HTTPS Certificates를 켜세요"))
		}
		if ti.served && ti.serve.Funnel {
			return stop(m("tailscale funnel opens the agent port to the internet; turn it off: tailscale funnel --https=%d off",
				"tailscale funnel이 에이전트 포트를 인터넷에 엽니다. 끄세요: tailscale funnel --https=%d off", ti.serve.HTTPSPort))
		}
		if ti.served {
			c.line("ok", m("Tailscale connected as %s; phones reach the agent at https://%s:%d", "Tailscale 연결됨: %s. 폰은 https://%s:%d로 에이전트에 닿습니다",
				ti.status.OwnerLogin, ti.serve.Host, ti.serve.HTTPSPort))
			break
		}
		port := env.servePort(ctx, cfg.Port)
		c.line("warn", m("Tailscale is connected (account %s), but it does not pass the phone's requests to the agent yet. This Tailscale setting does it, once per PC:",
			"Tailscale은 연결됐지만(계정 %s), 아직 폰의 요청을 에이전트로 넘기지 않습니다. 이 Tailscale 설정이 그 일을 합니다 (PC마다 한 번):", ti.status.OwnerLogin))
		fmt.Fprintf(out, "\n      tailscale serve --bg --https=%d http://127.0.0.1:%d\n\n", port, cfg.Port)
		say(out, m("It passes https://%s:%d to the agent, for devices on your own tailnet only (not the internet). Your other serve settings stay as they are.",
			"https://%s:%d로 오는 요청을 에이전트로 넘깁니다. 내 tailnet의 기기만 닿고 인터넷에는 열지 않습니다. 다른 serve 설정은 그대로입니다.", ti.status.DNSName, port))
		yourself := triedServe
		if !triedServe {
			yesNoPrompt(out, m("Type y and press Enter to let setup turn it on now. Just Enter shows how to do it yourself.",
				"setup이 지금 켜게 하려면 y를 누르고 Enter, 직접 하려면 그냥 Enter를 누르세요."))
			line, err := br.ReadString('\n')
			if err != nil && line == "" {
				fmt.Fprintln(out)
				return stop(serveNotYet)
			}
			yourself = !yesAnswers[normalizeAnswer(line)]
			if !yourself {
				triedServe = true
				if err := env.serveOn(ctx, port, cfg.Port); err != nil {
					c.line("warn", msg{"tailscale could not turn it on: " + err.Error(), "tailscale이 켜지 못했습니다: " + err.Error()})
					yourself = true
				} else {
					c.line("ok", m("turned it on; looking again", "켰습니다. 다시 확인합니다"))
					continue
				}
			}
		}
		if yourself {
			say(out, m("Run the line above in another PowerShell window; if it shows a link to turn on Serve for your tailnet, open it first. Press Enter here when it is done (Ctrl+C stops setup).",
				"위 줄을 다른 PowerShell 창에서 실행하세요. tailnet에 Serve를 켜라는 링크가 나오면 먼저 그 링크를 여세요. 다 되면 여기서 Enter를 누르세요 (Ctrl+C는 설정을 멈춤)."))
			if _, err := br.ReadString('\n'); err != nil {
				return stop(serveNotYet)
			}
		}
	}

	step(4, m("Run the agent", "에이전트 켜기"))
	if env.listening(cfg.Port) {
		c.line("ok", m("the agent runs on 127.0.0.1:%d", "에이전트가 127.0.0.1:%d에서 돌고 있습니다", cfg.Port))
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
	case strings.EqualFold(entry, autostartCommand(exe, false)) || strings.EqualFold(entry, autostartCommand(exe, true)):
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

	step(5, m("Pair the phone", "폰과 짝 맺기"))
	if n := env.paired(); n > 0 {
		c.line("ok", m("%d paired devices; to add a phone: askorus-agent pair", "짝 맺은 기기 %d개. 폰을 더 하려면: askorus-agent pair", n))
	} else {
		say(out, m("On the phone, install Tailscale and the Askorus app, and sign in to Tailscale with the same account as this PC.",
			"폰에 Tailscale과 Askorus 앱을 설치하고, Tailscale에는 이 PC와 같은 계정으로 로그인하세요."))
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
