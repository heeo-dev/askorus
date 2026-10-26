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
// nothing is served.
func ParseServe(b []byte, localPort int) (Serve, bool, error) {
	if len(strings.TrimSpace(string(b))) == 0 {
		return Serve{}, false, nil
	}
	var v struct {
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
	want := map[string]bool{}
	for _, h := range []string{"127.0.0.1", "localhost"} {
		p := strconv.Itoa(localPort)
		want["http://"+h+":"+p] = true
		want["http://"+h+":"+p+"/"] = true
	}
	for hostPort, web := range v.Web {
		if !want[web.Handlers["/"].Proxy] {
			continue
		}
		host, portStr, err := net.SplitHostPort(hostPort)
		if err != nil {
			continue
		}
		port, err := strconv.Atoi(portStr)
		if err != nil {
			continue
		}
		return Serve{Host: strings.ToLower(host), HTTPSPort: port, Funnel: v.AllowFunnel[hostPort]}, true, nil
	}
	return Serve{}, false, nil
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
