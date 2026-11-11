//go:build linux

package claude

import (
	"os"
	"strconv"
	"testing"
)

func TestParseProcStat(t *testing.T) {
	line := func(comm, state, start string) []byte {
		return []byte("1014 (" + comm + ") " + state + " 1 1014 1014 0 -1 4194560 100 0 0 0 5 3 0 0 20 0 11 0 " + start + " 1000000 2000 18446744073709551615 1 1 0 0 0 0 0 0 0 0 0 0 17 3 0 0 0 0 0\n")
	}
	for _, c := range []struct {
		name  string
		in    []byte
		state byte
		start string
		ok    bool
	}{
		{"plain", line("claude", "S", "32907"), 'S', "32907", true},
		{"a name with spaces and brackets", line("a b) R (c", "R", "6590"), 'R', "6590", true},
		{"ended, not collected", line("claude", "Z", "7"), 'Z', "7", true},
		{"no bracket", []byte("1014 claude S 1"), 0, "", false},
		{"cut short", []byte("1014 (claude) S 1 2 3"), 0, "", false},
		{"empty", nil, 0, "", false},
	} {
		state, start, ok := parseProcStat(c.in)
		if state != c.state || start != c.start || ok != c.ok {
			t.Errorf("%s: %q %q %v, want %q %q %v", c.name, state, start, ok, c.state, c.start, c.ok)
		}
	}
}

// The start time must match when one is given: the pid of a session file
// left behind may belong to another process by now.
func TestProcessAliveChecksTheStartTime(t *testing.T) {
	b, err := os.ReadFile("/proc/" + strconv.Itoa(os.Getpid()) + "/stat")
	if err != nil {
		t.Fatal(err)
	}
	_, start, ok := parseProcStat(b)
	if !ok {
		t.Fatalf("this process's stat line: %q", b)
	}
	if !processAlive(os.Getpid(), start) {
		t.Errorf("not running with its own start time %s", start)
	}
	if processAlive(os.Getpid(), start+"0") {
		t.Error("running with another start time")
	}
}
