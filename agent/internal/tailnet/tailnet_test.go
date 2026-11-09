package tailnet

import (
	"strings"
	"testing"
)

func TestParseStatus(t *testing.T) {
	b := []byte(`{
		"BackendState": "Running",
		"Self": {"DNSName": "My-PC.example.ts.net.", "UserID": 42, "HostName": "my-pc"},
		"User": {"42": {"ID": 42, "LoginName": "owner@example.com"}, "7": {"LoginName": "kid@example.com"}},
		"Peer": {}
	}`)
	s, err := ParseStatus(b)
	if err != nil {
		t.Fatal(err)
	}
	if !s.Running || s.DNSName != "my-pc.example.ts.net" || s.OwnerLogin != "owner@example.com" {
		t.Errorf("status = %+v", s)
	}
	s, err = ParseStatus([]byte(`{"BackendState":"Stopped"}`))
	if err != nil || s.Running || s.OwnerLogin != "" {
		t.Errorf("stopped = %+v %v", s, err)
	}
}

func TestParseServe(t *testing.T) {
	b := []byte(`{
		"TCP": {"443": {"HTTPS": true}, "8800": {"HTTPS": true}, "9000": {"HTTPS": true}},
		"Web": {
			"my-pc.example.ts.net:443": {"Handlers": {"/": {"Proxy": "http://127.0.0.1:45279"}}},
			"my-pc.example.ts.net:8800": {"Handlers": {"/": {"Proxy": "http://127.0.0.1:8790"}}},
			"my-pc.example.ts.net:9000": {"Handlers": {"/": {"Proxy": "http://localhost:8791"}}}
		},
		"AllowFunnel": {"my-pc.example.ts.net:9000": true}
	}`)
	s, ok, err := ParseServe(b, 8790)
	if err != nil || !ok || s.HTTPSPort != 8800 || s.Host != "my-pc.example.ts.net" || s.Funnel {
		t.Errorf("8790 = %+v %v %v", s, ok, err)
	}
	s, ok, _ = ParseServe(b, 8791)
	if !ok || !s.Funnel || s.HTTPSPort != 9000 {
		t.Errorf("8791 = %+v %v", s, ok)
	}
	if _, ok, _ := ParseServe(b, 1234); ok {
		t.Error("unserved port found")
	}
	if _, ok, err := ParseServe([]byte("\n"), 8790); ok || err != nil {
		t.Errorf("empty output = %v %v", ok, err)
	}
}

// R-89 (#34, found by Codex): a funnel on any serve that reaches the agent
// counts, whichever entry the map gives first.
func TestParseServeSeesFunnelOnAnyEntry(t *testing.T) {
	cases := map[string]struct {
		json string
		port int
	}{
		"second web port": {`{
			"Web": {
				"pc.example.ts.net:443": {"Handlers": {"/": {"Proxy": "http://127.0.0.1:8790"}}},
				"pc.example.ts.net:8443": {"Handlers": {"/": {"Proxy": "http://localhost:8790/"}}}
			},
			"AllowFunnel": {"pc.example.ts.net:8443": true}
		}`, 8443},
		"a path that is not /": {`{
			"Web": {
				"pc.example.ts.net:443": {"Handlers": {"/": {"Proxy": "http://127.0.0.1:8790"}}},
				"pc.example.ts.net:10000": {"Handlers": {"/api": {"Proxy": "http://127.0.0.1:8790/v1"}}}
			},
			"AllowFunnel": {"pc.example.ts.net:10000": true}
		}`, 10000},
		"a tcp forward": {`{
			"TCP": {"443": {"HTTPS": true}, "10001": {"TCPForward": "127.0.0.1:8790"}},
			"Web": {"pc.example.ts.net:443": {"Handlers": {"/": {"Proxy": "http://127.0.0.1:8790"}}}},
			"AllowFunnel": {"pc.example.ts.net:10001": true}
		}`, 10001},
	}
	for name, c := range cases {
		for i := 0; i < 50; i++ { // map order changes from run to run
			s, ok, err := ParseServe([]byte(c.json), 8790)
			if err != nil || !ok || !s.Funnel || s.HTTPSPort != c.port {
				t.Fatalf("%s (try %d) = %+v %v %v, want funnel on %d", name, i, s, ok, err, c.port)
			}
		}
	}
}

// With no funnel the phones get the lowest port whose "/" is the agent,
// every time; a funnel on another program's port does not count.
func TestParseServePicksThePhonePort(t *testing.T) {
	b := []byte(`{
		"Web": {
			"pc.example.ts.net:9443": {"Handlers": {"/": {"Proxy": "http://127.0.0.1:8790"}}},
			"pc.example.ts.net:8443": {"Handlers": {"/": {"Proxy": "http://127.0.0.1:8790"}}},
			"pc.example.ts.net:443": {"Handlers": {"/": {"Proxy": "http://127.0.0.1:45279"}}},
			"pc.example.ts.net:7000": {"Handlers": {"/x": {"Proxy": "http://127.0.0.1:8790"}}}
		},
		"AllowFunnel": {"pc.example.ts.net:443": true}
	}`)
	for i := 0; i < 50; i++ {
		s, ok, err := ParseServe(b, 8790)
		if err != nil || !ok || s.Funnel || s.HTTPSPort != 8443 || s.Host != "pc.example.ts.net" {
			t.Fatalf("try %d = %+v %v %v, want 8443 without funnel", i, s, ok, err)
		}
	}
	// Only a path that is not / reaches the agent: phones cannot use it.
	only := []byte(`{"Web": {"pc.example.ts.net:7000": {"Handlers": {"/x": {"Proxy": "http://127.0.0.1:8790"}}}}}`)
	if _, ok, _ := ParseServe(only, 8790); ok {
		t.Error("a serve without / counted as the phone port")
	}
}

func TestProxiesTo(t *testing.T) {
	for _, ok := range []string{"http://127.0.0.1:8790", "http://localhost:8790/", "http://LOCALHOST:8790/v1", "https://127.0.0.1:8790", "http://[::1]:8790"} {
		if !proxiesTo(ok, 8790) {
			t.Errorf("proxiesTo(%q) = false", ok)
		}
	}
	for _, bad := range []string{"", "http://127.0.0.1:8791", "http://10.0.0.5:8790", "http://127.0.0.1", "file:///x", "http://127.0.0.1:87900"} {
		if proxiesTo(bad, 8790) {
			t.Errorf("proxiesTo(%q) = true", bad)
		}
	}
	if !forwardsTo("127.0.0.1:8790", 8790) || !forwardsTo("tcp://localhost:8790", 8790) || forwardsTo("127.0.0.1:22", 8790) || forwardsTo("", 8790) {
		t.Error("forwardsTo")
	}
}

func TestParseServePortsAndHTTPS(t *testing.T) {
	ports, err := ParseServePorts([]byte(`{"TCP":{"443":{"HTTPS":true},"8443":{"HTTPS":true}},"Web":{"pc.ts.net:8790":{}}}`))
	if err != nil {
		t.Fatal(err)
	}
	got := map[int]bool{}
	for _, p := range ports {
		got[p] = true
	}
	if len(ports) != 3 || !got[443] || !got[8443] || !got[8790] {
		t.Errorf("ports = %v", ports)
	}
	s, _ := ParseStatus([]byte(`{"BackendState":"Running","CertDomains":["pc.example.ts.net"]}`))
	if !s.HTTPS {
		t.Error("HTTPS not seen")
	}
	s, _ = ParseStatus([]byte(`{"BackendState":"Running"}`))
	if s.HTTPS {
		t.Error("HTTPS without cert domains")
	}
}

func TestIsTailnetIP(t *testing.T) {
	for _, ip := range []string{"100.64.0.1", "100.127.255.254", "fd7a:115c:a1e0::1", "::ffff:100.100.1.1"} {
		if !IsTailnetIP(ip) {
			t.Errorf("%s should be a tailnet IP", ip)
		}
	}
	for _, ip := range []string{"127.0.0.1", "100.128.0.1", "192.168.0.10", "", "100.64.0.1, 1.2.3.4", "fd7a:115c:a1e1::1"} {
		if IsTailnetIP(ip) {
			t.Errorf("%q should not be a tailnet IP", ip)
		}
	}
}

// setup's serve (#37) adds one port that proxies to the agent on
// 127.0.0.1, in the background, and refuses ports out of range.
func TestServeOnArgs(t *testing.T) {
	got, err := serveOnArgs(8790, 8790)
	if err != nil || strings.Join(got, " ") != "serve --bg --https=8790 http://127.0.0.1:8790" {
		t.Errorf("serveOnArgs = %q, %v", got, err)
	}
	for _, p := range [][2]int{{0, 8790}, {8790, 0}, {70000, 8790}, {8790, -1}} {
		if _, err := serveOnArgs(p[0], p[1]); err == nil {
			t.Errorf("serveOnArgs(%d, %d) took a bad port", p[0], p[1])
		}
	}
}
