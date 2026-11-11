//go:build darwin

package main

import (
	"encoding/xml"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// #52: the launch agent's file must be XML that launchd reads, with the
// agent's path and the terminal's PATH as text, never as markup.
func TestPlistText(t *testing.T) {
	got := plistText(`/Users/a b/<x>&"y"/askorus-agent`, true, "/opt/homebrew/bin:/usr/bin:/Users/a/R&D", "/Users/a/Library/Caches/Askorus/background.out")
	d := xml.NewDecoder(strings.NewReader(got))
	d.Strict = true
	var texts []string
	for {
		tok, err := d.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("not XML: %v\n%s", err, got)
		}
		if c, ok := tok.(xml.CharData); ok {
			if s := strings.TrimSpace(string(c)); s != "" {
				texts = append(texts, s)
			}
		}
	}
	joined := strings.Join(texts, "|")
	for _, want := range []string{
		"Label|" + launchLabel,
		`ProgramArguments|/Users/a b/<x>&"y"/askorus-agent|serve|-test-page`,
		"RunAtLoad",
		detachedEnv + "|1",
		"PATH|/opt/homebrew/bin:/usr/bin:/Users/a/R&D",
		"StandardOutPath|/Users/a/Library/Caches/Askorus/background.out",
	} {
		if !strings.Contains(joined, want) {
			t.Errorf("the plist lacks %q:\n%s", want, joined)
		}
	}
	if !strings.Contains(got, `<!-- command: "/Users/a b/<x>&\"y\"/askorus-agent" serve -test-page -->`) {
		t.Errorf("the command comment:\n%s", got)
	}
}

func TestAutostartOnMac(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("PATH", "/opt/homebrew/bin:/usr/bin")
	plist := filepath.Join(home, "Library", "LaunchAgents", launchLabel+".plist")
	if autostartGet() != "" {
		t.Fatal("on before it was turned on")
	}
	if err := autostartSet("/Users/me/.local/bin/askorus-agent", false); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(plist)
	if err != nil || !strings.Contains(string(b), "<string>/opt/homebrew/bin:/usr/bin</string>") || !strings.Contains(string(b), "<string>serve</string>") {
		t.Fatalf("plist = %q, %v", b, err)
	}
	if got := autostartGet(); got != `"/Users/me/.local/bin/askorus-agent" serve` || got != autostartCommand("/Users/me/.local/bin/askorus-agent", false) {
		t.Errorf("autostartGet = %q", got)
	}
	if err := autostartRemove(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(plist); !os.IsNotExist(err) || autostartGet() != "" {
		t.Error("the plist stayed after autostart off")
	}
	if err := autostartRemove(); err != nil {
		t.Errorf("off again: %v", err)
	}
}

func TestParsePmsetSleep(t *testing.T) {
	out := "Battery Power:\n lidwake              1\n sleep                1\n displaysleep         2\nAC Power:\n lidwake              1\n sleep                10\n displaysleep         10\n"
	if got := parsePmsetSleep(out); got != 600 {
		t.Errorf("AC sleep = %d, want 600", got)
	}
	if got := parsePmsetSleep("AC Power:\n sleep                0\nBattery Power:\n sleep                5\n"); got != 0 {
		t.Errorf("never = %d, want 0", got)
	}
	for _, bad := range []string{"", "Battery Power:\n sleep 5\n", "AC Power:\n displaysleep 3\n"} {
		if got := parsePmsetSleep(bad); got != -1 {
			t.Errorf("parsePmsetSleep(%q) = %d, want -1", bad, got)
		}
	}
	// The real program on this Mac answers something this can read, or nothing.
	if got := sleepAfterAC(); got < -1 {
		t.Errorf("sleepAfterAC = %d", got)
	}
}
