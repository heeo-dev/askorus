package opencode

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/heeo-dev/askorus/agent/internal/core"
	"github.com/heeo-dev/askorus/agent/internal/fakecli"
)

// The test binary doubles as a fake opencode: with ASKORUS_FAKE_OPENCODE=1
// it records {args, cwd, pwd, config, stdin} to ASKORUS_FAKE_ARGS and
// prints what `opencode run --format json` prints. Sessions it makes are
// kept in ASKORUS_FAKE_SESSIONS for `session list`.
// ASKORUS_FAKE_MODE: "badmodel", "exit" (exit 1, nothing printed).
// The combined-answer, refine and cleanup tests use internal/fakecli
// instead.
func TestMain(m *testing.M) {
	if fakecli.Active() {
		os.Exit(fakecli.Main(os.Args[1:]))
	}
	if os.Getenv("ASKORUS_FAKE_OPENCODE") == "1" {
		os.Exit(fakeOpenCode(os.Args[1:]))
	}
	os.Exit(m.Run())
}

type fakeCall struct {
	Args   []string `json:"args"`
	Cwd    string   `json:"cwd"`
	PWD    string   `json:"pwd"`
	Config string   `json:"config"`
	Stdin  string   `json:"stdin"`
}

type fakeSession struct {
	ID        string `json:"id"`
	Directory string `json:"directory"`
}

func fakeOpenCode(args []string) int {
	if len(args) == 1 && args[0] == "--version" {
		fmt.Println("2.2.1")
		return 0
	}
	in, _ := io.ReadAll(os.Stdin)
	cwd, _ := os.Getwd()
	b, _ := json.Marshal(fakeCall{Args: args, Cwd: cwd, PWD: os.Getenv("PWD"), Config: os.Getenv("OPENCODE_CONFIG_CONTENT"), Stdin: string(in)})
	if f, err := os.OpenFile(os.Getenv("ASKORUS_FAKE_ARGS"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600); err == nil {
		f.Write(append(b, '\n'))
		f.Close()
	}
	var sessions []fakeSession
	sb, _ := os.ReadFile(os.Getenv("ASKORUS_FAKE_SESSIONS"))
	json.Unmarshal(sb, &sessions)
	if len(args) >= 2 && args[0] == "session" && args[1] == "list" {
		switch os.Getenv("ASKORUS_FAKE_MODE") {
		case "listexit":
			return 1
		case "listjunk":
			fmt.Println("Database is locked")
			return 0
		}
		// Every session, whatever its folder: the adapter must check that.
		b, _ := json.MarshalIndent(append([]fakeSession{}, sessions...), "", "  ")
		fmt.Println(string(b))
		return 0
	}
	id := "ses_e773ef84effedagCyC2bkvl1W9"
	for i, a := range args {
		if a == "--session" && i+1 < len(args) {
			id = args[i+1]
		}
	}
	if !slices.ContainsFunc(sessions, func(s fakeSession) bool { return s.ID == id }) {
		sessions = append(sessions, fakeSession{ID: id, Directory: os.Getenv("PWD")})
		b, _ := json.Marshal(sessions)
		os.WriteFile(os.Getenv("ASKORUS_FAKE_SESSIONS"), b, 0o600)
	}
	switch os.Getenv("ASKORUS_FAKE_MODE") {
	case "badmodel":
		fmt.Printf("{\"type\":\"error\",\"timestamp\":1,\"sessionID\":%q,\"error\":{\"type\":\"provider.no-route\",\"message\":\"Model unavailable: x\"}}\n", id)
		return 1
	case "exit":
		return 1
	case "slow": // the session starts, then no answer in time
		fmt.Printf("{\"type\":\"step_start\",\"timestamp\":1,\"sessionID\":%q,\"part\":{\"type\":\"step-start\"}}\n", id)
		time.Sleep(20 * time.Second)
		return 0
	}
	lines := strings.Split(strings.TrimSpace(string(in)), "\n")
	// The ask prompt's last line may name the answer language (#39): the question is before it.
	if n := len(lines); n >= 3 && strings.HasPrefix(lines[n-1], "(Write the answer in ") && lines[n-2] == "" {
		lines = lines[:n-2]
	}
	answer, _ := json.Marshal("fake: " + lines[len(lines)-1])
	ev := func(s string) { fmt.Print(s + "\r\n") }
	ev(fmt.Sprintf(`{"type":"step_start","timestamp":1,"sessionID":%q,"part":{"type":"step-start"}}`, id))
	ev(fmt.Sprintf(`{"type":"text","timestamp":1,"sessionID":%q,"part":{"type":"text","text":"검색해 볼게요."}}`, id))
	ev(fmt.Sprintf(`{"type":"tool_use","timestamp":1,"sessionID":%q,"part":{"type":"tool","tool":"websearch","state":{"status":"completed"}}}`, id))
	ev(fmt.Sprintf(`{"type":"step_finish","timestamp":1,"sessionID":%q,"part":{"type":"step-finish","reason":"tool-calls","tokens":{"input":100,"output":5,"cache":{"read":20,"write":0}}}}`, id))
	ev(fmt.Sprintf(`{"type":"step_start","timestamp":1,"sessionID":%q,"part":{"type":"step-start"}}`, id))
	ev(fmt.Sprintf(`{"type":"text","timestamp":1,"sessionID":%q,"part":{"type":"text","text":%s}}`, id, answer))
	// The last step_finish is sometimes never printed; neither here.
	return 0
}

func fakeAdapter(t *testing.T, tiers map[string]core.AskModel) (*Adapter, string, string) {
	t.Helper()
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	t.Setenv("ASKORUS_FAKE_OPENCODE", "1")
	t.Setenv("ASKORUS_FAKE_ARGS", filepath.Join(dir, "args.jsonl"))
	t.Setenv("ASKORUS_FAKE_SESSIONS", filepath.Join(dir, "sessions.json"))
	t.Setenv("ASKORUS_FAKE_MODE", "")
	folder := filepath.Join(dir, "ask")
	os.MkdirAll(folder, 0o700)
	return New(Options{Path: exe, Tiers: tiers}), filepath.Join(dir, "args.jsonl"), folder
}

func calls(t *testing.T, file string) []fakeCall {
	t.Helper()
	b, _ := os.ReadFile(file)
	var out []fakeCall
	for _, l := range strings.Split(strings.TrimSpace(string(b)), "\n") {
		if l == "" {
			continue
		}
		var c fakeCall
		if err := json.Unmarshal([]byte(l), &c); err != nil {
			t.Fatal(err)
		}
		out = append(out, c)
	}
	return out
}

func TestInfo(t *testing.T) {
	a, _, _ := fakeAdapter(t, nil)
	// A phone request that went away first must not leave opencode out of
	// asks for the next five minutes.
	gone, cancel := context.WithCancel(context.Background())
	cancel()
	a.Info(gone)
	info := a.Info(context.Background())
	if !info.Available || info.Version != "2.2.1" || !slices.Equal(info.Capabilities, []core.Capability{core.CapAsk}) {
		t.Errorf("info = %+v", info)
	}
	missing := New(Options{Path: filepath.Join(t.TempDir(), "opencode.exe")})
	if info := missing.Info(context.Background()); info.Available || !slices.Contains(info.Notices, NoticeNotFound) {
		t.Errorf("missing info = %+v", info)
	}
}

// 11-06: a one-off ask (a debate turn) and a new ask cut off delete their
// conversation; a carried-on one cut off keeps it.
func TestOneOffAndCutOffSessionsAreDeleted(t *testing.T) {
	a, argsFile, folder := fakeAdapter(t, nil)
	const id = "ses_e773ef84effedagCyC2bkvl1W9"
	last := func() fakeCall {
		cs := calls(t, argsFile)
		return cs[len(cs)-1]
	}
	isDelete := func(c fakeCall) bool { return slices.Equal(c.Args, deleteArgs(id)) && c.PWD == folder }

	res, err := a.Ask(context.Background(), core.AskRequest{Folder: folder, Prompt: "q", Tier: core.TierQuick, OneOff: true})
	if err != nil || res.SessionID != "" || res.Answer == "" || !isDelete(last()) {
		t.Fatalf("one-off: %+v %v, last call %q", res, err, last().Args)
	}
	if _, err := a.Ask(context.Background(), core.AskRequest{Folder: folder, Prompt: "q", Tier: core.TierQuick, OneOff: true, Resume: id}); err == nil {
		t.Error("a one-off ask resumed")
	}

	// A new ask that fails after its session started: deleted.
	t.Setenv("ASKORUS_FAKE_MODE", "badmodel")
	n := len(calls(t, argsFile))
	if _, err := a.Ask(context.Background(), core.AskRequest{Folder: folder, Prompt: "q", Tier: core.TierQuick}); err == nil || !isDelete(last()) || len(calls(t, argsFile)) != n+2 {
		t.Errorf("failed new ask: %v, last call %q", err, last().Args)
	}
	// A follow-up that fails keeps its conversation.
	n = len(calls(t, argsFile))
	if _, err := a.Ask(context.Background(), core.AskRequest{Folder: folder, Prompt: "q", Tier: core.TierQuick, Resume: id}); err == nil || isDelete(last()) {
		t.Errorf("failed follow-up: %v, last call %q (calls %d → %d)", err, last().Args, n, len(calls(t, argsFile)))
	}

	// A new ask that times out: deleted all the same.
	t.Setenv("ASKORUS_FAKE_MODE", "slow")
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if _, err := a.Ask(ctx, core.AskRequest{Folder: folder, Prompt: "q", Tier: core.TierQuick}); core.ErrorCode(err) != core.ErrCodeAskTimeout || !isDelete(last()) {
		t.Errorf("timed out: %v, last call %q", err, last().Args)
	}
}

func TestAskAndFollowUp(t *testing.T) {
	a, argsFile, folder := fakeAdapter(t, nil)
	prompt := core.AskPrompt("대한민국의 수도는?\n한 문장으로. 100% \"정확히\" & 짧게")
	res, err := a.Ask(context.Background(), core.AskRequest{Folder: folder, Prompt: prompt, Tier: core.TierStandard})
	if err != nil {
		t.Fatal(err)
	}
	c := calls(t, argsFile)[0]
	want := []string{"run", "--standalone", "--format", "json", "--agent", "askorus-ask", "-m", "opencode-go/deepseek-v4.1-flash#high"}
	if !reflect.DeepEqual(c.Args, want) || c.Stdin != prompt || c.PWD != folder {
		t.Errorf("call = %+v", c)
	}
	var cfg struct {
		Agent map[string]struct {
			Mode       string            `json:"mode"`
			Permission map[string]string `json:"permission"`
		} `json:"agent"`
	}
	if err := json.Unmarshal([]byte(c.Config), &cfg); err != nil {
		t.Fatalf("config %q: %v", c.Config, err)
	}
	perm := cfg.Agent["askorus-ask"].Permission
	if cfg.Agent["askorus-ask"].Mode != "primary" || !reflect.DeepEqual(perm, map[string]string{"*": "deny", "webfetch": "allow", "websearch": "allow"}) {
		t.Errorf("agent config = %s", c.Config)
	}
	// The answer is the last step's text, not the lead-in before the search.
	if res.Answer != `fake: 한 문장으로. 100% "정확히" & 짧게` || res.SessionID != "ses_e773ef84effedagCyC2bkvl1W9" ||
		res.Model != "opencode-go/deepseek-v4.1-flash#high" || res.InputTokens != 120 || res.OutputTokens != 5 {
		t.Errorf("result = %+v", res)
	}

	// A follow-up checks the session is the ask folder's, then continues it.
	if _, err := a.Ask(context.Background(), core.AskRequest{Folder: folder, Prompt: "더", Tier: core.TierDeep, Resume: res.SessionID}); err != nil {
		t.Fatal(err)
	}
	cs := calls(t, argsFile)
	if got := cs[1].Args; !reflect.DeepEqual(got, []string{"session", "list", "--format", "json", "--standalone"}) || cs[1].PWD != folder {
		t.Errorf("check = %+v", cs[1])
	}
	want = []string{"run", "--standalone", "--format", "json", "--agent", "askorus-ask", "-m", "opencode-go/deepseek-v4-pro#high", "--session", res.SessionID}
	if got := cs[2].Args; !reflect.DeepEqual(got, want) {
		t.Errorf("follow-up args = %q", got)
	}
}

func TestFollowUpNeedsAnAskSession(t *testing.T) {
	a, argsFile, folder := fakeAdapter(t, nil)
	// Unknown here (or another folder's): never run, since --session would
	// quietly start an empty session under that id.
	_, err := a.Ask(context.Background(), core.AskRequest{Folder: folder, Prompt: "q", Tier: core.TierStandard, Resume: "ses_0000000000000000000000fake"})
	if core.ErrorCode(err) != core.ErrCodeNotFound {
		t.Fatalf("err = %v", err)
	}
	// A real session of another folder (a coding session) is not carried
	// on as an ask either.
	other := fakeSession{ID: "ses_e773ef84effedagCyC2bkvlOTHR", Directory: t.TempDir()}
	b, _ := json.Marshal([]fakeSession{other})
	os.WriteFile(os.Getenv("ASKORUS_FAKE_SESSIONS"), b, 0o600)
	_, err = a.Ask(context.Background(), core.AskRequest{Folder: folder, Prompt: "q", Tier: core.TierStandard, Resume: other.ID})
	if core.ErrorCode(err) != core.ErrCodeNotFound {
		t.Fatalf("other folder's session: %v", err)
	}
	// When the list cannot be read, nothing runs.
	for _, mode := range []string{"listexit", "listjunk"} {
		t.Setenv("ASKORUS_FAKE_MODE", mode)
		_, err = a.Ask(context.Background(), core.AskRequest{Folder: folder, Prompt: "q", Tier: core.TierStandard, Resume: other.ID})
		if core.ErrorCode(err) != core.ErrCodeAskFailed {
			t.Errorf("%s: %v", mode, err)
		}
	}
	for _, c := range calls(t, argsFile) {
		if c.Args[0] == "run" {
			t.Errorf("ran %q", c.Args)
		}
	}
}

// 11-06 user decision: quick Flash low, standard Flash high, deep Pro high;
// the variants each model takes (Flash refused medium and xhigh).
func TestTiersAndEfforts(t *testing.T) {
	a, _, _ := fakeAdapter(t, nil)
	want := map[string]core.AskModel{
		core.TierQuick:    {Model: "opencode-go/deepseek-v4.1-flash", Effort: "low"},
		core.TierStandard: {Model: "opencode-go/deepseek-v4.1-flash", Effort: "high"},
		core.TierDeep:     {Model: "opencode-go/deepseek-v4-pro", Effort: "high"},
	}
	if got := a.AskTiers(); !reflect.DeepEqual(got, want) {
		t.Errorf("AskTiers = %v", got)
	}
	wantE := map[string][]string{
		"opencode-go/deepseek-v4.1-flash": {"low", "high", "max"},
		"opencode-go/deepseek-v4-pro":     {"high", "max"},
	}
	if got := a.AskEfforts(); !reflect.DeepEqual(got, wantE) {
		t.Errorf("AskEfforts = %v", got)
	}
	// A model named with its variant in agent.json is looked up without
	// it; one this adapter does not know offers only its own variant.
	b, _, _ := fakeAdapter(t, map[string]core.AskModel{
		core.TierQuick: {Model: "opencode-go/deepseek-v4-pro#max"},
		core.TierDeep:  {Model: "opencode-go/glm-5.2", Effort: "high"},
	})
	got := b.AskEfforts()
	if !reflect.DeepEqual(got["opencode-go/deepseek-v4-pro#max"], []string{"high", "max"}) || !reflect.DeepEqual(got["opencode-go/glm-5.2"], []string{"high"}) {
		t.Errorf("AskEfforts with overrides = %v", got)
	}
}

func TestModelsAndVariants(t *testing.T) {
	a, argsFile, folder := fakeAdapter(t, map[string]core.AskModel{core.TierQuick: {Model: "opencode-go/glm-5.2"}})
	if m := a.AskTiers()[core.TierQuick]; m.Model != "opencode-go/glm-5.2" || m.Effort != "" {
		t.Errorf("override keeps the old variant: %+v", m)
	}
	for _, c := range []struct {
		req  core.AskRequest
		want string
	}{
		{core.AskRequest{Tier: core.TierQuick}, "opencode-go/glm-5.2"},
		{core.AskRequest{Tier: core.TierStandard, Effort: "low"}, "opencode-go/deepseek-v4.1-flash#low"},
		{core.AskRequest{Tier: core.TierStandard, Model: "openrouter/anthropic/claude-x#high"}, "openrouter/anthropic/claude-x#high"},
		{core.AskRequest{Tier: core.TierStandard, Model: "openrouter/anthropic/claude-x#high", Effort: "max"}, "openrouter/anthropic/claude-x#max"},
	} {
		c.req.Folder, c.req.Prompt = folder, "q"
		if _, err := a.Ask(context.Background(), c.req); err != nil {
			t.Fatal(err)
		}
		cs := calls(t, argsFile)
		if got := cs[len(cs)-1].Args[7]; got != c.want {
			t.Errorf("%+v: model %q, want %q", c.req, got, c.want)
		}
	}
}

func TestAskErrors(t *testing.T) {
	a, argsFile, folder := fakeAdapter(t, nil)
	ask := func(req core.AskRequest) error {
		req.Folder, req.Prompt, req.Tier = folder, "q", core.TierStandard
		_, err := a.Ask(context.Background(), req)
		return err
	}
	t.Setenv("ASKORUS_FAKE_MODE", "badmodel")
	if err := ask(core.AskRequest{Model: "opencode-go/nope"}); core.ErrorCode(err) != core.ErrCodeBadModel || !strings.Contains(err.Error(), "Model unavailable") {
		t.Errorf("bad model: %v", err)
	}
	t.Setenv("ASKORUS_FAKE_MODE", "exit")
	if err := ask(core.AskRequest{}); core.ErrorCode(err) != core.ErrCodeAskFailed {
		t.Errorf("exit 1: %v", err)
	}
	t.Setenv("ASKORUS_FAKE_MODE", "")
	n := len(calls(t, argsFile))
	for name, req := range map[string]core.AskRequest{
		"no provider":     {Model: "deepseek"},
		"model as a flag": {Model: "--auto"},
		"bad variant":     {Effort: "MAX"},
		"bad session":     {Resume: "ses_../../x"},
	} {
		if err := ask(req); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	if got := len(calls(t, argsFile)); got != n {
		t.Errorf("refused asks ran opencode %d times", got-n)
	}
}

func TestCheckArgs(t *testing.T) {
	good := askArgs("opencode-go/deepseek-v4.1-flash#max", "")
	if err := checkArgs(good); err != nil {
		t.Fatal(err)
	}
	for name, args := range map[string][]string{
		"auto":        append(slices.Clone(good), "--auto"),
		"build agent": {"run", "--standalone", "--format", "json", "--agent", "build", "-m", "opencode-go/x"},
		"no agent":    {"run", "--standalone", "--format", "json", "-m", "opencode-go/x"},
		"service":     {"run", "--format", "json", "--agent", "askorus-ask", "-m", "opencode-go/x"},
		"prompt arg":  append(slices.Clone(good), "hello"),
		"share":       append(slices.Clone(good), "--share"),
	} {
		if err := checkArgs(args); err == nil {
			t.Errorf("%s: %q accepted", name, args)
		}
	}
}

func TestFindExeBehindTheNpmShim(t *testing.T) {
	npm := t.TempDir()
	os.WriteFile(filepath.Join(npm, "opencode.cmd"), []byte("@echo off\r\n"), 0o600)
	exe := filepath.Join(npm, "node_modules", "@opencode", "cli", "bin", "opencode.exe")
	os.MkdirAll(filepath.Dir(exe), 0o700)
	os.WriteFile(exe, []byte("MZ"), 0o700)
	t.Setenv("PATH", npm)
	t.Setenv("PATHEXT", ".COM;.EXE;.BAT;.CMD")
	if got, err := findExe(""); filepath.Ext(exe) == ".exe" && (err != nil || got != exe) && os.PathSeparator == '\\' {
		t.Errorf("findExe = %q, %v; want %q", got, err, exe)
	}
	if _, err := findExe(filepath.Join(npm, "opencode.cmd")); err == nil {
		t.Error("a .cmd setting was accepted")
	}
}
