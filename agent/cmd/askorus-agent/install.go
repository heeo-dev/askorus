package main

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/heeo-dev/askorus/agent/internal/adapter/claude"
	"github.com/heeo-dev/askorus/agent/internal/adapter/opencode"
	"github.com/heeo-dev/askorus/agent/internal/config"
	"github.com/heeo-dev/askorus/agent/internal/core"
	"github.com/heeo-dev/askorus/agent/internal/devices"
	"github.com/heeo-dev/askorus/agent/internal/tailnet"
)

// Installing and running the agent on the PC (A5, design: PC 설치 흐름).

// agentListening reports whether something answers on 127.0.0.1:port.
func agentListening(port int) bool {
	c, err := net.DialTimeout("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(port)), 500*time.Millisecond)
	if err != nil {
		return false
	}
	c.Close()
	return true
}

func waitListening(port int, want bool, d time.Duration) bool {
	deadline := time.Now().Add(d)
	for time.Now().Before(deadline) {
		if agentListening(port) == want {
			return true
		}
		time.Sleep(200 * time.Millisecond)
	}
	return agentListening(port) == want
}

// startCmd runs `serve` in the background without a window.
func startCmd(dir string, args []string, out io.Writer) error {
	fs := flag.NewFlagSet("start", flag.ContinueOnError)
	testPage := fs.Bool("test-page", false, "serve the phone-browser test page")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if !controlSupported() {
		return fail("start is only supported on Windows for now; run `askorus-agent serve`", "start는 지금 Windows에서만 됩니다. `askorus-agent serve`를 쓰세요")
	}
	cfg, err := config.Load(dir)
	if err != nil {
		return err
	}
	if agentListening(cfg.Port) {
		say(out, m("the agent already runs on 127.0.0.1:%d", "에이전트가 이미 127.0.0.1:%d에서 돌고 있습니다", cfg.Port))
		return nil
	}
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Join(dir, "logs"), 0o700); err != nil {
		return err
	}
	serveArgs := []string{"serve"}
	if *testPage {
		serveArgs = append(serveArgs, "-test-page")
	}
	pid, err := startDetached(exe, serveArgs, dir, filepath.Join(dir, "logs", "background.out"))
	if err != nil {
		return err
	}
	if !waitListening(cfg.Port, true, 15*time.Second) {
		return fail("the agent (pid %d) did not start listening; see %s", "에이전트(pid %d)가 열리지 않았습니다. %s를 보세요", pid, filepath.Join(dir, "logs", "background.out"))
	}
	say(out, m("the agent runs in the background on 127.0.0.1:%d (pid %d); stop it with: askorus-agent shutdown",
		"에이전트가 백그라운드에서 127.0.0.1:%d로 돕니다 (pid %d). 끄려면: askorus-agent shutdown", cfg.Port, pid))
	return nil
}

// pidFile holds the running agent's process id while it serves.
const pidFile = "agent.pid"

func readPID(dir string) int {
	b, err := os.ReadFile(filepath.Join(dir, pidFile))
	if err != nil {
		return 0
	}
	n, _ := strconv.Atoi(strings.TrimSpace(string(b)))
	return n
}

// shutdownCmd asks the running agent to stop and waits until its process
// has ended: it closes the port at once, then stops its folder servers
// with Ctrl+C.
func shutdownCmd(dir string, out io.Writer) error {
	cfg, err := config.Load(dir)
	if err != nil {
		return err
	}
	pid := readPID(dir)
	if err := signalStop(cfg.Port); err != nil {
		if !agentListening(cfg.Port) {
			say(out, m("the agent is not running", "에이전트가 돌고 있지 않습니다"))
			return nil
		}
		return fail("the agent on 127.0.0.1:%d did not take the stop signal (started from another account?)",
			"127.0.0.1:%d의 에이전트가 멈춤 신호를 받지 않았습니다 (다른 계정에서 켰나요?)", cfg.Port)
	}
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) && (agentListening(cfg.Port) || (pid > 0 && processRunning(pid))) {
		time.Sleep(200 * time.Millisecond)
	}
	if agentListening(cfg.Port) || (pid > 0 && processRunning(pid)) {
		return fail("the agent is still running after 30 s", "30초가 지나도 에이전트가 돌고 있습니다")
	}
	say(out, m("the agent stopped", "에이전트를 멈췄습니다"))
	return nil
}

func statusCmd(dir string, out io.Writer) error {
	cfg, err := config.Load(dir)
	if err != nil {
		return err
	}
	if agentListening(cfg.Port) {
		say(out, m("running on 127.0.0.1:%d", "127.0.0.1:%d에서 돌고 있습니다", cfg.Port))
	} else {
		say(out, m("not running", "돌고 있지 않습니다"))
	}
	if a := autostartGet(); a != "" {
		say(out, m("starts at logon: %s", "로그인할 때 켜짐: %s", a))
	} else {
		say(out, m("does not start at logon (askorus-agent autostart on)", "로그인할 때 켜지지 않음 (askorus-agent autostart on)"))
	}
	return nil
}

func autostartCmd(args []string, out io.Writer) error {
	testPage := len(args) == 2 && args[0] == "on" && args[1] == "-test-page"
	if !testPage && (len(args) != 1 || !slices.Contains([]string{"on", "off", "status"}, args[0])) {
		return fail("usage: askorus-agent autostart on [-test-page] | off | status", "사용법: askorus-agent autostart on [-test-page] | off | status")
	}
	switch args[0] {
	case "on":
		if !controlSupported() {
			return fail("autostart is only supported on Windows for now", "자동 시작은 지금 Windows에서만 됩니다")
		}
		if claude.SelfElevated() {
			return fail("run this from a normal (not administrator) terminal, so the agent starts with normal rights",
				"관리자가 아닌 일반 터미널에서 실행하세요. 그래야 에이전트가 일반 권한으로 켜집니다")
		}
		exe, err := os.Executable()
		if err != nil {
			return err
		}
		if err := autostartSet(exe, testPage); err != nil {
			return err
		}
		say(out, m("the agent starts at logon for this user, with normal rights: %s", "이 사용자로 로그인할 때 에이전트가 일반 권한으로 켜집니다: %s", autostartCommand(exe, testPage)))
	case "off":
		if err := autostartRemove(); err != nil {
			return err
		}
		say(out, m("the agent no longer starts at logon", "로그인할 때 에이전트가 켜지지 않게 했습니다"))
	default:
		if a := autostartGet(); a != "" {
			say(out, m("on: %s", "켜짐: %s", a))
		} else {
			say(out, m("off", "꺼짐"))
		}
	}
	return nil
}

// checker prints check's lines: ok, warn, bad or info. It counts the
// problems (bad) and the warnings.
type checker struct {
	out  io.Writer
	bad  int
	warn int
}

func (c *checker) line(level string, x msg) {
	mark := map[string]string{"ok": "[ok]  ", "warn": "[!]   ", "bad": "[X]   ", "info": "[i]   "}[level]
	switch level {
	case "bad":
		c.bad++
	case "warn":
		c.warn++
	}
	fmt.Fprintf(c.out, "%s%s\n      %s\n", mark, x.en, x.ko)
}

// setupTips are the once-per-PC steps check cannot see from here, so it
// always shows them. Trusting the ask folder lets an ask go on in the
// Claude app: opening it there is `claude --bg`, which only runs in a
// trusted folder (P2-2). Whether it is trusted is in Claude Code's own
// settings file, which also holds account details, so it is not read
// (N10); and the trust prompt is the person's to accept (R-10).
func setupTips(cfg config.Config) []msg {
	tips := []msg{
		m("once per PC, run `claude --rc` in a terminal and accept the Remote Control prompt", "PC마다 한 번, 터미널에서 `claude --rc`를 실행해 Remote Control 동의를 하세요"),
	}
	if folder, err := cfg.AskFolder(); err == nil {
		tips = append(tips, m("to continue asks in the Claude app, trust the ask folder once: in a terminal run cd \"%s\", then claude, accept the trust prompt and exit",
			"묻기를 Claude 앱에서 이어 가려면 묻기 폴더를 한 번 신뢰하세요: 터미널에서 cd \"%s\" 후 claude를 실행하고 신뢰를 허용한 뒤 나오면 됩니다", folder))
	}
	return append(tips, m("to get pushes on the phone, turn on \"Push when actions required\" and \"Push when Claude decides\" in Claude Code /config",
		"폰으로 알림을 받으려면 Claude Code /config에서 \"Push when actions required\"와 \"Push when Claude decides\"를 켜세요"))
}

// checkCmd looks at everything the agent needs (design: PC 설치 흐름).
// It only reads; the fixes it prints are for the person to run.
func checkCmd(dir string, out io.Writer) error {
	c := &checker{out: out}
	ctx := context.Background()
	cfg, err := config.Load(dir)
	if err != nil {
		c.line("bad", m("settings: %v", "설정 파일: %v", err))
		return errors.New("check failed")
	}
	if claude.SelfElevated() {
		c.line("bad", m("this terminal runs as administrator; run the agent from a normal terminal (sessions would get full rights)",
			"이 터미널은 관리자 권한입니다. 에이전트는 일반 터미널에서 켜세요 (세션이 모든 권한을 갖게 됨)"))
	} else {
		c.line("ok", m("normal user rights", "일반 사용자 권한"))
	}

	adapter, err := newAdapter(cfg)
	if err != nil {
		return err
	}
	info := adapter.Info(ctx)
	if info.Available {
		c.line("ok", m("Claude Code %s found", "Claude Code %s 찾음", info.Version))
	} else {
		c.line("bad", m("Claude Code not found or not working: install it and sign in", "Claude Code를 찾지 못했거나 동작하지 않습니다. 설치하고 로그인하세요"))
	}
	for _, n := range info.Notices {
		switch n {
		case claude.NoticeServiceElevated:
			c.line("bad", m("the Claude background service runs as administrator; in an administrator terminal run: claude daemon stop --any",
				"Claude 백그라운드 서비스가 관리자 권한입니다. 관리자 터미널에서 실행하세요: claude daemon stop --any"))
		case claude.NoticeRCDisabledEnv:
			c.line("bad", m("CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC or DISABLE_GROWTHBOOK is set: Remote Control cannot work",
				"CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC 또는 DISABLE_GROWTHBOOK이 켜져 있어 Remote Control을 쓸 수 없습니다"))
		}
	}

	// The AIs that only answer questions are optional: a missing one is
	// left out of asks.
	askOnly := askOnlyAdapters(cfg)
	for _, a := range askOnly {
		if ai := a.Info(ctx); ai.Available {
			c.line("ok", m("%s %s found (asks)", "%s %s 찾음 (묻기)", a.Agent(), ai.Version))
		} else {
			c.line("warn", m("%s not found or not working (%s); asks leave it out",
				"%s를 찾지 못했거나 동작하지 않습니다 (%s). 묻기에서 빠집니다", a.Agent(), strings.Join(ai.Notices, ", ")))
		}
	}
	if folder, err := cfg.AskFolder(); err == nil {
		c.line("ok", m("asks run in %s", "묻기는 %s에서 돕니다", folder))
	}
	// Who writes the combined answers, and where the answers go.
	st := writerStatuses(ctx, append([]core.Adapter{adapter}, askOnly...), judgeGate(cfg))
	for _, l := range writerLines(st, cfg.Judge.Off) {
		c.line(l.level, l.text)
	}

	ti := lookTailnet(ctx, cfg.Port)
	var be biError
	if errors.As(ti.problem, &be) {
		c.line("bad", msg{"Tailscale: " + be.en, "Tailscale: " + be.ko})
	} else {
		c.line("ok", m("Tailscale connected as %s (%s)", "Tailscale 연결됨: %s (%s)", ti.status.OwnerLogin, ti.status.DNSName))
		if !ti.status.HTTPS {
			c.line("bad", m("HTTPS certificates are off for the tailnet: turn on MagicDNS and HTTPS in the Tailscale admin console",
				"tailnet에 HTTPS 인증서가 꺼져 있습니다. Tailscale 관리 화면에서 MagicDNS와 HTTPS를 켜세요"))
		}
		switch {
		case ti.served && ti.serve.Funnel:
			c.line("bad", m("tailscale funnel exposes the agent port to the internet; turn it off: tailscale funnel --https=%d off",
				"tailscale funnel이 에이전트 포트를 인터넷에 엽니다. 끄세요: tailscale funnel --https=%d off", ti.serve.HTTPSPort))
		case ti.served:
			c.line("ok", m("phones reach the agent at https://%s:%d", "폰은 https://%s:%d로 에이전트에 닿습니다", ti.serve.Host, ti.serve.HTTPSPort))
		default:
			port := suggestServePort(ctx, cfg.Port)
			c.line("bad", m("tailscale serve does not point at the agent yet; run: tailscale serve --bg --https=%d http://127.0.0.1:%d",
				"tailscale serve가 아직 에이전트를 가리키지 않습니다. 실행하세요: tailscale serve --bg --https=%d http://127.0.0.1:%d", port, cfg.Port))
		}
	}

	if agentListening(cfg.Port) {
		c.line("ok", m("the agent runs on 127.0.0.1:%d", "에이전트가 127.0.0.1:%d에서 돌고 있습니다", cfg.Port))
	} else {
		c.line("warn", m("the agent is not running: askorus-agent start", "에이전트가 돌고 있지 않습니다: askorus-agent start"))
	}
	if controlSupported() {
		if autostartGet() != "" {
			c.line("ok", m("starts at logon", "로그인할 때 켜짐"))
		} else {
			c.line("warn", m("does not start at logon: askorus-agent autostart on", "로그인할 때 켜지지 않습니다: askorus-agent autostart on"))
		}
		if d, err := exeDir(); err == nil {
			if userPathHas(d) {
				c.line("ok", m("on this user's PATH: %s", "이 사용자의 PATH에 있음: %s", d))
			} else {
				c.line("warn", m("not on PATH, so terminals need the full path: askorus-agent path on", "PATH에 없어 터미널에서 전체 경로로 실행해야 합니다: askorus-agent path on"))
			}
		}
	}

	if store, err := devices.Open(dir); err == nil {
		if list, _ := store.List(); len(list) > 0 {
			c.line("ok", m("%d paired devices", "짝 맺은 기기 %d개", len(list)))
		} else {
			c.line("warn", m("no paired phone yet: askorus-agent pair", "아직 짝 맺은 폰이 없습니다: askorus-agent pair"))
		}
	}

	if len(cfg.Folders) == 0 {
		c.line("warn", m("no folders for new sessions: askorus-agent folders add PATH", "새 세션에 쓸 폴더가 없습니다: askorus-agent folders add 경로"))
	}
	for _, f := range cfg.Folders {
		if info, err := os.Stat(f.Path); err != nil || !info.IsDir() {
			c.line("bad", m("folder %q is missing: %s", "%q 폴더가 없습니다: %s", f.ID, f.Path))
			continue
		}
		srv := ""
		if f.Server.Enabled {
			srv = " (kept open)"
		}
		c.line("ok", m("folder %q: %s%s", "폴더 %q: %s%s", f.ID, f.Path, srv))
	}

	switch s := sleepAfterAC(); {
	case s == 0:
		c.line("ok", m("the PC does not sleep on AC power", "PC가 전원 연결 중에는 잠들지 않습니다"))
	case s > 0 && !cfg.KeepAwake:
		c.line("warn", m("the PC sleeps after %d minutes idle; a sleeping PC cannot be reached from the phone. Change the power settings, or set \"keepAwake\": true in %s",
			"PC가 %d분 동안 쓰지 않으면 잠듭니다. 잠든 PC에는 폰에서 닿을 수 없습니다. 전원 설정을 바꾸거나 %s에 \"keepAwake\": true를 넣으세요",
			s/60, filepath.Join(dir, config.FileName)))
	case s > 0:
		c.line("ok", m("the PC sleeps after %d minutes idle, but the agent keeps it awake while sessions run", "PC가 %d분 뒤 잠들지만, 세션이 도는 동안은 에이전트가 깨워 둡니다", s/60))
	}

	for _, tip := range setupTips(cfg) {
		c.line("info", tip)
	}

	fmt.Fprintln(out)
	if c.bad > 0 {
		say(out, m("%d problems, %d warnings", "문제 %d개, 주의 %d개", c.bad, c.warn))
		return errors.New("check found problems")
	}
	say(out, m("ready (%d warnings)", "준비됨 (주의 %d개)", c.warn))
	return nil
}

// suggestServePort picks an HTTPS port tailscale serve does not use yet,
// preferring the agent's own port number.
func suggestServePort(ctx context.Context, localPort int) int {
	used := map[int]bool{}
	if c, err := tailnet.New(); err == nil {
		if ports, err := c.ServePorts(ctx); err == nil {
			for _, p := range ports {
				used[p] = true
			}
		}
	}
	for p := localPort; p < localPort+100; p++ {
		if !used[p] {
			return p
		}
	}
	return localPort
}

// uninstallAutostart, uninstallPath and uninstallJudgeCleanup are
// replaced in tests so they never touch the real logon entry or PATH, or
// run the real opencode against the real judge folder.
var (
	uninstallAutostart    = autostartRemove
	uninstallPath         = userPathRemove
	uninstallJudgeCleanup = cleanupJudgeFolder
)

// cleanupJudgeTimeout bounds uninstall's cleanup of the judge folder.
const cleanupJudgeTimeout = 5 * time.Minute

// oldJudgeSchemaFile is the judge schema file codex read until C6d (#24),
// in the agent folder; uninstall still removes it.
const oldJudgeSchemaFile = "judge-schema-v1.json"

// cleanupJudgeFolder deletes the one-off conversations OpenCode left in the
// judge folder, as a comparison does after its OpenCode calls. With no
// refs, CleanupJudge deletes only those unchanged for over 10 minutes, so
// one from the last minutes before uninstall can stay (S3 decision 5). A
// missing folder or a missing opencode leaves nothing to delete.
func cleanupJudgeFolder(ctx context.Context, cfg config.Config, folder string) error {
	if st, err := os.Stat(folder); err != nil || !st.IsDir() {
		return nil
	}
	ctx, cancel := context.WithTimeout(ctx, cleanupJudgeTimeout)
	defer cancel()
	err := opencode.New(opencode.Options{Path: cfg.OpenCodePath}).CleanupJudge(ctx, folder, nil)
	if core.ErrorCode(err) == core.ErrCodeAgentUnavailable {
		return nil // no opencode here: it left no conversation
	}
	return err
}

// uninstallCmd undoes what installing set up: the background agent, the
// logon entry, the agent's own tailscale serve port and the device token
// hashes. Other serve settings and Claude Code are left alone.
func uninstallCmd(dir string, args []string, in io.Reader, out io.Writer) error {
	fs := flag.NewFlagSet("uninstall", flag.ContinueOnError)
	yes := fs.Bool("yes", false, "do not ask")
	all := fs.Bool("all", false, "also delete settings and logs (the whole agent folder)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	cfg, err := config.Load(dir)
	if err != nil {
		return err
	}
	ctx := context.Background()
	ti := lookTailnet(ctx, cfg.Port)
	plan := []msg{
		m("stop the agent if it runs", "돌고 있으면 에이전트를 멈춤"),
		m("remove the logon entry", "로그인할 때 켜기를 없앰"),
		m("disconnect every paired phone (delete %s)", "짝 맺은 폰을 모두 끊음 (%s 삭제)", devices.FileName),
	}
	exeFolder, _ := exeDir()
	onPath := exeFolder != "" && userPathHas(exeFolder)
	if onPath {
		plan = append(plan, m("remove %s from this user's PATH", "이 사용자의 PATH에서 %s를 뺌", exeFolder))
	}
	if ti.served {
		plan = append(plan, m("turn off tailscale serve on https://%s:%d (only this port)", "tailscale serve의 https://%s:%d만 끔 (이 포트만)", ti.serve.Host, ti.serve.HTTPSPort))
	}
	judgeFolder := ""
	if ask, err := cfg.AskFolder(); err == nil {
		judgeFolder = core.JudgeFolder(ask)
		plan = append(plan, m("delete OpenCode's leftover judge conversations in the judge folder", "판정 폴더에 남은 OpenCode 판정 대화를 지움"))
	}
	// The judge schema file of the judging days (codex's --output-schema,
	// until C6d, #24) may still be on a PC: uninstall removes it too.
	schemaFile := filepath.Join(dir, oldJudgeSchemaFile)
	synthSchemaFile := filepath.Join(dir, core.SynthSchemaFile)
	refineSchemaFile := filepath.Join(dir, core.RefineSchemaFile)
	plan = append(plan, m("delete %s", "%s 삭제", schemaFile), m("delete %s", "%s 삭제", synthSchemaFile), m("delete %s", "%s 삭제", refineSchemaFile))
	if *all {
		plan = append(plan, m("delete %s", "%s 삭제", dir))
	}
	say(out, m("This will:", "다음을 합니다:"))
	for _, p := range plan {
		fmt.Fprintf(out, "  - %s / %s\n", p.en, p.ko)
	}
	if !*yes {
		yesNoPrompt(out, m("Type y and press Enter to go ahead. Just Enter changes nothing.",
			"진행하려면 y를 누르고 Enter, 그만두려면 그냥 Enter를 누르세요."))
		line, _ := bufio.NewReader(in).ReadString('\n')
		if !yesAnswers[normalizeAnswer(line)] {
			say(out, m("nothing was changed", "아무것도 바꾸지 않았습니다"))
			return nil
		}
	}
	if agentListening(cfg.Port) {
		if err := shutdownCmd(dir, out); err != nil {
			return err
		}
	}
	if err := uninstallAutostart(); err != nil {
		return err
	}
	if onPath {
		if _, err := uninstallPath(exeFolder); err != nil {
			return err
		}
	}
	for _, f := range []string{devices.FileName, "pairing.json", "pairing-request.json", "pairing-decision.json", "pairing-result.json", "pairing-qr.png"} {
		os.Remove(filepath.Join(dir, f))
	}
	// The agent is stopped, so none of its one-off calls runs. Without
	// refs, CleanupJudge leaves the conversations of the last 10 minutes,
	// such as those of another agent process that still runs.
	if judgeFolder != "" {
		if err := uninstallJudgeCleanup(ctx, cfg, judgeFolder); err != nil {
			say(out, m("could not delete every OpenCode judge conversation (%s); they stay in %s, which only the agent uses",
				"OpenCode 판정 대화를 다 지우지 못했습니다 (%s). 에이전트만 쓰는 %s에 남아 있습니다", errCode(err), judgeFolder))
		}
	}
	os.Remove(schemaFile)
	os.Remove(synthSchemaFile)
	os.Remove(refineSchemaFile)
	if ti.served {
		if c, err := tailnet.New(); err == nil {
			if err := c.ServeOff(ctx, ti.serve.HTTPSPort); err != nil {
				say(out, m("could not turn off tailscale serve (%v); run: tailscale serve --https=%d off",
					"tailscale serve를 끄지 못했습니다 (%v). 직접 실행하세요: tailscale serve --https=%d off", err, ti.serve.HTTPSPort))
			}
		}
	}
	if *all {
		if err := os.RemoveAll(dir); err != nil {
			return err
		}
	}
	say(out, m("uninstalled; delete askorus-agent.exe yourself", "지웠습니다. askorus-agent.exe 파일은 직접 지우세요"))
	return nil
}

// errCode is the code of an adapter's error, or "internal"; its Detail is
// never printed.
func errCode(err error) string {
	if c := core.ErrorCode(err); c != "" {
		return c
	}
	return core.ErrCodeInternal
}

// funnelWatch ends serve when tailscale funnel opens the agent port to
// the internet while it runs (design 보안: Funnel이면 시작을 거부).
func funnelWatch(ctx context.Context, port int, every time.Duration, onFunnel func()) {
	t := time.NewTicker(every)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
		if ti := lookTailnet(ctx, port); ti.served && ti.serve.Funnel {
			onFunnel()
			return
		}
	}
}

// awakeWatch keeps the PC awake while a background session or a folder
// server runs, when the settings ask for it.
func awakeWatch(ctx context.Context, dir string, a core.Adapter, srv core.Server, every time.Duration) {
	// Windows keeps the "stay awake" request per thread.
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	t := time.NewTicker(every)
	defer t.Stop()
	defer keepAwake(false)
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
		cfg, err := config.Load(dir)
		if err != nil || !cfg.KeepAwake {
			keepAwake(false)
			continue
		}
		busy := len(srv.ServerFolders()) > 0
		if !busy {
			if res, err := a.ListSessions(ctx); err == nil {
				for _, s := range res.Sessions {
					if s.State == core.StateRunning {
						busy = true
						break
					}
				}
			}
		}
		keepAwake(busy)
	}
}
