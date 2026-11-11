//go:build !windows

package main

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

// ownCache gives the test a cache folder of its own, so the lock files of
// a real agent on this machine are never touched.
func ownCache(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, ".cache"))
	dir, err := runDir()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(dir, home) {
		t.Fatalf("the run folder %q is outside the test's home %q", dir, home)
	}
	return dir
}

func release(port int) {
	heldMu.Lock()
	defer heldMu.Unlock()
	if f := held[port]; f != nil {
		f.Close()
		delete(held, port)
	}
}

// #52: what the named event does on Windows, a lock file does here. The
// lock, not the file, says that an agent runs; shutdown's signal reaches
// the process whose id the file holds.
func TestLockTellsARunningAgentAndStopsIt(t *testing.T) {
	dir := ownCache(t)
	const port = 48790
	if agentOnPort(port) {
		t.Fatal("an agent before any serve")
	}
	if err := signalStop(port); err == nil {
		t.Fatal("stopped an agent that is not there")
	}
	// A file left behind by a crash: an id inside, nobody holding the lock.
	stale := filepath.Join(dir, "agent-"+strconv.Itoa(port)+".lock")
	if err := os.WriteFile(stale, []byte(strconv.Itoa(os.Getpid())+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if agentOnPort(port) || signalStop(port) == nil {
		t.Fatal("a lock file nobody holds counts as a running agent")
	}

	stop := listenStop(port)
	t.Cleanup(func() { release(port) })
	if !agentOnPort(port) || lockedPID(port) != os.Getpid() {
		t.Fatalf("serve holds the lock, but agentOnPort = %v, pid = %d", agentOnPort(port), lockedPID(port))
	}
	if agentOnPort(port + 1) {
		t.Error("another port counts as served")
	}
	select {
	case <-stop:
		t.Fatal("stopped before anyone asked")
	default:
	}
	// shutdown: the signal goes to the lock's holder, which is this test.
	if err := signalStop(port); err != nil {
		t.Fatal(err)
	}
	select {
	case <-stop:
	case <-time.After(5 * time.Second):
		t.Fatal("serve did not hear the stop")
	}
	// Once the agent is gone its lock is gone with it.
	release(port)
	if agentOnPort(port) {
		t.Error("an agent after its process gave the lock up")
	}
}

func TestStartDetachedRunsOnItsOwnAndLogs(t *testing.T) {
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	log := filepath.Join(dir, "background.out")
	// This test binary with a filter that matches nothing: it starts, prints PASS and ends.
	pid, err := startDetached(exe, []string{"-test.run", "^$"}, dir, log)
	if err != nil || pid <= 0 {
		t.Fatalf("startDetached = %d, %v", pid, err)
	}
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if b, _ := os.ReadFile(log); strings.Contains(string(b), "PASS") {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	b, _ := os.ReadFile(log)
	if !strings.Contains(string(b), "PASS") {
		t.Errorf("the log holds %q, want the program's output", b)
	}
	if info, err := os.Stat(log); err != nil || info.Mode().Perm() != 0o600 {
		t.Errorf("the log file: %v, %v", info, err)
	}
	if !processRunning(os.Getpid()) || processRunning(0) || processRunning(-1) {
		t.Error("processRunning")
	}
}

func TestSelfCommand(t *testing.T) {
	if got := selfCommand("/home/a b/askorus-agent", "serve", "-test-page"); got != `"/home/a b/askorus-agent" serve -test-page` {
		t.Errorf("selfCommand = %s", got)
	}
}
