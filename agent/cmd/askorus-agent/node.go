package main

import (
	"bufio"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/heeo-dev/askorus/agent/internal/api"
	"github.com/heeo-dev/askorus/agent/internal/config"
	"github.com/heeo-dev/askorus/agent/internal/tsnode"
)

// The agent's own Tailscale node (#57, package tsnode). `serve` runs it and
// writes what it looks like to nodeFile, so the other commands (pair,
// login, status, check, setup), which are separate processes, can read it.
// They ask the running agent for a new login link or a logout by leaving a
// word in nodeRequestFile. All of it is in the agent's settings folder,
// which only this user reads.
const (
	nodeDir         = "tailnet"         // the node's key, kept between runs
	nodeFile        = "tailnet.json"    // the running node, for the other commands
	nodeRequestFile = "tailnet-request" // "login" or "logout", taken by the running agent
)

// nodeInfo is nodeFile.
type nodeInfo struct {
	Port int `json:"port"`
	PID  int `json:"pid"`
	tsnode.Status
}

// signedIn reports whether phones can reach the node.
func (n nodeInfo) signedIn() bool {
	return n.State == tsnode.StateRunning && n.Name != ""
}

// agentRunning reports whether an agent of this user serves with that
// port number (its stop event or lock is there). Tests replace it.
var agentRunning = agentOnPort

func writeNodeInfo(dir string, n nodeInfo) error {
	b, err := json.MarshalIndent(n, "", "  ")
	if err != nil {
		return err
	}
	return config.WriteFileAtomic(filepath.Join(dir, nodeFile), append(b, '\n'))
}

// readNodeInfo reads what the running agent wrote. It is false when no
// agent runs: a file left by one that crashed says nothing.
func readNodeInfo(dir string) (nodeInfo, bool) {
	var b []byte
	var err error
	// The agent replaces the file on every change of state. On Windows a
	// read at that very moment can fail: read again before saying no agent runs.
	for try := 0; ; try++ {
		b, err = os.ReadFile(filepath.Join(dir, nodeFile))
		if err == nil || os.IsNotExist(err) || try >= 5 {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if err != nil {
		return nodeInfo{}, false
	}
	var n nodeInfo
	if json.Unmarshal(b, &n) != nil || n.Port == 0 || !agentRunning(n.Port) {
		return nodeInfo{}, false
	}
	return n, true
}

func requestNode(dir, what string) error {
	return config.WriteFileAtomic(filepath.Join(dir, nodeRequestFile), []byte(what+"\n"))
}

func takeNodeRequest(dir string) string {
	p := filepath.Join(dir, nodeRequestFile)
	b, err := os.ReadFile(p)
	if err != nil {
		return ""
	}
	os.Remove(p)
	return strings.TrimSpace(string(b))
}

// nodeAPI is the part of tsnode.Node the watch uses; tests put a fake in.
type nodeAPI interface {
	Status(ctx context.Context) (tsnode.Status, error)
	Login(ctx context.Context) error
	Logout(ctx context.Context) error
}

// agentNode is all serve needs of its node.
type agentNode interface {
	nodeAPI
	callerSource
	Listen(port int) (net.Listener, error)
	Close() error
}

// noNodeEnv makes serve run without its Tailscale node: nothing can reach
// the agent then, so it opens nothing and lets nobody in. It is for the
// tests of installing, starting and stopping the agent, which must not sign
// in anywhere or talk to Tailscale's servers (docs/testing.md).
const noNodeEnv = "ASKORUS_TEST_NO_TAILNET"

// stateNoNode is the node state such a run reports.
const stateNoNode = "NoNode"

func startNode(dir, pcName string) (agentNode, error) {
	if os.Getenv(noNodeEnv) == "1" {
		return &noNode{closed: make(chan struct{})}, nil
	}
	return tsnode.Start(filepath.Join(dir, nodeDir), tsnode.HostName(pcName), func(string, ...any) {})
}

// noNode stands where the node would be in a run without one. Its listener
// never gets a connection.
type noNode struct {
	once   sync.Once
	closed chan struct{}
}

func (n *noNode) Status(context.Context) (tsnode.Status, error) {
	return tsnode.Status{State: stateNoNode}, nil
}
func (n *noNode) Login(context.Context) error  { return nil }
func (n *noNode) Logout(context.Context) error { return nil }
func (n *noNode) Caller(context.Context, string) (string, string, bool) {
	return "", "", false
}
func (n *noNode) Listen(int) (net.Listener, error) { return n, nil }
func (n *noNode) Close() error {
	n.once.Do(func() { close(n.closed) })
	return nil
}

// As a listener: Accept waits until the node is closed.
func (n *noNode) Accept() (net.Conn, error) {
	<-n.closed
	return nil, net.ErrClosed
}
func (n *noNode) Addr() net.Addr { return &net.TCPAddr{} }

// nodeWatch follows the node while the agent serves: it keeps the latest
// state for the API (who the owner is, which names the node answers on),
// writes it for the other commands, tells the person at the console what to
// do, and carries out their requests.
type nodeWatch struct {
	node   nodeAPI
	dir    string
	port   int
	logger *slog.Logger
	out    io.Writer // the console
	// quiet is set when the console is a file (a background run): the
	// login link, the account and the device name stay out of it. A
	// link in a log someone shares would let its reader put this PC on
	// their own Tailscale network.
	quiet bool

	mu       sync.Mutex
	st       tsnode.Status
	told     string    // the state line last printed
	noLinkAt time.Time // since when the node needs a login but has no link
	askedAt  time.Time // when a link was last asked for
	fails    int       // status reads that failed in a row
	unsaved  bool      // the state file does not hold st yet (a write failed)
}

// statusFailsMax is how many status reads in a row may fail before the
// watch forgets what it knew: a node that cannot be read vouches for
// nobody, so the API lets no one in until it answers again.
const statusFailsMax = 5

func (w *nodeWatch) status() tsnode.Status {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.st
}

// owner is the account the node is signed in as, and "" until phones can
// reach it: the API lets nobody in before that.
func (w *nodeWatch) owner() string {
	st := w.status()
	if st.State != tsnode.StateRunning {
		return ""
	}
	return st.Owner
}

// hosts are the names and addresses the node answers on.
func (w *nodeWatch) hosts() []string {
	st := w.status()
	if st.Name == "" {
		return nil
	}
	out := []string{st.Name}
	if short, _, ok := strings.Cut(st.Name, "."); ok {
		out = append(out, short)
	}
	return append(out, st.IPs...)
}

func (w *nodeWatch) run(ctx context.Context) {
	t := time.NewTicker(time.Second)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
		w.once(ctx)
	}
}

// once looks at the node one time.
func (w *nodeWatch) once(ctx context.Context) {
	switch takeNodeRequest(w.dir) {
	case "login":
		if err := w.node.Login(ctx); err != nil {
			w.logger.Warn("tailscale login link not made", "err", err.Error())
		}
	case "logout":
		if err := w.node.Logout(ctx); err != nil {
			w.logger.Warn("tailscale logout failed", "err", err.Error())
		} else {
			w.logger.Info("tailscale: signed out on request")
		}
	}
	st, err := w.node.Status(ctx)
	if err != nil {
		w.mu.Lock()
		w.fails++
		if w.fails >= statusFailsMax {
			w.st = tsnode.Status{}
		}
		w.mu.Unlock()
		return
	}
	now := time.Now()
	w.mu.Lock()
	w.fails = 0
	changed := !sameNode(st, w.st)
	w.st = st
	// A node that has to sign in (first run, key expired, signed out) but
	// has no link yet is asked for one, so `check` and `login` can show it.
	needLink := st.State == tsnode.StateNeedsLogin && st.LoginURL == ""
	ask := false
	if !needLink {
		w.noLinkAt = time.Time{}
	} else if w.noLinkAt.IsZero() {
		w.noLinkAt = now
	} else if now.Sub(w.noLinkAt) > 5*time.Second && now.Sub(w.askedAt) > time.Minute {
		w.askedAt, ask = now, true
	}
	w.mu.Unlock()
	if ask {
		_ = w.node.Login(ctx)
	}
	w.mu.Lock()
	write := changed || w.unsaved
	w.mu.Unlock()
	if write {
		// The other commands (login, status, pair) read the state from this
		// file: one that could not be written is written on the next look,
		// or `login` would go on waiting for a sign-in that is done.
		err := writeNodeInfo(w.dir, nodeInfo{Port: w.port, PID: os.Getpid(), Status: st})
		if err != nil {
			w.logger.Warn("tailnet state not written", "err", err.Error())
		}
		w.mu.Lock()
		w.unsaved = err != nil
		w.mu.Unlock()
	}
	if !changed {
		return
	}
	w.tell(st)
}

func sameNode(a, b tsnode.Status) bool {
	return a.State == b.State && a.LoginURL == b.LoginURL && a.Name == b.Name && a.Owner == b.Owner &&
		a.KeyExpiry.Equal(b.KeyExpiry) && slices.Equal(a.IPs, b.IPs)
}

// tell prints what changed for the person at the console, once per state.
// The log gets the state only: the account and the link stay out of it.
func (w *nodeWatch) tell(st tsnode.Status) {
	var line msg
	switch {
	case w.quiet && st.State == tsnode.StateRunning && st.Name != "":
		line = m("signed in to Tailscale; phones reach this PC inside your Tailscale network only (askorus-agent status shows the name)",
			"Tailscale에 로그인됨. 폰은 내 Tailscale 망 안에서만 이 PC에 닿습니다 (이름은 askorus-agent status)")
	case w.quiet && st.State == tsnode.StateNeedsLogin && st.LoginURL != "":
		line = m("this PC is not signed in to Tailscale yet: run askorus-agent login",
			"이 PC가 아직 Tailscale에 로그인되지 않았습니다. 실행: askorus-agent login")
	case st.State == tsnode.StateRunning && st.Name != "":
		line = m("signed in to Tailscale as %s; phones reach this PC as %s:%d, inside your Tailscale network only",
			"Tailscale에 %s 계정으로 로그인됨. 폰은 내 Tailscale 망 안에서만 %s:%d로 이 PC에 닿습니다", st.Owner, st.Name, w.port)
	case st.State == tsnode.StateNeedsLogin && st.LoginURL != "":
		line = m("this PC is not signed in to Tailscale yet. Open this link in a browser (on any device) and sign in: %s  (or run: askorus-agent login)",
			"이 PC가 아직 Tailscale에 로그인되지 않았습니다. 브라우저에서(어느 기기든) 이 링크를 열고 로그인하세요: %s  (또는 실행: askorus-agent login)", st.LoginURL)
	case st.State == tsnode.StateNeedsApproval:
		line = m("signed in, but your Tailscale network asks an admin to approve new devices: approve this PC in the Tailscale admin console",
			"로그인했지만 Tailscale 망이 새 기기의 승인을 요구합니다. Tailscale 관리 화면에서 이 PC를 승인하세요")
	default:
		return
	}
	w.mu.Lock()
	same := w.told == line.en
	w.told = line.en
	w.mu.Unlock()
	if same {
		return
	}
	w.logger.Info("tailnet", "state", st.State)
	say(w.out, line)
}

// callerSource says who is on the other end of a connection the node took.
type callerSource interface {
	Caller(ctx context.Context, remoteAddr string) (login, ip string, ok bool)
}

// viaNode is the only way into the API: it asks the node who sent each
// request and hands that on (api.WithCaller). A request the node cannot
// place goes on without a caller, and the API refuses it.
func viaNode(n callerSource, h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if login, ip, ok := n.Caller(r.Context(), r.RemoteAddr); ok {
			r = api.WithCaller(r, api.Caller{Login: login, IP: ip})
		}
		h.ServeHTTP(w, r)
	})
}

// howToLogin is the command that signs the PC in to Tailscale.
func howToLogin() string { return "askorus-agent login" }

// loginCmd shows the Tailscale login link of the running agent and waits
// until the PC is signed in.
func loginCmd(dir string, out io.Writer) error {
	cfg, err := config.Load(dir)
	if err != nil {
		return err
	}
	if !agentRunning(cfg.Port) {
		return fail("the agent is not running; start it first: %s", "에이전트가 돌고 있지 않습니다. 먼저 켜세요: %s", howToStart())
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	return waitLogin(ctx, dir, out, loginWait)
}

// loginWait is how long login and setup wait for the person to sign in.
const loginWait = 15 * time.Minute

// loginPoll is how often waitLogin reads the node's state; tests shorten it.
var loginPoll = 500 * time.Millisecond

// waitLogin shows the login link (and a QR code of it, to open on the
// phone) and returns when the node is signed in.
func waitLogin(ctx context.Context, dir string, out io.Writer, limit time.Duration) error {
	deadline := time.Now().Add(limit)
	shown, asked, approval, missed := "", false, false, 0
	for {
		n, ok := readNodeInfo(dir)
		if ok {
			missed = 0
		}
		switch {
		case !ok:
			// One look that finds nothing can be the moment the agent replaces its
			// state file; three in a row is an agent that does not run.
			if missed++; missed < 3 {
				break
			}
			return fail("the agent is not running; start it first: %s", "에이전트가 돌고 있지 않습니다. 먼저 켜세요: %s", howToStart())
		case n.signedIn():
			say(out, m("signed in to Tailscale as %s. This PC is %s there.", "Tailscale에 %s 계정으로 로그인했습니다. 망에서 이 PC의 이름은 %s입니다.", n.Owner, n.Name))
			if shown == "" && !approval {
				// It was signed in already: the person may be here to change the account.
				say(out, m("to use another Tailscale account: askorus-agent logout, then askorus-agent login (the phone's app has to use the same account, and pairs again)",
					"다른 Tailscale 계정을 쓰려면: askorus-agent logout 뒤 askorus-agent login (폰의 앱도 같은 계정이어야 하고, 다시 짝 맺습니다)"))
			}
			return nil
		case n.State == tsnode.StateNeedsApproval:
			if !approval {
				approval = true
				say(out, m("Signed in. Your Tailscale network asks an admin to approve new devices: approve this PC in the Tailscale admin console (waiting).",
					"로그인했습니다. Tailscale 망이 새 기기의 승인을 요구합니다. Tailscale 관리 화면에서 이 PC를 승인하세요 (기다리는 중)."))
			}
		case n.LoginURL != "" && n.LoginURL != shown:
			if shown == "" {
				say(out, m("Askorus connects your phone and this PC through Tailscale: a private network that only your own devices can join (a service by Tailscale Inc., free for personal use). No port of this PC is opened to the internet and no server of ours is in between. Tailscale learns your account, the device names and when they connect, not your questions or answers. More: the README (What is Tailscale?).",
					"Askorus는 폰과 이 PC를 Tailscale로 잇습니다. 내 기기들만 들어오는 사설망입니다(Tailscale사의 서비스, 개인은 무료). 이 PC의 포트를 인터넷에 열지 않고, 중간에 우리 서버도 없습니다. Tailscale이 아는 것은 계정, 기기 이름, 접속한 때이고 질문과 답은 모릅니다. 자세한 것은 README의 'Tailscale이 뭔가요?'."))
			}
			shown = n.LoginURL
			say(out, m("Sign this PC in to Tailscale (once). Open this link in a browser, here or on the phone (the QR code is the same link), and sign in. Use the account the phone's Askorus app will use too; without one, you can make it on that page. Your password goes to Tailscale's page only. Nothing of Tailscale needs installing.",
				"이 PC를 Tailscale에 로그인합니다 (한 번). 이 링크를 이 PC나 폰의 브라우저에서 열고 로그인하세요 (QR도 같은 링크입니다). 폰의 Askorus 앱에서 쓸 계정과 같은 계정으로 하세요. 계정이 없으면 그 화면에서 만들 수 있습니다. 비밀번호는 Tailscale의 화면에만 넣습니다. Tailscale을 따로 설치할 필요는 없습니다."))
			fmt.Fprintf(out, "\n  %s\n\n", n.LoginURL)
			_ = printQR(out, n.LoginURL, true)
			say(out, m("Waiting for the sign-in (Ctrl+C stops waiting; the agent keeps running).", "로그인을 기다립니다 (Ctrl+C는 기다리기만 멈춤. 에이전트는 계속 돕니다)."))
		case n.LoginURL == "" && !asked:
			asked = true
			_ = requestNode(dir, "login")
		}
		if time.Now().After(deadline) {
			return fail("not signed in yet; run %s again when you are ready", "아직 로그인되지 않았습니다. 준비되면 %s를 다시 실행하세요", howToLogin())
		}
		select {
		case <-ctx.Done():
			say(out, m("stopped waiting; sign in with the link any time, or run %s again", "기다리기를 멈췄습니다. 언제든 그 링크로 로그인하거나 %s를 다시 실행하세요", howToLogin()))
			return nil
		case <-time.After(loginPoll):
		}
	}
}

// logoutCmd signs the running agent's node out of Tailscale: the device is
// removed from the person's Tailscale network and its key is forgotten.
func logoutCmd(dir string, args []string, in io.Reader, out io.Writer) error {
	fs := flag.NewFlagSet("logout", flag.ContinueOnError)
	yes := fs.Bool("yes", false, "do not ask")
	if err := fs.Parse(args); err != nil {
		return err
	}
	cfg, err := config.Load(dir)
	if err != nil {
		return err
	}
	if !agentRunning(cfg.Port) {
		return fail("the agent is not running; start it first: %s", "에이전트가 돌고 있지 않습니다. 먼저 켜세요: %s", howToStart())
	}
	n, ok := readNodeInfo(dir)
	if ok && n.State == tsnode.StateNeedsLogin {
		say(out, m("this PC is not signed in to Tailscale", "이 PC는 Tailscale에 로그인되어 있지 않습니다"))
		return nil
	}
	if !*yes {
		say(out, m("This signs this PC out of Tailscale: phones cannot reach it until you sign in again (%s). Paired phones stay paired.",
			"이 PC를 Tailscale에서 로그아웃합니다. 다시 로그인할 때까지(%s) 폰에서 닿을 수 없습니다. 짝 맺은 폰은 그대로입니다.", howToLogin()))
		yesNoPrompt(out, m("Type y and press Enter to go ahead. Just Enter changes nothing.",
			"진행하려면 y를 누르고 Enter, 그만두려면 그냥 Enter를 누르세요."))
		line, _ := bufio.NewReader(in).ReadString('\n')
		if !yesAnswers[normalizeAnswer(line)] {
			say(out, m("nothing was changed", "아무것도 바꾸지 않았습니다"))
			return nil
		}
	}
	if !nodeSignOut(dir, 20*time.Second) {
		return fail("the agent did not sign out in time; see `askorus-agent status`", "에이전트가 제때 로그아웃하지 못했습니다. `askorus-agent status`로 확인하세요")
	}
	say(out, m("signed out of Tailscale. Sign in again, with this or another account: %s", "Tailscale에서 로그아웃했습니다. 다시 로그인하려면(같은 계정이든 다른 계정이든): %s", howToLogin()))
	return nil
}

// nodeSignOut asks the running agent to sign its node out and waits until
// it has.
func nodeSignOut(dir string, limit time.Duration) bool {
	if err := requestNode(dir, "logout"); err != nil {
		return false
	}
	deadline := time.Now().Add(limit)
	for time.Now().Before(deadline) {
		time.Sleep(loginPoll)
		if n, ok := readNodeInfo(dir); !ok || n.State == tsnode.StateNeedsLogin {
			return true
		}
	}
	return false
}

// nodeLine is one line about the node for status and check: the level
// (ok, warn, bad) and the words.
func nodeLine(dir string, running bool, now time.Time) (string, msg) {
	n, ok := readNodeInfo(dir)
	switch {
	case !running || !ok:
		return "warn", m("Tailscale: not known while the agent is not running", "Tailscale: 에이전트가 돌고 있지 않아 알 수 없습니다")
	case n.signedIn():
		if !n.KeyExpiry.IsZero() && n.KeyExpiry.Sub(now) < 14*24*time.Hour {
			return "warn", m("signed in to Tailscale as %s (%s), but the sign-in ends on %s: run %s before then, or turn key expiry off for this device in the Tailscale admin console",
				"Tailscale에 %s 계정으로 로그인됨 (%s). 다만 로그인이 %s에 끝납니다. 그 전에 %s를 실행하거나, Tailscale 관리 화면에서 이 기기의 키 만료를 끄세요",
				n.Owner, n.Name, n.KeyExpiry.Local().Format("2006-01-02"), howToLogin())
		}
		return "ok", m("signed in to Tailscale as %s; phones reach this PC as %s:%d", "Tailscale에 %s 계정으로 로그인됨. 폰은 %s:%d로 이 PC에 닿습니다", n.Owner, n.Name, n.Port)
	case n.State == tsnode.StateNeedsApproval:
		return "bad", m("Tailscale: this PC waits for approval in the Tailscale admin console", "Tailscale: 이 PC가 Tailscale 관리 화면의 승인을 기다립니다")
	case n.State == tsnode.StateNeedsLogin:
		return "bad", m("this PC is not signed in to Tailscale: %s", "이 PC가 Tailscale에 로그인되어 있지 않습니다: %s", howToLogin())
	case n.State == stateNoNode:
		return "warn", m("this agent runs without Tailscale (a test run, %s=1): no phone can reach it", "이 에이전트는 Tailscale 없이 돕니다 (시험 실행, %s=1). 폰에서 닿을 수 없습니다", noNodeEnv)
	default:
		return "warn", m("Tailscale is connecting (%s)", "Tailscale에 연결하는 중입니다 (%s)", n.State)
	}
}
