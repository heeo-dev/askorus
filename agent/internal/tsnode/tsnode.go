// Package tsnode is the agent's own device on the person's Tailscale
// network (#57). The agent carries Tailscale inside (tsnet, userspace
// networking), so the PC needs no Tailscale installed and no setting of it
// changed: the node signs in once with a link, listens only inside the
// tailnet, and tells the agent who sent each request.
//
// It never opens a port on the PC's own network interfaces for the API.
// What it stores (the node key) lives in the agent's settings folder.
package tsnode

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"time"

	"tailscale.com/client/local"
	"tailscale.com/envknob"
	"tailscale.com/feature/buildfeatures"
	"tailscale.com/tsnet"
)

// BuildTags are the build tags the agent is built with. They leave out three
// parts of the Tailscale library the agent has no use for:
//
//   - its service for other devices (file sending, debug pages), which would
//     open a TCP port on the PC's own network interfaces: the agent opens
//     none, not even on 127.0.0.1;
//   - the diagnostics Tailscale's coordination server may ask a device for;
//   - the log uploader with its buffer file, where the login link would land.
//
// Start refuses to run in a binary built without them, so a build made
// another way cannot open that port unnoticed. The scripts, the workflow
// and the README name the same tags (TestBuildTagsEverywhere).
const BuildTags = "ts_omit_peerapiserver,ts_omit_c2n,ts_omit_logtail"

// ErrBuildTags is what Start returns in a binary built without BuildTags.
var ErrBuildTags = errors.New("this binary was built without -tags " + BuildTags + "; build it as the README says (Build from source)")

// builtRight reports whether this binary leaves those parts out.
func builtRight() bool {
	return !buildfeatures.HasPeerAPIServer && !buildfeatures.HasC2N && !buildfeatures.HasLogTail
}

// Backend states, as Tailscale names them.
const (
	StateRunning    = "Running"
	StateNeedsLogin = "NeedsLogin"
	// StateNeedsApproval: the tailnet's admin must approve the device.
	StateNeedsApproval = "NeedsMachineAuth"
)

// Status is what the agent shows about its node.
type Status struct {
	State string `json:"state"`
	// LoginURL is the link to open in a browser while State is NeedsLogin.
	LoginURL string `json:"loginURL,omitempty"`
	// Name is the node's MagicDNS name, lowercase, without the last dot.
	Name string `json:"name,omitempty"`
	// Owner is the login of the account the node is signed in as.
	Owner string   `json:"owner,omitempty"`
	IPs   []string `json:"ips,omitempty"`
	// KeyExpiry is when the node has to sign in again; zero when it never does.
	KeyExpiry time.Time `json:"keyExpiry,omitzero"`
	Health    []string  `json:"health,omitempty"`
}

// Node is a started node.
type Node struct {
	srv *tsnet.Server
	lc  *local.Client
}

// HostName makes the node's device name from the PC's name: "askorus-" and
// what a DNS label may hold.
func HostName(pc string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(pc) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
		case r == '-' || r == '_' || r == ' ' || r == '.':
			b.WriteByte('-')
		}
	}
	name := strings.Trim(b.String(), "-")
	if name == "" {
		name = "pc"
	}
	name = "askorus-" + name
	if len(name) > 63 {
		name = strings.TrimRight(name[:63], "-")
	}
	return name
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

// Start brings the node up in the background and returns at once. dir keeps
// the node key between runs. logf gets the few lines meant for a person
// (state changes, the login link); the backend's own long log is dropped.
func Start(dir, hostname string, logf func(format string, a ...any)) (*Node, error) {
	if !builtRight() {
		return nil, ErrBuildTags
	}
	if dir == "" {
		return nil, errors.New("tsnode: no folder")
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	// Tailscale's own files stay in the agent's folder, not in the folder a
	// Tailscale installed on this PC uses.
	os.Setenv("TS_LOGS_DIR", dir)
	// No diagnostic logs to Tailscale's log server (N1). The uploader is not
	// built in (BuildTags); this also tells the library not to try.
	envknob.SetNoLogsNoSupport()
	dropLogBuffers(dir)

	srv := &tsnet.Server{Dir: dir, Hostname: hostname, UserLogf: logf}
	if err := srv.Start(); err != nil {
		return nil, err
	}
	lc, err := srv.LocalClient()
	if err != nil {
		srv.Close()
		return nil, err
	}
	return &Node{srv: srv, lc: lc}, nil
}

// Close takes the node down. The key stays, so the next Start needs no login.
func (n *Node) Close() error { return n.srv.Close() }

// Status reports the node's state.
func (n *Node) Status(ctx context.Context) (Status, error) {
	st, err := n.lc.StatusWithoutPeers(ctx)
	if err != nil {
		return Status{}, err
	}
	out := Status{State: st.BackendState, LoginURL: st.AuthURL, Health: st.Health}
	for _, ip := range st.TailscaleIPs {
		out.IPs = append(out.IPs, ip.String())
	}
	if st.Self != nil {
		out.Name = strings.ToLower(strings.TrimSuffix(st.Self.DNSName, "."))
		if u, ok := st.User[st.Self.UserID]; ok {
			out.Owner = u.LoginName
		}
		if st.Self.KeyExpiry != nil {
			out.KeyExpiry = *st.Self.KeyExpiry
		}
	}
	return out, nil
}

// Listen accepts connections to the port from inside the tailnet only.
func (n *Node) Listen(port int) (net.Listener, error) {
	return n.srv.Listen("tcp", fmt.Sprintf(":%d", port))
}

// Caller is who is on the other end of a connection the node accepted: the
// login of that device's owner and its address inside the tailnet. A device
// with no person behind it (a tagged one) is not a caller.
func (n *Node) Caller(ctx context.Context, remoteAddr string) (login, ip string, ok bool) {
	who, err := n.lc.WhoIs(ctx, remoteAddr)
	if err != nil || who == nil || who.Node == nil || who.UserProfile == nil {
		return "", "", false
	}
	if who.Node.IsTagged() || who.UserProfile.LoginName == "" {
		return "", "", false
	}
	host, _, err := net.SplitHostPort(remoteAddr)
	if err != nil {
		return "", "", false
	}
	return who.UserProfile.LoginName, host, true
}

// Login asks Tailscale for a fresh login link; it shows up in Status.
func (n *Node) Login(ctx context.Context) error { return n.lc.StartLoginInteractive(ctx) }

// Logout removes the node from the tailnet and forgets its key.
func (n *Node) Logout(ctx context.Context) error { return n.lc.Logout(ctx) }
