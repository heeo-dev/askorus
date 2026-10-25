package tailnet

import "testing"

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
