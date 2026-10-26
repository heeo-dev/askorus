package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/asksemble/asksemble/agent/internal/devices"
)

// Tests never touch the real logon entry in the registry.
func init() { uninstallAutostart = func() error { return nil } }

// Saying no to uninstall changes nothing.
func TestUninstallDeclined(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "agent.json"), []byte(`{"port": 8799}`), 0o600)
	os.WriteFile(filepath.Join(dir, devices.FileName), []byte("[]\n"), 0o600)
	var out bytes.Buffer
	if err := uninstallCmd(dir, nil, strings.NewReader("n\n"), &out); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, devices.FileName)); err != nil {
		t.Error("declined uninstall deleted the device file")
	}
	if !strings.Contains(out.String(), "아무것도 바꾸지 않았습니다") {
		t.Errorf("output = %q", out.String())
	}
}

func TestUninstallAllInTempDir(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "agent")
	os.MkdirAll(dir, 0o700)
	os.WriteFile(filepath.Join(dir, "agent.json"), []byte(`{"port": 8799}`), 0o600)
	os.WriteFile(filepath.Join(dir, devices.FileName), []byte("[]\n"), 0o600)
	var out bytes.Buffer
	if err := uninstallCmd(dir, []string{"-yes", "-all"}, strings.NewReader(""), &out); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Error("-all left the agent folder")
	}
}

func TestCheckerCounts(t *testing.T) {
	var out bytes.Buffer
	c := &checker{out: &out}
	c.line("ok", m("a", "가"))
	c.line("warn", m("b", "나"))
	c.line("bad", m("c", "다"))
	if c.bad != 1 || c.warn != 1 || !strings.Contains(out.String(), "[X]") || !strings.Contains(out.String(), "다") {
		t.Errorf("checker = %+v %q", c, out.String())
	}
}
