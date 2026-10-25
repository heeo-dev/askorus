package claude

import (
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"
)

// The test binary doubles as a fake claude executable: when
// ASKSEMBLE_FAKE_CLAUDE=1 it records its arguments and prints canned output
// instead of running tests.
//
//	ASKSEMBLE_FAKE_ARGS    file to append the JSON argument list to
//	ASKSEMBLE_FAKE_AGENTS  file whose content `agents --json --all` prints
//	ASKSEMBLE_FAKE_EXIT    exit code
//	ASKSEMBLE_FAKE_SLEEP   duration to sleep first
func TestMain(m *testing.M) {
	if os.Getenv("ASKSEMBLE_FAKE_CLAUDE") == "1" {
		os.Exit(fakeClaude(os.Args[1:]))
	}
	os.Exit(m.Run())
}

func fakeClaude(args []string) int {
	if p := os.Getenv("ASKSEMBLE_FAKE_ARGS"); p != "" {
		b, _ := json.Marshal(args)
		f, err := os.OpenFile(p, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
		if err == nil {
			f.Write(append(b, '\n'))
			f.Close()
		}
	}
	if d, err := time.ParseDuration(os.Getenv("ASKSEMBLE_FAKE_SLEEP")); err == nil {
		time.Sleep(d)
	}
	switch strings.Join(args, " ") {
	case "--version":
		fmt.Println("2.1.310 (Claude Code)")
	case "agents --json --all":
		out := "[]"
		if p := os.Getenv("ASKSEMBLE_FAKE_AGENTS"); p != "" {
			if b, err := os.ReadFile(p); err == nil {
				out = string(b)
			}
		}
		fmt.Print(out)
	default:
		fmt.Fprintln(os.Stderr, "fake claude: unexpected args")
		return 2
	}
	code, _ := strconv.Atoi(os.Getenv("ASKSEMBLE_FAKE_EXIT"))
	return code
}

// fakeRunner returns a Runner that runs this test binary as claude, and the
// file its arguments are recorded in.
func fakeRunner(t *testing.T) (*Runner, string) {
	t.Helper()
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	argsFile := t.TempDir() + string(os.PathSeparator) + "args.jsonl"
	t.Setenv("ASKSEMBLE_FAKE_CLAUDE", "1")
	t.Setenv("ASKSEMBLE_FAKE_ARGS", argsFile)
	r, err := NewRunner(exe)
	if err != nil {
		t.Fatal(err)
	}
	return r, argsFile
}

func recordedArgs(t *testing.T, file string) [][]string {
	t.Helper()
	b, err := os.ReadFile(file)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		t.Fatal(err)
	}
	var out [][]string
	for _, line := range strings.Split(strings.TrimSpace(string(b)), "\n") {
		var a []string
		if err := json.Unmarshal([]byte(line), &a); err != nil {
			t.Fatal(err)
		}
		out = append(out, a)
	}
	return out
}
