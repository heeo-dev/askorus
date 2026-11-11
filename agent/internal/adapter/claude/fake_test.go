package claude

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/heeo-dev/askorus/agent/internal/fakecli"
)

// The test binary doubles as a fake claude executable: when
// ASKORUS_FAKE_CLAUDE=1 it records its arguments and acts like claude
// instead of running tests.
//
//	ASKORUS_FAKE_ARGS       file to append {args, cwd} to
//	ASKORUS_FAKE_AGENTS     file whose content `agents --json --all` prints
//	ASKORUS_FAKE_STATE      agents list the fake keeps up to date (--bg, stop, rm)
//	ASKORUS_FAKE_PROJECTS   projects folder where --bg writes remote links
//	ASKORUS_FAKE_UNTRUSTED  "1": --bg fails like an untrusted folder
//	ASKORUS_FAKE_NOLINK     "1": --bg never writes a remote link
//	ASKORUS_FAKE_STOP_DROPS "1": stop also drops the session from the list
//	ASKORUS_FAKE_RM_BUSY    file: while it exists, rm fails once (and deletes it)
//	ASKORUS_FAKE_EXIT       exit code
//	ASKORUS_FAKE_SLEEP      duration to sleep first
//
// Combined answers and refining (synth_test.go, refine_test.go) use the
// shared fake of internal/fakecli instead,
// which takes over when ASKORUS_FAKECLI is set (fakecli.Use).
func TestMain(m *testing.M) {
	if fakecli.Active() {
		os.Exit(fakecli.Main(os.Args[1:]))
	}
	if os.Getenv("ASKORUS_FAKE_CLAUDE") == "1" {
		os.Exit(fakeClaude(os.Args[1:]))
	}
	os.Exit(m.Run())
}

type fakeCall struct {
	Args  []string `json:"args"`
	Cwd   string   `json:"cwd"`
	Stdin string   `json:"stdin,omitempty"`
}

func fakeClaude(args []string) int {
	stdin := ""
	if len(args) > 0 && args[0] == "-p" {
		b, _ := io.ReadAll(os.Stdin)
		stdin = string(b)
	}
	if p := os.Getenv("ASKORUS_FAKE_ARGS"); p != "" {
		cwd, _ := os.Getwd()
		b, _ := json.Marshal(fakeCall{Args: args, Cwd: cwd, Stdin: stdin})
		f, err := os.OpenFile(p, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
		if err == nil {
			f.Write(append(b, '\n'))
			f.Close()
		}
	}
	if d, err := time.ParseDuration(os.Getenv("ASKORUS_FAKE_SLEEP")); err == nil {
		time.Sleep(d)
	}
	code, _ := strconv.Atoi(os.Getenv("ASKORUS_FAKE_EXIT"))
	switch {
	case strings.Join(args, " ") == "--version":
		fmt.Println("2.1.310 (Claude Code)")
	case strings.Join(args, " ") == "agents --json --all":
		out := "[]"
		for _, env := range []string{"ASKORUS_FAKE_STATE", "ASKORUS_FAKE_AGENTS"} {
			if p := os.Getenv(env); p != "" {
				if b, err := os.ReadFile(p); err == nil {
					out = string(b)
					break
				}
			}
		}
		fmt.Print(out)
	case len(args) > 0 && args[0] == "-p":
		return fakeAsk(args, stdin)
	case len(args) > 0 && args[0] == "--bg":
		return fakeBackground(args)
	case len(args) > 0 && args[0] == "remote-control":
		return fakeRemoteControl(args)
	case len(args) == 2 && (args[0] == "stop" || args[0] == "rm"):
		return fakeStopRemove(args[0], args[1])
	default:
		fmt.Fprintln(os.Stderr, "fake claude: unexpected args")
		return 2
	}
	return code
}

func fakeFlag(args []string, name string) string {
	for i := 0; i+1 < len(args); i++ {
		if args[i] == name && !strings.HasPrefix(args[i+1], "-") {
			return args[i+1]
		}
	}
	return ""
}

func fakeBackground(args []string) int {
	cwd, _ := os.Getwd()
	if os.Getenv("ASKORUS_FAKE_UNTRUSTED") == "1" {
		fmt.Fprintf(os.Stderr, "Workspace not trusted. Run claude in %s once and accept the trust prompt, then retry.\n", cwd)
		return 1
	}
	state := fakeLoadState()
	id := fakeFlag(args, "--resume")
	name := firstNonEmpty(fakeFlag(args, "-n"), fakeFlag(args, "--remote-control"))
	if id == "" {
		raw := make([]byte, 16)
		rand.Read(raw)
		h := hex.EncodeToString(raw)
		id = h[:8] + "-" + h[8:12] + "-4" + h[13:16] + "-8" + h[17:20] + "-" + h[20:32]
	}
	idx := -1
	for i, e := range state {
		if e.SessionID == id {
			idx = i
		}
	}
	if idx < 0 {
		state = append(state, agentEntry{ID: id[:8], SessionID: id, Kind: kindBackground, Name: name})
		idx = len(state) - 1
	}
	e := &state[idx]
	e.PID, e.Status, e.State, e.Cwd = 4242, "idle", "blocked", cwd
	if name != "" {
		e.Name = name
	}
	fakeSaveState(state)
	if os.Getenv("ASKORUS_FAKE_NOLINK") != "1" {
		fakeWriteLink(id, cwd, e.Name)
	}
	fmt.Printf("backgrounded · %s · %s (idle — send a prompt to start)\n", e.ID, e.Name)
	return 0
}

// fakeAsk acts like `claude -p --output-format json` with the prompt on
// stdin: one result object, and a transcript in ASKORUS_FAKE_PROJECTS.
//
//	ASKORUS_FAKE_ASK "badmodel": an unknown model, "noresume": the
//	                   resumed session does not exist, "garbage": no JSON
func fakeAsk(args []string, prompt string) int {
	cwd, _ := os.Getwd()
	id := fakeFlag(args, "--resume")
	switch os.Getenv("ASKORUS_FAKE_ASK") {
	case "noresume":
		fmt.Fprintf(os.Stderr, "No conversation found with session ID: %s\n", id)
		return 1
	case "garbage":
		fmt.Println("not json")
		return 0
	}
	if id == "" {
		raw := make([]byte, 16)
		rand.Read(raw)
		h := hex.EncodeToString(raw)
		id = h[:8] + "-" + h[8:12] + "-4" + h[13:16] + "-8" + h[17:20] + "-" + h[20:32]
	}
	model := "claude-" + fakeFlag(args, "--model") + "-5-5"
	lines := strings.Split(strings.TrimSpace(prompt), "\n")
	// The ask prompt's last line may name the answer language (#39): the question is before it.
	if n := len(lines); n >= 3 && strings.HasPrefix(lines[n-1], "(Write the answer in ") && lines[n-2] == "" {
		lines = lines[:n-2]
	}
	out := map[string]any{
		"type": "result", "subtype": "success", "is_error": false, "session_id": id,
		"result":     "fake answer: " + lines[len(lines)-1],
		"usage":      map[string]int{"input_tokens": 2, "cache_read_input_tokens": 10, "output_tokens": 7},
		"modelUsage": map[string]any{model: map[string]float64{"costUSD": 0.01}},
	}
	code := 0
	if os.Getenv("ASKORUS_FAKE_ASK") == "badmodel" {
		fmt.Fprintf(os.Stderr, "[claude-code:unrecognized_model] {\"model\":%q}\n", fakeFlag(args, "--model"))
		out["is_error"], out["result"], out["modelUsage"] = true, "There's an issue with the selected model.", map[string]any{}
		out["api_error_status"], code = 404, 1
	}
	if root := os.Getenv("ASKORUS_FAKE_PROJECTS"); root != "" {
		dir := filepath.Join(root, "ask")
		os.MkdirAll(dir, 0o700)
		u, _ := json.Marshal(map[string]any{"type": "user", "message": map[string]string{"content": prompt},
			"timestamp": time.Now().UTC().Format("2006-01-02T15:04:05.000Z"), "cwd": cwd, "sessionId": id, "entrypoint": "sdk-cli"})
		f, err := os.OpenFile(filepath.Join(dir, id+".jsonl"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
		if err == nil {
			f.Write(append(u, '\n'))
			f.Close()
		}
	}
	b, _ := json.Marshal(out)
	fmt.Println(string(b))
	return code
}

// fakeRemoteControl acts like `claude remote-control` in a terminal: it
// draws a Connected screen and ends cleanly on Ctrl+C.
//
//	ASKORUS_FAKE_SERVE_EXIT "1": end by itself after a moment (a crash)
func fakeRemoteControl(args []string) int {
	cwd, _ := os.Getwd()
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt)
	fakeRawInput()
	keys := make(chan byte, 4)
	go func() {
		b := make([]byte, 16)
		for {
			n, err := os.Stdin.Read(b)
			for _, c := range b[:n] {
				keys <- c
			}
			if err != nil {
				return
			}
		}
	}()
	if os.Getenv("ASKORUS_FAKE_NO_CONSENT") == "1" {
		// The real screen of Claude Code 2.1.322 before Remote Control was ever turned on (#52).
		fmt.Print("Take this session with you and pick up right where you left off on any device.\r\n")
		fmt.Print("The session keeps running on this machine. Use your other devices as a remote\r\ncontrol. Press Ctrl+C to stop.\r\n\r\n")
		fmt.Print("Enable Remote Control? (y/n) ")
		for c := range keys {
			switch c {
			case 'y', 'Y':
				os.WriteFile(filepath.Join(cwd, "CONSENT-BY-AGENT"), nil, 0o600) // the test fails on this
				return 0
			case 0x03, 'n', 'N':
				fmt.Print("\r\nRemote Control did not start: \"Enable Remote Control?\" got no answer. Run `claude remote-control` and answer y once; it won't ask again.\r\n")
				return 1
			}
		}
		return 1
	}
	if os.Getenv("ASKORUS_FAKE_UNTRUSTED") == "1" {
		// The real server asks in a terminal and waits for an answer.
		fmt.Printf("Quick safety check: is this a project you created or one you trust?\r\n")
		fmt.Printf("Trust %s? [y/N] \r\n", cwd)
		for c := range keys {
			switch c {
			case 'y', 'Y':
				os.WriteFile(filepath.Join(cwd, "TRUSTED-BY-AGENT"), nil, 0o600) // the test fails on this
				fmt.Print("\r\n")
				return 0
			case 0x03, 'n', 'N', '\r':
				fmt.Printf("Remote Control did not start: %s is not trusted. Run `claude rc` again to trust it.\r\n", cwd)
				fmt.Print("\r\n.\r\n")
				return 1
			}
		}
		return 1
	}
	capacity := fakeFlag(args, "--capacity")
	fmt.Print("\x1b[?25l·|· Connecting · fake · main\r\n")
	time.Sleep(100 * time.Millisecond)
	fmt.Printf("\x1b[1A\x1b[J·✔︎· Connected · fake · main\r\n    Capacity: 0/%s · New sessions will be created in the current directory\r\n", capacity)
	fmt.Print("Continue coding in the Claude mobile app or https://claude.ai/code?environment=env_FAKE123\r\n")
	fmt.Print("Or ask Claude to work in this folder from your projects on claude.ai\r\n")
	fmt.Print("space to show QR code · w to toggle spawn mode\r\n")
	if os.Getenv("ASKORUS_FAKE_SERVE_EXIT") == "1" {
		time.Sleep(200 * time.Millisecond)
		return 3
	}
	deadline := time.After(60 * time.Second)
	for {
		select {
		case c := <-keys:
			if c != 0x03 {
				continue // other keys (space: QR, w: spawn mode) do not stop it
			}
			fmt.Print("Environment preserved. Restart `claude remote-control` to reconnect existing sessions.\r\n")
			return 0
		case <-stop:
			return 5 // the real server ignores the signal; reaching this is a bug
		case <-deadline:
			return 4
		}
	}
}

func fakeStopRemove(verb, target string) int {
	// ASKORUS_FAKE_RM_BUSY names a file; while it exists, rm fails, like
	// the Claude service settling a just-stopped job. The file holds how
	// many more times to fail (empty: once); it is deleted on the last one.
	// With ASKORUS_FAKE_RM_SILENT=1 the failure prints nothing.
	if busy := os.Getenv("ASKORUS_FAKE_RM_BUSY"); verb == "rm" && busy != "" {
		if b, err := os.ReadFile(busy); err == nil {
			if n, _ := strconv.Atoi(strings.TrimSpace(string(b))); n > 1 {
				os.WriteFile(busy, []byte(strconv.Itoa(n-1)), 0o600)
			} else {
				os.Remove(busy)
			}
			if os.Getenv("ASKORUS_FAKE_RM_SILENT") != "1" {
				fmt.Fprintln(os.Stderr, "session is still settling")
			}
			return 1
		}
	}
	state := fakeLoadState()
	out := state[:0]
	found := false
	for _, e := range state {
		if e.ID == target || e.SessionID == target || e.Name == target {
			found = true
			if verb == "rm" || os.Getenv("ASKORUS_FAKE_STOP_DROPS") == "1" {
				continue
			}
			e.PID, e.Status, e.State = 0, "", "stopped"
		}
		out = append(out, e)
	}
	if !found {
		fmt.Fprintln(os.Stderr, "no such session")
		return 1
	}
	fakeSaveState(out)
	fmt.Printf("%s %s\n", map[string]string{"stop": "stopped", "rm": "removed"}[verb], target)
	return 0
}

func fakeLoadState() []agentEntry {
	var state []agentEntry
	if b, err := os.ReadFile(os.Getenv("ASKORUS_FAKE_STATE")); err == nil {
		json.Unmarshal(b, &state)
	}
	return state
}

func fakeSaveState(state []agentEntry) {
	if state == nil {
		state = []agentEntry{}
	}
	b, _ := json.Marshal(state)
	os.WriteFile(os.Getenv("ASKORUS_FAKE_STATE"), b, 0o600)
}

// fakeWriteLink appends a bridge_status line to the session's transcript,
// creating it in a "fake" project folder when it does not exist.
func fakeWriteLink(id, cwd, name string) {
	root := os.Getenv("ASKORUS_FAKE_PROJECTS")
	if root == "" {
		return
	}
	matches, _ := filepath.Glob(filepath.Join(root, "*", id+".jsonl"))
	path := filepath.Join(root, "fake", id+".jsonl")
	if len(matches) > 0 {
		path = matches[0]
	}
	os.MkdirAll(filepath.Dir(path), 0o700)
	now := time.Now().UTC().Format("2006-01-02T15:04:05.000Z")
	lines := ""
	if len(matches) == 0 {
		an, _ := json.Marshal(map[string]string{"type": "agent-name", "agentName": name, "sessionId": id})
		lines += string(an) + "\n"
	}
	bs, _ := json.Marshal(map[string]any{"type": "system", "subtype": "bridge_status", "url": "https://claude.ai/code/session_fake" + id[:8],
		"timestamp": now, "cwd": cwd, "isSidechain": false, "sessionId": id})
	lines += string(bs) + "\n"
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err == nil {
		f.WriteString(lines)
		f.Close()
	}
}

// fakeRunner returns a Runner that runs this test binary as claude, and the
// file its calls are recorded in.
func fakeRunner(t *testing.T) (*Runner, string) {
	t.Helper()
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	argsFile := t.TempDir() + string(os.PathSeparator) + "args.jsonl"
	t.Setenv("ASKORUS_FAKE_CLAUDE", "1")
	t.Setenv("ASKORUS_FAKE_ARGS", argsFile)
	r, err := NewRunner(exe)
	if err != nil {
		t.Fatal(err)
	}
	return r, argsFile
}

func recordedCalls(t *testing.T, file string) []fakeCall {
	t.Helper()
	b, err := os.ReadFile(file)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		t.Fatal(err)
	}
	var out []fakeCall
	for _, line := range strings.Split(strings.TrimSpace(string(b)), "\n") {
		var c fakeCall
		if err := json.Unmarshal([]byte(line), &c); err != nil {
			t.Fatal(err)
		}
		out = append(out, c)
	}
	return out
}

func recordedArgs(t *testing.T, file string) [][]string {
	t.Helper()
	var out [][]string
	for _, c := range recordedCalls(t, file) {
		out = append(out, c.Args)
	}
	return out
}
