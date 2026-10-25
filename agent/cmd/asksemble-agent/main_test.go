package main

import (
	"bytes"
	"net/url"
	"strings"
	"testing"
)

func TestPairURL(t *testing.T) {
	link := PairURL("pc.example.ts.net", 8800, "ABCDEFGHIJKLMNOPQRSTUVWXYZ")
	u, err := url.Parse(link)
	if err != nil {
		t.Fatal(err)
	}
	q := u.Query()
	if u.Scheme != "asksemble" || u.Host != "pair" || q.Get("v") != "1" || q.Get("host") != "pc.example.ts.net" || q.Get("port") != "8800" || q.Get("code") != "ABCDEFGHIJKLMNOPQRSTUVWXYZ" {
		t.Errorf("link = %s", link)
	}
}

func TestNormalizeAnswer(t *testing.T) {
	for in, want := range map[string]string{"y": "y", "Y\r": "y", string(rune(0xFEFF)) + "y": "y", " Yes ": "yes", "": ""} {
		if got := normalizeAnswer(in); got != want {
			t.Errorf("normalizeAnswer(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestGroupCode(t *testing.T) {
	if got := groupCode("ABCDEFGHIJKLMNOPQRSTUVWXYZ"); got != "ABCD-EFGH-IJKL-MNOP-QRST-UVWX-YZ" {
		t.Errorf("groupCode = %q", got)
	}
}

func TestPrintQR(t *testing.T) {
	for _, dark := range []bool{true, false} {
		var b bytes.Buffer
		if err := printQR(&b, PairURL("pc.example.ts.net", 8800, "ABCDEFGHIJKLMNOPQRSTUVWXYZ"), dark); err != nil {
			t.Fatal(err)
		}
		lines := strings.Split(strings.TrimRight(b.String(), "\n"), "\n")
		width := len([]rune(lines[0]))
		if width < 25 || len(lines) < width/2 {
			t.Errorf("QR looks wrong: %d lines of %d", len(lines), width)
		}
		for _, l := range lines {
			if len([]rune(l)) != width {
				t.Fatal("ragged QR lines")
			}
		}
	}
	if png, err := qrPNG("x"); err != nil || !bytes.HasPrefix(png, []byte("\x89PNG")) {
		t.Errorf("png = %v", err)
	}
}
