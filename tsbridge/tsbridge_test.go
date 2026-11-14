package tsbridge

import (
	"context"
	"net/netip"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"tailscale.com/ipn/ipnstate"
	"tailscale.com/tailcfg"
	"tailscale.com/types/key"
	"tailscale.com/types/views"
)

// tailnet is a signed-in node (user 1) that sees: its own PC, the PC's
// agent node, a device a friend shared in, and a tagged server.
func tailnet() *ipnstate.Status {
	peer := func(name string, user tailcfg.UserID, ip string, mut func(*ipnstate.PeerStatus)) *ipnstate.PeerStatus {
		p := &ipnstate.PeerStatus{
			HostName:     strings.SplitN(name, ".", 2)[0],
			DNSName:      name + ".",
			UserID:       user,
			TailscaleIPs: []netip.Addr{netip.MustParseAddr(ip)},
			Online:       true,
		}
		if mut != nil {
			mut(p)
		}
		return p
	}
	tags := views.SliceOf([]string{"tag:server"})
	return &ipnstate.Status{
		BackendState: "Running",
		Self:         &ipnstate.PeerStatus{UserID: 1, DNSName: "askorus-phone.tail0.ts.net."},
		Peer: map[key.NodePublic]*ipnstate.PeerStatus{
			key.NewNode().Public(): peer("askorus-pc.tail0.ts.net", 1, "100.64.0.1", nil),
			key.NewNode().Public(): peer("asleep-pc.tail0.ts.net", 1, "100.64.0.2", func(p *ipnstate.PeerStatus) { p.Online = false }),
			key.NewNode().Public(): peer("friend-pc.tail9.ts.net", 2, "100.64.0.3", nil),
			key.NewNode().Public(): peer("shared-out.tail0.ts.net", 1, "100.64.0.4", func(p *ipnstate.PeerStatus) { p.ShareeNode = true }),
			key.NewNode().Public(): peer("server.tail0.ts.net", 1, "100.64.0.5", func(p *ipnstate.PeerStatus) { p.Tags = &tags }),
		},
	}
}

// Only a device of the person's own account gets requests (#57, #42): a
// pairing link someone sent, a friend's shared PC, or a server with no
// person behind it must never get the phone's token or questions.
func TestMatchPeer(t *testing.T) {
	st := tailnet()
	for host, want := range map[string]peerInfo{
		"askorus-pc.tail0.ts.net":  {Known: true, SameUser: true, Online: true, Name: "askorus-pc", IP: "100.64.0.1"},
		"ASKORUS-PC.tail0.ts.net.": {Known: true, SameUser: true, Online: true, Name: "askorus-pc", IP: "100.64.0.1"},
		"100.64.0.1":               {Known: true, SameUser: true, Online: true, Name: "askorus-pc", IP: "100.64.0.1"},
		"asleep-pc.tail0.ts.net":   {Known: true, SameUser: true, Online: false, Name: "asleep-pc", IP: "100.64.0.2"},
		"friend-pc.tail9.ts.net":   {Known: true, SameUser: false, Online: true, Name: "friend-pc", IP: "100.64.0.3"},
		"shared-out.tail0.ts.net":  {Known: true, SameUser: false, Online: true, Name: "shared-out", IP: "100.64.0.4"},
		"server.tail0.ts.net":      {Known: true, SameUser: false, Online: true, Name: "server", IP: "100.64.0.5"},
		// Not in the tailnet's own table: a name on the internet, a name
		// that only looks like the PC's, an address outside.
		"evil.example.com":                 {},
		"askorus-pc.tail0.ts.net.evil.com": {},
		"askorus-pc":                       {},
		"8.8.8.8":                          {},
		"":                                 {},
	} {
		got, err := matchPeer(st, host)
		if err != nil || got != want {
			t.Errorf("%q = %+v, %v; want %+v", host, got, err, want)
		}
	}
}

// Before the app is signed in nothing is a known device, and the error
// says which of the two it is, so the app can show the login step.
func TestMatchPeerStates(t *testing.T) {
	for state, code := range map[string]string{"NeedsLogin": CodeNeedsLogin, "NoState": CodeNeedsLogin, "Starting": CodeNotConnected} {
		_, err := matchPeer(&ipnstate.Status{BackendState: state}, "askorus-pc.tail0.ts.net")
		if got := errorCode(err); got != code {
			t.Errorf("%s: %v (code %q), want %q", state, err, got, code)
		}
	}
	// A node with no user of its own (should not happen) owns nothing.
	st := tailnet()
	st.Self.UserID = 0
	for _, p := range st.Peer {
		p.UserID = 0
	}
	if got, _ := matchPeer(st, "askorus-pc.tail0.ts.net"); got.SameUser {
		t.Errorf("a node without a user owns %+v", got)
	}
}

// R-113. Right after the app starts the node is up before the list of the
// tailnet's devices has arrived. A request made then waits for the list
// instead of saying that the person's own PC is not one of their devices.
func TestAwaitPeerWaitsForTheListAfterStart(t *testing.T) {
	const pc = "askorus-pc.tail0.ts.net"
	oldWait, oldPoll := peerWait, peerPoll
	peerWait, peerPoll = 400*time.Millisecond, 10*time.Millisecond
	defer func() { peerWait, peerPoll = oldWait, oldPoll }()
	ctx := context.Background()

	// The list is empty for the first few reads (the node is coming up), then it is there.
	empty := &ipnstate.Status{BackendState: "Starting", Self: &ipnstate.PeerStatus{UserID: 1}}
	notYet := &ipnstate.Status{BackendState: "Starting"}
	reads := 0
	late := func(context.Context, bool) (*ipnstate.Status, error) {
		reads++
		switch {
		case reads <= 2:
			return notYet, nil
		case reads <= 4:
			return empty, nil
		}
		return tailnet(), nil
	}
	p, err := awaitPeer(ctx, pc, time.Now(), late)
	if err != nil || !p.Known || !p.SameUser || reads != 5 {
		t.Errorf("after the list arrived: %+v, %v (%d reads)", p, err, reads)
	}

	// A node that has been running for a while answers at once: a name it
	// does not know is not one of its devices. One fresh read, no waiting.
	reads = 0
	up := func(context.Context, bool) (*ipnstate.Status, error) { reads++; return tailnet(), nil }
	t0 := time.Now()
	p, err = awaitPeer(ctx, "evil.example.com", time.Now().Add(-time.Hour), up)
	if err != nil || p.Known || reads != 2 || time.Since(t0) > 200*time.Millisecond {
		t.Errorf("an unknown name on a running node: %+v, %v (%d reads, %s)", p, err, reads, time.Since(t0))
	}
	// Someone else's device is known at once, and not the person's.
	if p, err := awaitPeer(ctx, "friend-pc.tail9.ts.net", time.Now(), up); err != nil || !p.Known || p.SameUser {
		t.Errorf("a friend's device: %+v, %v", p, err)
	}

	// A node that just started and really has no such device: it waits its
	// few seconds, then says so.
	reads = 0
	p, err = awaitPeer(ctx, "evil.example.com", time.Now(), up)
	if err != nil || p.Known || reads < 5 {
		t.Errorf("an unknown name right after the start: %+v, %v (%d reads)", p, err, reads)
	}

	// A node that never comes up (the phone is offline): the tailnet is not
	// up, which is not the same as "not your device".
	stuck := func(context.Context, bool) (*ipnstate.Status, error) { return empty, nil }
	if _, err := awaitPeer(ctx, pc, time.Now(), stuck); errorCode(err) != CodeNotConnected {
		t.Errorf("a node that never came up: %v", err)
	}
	// Not signed in: nothing to wait for.
	t0 = time.Now()
	login := func(context.Context, bool) (*ipnstate.Status, error) {
		return &ipnstate.Status{BackendState: "NeedsLogin"}, nil
	}
	if _, err := awaitPeer(ctx, pc, time.Now(), login); errorCode(err) != CodeNeedsLogin || time.Since(t0) > 200*time.Millisecond {
		t.Errorf("not signed in: %v after %s", err, time.Since(t0))
	}
	// The request's own time limit ends the wait.
	short, cancel := context.WithTimeout(ctx, 30*time.Millisecond)
	defer cancel()
	t0 = time.Now()
	if _, err := awaitPeer(short, pc, time.Now(), stuck); errorCode(err) != CodeNotConnected || time.Since(t0) > 300*time.Millisecond {
		t.Errorf("a request whose time is up: %v after %s", err, time.Since(t0))
	}
}

// Fetch refuses before it sends anything: no node, a bad address, https.
func TestFetchRefusals(t *testing.T) {
	if _, err := Fetch("GET", "http://askorus-pc.tail0.ts.net:8790/v1/info", "", "", 1000); errorCode(err) != CodeNotStarted {
		t.Errorf("not started: %v", err)
	}
	if _, err := Peer("askorus-pc.tail0.ts.net"); errorCode(err) != CodeNotStarted {
		t.Errorf("peer, not started: %v", err)
	}
	if errorCode(nil) != "" || errorCode(coded(CodeTooLarge, "x")) != CodeTooLarge {
		t.Error("errorCode")
	}
	for _, e := range []string{"dial tcp: i/o timeout", "context deadline exceeded: x", "x: tailnet_login"} {
		if errorCode(errString(e)) != "" {
			t.Errorf("%q has a code", e)
		}
	}
}

type errString string

func (e errString) Error() string { return string(e) }

// A request goes to the address the check was made on, through the tunnel
// only (#57 review). The library's own dial would ask the phone's DNS for a
// name the tailnet no longer has, and would reach an address that is no
// device of the tailnet over the phone's own network: the token would leave
// in plain text. So the request is aimed at the checked device's Tailscale
// address, and the dial takes nothing else.
func TestTargetIsTheCheckedAddress(t *testing.T) {
	u, _ := url.Parse("http://askorus-pc.tail0.ts.net:8790/v1/info?x=1")
	got, err := targetOf(u, peerInfo{Known: true, SameUser: true, IP: "100.64.0.1"})
	if err != nil || got != "http://100.64.0.1:8790/v1/info?x=1" {
		t.Errorf("target = %q, %v", got, err)
	}
	if u.Host != "askorus-pc.tail0.ts.net:8790" {
		t.Errorf("the request's own address changed: %q", u.Host)
	}
	u6, _ := url.Parse("http://askorus-pc.tail0.ts.net/v1/info")
	if got, err := targetOf(u6, peerInfo{IP: "fd7a:115c:a1e0::1"}); err != nil || got != "http://[fd7a:115c:a1e0::1]:80/v1/info" {
		t.Errorf("IPv6 target = %q, %v", got, err)
	}
	// A device with no address yet, or one outside the tailnet's ranges
	// (a list that was tampered with), gets nothing.
	for _, ip := range []string{"", "not-an-ip", "93.184.216.34", "192.168.0.5", "127.0.0.1"} {
		if got, err := targetOf(u, peerInfo{Known: true, SameUser: true, IP: ip}); err == nil {
			t.Errorf("IP %q: target %q", ip, got)
		}
	}
}

func TestDialTakesOnlyTailnetAddresses(t *testing.T) {
	dial := dialTailnet(nil) // refused before the node is asked
	for _, addr := range []string{
		"askorus-pc.tail0.ts.net:8790", // a name: never looked up
		"evil.example.com:80",
		"93.184.216.34:80", // the internet
		"192.168.0.5:8790", // the phone's own network
		"127.0.0.1:8790",
		"100.64.0.1", // no port
	} {
		c, err := dial(context.Background(), "tcp", addr)
		if err == nil {
			c.Close()
			t.Errorf("%s was dialed", addr)
		} else if errorCode(err) != CodeBadRequest {
			t.Errorf("%s: %v", addr, err)
		}
	}
}

// The log kept in memory holds no login link: whoever opens one puts this
// phone on their own Tailscale network.
func TestLogKeepsNoLoginLink(t *testing.T) {
	var r logRing
	r.addf("To start this tsnet server, restart with TS_AUTHKEY set, or go to: %s", "https://login.tailscale.com/a/abc123def")
	r.addf("AuthURL is https://login.tailscale.com/a/zzz999\n")
	got := strings.Join(r.lines, "\n")
	if strings.Contains(got, "login.tailscale.com") || strings.Contains(got, "abc123def") || strings.Contains(got, "zzz999") {
		t.Errorf("the log holds a login link:\n%s", got)
	}
	if strings.Count(got, "(login link)") != 2 {
		t.Errorf("log = %s", got)
	}
}

// The bridge and the agent are built with the same tags, and the build
// script passes them.
func TestBuildTagsMatch(t *testing.T) {
	if _, err := os.Stat(filepath.Join("..", "agent", "go.mod")); err != nil {
		t.Skip("the bridge's folder alone, not the whole repository")
	}
	for _, f := range []string{"build-android.sh", filepath.Join("..", "agent", "internal", "tsnode", "tsnode.go")} {
		b, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(b), buildTags) {
			t.Errorf("%s does not name the tags %s", f, buildTags)
		}
	}
}

// The app finds its PCs in this list instead of in a QR code (#58): only
// the devices a request may go to are in it, so a friend's shared PC or a
// tagged server is never offered for pairing.
func TestOwnDevices(t *testing.T) {
	list, err := ownDevices(tailnet())
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, d := range list {
		names = append(names, d.Name)
	}
	if want := "askorus-pc.tail0.ts.net asleep-pc.tail0.ts.net"; strings.Join(names, " ") != want {
		t.Errorf("own devices = %q, want %q", names, want)
	}
	if list[0].Host != "askorus-pc" || !list[0].Online || list[1].Online {
		t.Errorf("devices = %+v", list)
	}
	// Not signed in: an error with its code, not an empty list.
	if _, err := ownDevices(&ipnstate.Status{BackendState: "NeedsLogin"}); errorCode(err) != CodeNeedsLogin {
		t.Errorf("not signed in: %v", err)
	}
	if _, err := ownDevices(&ipnstate.Status{BackendState: "Starting"}); errorCode(err) != CodeNotConnected {
		t.Errorf("starting: %v", err)
	}
	if _, err := Peers(); errorCode(err) != CodeNotStarted {
		t.Errorf("not started: %v", err)
	}
}
