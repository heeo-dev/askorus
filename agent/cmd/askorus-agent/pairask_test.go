package main

import (
	"bytes"
	"io"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/heeo-dev/askorus/agent/internal/config"
	"github.com/heeo-dev/askorus/agent/internal/devices"
	"github.com/heeo-dev/askorus/agent/internal/pairing"
)

// pair opens a window; a phone of the same Tailscale account that taps the
// PC in the app is taken at once, with no question typed at the PC, and the
// command says who came in with the two digits the phone shows (#58).
func TestPairWindowTakesAPhoneWithoutAQuestion(t *testing.T) {
	agentRuns(t, true)
	dir := t.TempDir()
	if err := config.Save(dir, config.Default()); err != nil {
		t.Fatal(err)
	}
	if err := writeNodeInfo(dir, signedInNode()); err != nil {
		t.Fatal(err)
	}
	store, err := devices.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	// The running agent's side of the pairing.
	p := &pairing.Pairer{Dir: dir, Devices: store, Poll: 10 * time.Millisecond, Timeout: 5 * time.Second}

	in, feed := io.Pipe() // the person types nothing
	defer feed.Close()
	var out bytes.Buffer
	done := make(chan error, 1)
	go func() { done <- pair(dir, nil, in, &out) }()

	// The phone asks once the window is open.
	deadline := time.Now().Add(5 * time.Second)
	for !p.WindowIsOpen() {
		if time.Now().After(deadline) {
			t.Fatal("pair opened no window")
		}
		time.Sleep(10 * time.Millisecond)
	}
	if _, err := p.StartDirect("Galaxy S23", "47", pairing.Origin{Login: "me@example.com", RemoteIP: "100.64.0.9"}, nil); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("pair: %v\n%s", err, out.String())
		}
	case <-time.After(5 * time.Second):
		t.Fatalf("pair did not end after the phone came in:\n%s", out.String())
	}
	got := out.String()
	for _, s := range []string{"tap this PC", "이 PC를 누르세요", "Paired Galaxy S23, number 47", "숫자 47", "askorus-agent devices revoke"} {
		if !strings.Contains(got, s) {
			t.Errorf("output lacks %q:\n%s", s, got)
		}
	}
	if strings.Contains(got, "[y/N]") {
		t.Errorf("pair asked a question for a phone that tapped the PC:\n%s", got)
	}
	if list, _ := store.List(); len(list) != 1 || list[0].Name != "Galaxy S23" {
		t.Errorf("devices = %+v", list)
	}
}

// The question on the PC's screen names the phone, its two digits and where
// the request came from, in both languages, and says which button allows.
func TestPairQuestionWords(t *testing.T) {
	q := pairQuestion(pairing.Question{DeviceName: "Galaxy S23", Match: "47", Login: "me@example.com", RemoteIP: "100.64.0.9"})
	want := []string{"Galaxy S23", "number 47", "me@example.com", "100.64.0.9", "same number", "숫자 47", "Tailscale 계정 me@example.com", "같은 숫자"}
	if runtime.GOOS == "windows" {
		// Windows draws its own Yes and No buttons: the text says which is which.
		want = append(want, "Yes allows it", "예: 허용")
	} else if strings.Contains(q.Text, "Yes allows it") {
		t.Error("the text names Yes and No where the buttons are labelled Allow and Refuse")
	}
	for _, s := range want {
		if !strings.Contains(q.Text, s) {
			t.Errorf("text lacks %q:\n%s", s, q.Text)
		}
	}
	if q.Title != "Askorus" || q.Allow == "" || q.Deny == "" {
		t.Errorf("question = %+v", q)
	}
}

// The run without a Tailscale node never shows a window.
func TestNoQuestionOnTheScreenInTheTestRun(t *testing.T) {
	t.Setenv(noNodeEnv, "1")
	if screenConfirm() != nil {
		t.Error("the test run would show a dialog")
	}
}
