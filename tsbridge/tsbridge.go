// Package tsbridge puts a Tailscale node inside the phone app (#57), so
// the app reaches the person's PC without a Tailscale app and its system
// VPN. The node lives in the app's own process (tsnet, userspace
// networking): no VPN permission, no key icon, nothing other apps can see.
//
// The app binds this package with gomobile and calls it from its native
// module (app/modules/tailnet). The API is a handful of plain functions
// with JSON strings, because that is what crosses the Java bridge cleanly.
//
// One rule is kept here and not in the app's JavaScript: a request goes
// only to a device of the person's own Tailscale account (Fetch). A
// pairing link someone else sent, or a changed address, cannot take the
// phone's token or questions anywhere else.
package tsbridge

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"tailscale.com/client/local"
	"tailscale.com/envknob"
	"tailscale.com/feature/buildfeatures"
	"tailscale.com/ipn/ipnstate"
	"tailscale.com/net/tsaddr"
	"tailscale.com/tsnet"
)

// buildTags are the build tags the bridge is built with (build-android.sh).
// They leave out three parts of the Tailscale library the app has no use
// for: its service for other devices (file sending, debug pages), which
// would open a TCP port on the phone's own network interfaces; the
// diagnostics Tailscale's coordination server may ask a device for; and the
// log uploader with its buffer file, where the login link would land. Start
// refuses to run in a library built without them. The agent is built with
// the same tags (tsnode.BuildTags).
const buildTags = "ts_omit_peerapiserver,ts_omit_c2n,ts_omit_logtail"

func builtRight() bool {
	return !buildfeatures.HasPeerAPIServer && !buildfeatures.HasC2N && !buildfeatures.HasLogTail
}

// maxBody bounds one answer from the PC (the agent's largest answers are
// conversation previews and combined answers, far below this).
const maxBody = 32 << 20

// Error codes the app tells apart. They are the start of the error's text,
// before a colon.
const (
	CodeNotStarted   = "tailnet_off"           // Start was not called
	CodeNeedsLogin   = "tailnet_login"         // the app is not signed in to Tailscale
	CodeNotOwnDevice = "tailnet_not_own"       // the address is not a device of the person's account
	CodeUnknownHost  = "tailnet_unknown_host"  // no device of the tailnet has that name
	CodeBadRequest   = "tailnet_bad_request"   // the request itself is wrong
	CodeTooLarge     = "tailnet_too_large"     // the answer is larger than maxBody
	CodeNotConnected = "tailnet_not_connected" // signed in, but the tailnet is not up yet
)

func coded(code, format string, a ...any) error {
	return fmt.Errorf("%s: %s", code, fmt.Sprintf(format, a...))
}

var (
	mu      sync.Mutex
	srv     *tsnet.Server
	lc      *local.Client
	httpc   *http.Client
	started time.Time
	runAt   time.Time // when the node first reported Running
	verbose bool

	peersMu sync.Mutex
	peersAt time.Time
	peers   *ipnstate.Status
)

// SetVerbose prints the backend's own (very long) log to the system log.
// Call it before Start.
func SetVerbose(on bool) {
	mu.Lock()
	defer mu.Unlock()
	verbose = on
}

// dropLogBuffers empties the library's log buffer files in dir. Built with
// the tags nothing is written to them; a build without the tags (before
// they were set) may have left lines there, the login link among them.
func dropLogBuffers(dir string) {
	for _, name := range []string{"tailscaled.log1.txt", "tailscaled.log2.txt"} {
		p := filepath.Join(dir, name)
		if fi, err := os.Stat(p); err == nil && fi.Size() > 0 {
			_ = os.Truncate(p, 0)
		}
	}
}

// Start brings the node up in the background and returns at once. dir is a
// private folder of the app that keeps the node's key between runs;
// hostname is the device name shown in the person's Tailscale admin page.
// Calling it again while the node is up does nothing.
func Start(dir, hostname string) error {
	mu.Lock()
	defer mu.Unlock()
	if srv != nil {
		return nil
	}
	if !builtRight() {
		return coded(CodeBadRequest, "built without -tags %s", buildTags)
	}
	if dir == "" {
		return coded(CodeBadRequest, "no folder")
	}
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	// An Android app has no home, cache or temp folder that Go knows of, and
	// Tailscale stops the whole process ("no safe place found to store log
	// state") when it finds no folder for its logs. Point it at ours.
	os.Setenv("TS_LOGS_DIR", dir)
	// No diagnostic logs to Tailscale's log server: the app sends nothing
	// the person did not ask for (N1).
	envknob.SetNoLogsNoSupport()
	dropLogBuffers(dir)

	s := &tsnet.Server{
		Dir:      dir,
		Hostname: hostname,
		// The lines meant for a person (they hold the login link) stay in
		// memory only: the system log is readable by other tools.
		UserLogf: func(format string, a ...any) { recent.addf(format, a...) },
		// The backend's own log is long; it is kept in memory for
		// RecentLog and only printed when verbose.
		Logf: func(format string, a ...any) {
			recent.addf(format, a...)
			if verbose {
				log.Printf(format, a...)
			}
		},
	}
	if err := s.Start(); err != nil {
		recent.addf("start: %v", err)
		return err
	}
	c, err := s.LocalClient()
	if err != nil {
		s.Close()
		return err
	}
	srv, lc = s, c
	started, runAt = time.Now(), time.Time{}
	httpc = &http.Client{
		Transport: &http.Transport{
			DialContext:           dialTailnet(s),
			TLSClientConfig:       &tls.Config{MinVersion: tls.VersionTLS12},
			TLSHandshakeTimeout:   15 * time.Second,
			ResponseHeaderTimeout: 60 * time.Second,
			IdleConnTimeout:       60 * time.Second,
			MaxIdleConnsPerHost:   4,
		},
		// The agent never redirects. Following one could take the device
		// token to another server.
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
	return nil
}

// Stop takes the node down. The key stays in the folder, so the next Start
// needs no new login.
func Stop() {
	mu.Lock()
	s := srv
	srv, lc, httpc = nil, nil, nil
	mu.Unlock()
	forgetPeers()
	if s != nil {
		s.Close()
	}
}

// NetworkChanged tells the node that the phone's network changed (Wi-Fi to
// mobile data, back from sleep). On Android the node itself looks only
// every ten minutes, to save battery, and counts on the app to say so.
func NetworkChanged() {
	mu.Lock()
	s := srv
	mu.Unlock()
	if s == nil {
		return
	}
	if m, ok := s.Sys().NetMon.GetOK(); ok {
		m.InjectEvent()
	}
}

// logRing keeps the last lines of the node's log in memory, for looking
// into a problem on a phone where nobody reads the system log.
type logRing struct {
	mu    sync.Mutex
	lines []string
}

const logRingMax = 400

// loginLink matches a Tailscale login link. Whoever opens one puts this
// phone on their own Tailscale network, so it is not kept in the log: the
// app gets it from Status when the person asks to sign in.
var loginLink = regexp.MustCompile(`https://login\.tailscale\.com/[^\s"']+`)

func (r *logRing) addf(format string, a ...any) {
	text := loginLink.ReplaceAllString(fmt.Sprintf(format, a...), "(login link)")
	line := time.Now().Format("15:04:05.000 ") + strings.TrimRight(text, "\n")
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.lines) >= logRingMax {
		r.lines = append(r.lines[:0], r.lines[len(r.lines)-logRingMax/2:]...)
	}
	r.lines = append(r.lines, line)
}

var recent logRing

// RecentLog returns the last lines of the node's log, oldest first.
func RecentLog() string {
	recent.mu.Lock()
	defer recent.mu.Unlock()
	return strings.Join(recent.lines, "\n")
}

// status is what Status returns as JSON.
type status struct {
	// State is Tailscale's backend state: NoState, NeedsLogin,
	// NeedsMachineAuth, Starting, Running, Stopped. "Off" when the node is
	// not started.
	State string `json:"state"`
	// AuthURL is the login link to open in a browser while State is
	// NeedsLogin.
	AuthURL string `json:"authURL,omitempty"`
	Name    string `json:"name,omitempty"`  // this node's MagicDNS name
	Owner   string `json:"owner,omitempty"` // the login of the account it is signed in as
	// KeyExpiry is when the node has to sign in again (RFC 3339), "" when
	// it never does.
	KeyExpiry string   `json:"keyExpiry,omitempty"`
	IPs       []string `json:"ips,omitempty"`
	Health    []string `json:"health,omitempty"`
	// UpMs is how long Start to Running took, once known.
	UpMs int64 `json:"upMs,omitempty"`
}

// Status reports the node's state as JSON (see the status type).
func Status() (string, error) {
	mu.Lock()
	c, t0 := lc, started
	mu.Unlock()
	out := status{State: "Off"}
	if c != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		st, err := c.StatusWithoutPeers(ctx)
		if err != nil {
			return "", err
		}
		out.State = st.BackendState
		out.AuthURL = st.AuthURL
		out.Health = st.Health
		for _, ip := range st.TailscaleIPs {
			out.IPs = append(out.IPs, ip.String())
		}
		if st.Self != nil {
			out.Name = strings.ToLower(strings.TrimSuffix(st.Self.DNSName, "."))
			if u, ok := st.User[st.Self.UserID]; ok {
				out.Owner = u.LoginName
			}
			if st.Self.KeyExpiry != nil {
				out.KeyExpiry = st.Self.KeyExpiry.UTC().Format(time.RFC3339)
			}
		}
		if st.BackendState == "Running" {
			mu.Lock()
			if runAt.IsZero() {
				runAt = time.Now()
			}
			out.UpMs = runAt.Sub(t0).Milliseconds()
			mu.Unlock()
		}
	}
	b, err := json.Marshal(out)
	return string(b), err
}

// Login asks Tailscale for a fresh login link. It shows up as authURL in
// Status a moment later.
func Login() error {
	mu.Lock()
	c := lc
	mu.Unlock()
	if c == nil {
		return coded(CodeNotStarted, "not started")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return c.StartLoginInteractive(ctx)
}

// Logout removes this node from the tailnet and forgets its key.
func Logout() error {
	mu.Lock()
	c := lc
	mu.Unlock()
	if c == nil {
		return coded(CodeNotStarted, "not started")
	}
	forgetPeers()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return c.Logout(ctx)
}

// Response is the answer to one Fetch.
type Response struct {
	Status int
	// Headers is a JSON object of the answer's headers (first value of
	// each).
	Headers string
	Body    string
	// Ms is how long the request took.
	Ms int64
}

// peerInfo is what Peer says about a name, as JSON.
type peerInfo struct {
	// Known: a device of the tailnet has that name or address.
	Known bool `json:"known"`
	// SameUser: it belongs to the account this app is signed in as. Only
	// such a device gets requests.
	SameUser bool   `json:"sameUser"`
	Online   bool   `json:"online"`
	Name     string `json:"name,omitempty"` // its host name
	IP       string `json:"ip,omitempty"`
}

// Peer says whether host (a MagicDNS name or a tailnet address) is a
// device of the person's own Tailscale account, as JSON. The app asks
// before it sends a pairing code (#42); Fetch applies the same rule to
// every request.
func Peer(host string) (string, error) {
	mu.Lock()
	c := lc
	mu.Unlock()
	if c == nil {
		return "", coded(CodeNotStarted, "not started")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	p, err := lookPeer(ctx, c, host, true)
	if err != nil {
		return "", err
	}
	b, err := json.Marshal(p)
	return string(b), err
}

// peerCacheFor is how long the list of the tailnet's devices is reused. It
// is asked for on every request; the list changes rarely.
const peerCacheFor = 5 * time.Second

func forgetPeers() {
	peersMu.Lock()
	peers, peersAt = nil, time.Time{}
	peersMu.Unlock()
}

func peerStatus(ctx context.Context, c *local.Client, fresh bool) (*ipnstate.Status, error) {
	peersMu.Lock()
	if !fresh && peers != nil && time.Since(peersAt) < peerCacheFor {
		st := peers
		peersMu.Unlock()
		return st, nil
	}
	peersMu.Unlock()
	st, err := c.Status(ctx)
	if err != nil {
		return nil, err
	}
	peersMu.Lock()
	peers, peersAt = st, time.Now()
	peersMu.Unlock()
	return st, nil
}

// lookPeer finds host among the tailnet's devices. The name is matched
// against the tailnet's own name table, never resolved through the phone's
// DNS: a name outside the tailnet is simply not there.
func lookPeer(ctx context.Context, c *local.Client, host string, fresh bool) (peerInfo, error) {
	st, err := peerStatus(ctx, c, fresh)
	if err != nil {
		return peerInfo{}, err
	}
	return matchPeer(st, host)
}

// matchPeer is lookPeer on a list already read.
func matchPeer(st *ipnstate.Status, host string) (peerInfo, error) {
	if st.BackendState == "NeedsLogin" || st.BackendState == "NoState" && st.Self == nil {
		return peerInfo{}, coded(CodeNeedsLogin, "not signed in to Tailscale")
	}
	if st.Self == nil {
		return peerInfo{}, coded(CodeNotConnected, "the tailnet is not up yet (%s)", st.BackendState)
	}
	want := dnsName(host)
	ip := net.ParseIP(strings.Trim(host, "[]"))
	for _, peer := range st.Peer {
		match := want != "" && dnsName(peer.DNSName) == want
		if !match && ip != nil {
			for _, a := range peer.TailscaleIPs {
				if a.String() == ip.String() {
					match = true
				}
			}
		}
		if !match {
			continue
		}
		p := peerInfo{Known: true, Online: peer.Online, Name: peer.HostName}
		// Only a device this account owns: one shared in by someone else has
		// another user id, a tagged one has no person behind it, and a
		// device listed only because this account shares with its owner
		// (ShareeNode) is not the person's either.
		p.SameUser = ownPeer(st, peer)
		if len(peer.TailscaleIPs) > 0 {
			p.IP = peer.TailscaleIPs[0].String()
		}
		return p, nil
	}
	return peerInfo{}, nil
}

// ownPeer reports whether peer is a device of the account this phone is
// signed in as: the same user, no tags, and not one that is listed only
// because the account shares with its owner.
func ownPeer(st *ipnstate.Status, peer *ipnstate.PeerStatus) bool {
	return st.Self != nil && peer.UserID == st.Self.UserID && peer.UserID != 0 && !peer.ShareeNode && len(peerTags(peer)) == 0
}

// ownDevice is one device of the person's own account, as Peers lists it.
type ownDevice struct {
	// Name is its name in the tailnet (MagicDNS), lowercase, without the last dot.
	Name string `json:"name"`
	// Host is its host name (what the device calls itself).
	Host   string `json:"host"`
	Online bool   `json:"online"`
	OS     string `json:"os,omitempty"`
}

// Peers lists, as JSON, the other devices of the account this phone is
// signed in as: the only devices a request may go to (Fetch). The app
// looks among them for the person's PCs, so pairing needs no QR code to
// say where the PC is (#58). The list is read fresh.
func Peers() (string, error) {
	mu.Lock()
	c := lc
	mu.Unlock()
	if c == nil {
		return "", coded(CodeNotStarted, "not started")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	st, err := peerStatus(ctx, c, true)
	if err != nil {
		return "", err
	}
	list, err := ownDevices(st)
	if err != nil {
		return "", err
	}
	b, err := json.Marshal(list)
	return string(b), err
}

// ownDevices is Peers on a list already read, sorted by name.
func ownDevices(st *ipnstate.Status) ([]ownDevice, error) {
	if st.BackendState == "NeedsLogin" || st.BackendState == "NoState" && st.Self == nil {
		return nil, coded(CodeNeedsLogin, "not signed in to Tailscale")
	}
	if st.Self == nil {
		return nil, coded(CodeNotConnected, "the tailnet is not up yet (%s)", st.BackendState)
	}
	out := []ownDevice{}
	for _, peer := range st.Peer {
		if !ownPeer(st, peer) || dnsName(peer.DNSName) == "" {
			continue
		}
		out = append(out, ownDevice{Name: dnsName(peer.DNSName), Host: peer.HostName, Online: peer.Online, OS: peer.OS})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

func peerTags(p *ipnstate.PeerStatus) []string {
	if p.Tags == nil {
		return nil
	}
	return p.Tags.AsSlice()
}

// dialTailnet opens a connection to a Tailscale address through the node's
// own network stack and nowhere else. The library's ordinary dial looks a
// name up with the phone's DNS when the tailnet no longer has it, and
// reaches an address that is no device of the tailnet over the phone's own
// network: either way a token could leave the tunnel in plain text. Here
// the address is an IP of the tailnet (the one Fetch has just checked to
// be the person's own device) and the only way out is the tunnel, where an
// address nobody holds simply gets no answer.
func dialTailnet(s *tsnet.Server) func(ctx context.Context, network, addr string) (net.Conn, error) {
	return func(ctx context.Context, network, addr string) (net.Conn, error) {
		ipp, err := netip.ParseAddrPort(addr)
		if err != nil || !tsaddr.IsTailscaleIP(ipp.Addr()) {
			return nil, coded(CodeBadRequest, "not an address inside the Tailscale network")
		}
		d, ok := s.Sys().Dialer.GetOK()
		if !ok || d.NetstackDialTCP == nil {
			return nil, coded(CodeNotConnected, "the tailnet is not up yet")
		}
		return d.NetstackDialTCP(ctx, ipp)
	}
}

// targetOf is where a request for u goes once its device is checked: the
// device's Tailscale address in place of the name, so the connection is made
// to what was checked and no name is looked up again.
func targetOf(u *url.URL, p peerInfo) (string, error) {
	ip, err := netip.ParseAddr(p.IP)
	if err != nil || !tsaddr.IsTailscaleIP(ip) {
		return "", coded(CodeNotConnected, "that device has no address in the tailnet yet")
	}
	port := u.Port()
	if port == "" {
		port = "80"
	}
	t := *u
	t.Host = net.JoinHostPort(ip.String(), port)
	return t.String(), nil
}

// Fetch sends one HTTP request through the node and waits for the whole
// answer. headers is a JSON object of strings; body may be empty. The
// address must be a device of the person's own Tailscale account, reached
// by plain http: the tailnet already encrypts between the two devices and
// knows who is on the other end.
func Fetch(method, rawURL, headers, body string, timeoutMs int) (*Response, error) {
	mu.Lock()
	c, hc := lc, httpc
	mu.Unlock()
	if hc == nil {
		return nil, coded(CodeNotStarted, "not started")
	}
	if timeoutMs <= 0 {
		timeoutMs = 15000
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(timeoutMs)*time.Millisecond)
	defer cancel()

	u, err := url.Parse(rawURL)
	if err != nil || u.Hostname() == "" || u.User != nil {
		return nil, coded(CodeBadRequest, "bad address")
	}
	if u.Scheme != "http" {
		return nil, coded(CodeBadRequest, "only http inside the tailnet")
	}
	p, err := lookPeer(ctx, c, u.Hostname(), false)
	if err == nil && !p.Known {
		// The device may have joined since the list was read.
		p, err = lookPeer(ctx, c, u.Hostname(), true)
	}
	if err != nil {
		return nil, err
	}
	if !p.Known || !p.SameUser {
		// Nothing kept open from before goes on being used for that address.
		hc.CloseIdleConnections()
	}
	if !p.Known {
		return nil, coded(CodeUnknownHost, "no device of your Tailscale network has that name")
	}
	if !p.SameUser {
		return nil, coded(CodeNotOwnDevice, "that device is not one of your own Tailscale account")
	}
	target, err := targetOf(u, p)
	if err != nil {
		return nil, err
	}

	var rd io.Reader
	if body != "" {
		rd = strings.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, target, rd)
	if err != nil {
		return nil, coded(CodeBadRequest, "%v", err)
	}
	req.Host = u.Host // the agent answers to its own names only
	if headers != "" {
		var h map[string]string
		if err := json.Unmarshal([]byte(headers), &h); err != nil {
			return nil, coded(CodeBadRequest, "headers are not a JSON object of strings")
		}
		for k, v := range h {
			req.Header.Set(k, v)
		}
	}
	t0 := time.Now()
	res, err := hc.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	b, err := io.ReadAll(io.LimitReader(res.Body, maxBody+1))
	if err != nil {
		return nil, err
	}
	if len(b) > maxBody {
		return nil, coded(CodeTooLarge, "answer too large")
	}
	out := &Response{Status: res.StatusCode, Body: string(b), Ms: time.Since(t0).Milliseconds()}
	hm := map[string]string{}
	for k := range res.Header {
		hm[k] = res.Header.Get(k)
	}
	hb, _ := json.Marshal(hm)
	out.Headers = string(hb)
	return out, nil
}

// errorCode is the code at the start of one of this package's errors, or
// "" for any other error (a network failure, a timeout). The app's native
// module reads the same code from the error's text.
func errorCode(err error) string {
	if err == nil {
		return ""
	}
	code, _, ok := strings.Cut(err.Error(), ": ")
	if !ok || !strings.HasPrefix(code, "tailnet_") {
		return ""
	}
	return code
}

func dnsName(s string) string {
	return strings.TrimSuffix(strings.ToLower(strings.TrimSpace(s)), ".")
}
