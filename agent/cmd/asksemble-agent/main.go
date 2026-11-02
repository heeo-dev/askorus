// Command asksemble-agent is the Asksemble PC agent.
package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"text/tabwriter"
	"time"
	"unicode/utf8"

	"github.com/asksemble/asksemble/agent/internal/adapter/claude"
	"github.com/asksemble/asksemble/agent/internal/adapter/codex"
	"github.com/asksemble/asksemble/agent/internal/adapter/opencode"
	"github.com/asksemble/asksemble/agent/internal/api"
	"github.com/asksemble/asksemble/agent/internal/compares"
	"github.com/asksemble/asksemble/agent/internal/config"
	"github.com/asksemble/asksemble/agent/internal/core"
	"github.com/asksemble/asksemble/agent/internal/devices"
	"github.com/asksemble/asksemble/agent/internal/hidden"
	"github.com/asksemble/asksemble/agent/internal/logx"
	"github.com/asksemble/asksemble/agent/internal/pairing"
	"github.com/asksemble/asksemble/agent/internal/tailnet"
)

// version is set at release time with -ldflags "-X main.version=...".
var version = "0.1.0-dev"

const usage = `Asksemble PC agent (not an official Anthropic product)
Asksemble PC 에이전트 (Anthropic의 공식 제품이 아닙니다)

Usage / 사용법:
  asksemble-agent serve [-port N] [-test-page]
      start the API on 127.0.0.1
      127.0.0.1에서 API를 엽니다
      (-test-page: phone-browser test page at /test/ / 폰 브라우저 시험 페이지를 /test/에 엽니다)
  asksemble-agent pair [-host H] [-port N] [-light-terminal] [-test-page]
      show a QR code for 5 minutes to pair a phone
      폰과 짝을 맺는 QR을 5분 동안 보여 줍니다
      (-test-page: the QR opens the test page / QR이 시험 페이지를 엽니다)
  asksemble-agent sessions [-state S] [-q TEXT] [-folder DIR] [-json]
      print the session list
      세션 목록을 보여 줍니다
  asksemble-agent info
      print what the agent can do
      에이전트가 할 수 있는 일을 보여 줍니다
  asksemble-agent devices [list]
      list paired devices
      짝을 맺은 기기를 보여 줍니다
  asksemble-agent devices revoke ID|NAME
      disconnect a device
      기기 연결을 끊습니다
  asksemble-agent folders [list] | folders add [-id ID] PATH | folders remove ID
      folders new sessions may start in
      새 세션을 열 수 있는 폴더
  asksemble-agent folders serve ID on|off [-spawn same-dir|worktree] [-capacity N]
      keep a folder open so the Claude app can start sessions there (off by default)
      폴더를 열어 두어 Claude 앱에서 바로 새 세션을 만들게 합니다 (기본은 꺼짐)
  asksemble-agent open SESSION_ID
      wake a session and print its remote link
      세션을 켜고 원격 링크를 보여 줍니다
  asksemble-agent new -folder ID [-name N] [-model M] [-effort E]
      start a new remote session
      새 원격 세션을 시작합니다
  asksemble-agent ask [-agents claude,codex,opencode] [-tier quick|standard|deep] [-follow ID,ID] QUESTION
      ask several AIs at once, answer only (web search on, no file writes)
      여러 AI에게 한 번에 묻습니다 (답만, 웹 검색 켬, 파일 쓰기 없음)
  asksemble-agent compare -q FILE -answer AI=FILE ... [-model AI=MODEL] [-earlier FILE] [-judges A,B] [-json]
          [-synth [-writer AI] [-tier quick|standard|deep]]
      split the answers into sentences and have the AIs judge each other's (the judging AI's company receives the question and the answers)
      답을 문장으로 나눠 AI끼리 판정합니다 (판정하는 AI의 회사가 질문과 답을 받습니다)
      -synth: then one AI writes a combined answer (its company receives the question, every answer and the results)
      -synth: 그다음 AI 하나가 종합 답변을 씁니다 (그 AI의 회사가 질문, 모든 답, 비교 결과를 받습니다)
  asksemble-agent stop SESSION_ID
      stop a background session
      백그라운드 세션을 멈춥니다
  asksemble-agent rm SESSION_ID
      remove a session from the list (the transcript stays)
      세션을 목록에서 지웁니다 (대화 기록은 남습니다)
  asksemble-agent hidden [list | unhide SESSION_ID]
      sessions removed from the list
      목록에서 지운 세션
  asksemble-agent check
      check everything the agent needs and print fixes
      에이전트에 필요한 것을 점검하고 고칠 방법을 보여 줍니다
  asksemble-agent start [-test-page] | shutdown | status
      run the agent in the background, stop it, or show whether it runs
      에이전트를 백그라운드로 켜고, 끄고, 상태를 봅니다
  asksemble-agent autostart on [-test-page] | off | status
      start the agent at logon, with normal rights
      로그인할 때 에이전트를 일반 권한으로 켭니다
  asksemble-agent path on | off | status
      put the folder of this asksemble-agent.exe on this user's PATH
      이 asksemble-agent.exe가 있는 폴더를 이 사용자의 PATH에 넣습니다
  asksemble-agent uninstall [-yes] [-all]
      undo the setup (logon entry, PATH entry, the agent's serve port, paired phones)
      설치한 것을 되돌립니다 (자동 시작, PATH, 에이전트 serve 포트, 짝 맺은 폰)
  asksemble-agent version

Settings live in %s (override with ASKSEMBLE_AGENT_DIR).
설정 폴더: %s (ASKSEMBLE_AGENT_DIR로 바꿀 수 있습니다)
`

func main() {
	if err := run(os.Args[1:], os.Stdin, os.Stdout); err != nil {
		printError(os.Stderr, err)
		os.Exit(1)
	}
}

func run(args []string, in io.Reader, out io.Writer) error {
	dir, err := config.Dir()
	if err != nil {
		return err
	}
	if len(args) == 0 || args[0] == "-h" || args[0] == "--help" || args[0] == "help" {
		fmt.Fprintf(out, usage, dir, dir)
		return nil
	}
	switch args[0] {
	case "version":
		fmt.Fprintln(out, "asksemble-agent", version)
		return nil
	case "serve":
		return serve(dir, args[1:])
	case "pair":
		return pair(dir, args[1:], in, out)
	case "sessions":
		return sessions(dir, args[1:], out)
	case "info":
		return info(dir, out)
	case "devices":
		return devicesCmd(dir, args[1:], out)
	case "folders":
		return foldersCmd(dir, args[1:], out)
	case "open":
		return openCmd(dir, args[1:], out)
	case "new":
		return newCmd(dir, args[1:], out)
	case "ask":
		return askCmd(dir, args[1:], out)
	case "compare":
		return compareCmd(dir, args[1:], in, out)
	case "stop":
		return stopCmd(dir, args[1:], out)
	case "rm":
		return rmCmd(dir, args[1:], out)
	case "hidden":
		return hiddenCmd(dir, args[1:], out)
	case "start":
		return startCmd(dir, args[1:], out)
	case "shutdown":
		return shutdownCmd(dir, out)
	case "status":
		return statusCmd(dir, out)
	case "autostart":
		return autostartCmd(args[1:], out)
	case "path":
		return pathCmd(args[1:], out)
	case "check":
		return checkCmd(dir, out)
	case "uninstall":
		return uninstallCmd(dir, args[1:], in, out)
	}
	return fail("unknown command %q (try: asksemble-agent help)", "모르는 명령 %q입니다 (asksemble-agent help로 확인하세요)", args[0])
}

func newAdapter(cfg config.Config) (*claude.Adapter, error) {
	claudeDir, err := claude.DefaultClaudeDir()
	if err != nil {
		return nil, err
	}
	return claude.New(claude.Options{
		ClaudeDir: claudeDir, ClaudePath: cfg.ClaudePath,
		DefaultModel: cfg.DefaultModel, DefaultEffort: cfg.DefaultEffort,
		AskTiers: cfg.Ask.Tiers[claude.Name],
	}), nil
}

// judgeMinVersions maps each AI to the oldest version of its CLI the judge
// command was tested with. An AI not listed never judges.
func judgeMinVersions() map[string]string {
	return map[string]string{
		claude.Name:   claude.JudgeMinVersion,
		codex.Name:    codex.JudgeMinVersion,
		opencode.Name: opencode.JudgeMinVersion,
	}
}

// judgeGate decides which AIs judge, as serve's API does: the settings'
// judge block, the tested versions and the judge folder inside the ask
// folder ("" without an ask folder, and then no AI judges).
func judgeGate(cfg config.Config) compares.Gate {
	g := compares.Gate{Settings: cfg.Judge, MinVersions: judgeMinVersions()}
	if ask, err := cfg.AskFolder(); err == nil {
		g.Folder = core.JudgeFolder(ask)
	}
	return g
}

// askOnlyAdapters are the AIs that only answer questions for now (Phase 2;
// their sessions come in Phase 3). A missing one is reported, not an error.
func askOnlyAdapters(cfg config.Config) []core.Adapter {
	return []core.Adapter{
		codex.New(codex.Options{Path: cfg.CodexPath, Tiers: cfg.Ask.Tiers[codex.Name]}),
		opencode.New(opencode.Options{Path: cfg.OpenCodePath, Tiers: cfg.Ask.Tiers[opencode.Name]}),
	}
}

// foldersFrom rereads the allowed folders from the settings file, so
// `folders add` applies to a running server. A broken file keeps the last
// good list.
func foldersFrom(dir string, initial []config.Folder) func() []config.Folder {
	var mu sync.Mutex
	last := initial
	return func() []config.Folder {
		mu.Lock()
		defer mu.Unlock()
		if cfg, err := config.Load(dir); err == nil {
			last = cfg.Folders
		}
		return last
	}
}

func loadAdapter(dir string) (*claude.Adapter, error) {
	cfg, err := config.Load(dir)
	if err != nil {
		return nil, err
	}
	return newAdapter(cfg)
}

// tailnetInfo is what serve and pair learn from the tailscale CLI.
type tailnetInfo struct {
	status  tailnet.Status
	serve   tailnet.Serve
	served  bool  // tailscale serve proxies to our port
	problem error // nil when tailscale could be read
}

func lookTailnet(ctx context.Context, localPort int) tailnetInfo {
	c, err := tailnet.New()
	if err != nil {
		return tailnetInfo{problem: fail("tailscale CLI not found", "tailscale 명령을 찾지 못했습니다")}
	}
	var ti tailnetInfo
	if ti.status, err = c.Status(ctx); err != nil {
		ti.problem = fail("tailscale status failed", "tailscale status가 실패했습니다")
		return ti
	}
	if !ti.status.Running {
		ti.problem = fail("tailscale is not connected", "Tailscale이 연결되어 있지 않습니다")
		return ti
	}
	if ti.serve, ti.served, err = c.ServeFor(ctx, localPort); err != nil {
		ti.problem = fail("tailscale serve status failed", "tailscale serve status가 실패했습니다")
	}
	return ti
}

func serve(dir string, args []string) error {
	fs := flag.NewFlagSet("serve", flag.ContinueOnError)
	port := fs.Int("port", 0, "port on 127.0.0.1 (default from settings)")
	testPage := fs.Bool("test-page", false, "serve the phone-browser test page at /test/")
	if err := fs.Parse(args); err != nil {
		return err
	}
	// N3: every session the agent starts inherits its rights.
	if claude.SelfElevated() {
		return errElevated()
	}
	cfg, err := config.Load(dir)
	if err != nil {
		return err
	}
	if *port != 0 {
		cfg.Port = *port
		if err := cfg.Validate(); err != nil {
			return err
		}
	}
	logger, closeLog, err := openLog(dir)
	if err != nil {
		return err
	}
	defer closeLog()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	// Tailscale tells us the owner account and our MagicDNS name. Without
	// it the API still works locally, but phones cannot pair.
	ti := lookTailnet(ctx, cfg.Port)
	if ti.served && ti.serve.Funnel {
		return fail("tailscale funnel exposes this port to the internet; turn it off first (tailscale funnel --https=%d off)",
			"tailscale funnel이 이 포트를 인터넷에 열어 두었습니다. 먼저 끄세요 (tailscale funnel --https=%d off)", ti.serve.HTTPSPort)
	}
	owner := cfg.TailscaleOwner
	if owner == "" {
		owner = ti.status.OwnerLogin
	}
	hosts := append([]string{}, cfg.ServeHosts...)
	if ti.status.DNSName != "" {
		hosts = append(hosts, ti.status.DNSName)
	}
	var be biError
	if errors.As(ti.problem, &be) {
		say(os.Stderr, msg{
			"warning: " + be.en + "; phones cannot pair until it is fixed",
			"경고: " + be.ko + ". 고칠 때까지 폰과 짝을 맺을 수 없습니다",
		})
	} else if !ti.served {
		say(os.Stderr, m("note: tailscale serve does not point at 127.0.0.1:%d yet, so phones cannot reach the agent",
			"참고: tailscale serve가 아직 127.0.0.1:%d를 가리키지 않아 폰에서 에이전트에 닿을 수 없습니다", cfg.Port))
	}

	store, err := devices.Open(dir)
	if err != nil {
		return err
	}
	hid, err := hidden.Open(dir)
	if err != nil {
		return err
	}
	adapter, err := newAdapter(cfg)
	if err != nil {
		return err
	}
	host, _ := os.Hostname()
	rec := newReconciler(dir, adapter, logger)
	adapter.OnServerState(func(folder string, st core.ServeStatus) {
		logger.Info("folder server", "folder", folder, "state", st.State, "restarts", st.Restarts, "err", st.Error)
	})
	// Without an ask folder asks are off; remote sessions still work.
	askFolder, err := cfg.AskFolder()
	if err != nil {
		logger.Error("no ask folder; asks are off", "err", err.Error())
		askFolder = ""
	}
	askOnly := askOnlyAdapters(cfg)
	apiSrv := api.New(api.Options{
		Version:         version,
		Hostname:        host,
		Adapters:        append([]core.Adapter{adapter}, askOnly...),
		Devices:         store,
		Pairer:          &pairing.Pairer{Dir: dir, Devices: store},
		Logger:          logger,
		ServeHosts:      hosts,
		TailscaleOwner:  owner,
		HiddenFolders:   cfg.HiddenFolders,
		TestPage:        *testPage,
		Hidden:          hid,
		Folders:         foldersFrom(dir, cfg.Folders),
		SetFolderServer: setFolderServer(dir),
		ApplyFolders:    rec.trigger,
		HistoryFolders:  historyFoldersOn(dir),
		AddFolder:       addFolder(dir),
		AskFolder:       askFolder,
		// Comparison judging (P2-3): the AIs judge in <askFolder>\judge,
		// each only with a CLI as new as its judge command was tested with.
		Judge:            cfg.Judge,
		JudgeMinVersions: judgeMinVersions(),
	})
	srv := api.HTTPServer(apiSrv.Handler())
	ln, err := api.Listen(cfg.Port)
	if err != nil {
		return fail("cannot listen on 127.0.0.1:%d (%v)", "127.0.0.1:%d를 열 수 없습니다 (%v)", cfg.Port, err)
	}
	if list, _ := store.List(); len(list) == 0 {
		say(os.Stderr, m("no paired devices yet: run `asksemble-agent pair` in another window",
			"아직 짝을 맺은 기기가 없습니다. 다른 창에서 `asksemble-agent pair`를 실행하세요"))
	}
	logger.Info("listening", "addr", ln.Addr().String(), "version", version, "tailscale_served", ti.served)
	_ = config.WriteFileAtomic(filepath.Join(dir, pidFile), []byte(strconv.Itoa(os.Getpid())+"\n"))
	defer os.Remove(filepath.Join(dir, pidFile))
	say(os.Stderr, m("asksemble-agent %s listening on http://%s (Ctrl+C to stop)",
		"asksemble-agent %s가 http://%s에서 기다립니다 (Ctrl+C로 멈춤)", version, ln.Addr()))
	if *testPage {
		say(os.Stderr, m("test page is on at /test/ (for trying pairing in a phone browser; leave it off otherwise)",
			"시험 페이지가 /test/에 켜져 있습니다 (폰 브라우저로 짝 맺기를 시험할 때만 켜세요)"))
	}

	go adapter.Warm(ctx)
	for _, a := range askOnly {
		go a.Info(ctx) // reads each version once, so the first phone request is fast
	}
	recCtx, stopRec := context.WithCancel(ctx)
	go rec.run(recCtx)
	exposed := make(chan struct{})
	go funnelWatch(recCtx, cfg.Port, time.Minute, func() { close(exposed) })
	go awakeWatch(recCtx, dir, adapter, adapter, time.Minute)
	stopReq := listenStop(cfg.Port) // `asksemble-agent shutdown`
	errc := make(chan error, 1)
	go func() { errc <- srv.Serve(ln) }()
	var serveErr error
	select {
	case err := <-errc:
		if !errors.Is(err, http.ErrServerClosed) {
			serveErr = err
		}
	case <-ctx.Done():
	case <-stopReq:
		logger.Info("stop requested")
	case <-exposed:
		logger.Error("tailscale funnel exposes the agent port; stopping")
		serveErr = fail("tailscale funnel now exposes the agent port to the internet; stopped. Turn it off first",
			"tailscale funnel이 에이전트 포트를 인터넷에 열어서 멈췄습니다. 먼저 끄세요")
	}
	shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	logger.Info("stopping")
	err = srv.Shutdown(shutdown)
	apiSrv.Close() // ends asks still running, with their AI processes
	// Folder servers end with the agent; Ctrl+C lets each clean up.
	stopRec()
	<-rec.done
	if n := len(adapter.ServerFolders()); n > 0 {
		say(os.Stderr, m("stopping %d folder servers…", "폴더 서버 %d개를 멈추는 중…", n))
		adapter.StopAllServers()
	}
	if serveErr != nil {
		return serveErr
	}
	return err
}

// openLog writes JSON lines to stderr and <dir>/logs/agent.log.
func openLog(dir string) (*slog.Logger, func(), error) {
	logDir := filepath.Join(dir, "logs")
	if err := os.MkdirAll(logDir, 0o700); err != nil {
		return nil, nil, err
	}
	f, err := os.OpenFile(filepath.Join(logDir, "agent.log"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return nil, nil, err
	}
	return logx.New(io.MultiWriter(os.Stderr, f), slog.LevelInfo), func() { f.Close() }, nil
}

// PairURL is what the QR code holds. The app also opens it as a deep link.
func PairURL(host string, port int, code string) string {
	q := url.Values{}
	q.Set("v", "1")
	q.Set("host", host)
	q.Set("port", strconv.Itoa(port))
	q.Set("code", code)
	return "asksemble://pair?" + q.Encode()
}

// TestPageURL opens the test page with the code in the fragment, which the
// browser never sends to the server.
func TestPageURL(host string, port int, code string) string {
	return "https://" + net.JoinHostPort(host, strconv.Itoa(port)) + "/test/#code=" + url.QueryEscape(code)
}

func pair(dir string, args []string, in io.Reader, out io.Writer) error {
	fs := flag.NewFlagSet("pair", flag.ContinueOnError)
	hostFlag := fs.String("host", "", "MagicDNS name the phone connects to (default from tailscale)")
	portFlag := fs.Int("port", 0, "HTTPS port of tailscale serve (default from tailscale serve status)")
	light := fs.Bool("light-terminal", false, "draw the QR code for a light terminal background")
	testPage := fs.Bool("test-page", false, "make the QR open the test page (serve must run with -test-page)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	cfg, err := config.Load(dir)
	if err != nil {
		return err
	}
	conn, err := net.DialTimeout("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(cfg.Port)), time.Second)
	if err != nil {
		return fail("the agent is not running on 127.0.0.1:%d; start `asksemble-agent serve` first",
			"에이전트가 127.0.0.1:%d에서 돌고 있지 않습니다. 먼저 `asksemble-agent serve`를 실행하세요", cfg.Port)
	}
	conn.Close()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	host, port := *hostFlag, *portFlag
	if host == "" || port == 0 {
		ti := lookTailnet(ctx, cfg.Port)
		if ti.problem != nil {
			return ti.problem
		}
		if !ti.served {
			return fail("tailscale serve does not point at the agent yet. Pick a free HTTPS port and run: tailscale serve --bg --https=<port> http://127.0.0.1:%d",
				"tailscale serve가 아직 에이전트를 가리키지 않습니다. 빈 HTTPS 포트를 골라 실행하세요: tailscale serve --bg --https=<포트> http://127.0.0.1:%d", cfg.Port)
		}
		if ti.serve.Funnel {
			return fail("tailscale funnel is on for the agent port; turn it off first",
				"에이전트 포트에 tailscale funnel이 켜져 있습니다. 먼저 끄세요")
		}
		if host == "" {
			host = ti.serve.Host
		}
		if port == 0 {
			port = ti.serve.HTTPSPort
		}
	}

	code, w, err := pairing.Open(dir, time.Now())
	if err != nil {
		return err
	}
	defer pairing.Close(dir)
	link := PairURL(host, port, code)
	scan := m("Scan this with the Asksemble app (Settings > Add PC):", "Asksemble 앱으로 이 QR을 찍으세요(설정 > PC 추가):")
	if *testPage {
		link = TestPageURL(host, port, code)
		scan = m("Scan this with the phone camera; it opens the test page:", "폰 카메라로 이 QR을 찍으세요. 시험 페이지가 열립니다:")
	}
	pngPath := filepath.Join(dir, "pairing-qr.png")
	if png, err := qrPNG(link); err == nil && os.WriteFile(pngPath, png, 0o600) == nil {
		defer os.Remove(pngPath)
	} else {
		pngPath = ""
	}

	say(out, scan)
	fmt.Fprintln(out)
	if err := printQR(out, link, !*light); err != nil {
		return err
	}
	fmt.Fprintf(out, "\nPC:                https://%s:%d\n", host, port)
	fmt.Fprintf(out, "Link / 링크:       %s\n", link)
	fmt.Fprintf(out, "Code / 코드:       %s\n", groupCode(code))
	if pngPath != "" {
		fmt.Fprintf(out, "Image / 이미지:    %s\n", pngPath)
	}
	expires := w.ExpiresAt.Local().Format("15:04:05")
	fmt.Fprintf(out, "Expires / 만료:    %s (Ctrl+C to cancel / Ctrl+C로 취소)\n\n", expires)

	lines := make(chan string)
	go func() {
		sc := bufio.NewScanner(in)
		for sc.Scan() {
			lines <- sc.Text()
		}
		close(lines)
	}()

	tick := time.NewTicker(300 * time.Millisecond)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			say(out, m("cancelled", "취소했습니다"))
			return nil
		case <-tick.C:
		}
		if req, ok := pairing.PendingRequest(dir, w.ID); ok {
			return decide(ctx, dir, w.ID, req, lines, out)
		}
		if res, ok := pairing.ResultFor(dir, w.ID); ok {
			return reportResult(res, out)
		}
		if !pairing.WindowOpen(dir, w.ID) {
			return fail("the pairing window was closed (too many wrong codes); run pair again",
				"틀린 코드가 너무 많아 짝 맺기 창이 닫혔습니다. pair를 다시 실행하세요")
		}
		if time.Now().After(w.ExpiresAt) {
			return fail("the code expired; run pair again", "코드가 만료됐습니다. pair를 다시 실행하세요")
		}
	}
}

func decide(ctx context.Context, dir, id string, req pairing.Request, lines <-chan string, out io.Writer) error {
	say(out, m("Allow %q to connect? (Tailscale account %s, address %s)",
		"%q 기기의 연결을 허용할까요? (Tailscale 계정 %s, 주소 %s)", req.DeviceName, req.Login, req.RemoteIP))
	fmt.Fprint(out, "[y/N]: ")
	var answer string
	select {
	case <-ctx.Done():
		pairing.Decide(dir, id, false)
		fmt.Fprintln(out)
		say(out, m("cancelled", "취소했습니다"))
		return nil
	case line, ok := <-lines:
		if !ok {
			fmt.Fprintln(out)
			say(out, m("(no input)", "(입력 없음)"))
		}
		answer = normalizeAnswer(line)
	}
	allow := yesAnswers[answer]
	if !allow && answer != "" && !noAnswers[answer] {
		say(out, m("(%q is not y; refusing)", "(%q는 y가 아니라서 거절합니다)", answer))
	}
	if err := pairing.Decide(dir, id, allow); err != nil {
		return err
	}
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if res, ok := pairing.ResultFor(dir, id); ok {
			return reportResult(res, out)
		}
		time.Sleep(100 * time.Millisecond)
	}
	return fail("the agent did not confirm; check `asksemble-agent devices`",
		"에이전트가 결과를 알려 주지 않았습니다. `asksemble-agent devices`로 확인하세요")
}

var (
	yesAnswers = map[string]bool{"y": true, "yes": true, "예": true, "네": true}
	noAnswers  = map[string]bool{"n": true, "no": true, "아니오": true, "아니요": true}
)

func reportResult(res pairing.Result, out io.Writer) error {
	if res.OK {
		say(out, m("Paired %s (id %s). Disconnect it any time with: asksemble-agent devices revoke %[2]s",
			"%s 기기와 짝을 맺었습니다 (id %s). 끊으려면 언제든: asksemble-agent devices revoke %[2]s", res.DeviceName, res.DeviceID))
		return nil
	}
	switch res.Reason {
	case "denied":
		say(out, m("Refused. The phone was not paired.", "거절했습니다. 폰과 짝을 맺지 않았습니다."))
		return nil
	case "timeout":
		return fail("no answer in time; run pair again", "제때 답이 없었습니다. pair를 다시 실행하세요")
	}
	return fail("pairing failed (%s)", "짝 맺기에 실패했습니다 (%s)", res.Reason)
}

// normalizeAnswer lowercases a y/N answer. PowerShell pipes can start the
// input with a byte order mark.
func normalizeAnswer(line string) string {
	return strings.ToLower(strings.TrimSpace(strings.TrimPrefix(line, string(rune(0xFEFF)))))
}

func groupCode(code string) string {
	var parts []string
	for len(code) > 4 {
		parts = append(parts, code[:4])
		code = code[4:]
	}
	return strings.Join(append(parts, code), "-")
}

// sessionFilter is the CLI sessions filter: the settings' hidden folders
// and, like GET /v1/sessions, the judge folder inside the ask folder
// (P2-3), where only judges run. The settings' list is never appended to.
func sessionFilter(cfg config.Config, q, folder string) core.Filter {
	f := core.Filter{Query: q, Folder: folder, Hidden: cfg.HiddenFolders}
	if ask, err := cfg.AskFolder(); err == nil {
		if jf := core.JudgeFolder(ask); jf != "" {
			f.Hidden = slices.Concat(cfg.HiddenFolders, []string{jf})
		}
	}
	return f
}

func sessions(dir string, args []string, out io.Writer) error {
	fs := flag.NewFlagSet("sessions", flag.ContinueOnError)
	state := fs.String("state", "", "comma-separated states: running,stopped,terminal,history,unknown")
	q := fs.String("q", "", "search title, name and last prompt")
	folder := fs.String("folder", "", "only sessions in this folder or below")
	asJSON := fs.Bool("json", false, "print JSON like GET /v1/sessions")
	if err := fs.Parse(args); err != nil {
		return err
	}
	cfg, err := config.Load(dir)
	if err != nil {
		return err
	}
	adapter, err := newAdapter(cfg)
	if err != nil {
		return err
	}
	f := sessionFilter(cfg, *q, *folder)
	if hid, err := hidden.Open(dir); err == nil {
		f.HiddenIDs = hid.Set()
	}
	if *state != "" {
		for _, s := range strings.Split(*state, ",") {
			st := core.State(strings.TrimSpace(s))
			if !core.ValidState(st) {
				return fail("unknown state %q", "모르는 상태 %q입니다", s)
			}
			f.States = append(f.States, st)
		}
	}
	start := time.Now()
	res, err := adapter.ListSessions(context.Background())
	if err != nil {
		return err
	}
	list := f.Apply(res.Sessions)
	took := time.Since(start)

	if *asJSON {
		enc := json.NewEncoder(out)
		enc.SetIndent("", "  ")
		return enc.Encode(api.SessionsResponse{Sessions: list, Warnings: append([]string{}, res.Warnings...)})
	}
	// Korean column names go on their own line: tabwriter counts a Hangul
	// character as one column although terminals draw it two wide.
	fmt.Fprintln(out, "(상태 · 마지막 활동 · 제목 · 폴더 / running 실행 중, stopped 멈춤, terminal 터미널, history 기록만)")
	tw := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "STATE\tLAST ACTIVE\tTITLE\tFOLDER")
	for _, s := range list {
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\n", s.State, s.LastActive.Local().Format("2006-01-02 15:04"), short(s.Title, 40), s.Folder)
	}
	tw.Flush()
	fmt.Fprintln(out)
	summary := m("%d sessions in %d ms", "세션 %d개, %dms", len(list), took.Milliseconds())
	if len(res.Warnings) > 0 {
		w := strings.Join(res.Warnings, ", ")
		summary.en += " (warnings: " + w + ")"
		summary.ko += " (경고: " + w + ")"
	}
	say(out, summary)
	return nil
}

func info(dir string, out io.Writer) error {
	cfg, err := config.Load(dir)
	if err != nil {
		return err
	}
	adapter, err := newAdapter(cfg)
	if err != nil {
		return err
	}
	return printInfo(context.Background(), append([]core.Adapter{adapter}, askOnlyAdapters(cfg)...), judgeGate(cfg), out)
}

// printInfo prints every AI's Info as GET /v1/info's agents shows it:
// with its ask tiers, and for an AI that judges the capability judge and
// its judge model, effort and company (compares.Announce, as the API).
func printInfo(ctx context.Context, adapters []core.Adapter, g compares.Gate, out io.Writer) error {
	infos, _ := compares.Announce(ctx, adapters, g)
	enc := json.NewEncoder(out)
	enc.SetIndent("", "  ")
	return enc.Encode(infos)
}

func devicesCmd(dir string, args []string, out io.Writer) error {
	store, err := devices.Open(dir)
	if err != nil {
		return err
	}
	if len(args) == 0 {
		args = []string{"list"}
	}
	switch args[0] {
	case "list":
		list, err := store.List()
		if err != nil {
			return err
		}
		fmt.Fprintln(out, "(id · 이름 · 짝 맺은 때)")
		tw := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
		fmt.Fprintln(tw, "ID\tNAME\tPAIRED")
		for _, d := range list {
			fmt.Fprintf(tw, "%s\t%s\t%s\n", d.ID, d.Name, d.CreatedAt.Local().Format("2006-01-02 15:04"))
		}
		return tw.Flush()
	case "revoke":
		if len(args) != 2 {
			return fail("usage: asksemble-agent devices revoke ID|NAME", "사용법: asksemble-agent devices revoke ID|이름")
		}
		d, err := store.Revoke(args[1])
		if errors.Is(err, devices.ErrNotFound) {
			return fail("no device %q", "%q 기기가 없습니다", args[1])
		}
		if err != nil {
			return err
		}
		say(out, m("revoked %s (%s)", "%s 기기의 연결을 끊었습니다 (%s)", d.Name, d.ID))
		return nil
	}
	return fail("unknown devices command %q", "모르는 devices 명령 %q입니다", args[0])
}

func short(s string, n int) string {
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	return string([]rune(s)[:n-1]) + "…"
}
