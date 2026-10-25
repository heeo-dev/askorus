// Package tailnet reads Tailscale state with the tailscale CLI. It only runs
// read-only commands; it never changes serve, funnel or any other setting.
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
	}
	if err := json.Unmarshal(b, &v); err != nil {
		return Status{}, fmt.Errorf("tailscale status: %w", err)
	}
	s := Status{Running: v.BackendState == "Running"}
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
