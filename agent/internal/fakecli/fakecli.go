// Package fakecli is a fake claude, codex and opencode for tests (P2-3,
// docs/testing.md). A test binary that calls Main from its TestMain acts
// as the AI programs: it picks the AI from ASKSEMBLE_FAKECLI, and what to
// do from the shape of its arguments, the way the adapters build them:
// --version, an ask, a judge, and OpenCode's session list and delete. Every
// call but --version is recorded with its arguments, folder, the
// environment the adapters set and stdin, so a test can check that the
// question went on stdin and never into an argument.
//
// Only tests import this package; TestNotInAgent checks that the agent
// does not. Hook it in as the first line of a package's TestMain:
//
//	if fakecli.Active() {
//		os.Exit(fakecli.Main(os.Args[1:]))
//	}
//
// and point the adapter at the exe that Use (one AI) or Install (all three
// under their own names) returns.
//
// Environment:
//
//	ASKSEMBLE_FAKECLI          claude, codex, opencode, or auto: the AI named
//	                           by the executable (claude.exe, codex.exe, …)
//	ASKSEMBLE_FAKECLI_RECORD   folder; each call writes <unixnano>-<pid>.json
//	ASKSEMBLE_FAKECLI_STATE    folder for OpenCode's sessions.json (with a
//	                           lock file) and the _once flags
//	ASKSEMBLE_FAKE_JUDGE       judge mode: "prose" for every AI, or per AI
//	                           "codex=usage_limit,claude=prose_once"; _once
//	                           is only that AI's first judge call, later
//	                           ones follow rule (see Modes)
//	ASKSEMBLE_FAKECLI_SLEEP    how long mode slow waits (default 60s)
//	ASKSEMBLE_FAKECLI_VERSION  "0.163.0" for every AI or "codex=0.163.0";
//	                           default the tested minimum of each AI
//	ASKSEMBLE_FAKECLI_FAIL     OpenCode session commands that fail: "list"
//	                           (exit 1), "listjunk" (no JSON), "delete" (every
//	                           delete) or "delete=<id>", comma separated
//	ASKSEMBLE_FAKECLI_ANSWERS  path of a JSON file {role: answer text}: an ask
//	                           of a role named there answers that text
//	                           instead of "fake…: <the prompt's last line>"
//	                           (S3 scenario tests); sessions are unchanged
//	ASKSEMBLE_FAKE_ASK         ask mode: "fail" for every AI, or per AI
//	                           "opencode=fail,codex=empty"; fail is the AI's
//	                           usage limit (exit 1), empty an answer with
//	                           no text, ok (the default) an answer
package fakecli

import (
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"sort"
	"strings"
	"testing"
	"time"
)

// Environment variables (see the package comment).
const (
	EnvRole    = "ASKSEMBLE_FAKECLI"
	EnvRecord  = "ASKSEMBLE_FAKECLI_RECORD"
	EnvState   = "ASKSEMBLE_FAKECLI_STATE"
	EnvJudge   = "ASKSEMBLE_FAKE_JUDGE"
	EnvSleep   = "ASKSEMBLE_FAKECLI_SLEEP"
	EnvVersion = "ASKSEMBLE_FAKECLI_VERSION"
	EnvFail    = "ASKSEMBLE_FAKECLI_FAIL"
	EnvAnswers = "ASKSEMBLE_FAKECLI_ANSWERS"
	EnvAsk     = "ASKSEMBLE_FAKE_ASK"
)

// Ask modes of ASKSEMBLE_FAKE_ASK.
const (
	AskOK    = "ok"    // the default: an answer
	AskFail  = "fail"  // the AI's usage limit: Claude is_error, Codex turn.failed, OpenCode an error event; exit 1
	AskEmpty = "empty" // the ask ends well with no answer text
)

// AskModes lists the ask modes.
var AskModes = []string{AskOK, AskFail, AskEmpty}

// Roles.
const (
	RoleClaude   = "claude"
	RoleCodex    = "codex"
	RoleOpenCode = "opencode"
	RoleAuto     = "auto" // from the executable's name; Install uses it
)

// Roles lists the AI programs the fake plays.
var Roles = []string{RoleClaude, RoleCodex, RoleOpenCode}

// Versions are what --version prints by default: the tested minimum of
// each AI (P2-3 version table).
var Versions = map[string]string{RoleClaude: "2.1.314", RoleCodex: "0.164.0", RoleOpenCode: "2.2.1"}

// DefaultSleep is how long mode slow waits when ASKSEMBLE_FAKECLI_SLEEP
// is not set.
const DefaultSleep = 60 * time.Second

// Call is one recorded run.
type Call struct {
	Role  string   `json:"role"`
	Args  []string `json:"args"`
	Dir   string   `json:"dir"` // the process folder
	Stdin string   `json:"stdin"`
	// Env holds only PWD and OPENCODE_CONFIG_CONTENT, when they are set.
	Env map[string]string `json:"env,omitempty"`
	// Schema is the content of codex's --output-schema file.
	Schema string    `json:"schema,omitempty"`
	At     time.Time `json:"at"`
}

// Session is one OpenCode conversation the fake keeps, in the fields and
// order `opencode session list --format json` prints. Times are Unix
// milliseconds.
type Session struct {
	ID        string `json:"id"`
	Title     string `json:"title"`
	Updated   int64  `json:"updated"`
	Created   int64  `json:"created"`
	ProjectID string `json:"projectId"`
	Directory string `json:"directory"`
}

// Active reports whether this process was started as a fake AI program.
func Active() bool { return os.Getenv(EnvRole) != "" }

// Main plays the AI program and returns the exit code.
func Main(args []string) int {
	role, err := roleOf(os.Getenv(EnvRole))
	if err != nil {
		fmt.Fprintln(os.Stderr, "fakecli:", err)
		return 2
	}
	if len(args) == 1 && args[0] == "--version" {
		return printVersion(role)
	}
	in, _ := io.ReadAll(os.Stdin)
	c := &Call{Role: role, Args: args, Stdin: string(in), At: time.Now().UTC()}
	c.Dir, _ = os.Getwd()
	for _, k := range []string{"PWD", "OPENCODE_CONFIG_CONTENT"} {
		if v, ok := os.LookupEnv(k); ok {
			if c.Env == nil {
				c.Env = map[string]string{}
			}
			c.Env[k] = v
		}
	}
	var schemaErr error
	if role == RoleCodex {
		if p := flagValue(args, "--output-schema"); p != "" {
			b, err := os.ReadFile(p)
			c.Schema, schemaErr = string(b), err
		}
	}
	record(c)
	switch role {
	case RoleClaude:
		return claudeMain(c)
	case RoleCodex:
		return codexMain(c, schemaErr)
	default:
		return opencodeMain(c)
	}
}

func roleOf(v string) (string, error) {
	if v == RoleAuto {
		// The name it was started by: a hard link of the test binary.
		name := ""
		if len(os.Args) > 0 {
			name = filepath.Base(os.Args[0])
		}
		if exe, err := os.Executable(); (name == "" || name == ".") && err == nil {
			name = filepath.Base(exe)
		}
		v = strings.TrimSuffix(strings.ToLower(name), ".exe")
	}
	if !slices.Contains(Roles, v) {
		return "", fmt.Errorf("unknown role %q", v)
	}
	return v, nil
}

// printVersion prints what each AI's --version prints.
func printVersion(role string) int {
	v, err := pick(os.Getenv(EnvVersion), role, Versions[role], func(v string) bool { return v != "" })
	if err != nil {
		fmt.Fprintln(os.Stderr, "fakecli:", err)
		return 2
	}
	switch role {
	case RoleClaude:
		fmt.Println(v + " (Claude Code)")
	case RoleCodex:
		fmt.Println("codex-cli " + v)
	default:
		fmt.Println(v)
	}
	return 0
}

// pick reads a setting that is either one value for every AI or
// "role=value" items, comma separated; an AI's own item wins.
func pick(setting, role, def string, valid func(string) bool) (string, error) {
	all, own := "", ""
	allSet, ownSet := false, false
	seen := map[string]bool{}
	for _, item := range strings.Split(setting, ",") {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		key, val, perRole := strings.Cut(item, "=")
		if !perRole {
			key, val = "", item
		}
		key, val = strings.TrimSpace(key), strings.TrimSpace(val)
		switch {
		case perRole && !slices.Contains(Roles, key):
			return "", fmt.Errorf("unknown role %q in %q", key, setting)
		case seen[key]:
			return "", fmt.Errorf("%q is set twice in %q", key, setting)
		case !valid(val):
			return "", fmt.Errorf("unknown value %q in %q", val, setting)
		}
		seen[key] = true
		switch {
		case !perRole:
			all, allSet = val, true
		case key == role:
			own, ownSet = val, true
		}
	}
	switch {
	case ownSet:
		return own, nil
	case allSet:
		return all, nil
	}
	return def, nil
}

// record writes c to the record folder, one file per call so parallel
// calls never share a file. It is renamed into place whole.
func record(c *Call) {
	dir := os.Getenv(EnvRecord)
	if dir == "" {
		return
	}
	b, err := json.Marshal(c)
	if err != nil {
		return
	}
	name := filepath.Join(dir, fmt.Sprintf("%d-%d", time.Now().UnixNano(), os.Getpid()))
	if os.WriteFile(name+".tmp", b, 0o600) == nil {
		os.Rename(name+".tmp", name+".json")
	}
}

// sleepSlow waits like a judge that never ends in time.
func sleepSlow() {
	d, err := time.ParseDuration(os.Getenv(EnvSleep))
	if err != nil {
		d = DefaultSleep
	}
	time.Sleep(d)
}

// flagValue is the value after name, or "".
func flagValue(args []string, name string) string {
	for i := 0; i+1 < len(args); i++ {
		if args[i] == name {
			return args[i+1]
		}
	}
	return ""
}

// askMode is role's ask mode (ASKSEMBLE_FAKE_ASK).
func askMode(role string) (string, error) {
	return pick(os.Getenv(EnvAsk), role, AskOK, func(v string) bool { return slices.Contains(AskModes, v) })
}

// askText is what an ask of role answers: its text in the answers file,
// or prefix and the prompt's last line.
func askText(role, prefix, prompt string) (string, error) {
	if p := os.Getenv(EnvAnswers); p != "" {
		b, err := os.ReadFile(p)
		if err != nil {
			return "", err
		}
		texts := map[string]string{}
		if err := json.Unmarshal(b, &texts); err != nil {
			return "", fmt.Errorf("answers file: %w", err)
		}
		if text, ok := texts[role]; ok {
			return text, nil
		}
	}
	lines := strings.Split(strings.TrimSpace(prompt), "\n")
	return prefix + lines[len(lines)-1], nil
}

// SetAnswers writes the answers file for asks (ASKSEMBLE_FAKECLI_ANSWERS)
// and points the fake at it, for the rest of the test.
func SetAnswers(t testing.TB, texts map[string]string) {
	t.Helper()
	b, err := json.Marshal(texts)
	if err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(t.TempDir(), "answers.json")
	if err := os.WriteFile(p, b, 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv(EnvAnswers, p)
}

func unexpected(role string) int {
	fmt.Fprintf(os.Stderr, "fake %s: unexpected arguments\n", role)
	return 2
}

// newUUID is a random id in the form claude sessions and codex threads
// have.
func newUUID() string {
	b := make([]byte, 16)
	rand.Read(b)
	b[6] = b[6]&0x0f | 0x40
	b[8] = b[8]&0x3f | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

// --- state: OpenCode sessions and the _once flags ---

const (
	sessionsFile = "sessions.json"
	lockFile     = "sessions.lock"
	lockStale    = 10 * time.Second
)

// withLock runs f while it holds the state folder's lock file. A lock
// left by a killed process is taken over after lockStale.
func withLock(dir string, f func() error) error {
	if dir == "" {
		return errors.New("fakecli: no state folder")
	}
	lock := filepath.Join(dir, lockFile)
	deadline := time.Now().Add(2 * lockStale)
	for {
		fh, err := os.OpenFile(lock, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
		if err == nil {
			fh.Close()
			break
		}
		if info, serr := os.Stat(lock); serr == nil && time.Since(info.ModTime()) > lockStale {
			os.Remove(lock)
			continue
		}
		if time.Now().After(deadline) {
			return errors.New("fakecli: the state lock stays taken")
		}
		time.Sleep(5 * time.Millisecond)
	}
	defer os.Remove(lock)
	return f()
}

func loadSessions(dir string) ([]Session, error) {
	b, err := os.ReadFile(filepath.Join(dir, sessionsFile))
	if errors.Is(err, os.ErrNotExist) {
		return []Session{}, nil
	}
	if err != nil {
		return nil, err
	}
	out := []Session{}
	return out, json.Unmarshal(b, &out)
}

func saveSessions(dir string, ss []Session) error {
	b, err := json.MarshalIndent(ss, "", "  ")
	if err != nil {
		return err
	}
	p := filepath.Join(dir, sessionsFile)
	if err := os.WriteFile(p+".tmp", b, 0o600); err != nil {
		return err
	}
	return os.Rename(p+".tmp", p)
}

// updateSessions changes the saved sessions under the lock.
func updateSessions(f func([]Session) ([]Session, error)) error {
	dir := os.Getenv(EnvState)
	return withLock(dir, func() error {
		ss, err := loadSessions(dir)
		if err != nil {
			return err
		}
		ss, err = f(ss)
		if err != nil {
			return err
		}
		return saveSessions(dir, ss)
	})
}

// firstCall reports whether this is the first judge call of role in mode
// (for _once). Creating the flag file is the count, so parallel calls
// agree on which one was first. Without a state folder every call is the
// first.
func firstCall(role, mode string) bool {
	dir := os.Getenv(EnvState)
	if dir == "" {
		return true
	}
	f, err := os.OpenFile(filepath.Join(dir, "once-"+role+"-"+mode), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return false
	}
	f.Close()
	return true
}

// --- for tests ---

// Use makes this test binary the fake AI program role for the rest of
// the test: it sets the environment with t.Setenv (so the test may not be
// parallel), with new record and state folders and every mode back to its
// default, and returns the executable to give the adapter.
func Use(t testing.TB, role string) (exe string) {
	t.Helper()
	if !slices.Contains(Roles, role) {
		t.Fatalf("fakecli.Use: unknown role %q", role)
	}
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	setEnv(t, role)
	return exe
}

// Install puts the test binary in a new folder as claude, codex and
// opencode (claude.exe … on Windows; hard links, or copies), with role
// auto, for tests that run all three through the agent (S3). It returns
// role → executable. The package whose test binary this is must call
// Main from its TestMain.
func Install(t testing.TB) map[string]string {
	t.Helper()
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	ext := ""
	if runtime.GOOS == "windows" {
		ext = ".exe"
	}
	out := map[string]string{}
	for _, role := range Roles {
		dst := filepath.Join(dir, role+ext)
		if err := os.Link(exe, dst); err != nil {
			if err := copyFile(exe, dst); err != nil {
				t.Fatal(err)
			}
		}
		out[role] = dst
	}
	setEnv(t, RoleAuto)
	return out
}

func setEnv(t testing.TB, role string) {
	t.Helper()
	t.Setenv(EnvRole, role)
	t.Setenv(EnvRecord, t.TempDir())
	t.Setenv(EnvState, t.TempDir())
	for _, k := range []string{EnvJudge, EnvSleep, EnvVersion, EnvFail, EnvAnswers, EnvAsk} {
		t.Setenv(k, "")
	}
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o755)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}

// Calls returns the recorded calls, oldest first.
func Calls(t testing.TB) []Call {
	t.Helper()
	dir := os.Getenv(EnvRecord)
	if dir == "" {
		t.Fatal("fakecli.Calls: call Use or Install first")
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	type named struct {
		name string
		c    Call
	}
	var list []named
	for _, e := range entries {
		if !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		b, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			t.Fatal(err)
		}
		var c Call
		if err := json.Unmarshal(b, &c); err != nil {
			t.Fatalf("fakecli: record %s: %v", e.Name(), err)
		}
		list = append(list, named{e.Name(), c})
	}
	sort.SliceStable(list, func(i, j int) bool {
		if !list[i].c.At.Equal(list[j].c.At) {
			return list[i].c.At.Before(list[j].c.At)
		}
		return list[i].name < list[j].name
	})
	out := make([]Call, len(list))
	for i, n := range list {
		out[i] = n.c
	}
	return out
}

// Sessions returns the OpenCode sessions the fake keeps.
func Sessions(t testing.TB) []Session {
	t.Helper()
	dir := os.Getenv(EnvState)
	var out []Session
	err := withLock(dir, func() error {
		var err error
		out, err = loadSessions(dir)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

// SetSessions replaces the OpenCode sessions the fake keeps.
func SetSessions(t testing.TB, ss []Session) {
	t.Helper()
	if ss == nil {
		ss = []Session{}
	}
	dir := os.Getenv(EnvState)
	if err := withLock(dir, func() error { return saveSessions(dir, ss) }); err != nil {
		t.Fatal(err)
	}
}
