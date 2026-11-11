//go:build linux || darwin

package claude

import (
	"bufio"
	"os"
	"strings"
	"testing"
	"time"
)

// readUntil reads the terminal's output until it holds want.
func readUntil(t *testing.T, p serverProcess, want string) string {
	t.Helper()
	got := make(chan string, 1)
	go func() {
		var all strings.Builder
		r := bufio.NewReader(p.Output())
		b := make([]byte, 256)
		for {
			n, err := r.Read(b)
			all.Write(b[:n])
			if strings.Contains(all.String(), want) || err != nil {
				got <- all.String()
				return
			}
		}
	}()
	select {
	case s := <-got:
		if !strings.Contains(s, want) {
			t.Fatalf("the terminal said %q, want %q in it", s, want)
		}
		return s
	case <-time.After(10 * time.Second):
		t.Fatalf("no %q from the terminal in 10 s", want)
		return ""
	}
}

func waitEnd(t *testing.T, p serverProcess) error {
	t.Helper()
	done := make(chan error, 1)
	go func() { done <- p.Wait() }()
	select {
	case err := <-done:
		return err
	case <-time.After(10 * time.Second):
		p.Kill()
		t.Fatal("the program did not end in 10 s")
		return nil
	}
}

// #52: a folder server needs a real terminal on macOS and Linux as it does
// on Windows. The program sees a terminal of the size given, runs in the
// folder given, and Ctrl+C written to the terminal reaches a program that
// reads keys itself as the byte 0x03, which is how the real server takes
// it. (A program that does not read keys gets a signal from the terminal
// instead; that is not tested here: on GitHub's macOS runner such a
// program, cat, was not ended by it, while on Linux it was.)
func TestPTYRunsAProgramOnATerminal(t *testing.T) {
	dir := t.TempDir()
	p, err := startPTY("/bin/sh", dir, []string{"-c",
		"test -t 0 && test -t 1 && echo on-a-terminal; pwd; stty size; stty raw -echo; echo ready; dd bs=1 count=1 2>/dev/null | od -An -tx1; echo key-read"})
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	if p.PID() <= 0 {
		t.Errorf("pid = %d", p.PID())
	}
	out := readUntil(t, p, "ready")
	if !strings.Contains(out, "on-a-terminal") {
		t.Errorf("the program saw no terminal: %q", out)
	}
	if !strings.Contains(out, "40 120") {
		t.Errorf("the terminal's size is not 40 rows by 120 columns: %q", out)
	}
	base := dir[strings.LastIndexByte(dir, '/')+1:]
	if !strings.Contains(out, base) {
		t.Errorf("the program did not run in %s: %q", dir, out)
	}
	if err := p.Interrupt(); err != nil {
		t.Fatal(err)
	}
	if key := readUntil(t, p, "key-read"); !strings.Contains(key, "03") {
		t.Errorf("the program read %q, want the byte 03 (Ctrl+C)", key)
	}
	if err := waitEnd(t, p); err != nil {
		t.Errorf("Wait = %v, want none", err)
	}
	if processAlive(p.PID(), "") {
		t.Errorf("pid %d still runs", p.PID())
	}
}

func TestPTYExitCodeAndKill(t *testing.T) {
	dir := t.TempDir()
	p, err := startPTY("/bin/sh", dir, []string{"-c", "exit 3"})
	if err != nil {
		t.Fatal(err)
	}
	if err := waitEnd(t, p); err == nil || err.Error() != "exit code 3" {
		t.Errorf("Wait = %v, want exit code 3", err)
	}
	p.Close()

	p, err = startPTY("/bin/sh", dir, []string{"-c", "exit 0"})
	if err != nil {
		t.Fatal(err)
	}
	if err := waitEnd(t, p); err != nil {
		t.Errorf("Wait = %v, want none", err)
	}
	p.Close()

	// Kill ends the session: the program and the child it started, which ignores Ctrl+C.
	p, err = startPTY("/bin/sh", dir, []string{"-c", "trap '' INT; sleep 300 & echo started; wait"})
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	readUntil(t, p, "started")
	children := 0
	for pid, parent := range processParents() {
		if parent == p.PID() {
			children++
			defer func(pid int) {
				deadline := time.Now().Add(5 * time.Second)
				for time.Now().Before(deadline) && processAlive(pid, "") {
					time.Sleep(50 * time.Millisecond)
				}
				if processAlive(pid, "") {
					t.Errorf("the child %d outlived Kill", pid)
				}
			}(pid)
		}
	}
	if children != 1 {
		t.Errorf("processParents shows %d children of the program, want 1", children)
	}
	if err := p.Kill(); err != nil {
		t.Fatal(err)
	}
	if err := waitEnd(t, p); err == nil {
		t.Error("Wait after Kill = nil")
	}
	if elevated, known := processElevated(os.Getpid()); !known || elevated != (os.Geteuid() == 0) {
		t.Errorf("processElevated(self) = %v, %v", elevated, known)
	}
}
