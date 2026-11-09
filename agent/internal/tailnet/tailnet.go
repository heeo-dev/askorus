// Package tailnet reads Tailscale state with the tailscale CLI. Apart from
// ServeOff (uninstall, the agent's own port, after confirmation) it only
// runs read-only commands; it never changes funnel or any other setting.
package tailnet

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"
)

// Status is the part of `tailscale status --json` the agent needs.
type Status struct {
	Running    bool
	DNSName    string // MagicDNS name without the trailing dot, lowercase
	OwnerLogin string // login of the user this PC is signed in as
	HTTPS      bool   // HTTPS certificates are enabled for the tailnet
}

// Serve is how tailscale serve exposes one local port.
type Serve struct {
	Host      string // MagicDNS name
	HTTPSPort int
	Funnel    bool // exposed to the internet: the agent must refuse to run
}

// Client runs the tailscale CLI.
type Client struct {
	exe string
}

var ErrNotFound = errors.New("tailscale CLI not found")

// New finds the tailscale CLI on PATH or in its usual install folder.
func New() (*Client, error) {
	if p, err := exec.LookPath("tailscale"); err == nil {
		return &Client{exe: p}, nil
	}
	if runtime.GOOS == "windows" {
		p := filepath.Join(os.Getenv("ProgramFiles"), "Tailscale", "tailscale.exe")
		if _, err := os.Stat(p); err == nil {
			return &Client{exe: p}, nil
		}
	}
	return nil, ErrNotFound
}

// The only argument lists this package ever passes.
var (
	argsStatus      = []string{"status", "--json"}
	argsServeStatus = []string{"serve", "status", "--json"}
)

func (c *Client) run(ctx context.Context, args []string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, c.exe, args...)
	hideWindow(cmd)
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("tailscale %s: %w", strings.Join(args, " "), err)
	}
	return out, nil
}

// Status runs tailscale status --json.
func (c *Client) Status(ctx context.Context) (Status, error) {
	b, err := c.run(ctx, argsStatus)
	if err != nil {
		return Status{}, err
	}
	return ParseStatus(b)
}

// ServeFor finds the serve entry that proxies to 127.0.0.1:localPort.
func (c *Client) ServeFor(ctx context.Context, localPort int) (Serve, bool, error) {
	b, err := c.run(ctx, argsServeStatus)
	if err != nil {
		return Serve{}, false, err
	}
	return ParseServe(b, localPort)
}

// ParseStatus reads `tailscale status --json`.
func ParseStatus(b []byte) (Status, error) {
	var v struct {
		BackendState string
		Self         *struct {
			DNSName string
			UserID  int64
		}
		User map[string]struct {
			LoginName string
		}
		CertDomains []string
	}
	if err := json.Unmarshal(b, &v); err != nil {
		return Status{}, fmt.Errorf("tailscale status: %w", err)
	}
	s := Status{Running: v.BackendState == "Running", HTTPS: len(v.CertDomains) > 0}
	if v.Self != nil {
		s.DNSName = strings.ToLower(strings.TrimSuffix(v.Self.DNSName, "."))
		if u, ok := v.User[strconv.FormatInt(v.Self.UserID, 10)]; ok {
			s.OwnerLogin = u.LoginName
		}
	}
	return s, nil
}

// ParseServe reads `tailscale serve status --json`. An empty output means
// nothing is served. The phones use the serve whose "/" handler proxies to
// the agent (the lowest such port when there are several). Funnel is set
// when ANY serve reaches the agent from the internet: a web handler on any
// path or a TCP forward to the agent port, on any port with funnel on. Then
// the port returned is that one, for the message that says what to turn
// off. Before, only the first matching entry of the map was looked at, so
// a funnel on a second one could go unseen (#34, found by Codex).
func ParseServe(b []byte, localPort int) (Serve, bool, error) {
	if len(strings.TrimSpace(string(b))) == 0 {
		return Serve{}, false, nil
	}
	var v struct {
		TCP map[string]struct {
			TCPForward string
		}
		Web map[string]struct {
			Handlers map[string]struct {
				Proxy string
			}
		}
		AllowFunnel map[string]bool
	}
	if err := json.Unmarshal(b, &v); err != nil {
		return Serve{}, false, fmt.Errorf("tailscale serve status: %w", err)
	}
	var phone, funnel *Serve
	pick := func(cur **Serve, s Serve) {
		if *cur == nil || s.HTTPSPort < (*cur).HTTPSPort {
			*cur = &s
		}
	}
	for hostPort, web := range v.Web {
		host, portStr, err := net.SplitHostPort(hostPort)
		if err != nil {
			continue
		}
		port, err := strconv.Atoi(portStr)
		if err != nil {
			continue
		}
		root, hit := false, false
		for path, h := range web.Handlers {
			if proxiesTo(h.Proxy, localPort) {
				hit = true
				root = root || path == "/"
			}
		}
		s := Serve{Host: strings.ToLower(host), HTTPSPort: port, Funnel: v.AllowFunnel[hostPort]}
		if hit && s.Funnel {
			pick(&funnel, s)
		}
		if root {
			pick(&phone, s)
		}
	}
	for portStr, tcp := range v.TCP {
		port, err := strconv.Atoi(portStr)
		if err != nil || !forwardsTo(tcp.TCPForward, localPort) {
			continue
		}
		for hostPort, on := range v.AllowFunnel {
			if on && strings.HasSuffix(hostPort, ":"+portStr) {
				host, _, _ := net.SplitHostPort(hostPort)
				pick(&funnel, Serve{Host: strings.ToLower(host), HTTPSPort: port, Funnel: true})
			}
		}
	}
	switch {
	case funnel != nil:
		return *funnel, true, nil
	case phone != nil:
		return *phone, true, nil
	}
	return Serve{}, false, nil
}

// localHost reports whether host is this PC's loopback address.
func localHost(host string) bool {
	switch strings.ToLower(host) {
	case "127.0.0.1", "localhost", "::1":
		return true
	}
	return false
}

// proxiesTo reports whether a serve proxy target (e.g.
// "http://127.0.0.1:8790" or "http://localhost:8790/x") is the agent port.
func proxiesTo(proxy string, localPort int) bool {
	u, err := url.Parse(proxy)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") {
		return false
	}
	return localHost(u.Hostname()) && u.Port() == strconv.Itoa(localPort)
}

// forwardsTo reports whether a TCP forward target ("127.0.0.1:8790") is the
// agent port.
func forwardsTo(target string, localPort int) bool {
	host, port, err := net.SplitHostPort(strings.TrimPrefix(target, "tcp://"))
	return err == nil && localHost(host) && port == strconv.Itoa(localPort)
}

// ServePorts lists the HTTPS ports tailscale serve already uses, so a new
// one can be picked without touching them.
func (c *Client) ServePorts(ctx context.Context) ([]int, error) {
	b, err := c.run(ctx, argsServeStatus)
	if err != nil {
		return nil, err
	}
	return ParseServePorts(b)
}

// ParseServePorts reads the used ports out of `tailscale serve status --json`.
func ParseServePorts(b []byte) ([]int, error) {
	if len(strings.TrimSpace(string(b))) == 0 {
		return nil, nil
	}
	var v struct {
		TCP map[string]json.RawMessage
		Web map[string]json.RawMessage
	}
	if err := json.Unmarshal(b, &v); err != nil {
		return nil, fmt.Errorf("tailscale serve status: %w", err)
	}
	seen := map[int]bool{}
	var out []int
	add := func(p string) {
		if n, err := strconv.Atoi(p); err == nil && !seen[n] {
			seen[n] = true
			out = append(out, n)
		}
	}
	for p := range v.TCP {
		add(p)
	}
	for hp := range v.Web {
		if _, p, err := net.SplitHostPort(hp); err == nil {
			add(p)
		}
	}
	return out, nil
}

// ServeOff turns off tailscale serve on one HTTPS port. The agent calls it
// only from `uninstall`, after the person confirmed, and only for the port
// that proxies to the agent; every other serve setting stays.
func (c *Client) ServeOff(ctx context.Context, httpsPort int) error {
	if httpsPort < 1 || httpsPort > 65535 {
		return errors.New("bad port")
	}
	_, err := c.run(ctx, []string{"serve", "--https=" + strconv.Itoa(httpsPort), "off"})
	return err
}

// ServeOn makes tailscale serve pass https://<this PC>:httpsPort on the
// tailnet to the agent on 127.0.0.1:localPort, in the background so it
// stays after a restart. The agent calls it only from `setup`, after the
// person typed y (#37, 2026-11-09 decision); it adds this one port and no
// other serve setting changes. A failure carries what tailscale printed:
// the first serve on a tailnet can ask for Serve to be turned on in the
// admin console, with a link.
func (c *Client) ServeOn(ctx context.Context, httpsPort, localPort int) error {
	args, err := serveOnArgs(httpsPort, localPort)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, c.exe, args...)
	hideWindow(cmd)
	out, err := cmd.CombinedOutput()
	if err != nil {
		said := strings.TrimSpace(string(out))
		if len(said) > 600 {
			said = said[:600] + "…"
		}
		if said == "" {
			return fmt.Errorf("tailscale %s: %w", strings.Join(args, " "), err)
		}
		return fmt.Errorf("tailscale %s: %w: %s", strings.Join(args, " "), err, said)
	}
	return nil
}

func serveOnArgs(httpsPort, localPort int) ([]string, error) {
	if httpsPort < 1 || httpsPort > 65535 || localPort < 1 || localPort > 65535 {
		return nil, errors.New("bad port")
	}
	return []string{"serve", "--bg", "--https=" + strconv.Itoa(httpsPort), "http://127.0.0.1:" + strconv.Itoa(localPort)}, nil
}

var (
	cgnat = netip.MustParsePrefix("100.64.0.0/10")
	tsULA = netip.MustParsePrefix("fd7a:115c:a1e0::/48")
)

// IsTailnetIP reports whether s is a Tailscale address.
func IsTailnetIP(s string) bool {
	a, err := netip.ParseAddr(strings.TrimSpace(s))
	if err != nil {
		return false
	}
	a = a.Unmap()
	return cgnat.Contains(a) || tsULA.Contains(a)
}
