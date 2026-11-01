package codex

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

	"github.com/asksemble/asksemble/agent/internal/core"
	"github.com/asksemble/asksemble/agent/internal/fakecli"
	"github.com/asksemble/asksemble/agent/internal/proc"
)

// The test binary doubles as a fake codex. The judge tests use the shared
// fake (internal/fakecli). The ask tests keep their own: with
// ASKSEMBLE_FAKE_CODEX=1 it records {args, cwd, stdin} to
// ASKSEMBLE_FAKE_ARGS and prints what `codex exec --json` prints.
// ASKSEMBLE_FAKE_MODE: "badmodel", "noresume", "nothing" (exit 0, no turn).
func TestMain(m *testing.M) {
	if fakecli.Active() {
		os.Exit(fakecli.Main(os.Args[1:]))
	}
	if os.Getenv("ASKSEMBLE_FAKE_CODEX") == "1" {
		os.Exit(fakeCodex(os.Args[1:]))
	}
	os.Exit(m.Run())
}

type fakeCall struct {
	Args  []string `json:"args"`
	Cwd   string   `json:"cwd"`
	Stdin string   `json:"stdin"`
}

func fakeCodex(args []string) int {
	if len(args) == 1 && args[0] == "--version" {
		fmt.Println("codex-cli 0.164.0")
		return 0
	}
	in, _ := io.ReadAll(os.Stdin)
	cwd, _ := os.Getwd()
	b, _ := json.Marshal(fakeCall{Args: args, Cwd: cwd, Stdin: string(in)})
	if f, err := os.OpenFile(os.Getenv("ASKSEMBLE_FAKE_ARGS"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600); err == nil {
		f.Write(append(b, '\n'))
		f.Close()
	}
	thread := "01a188b9-9dfe-7b92-b37c-d12a1eca2234"
	if slices.Contains(args, "resume") {
		thread = args[len(args)-2]
	}
	switch os.Getenv("ASKSEMBLE_FAKE_MODE") {
	case "noresume":
		fmt.Fprintf(os.Stderr, "Error: thread/resume: thread/resume failed: no rollout found for thread id %s (code -32600)\n", thread)
		return 1
	case "badmodel":
		inner := `{"type":"error","status":400,"error":{"type":"invalid_request_error","message":"The 'x' model is not supported when using Codex with a ChatGPT account.","code":"model_not_found"}}`
		q, _ := json.Marshal(inner)
		fmt.Printf("{\"type\":\"thread.started\",\"thread_id\":%q}\n", thread)
		fmt.Println(`{"type":"item.completed","item":{"id":"item_0","type":"error","message":"Model metadata for x not found."}}`)
		fmt.Printf("{\"type\":\"error\",\"message\":%s}\n{\"type\":\"turn.failed\",\"error\":{\"message\":%s}}\n", q, q)
		return 1
	case "nothing":
		return 0
	}
	lines := strings.Split(strings.TrimSpace(string(in)), "\n")
	fmt.Printf("{\"type\":\"thread.started\",\"thread_id\":%q}\r\n", thread)
	fmt.Println(`{"type":"turn.started"}`)
	fmt.Println(`{"type":"item.completed","item":{"id":"item_0","type":"agent_message","text":"확인하겠습니다."}}`)
	fmt.Println(`{"type":"item.completed","item":{"id":"item_1","type":"web_search","id":"exec-1","query":"q"}}`)
	a, _ := json.Marshal("fake: " + lines[len(lines)-1])
	fmt.Printf("{\"type\":\"item.completed\",\"item\":{\"id\":\"item_2\",\"type\":\"agent_message\",\"text\":%s}}\n", a)
	fmt.Println(`{"type":"turn.completed","usage":{"input_tokens":100,"cached_input_tokens":80,"output_tokens":9}}`)
	return 0
}

func fakeAdapter(t *testing.T, tiers map[string]core.AskModel) (*Adapter, string, string) {
	t.Helper()
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	argsFile := filepath.Join(t.TempDir(), "args.jsonl")
	t.Setenv("ASKSEMBLE_FAKE_CODEX", "1")
	t.Setenv("ASKSEMBLE_FAKE_ARGS", argsFile)
	t.Setenv("ASKSEMBLE_FAKE_MODE", "")
	return New(Options{Path: exe, Tiers: tiers}), argsFile, t.TempDir()
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
	// A phone request that went away first must not leave codex out of
	// asks for the next five minutes.
	gone, cancel := context.WithCancel(context.Background())
	cancel()
	a.Info(gone)
	info := a.Info(context.Background())
	if !info.Available || info.Version != "0.164.0" || !slices.Equal(info.Capabilities, []core.Capability{core.CapAsk}) || len(info.Notices) != 0 {
		t.Errorf("info = %+v", info)
	}
	missing := New(Options{Path: filepath.Join(t.TempDir(), "codex.exe")})
	if info := missing.Info(context.Background()); info.Available || len(info.Capabilities) != 0 || !slices.Contains(info.Notices, NoticeNotFound) {
		t.Errorf("missing info = %+v", info)
	}
	if res, err := a.ListSessions(context.Background()); err != nil || res.Sessions == nil || len(res.Sessions) != 0 {
		t.Errorf("sessions = %+v %v", res, err)
	}
}

// wantOff is featuresOff written out: a name dropped from, added to or
// moved in the list fails the exact-argument tests of asks and judges.
var wantOff = []string{
	"shell_tool", "unified_exec", "hooks", "apps", "plugins", "remote_plugin",
	"multi_agent", "browser_use", "browser_use_external", "in_app_browser", "computer_use",
	"image_generation", "view_image", "goals", "sleep_tool", "tool_suggest", "skill_search",
	"skill_mcp_dependency_install",
}

// wantDisables is "--disable", name for each of wantOff.
func wantDisables() []string {
	var out []string
	for _, f := range wantOff {
		out = append(out, "--disable", f)
	}
	return out
}

const testThread = "01a188b9-9dfe-7b92-b37c-d12a1eca2234"

func TestAskAndFollowUp(t *testing.T) {
	a, argsFile, folder := fakeAdapter(t, nil)
	prompt := core.AskPrompt("대한민국의 수도는?\n한 문장으로. 100% \"정확히\" & 짧게")
	res, err := a.Ask(context.Background(), core.AskRequest{Folder: folder, Prompt: prompt, Tier: core.TierStandard})
	if err != nil {
		t.Fatal(err)
	}
	c := calls(t, argsFile)[0]
	want := slices.Concat([]string{"--search", "exec", "-s", "read-only", "--ignore-user-config", "--skip-git-repo-check", "--json",
		"-m", "gpt-6.1-terra", "-c", `model_reasoning_effort="medium"`}, wantDisables(), []string{"-"})
	if !reflect.DeepEqual(c.Args, want) {
		t.Errorf("args = %q\nwant %q", c.Args, want)
	}
	if c.Stdin != prompt {
		t.Errorf("stdin = %q", c.Stdin)
	}
	for _, arg := range c.Args {
		if strings.Contains(arg, "\n") || strings.Contains(arg, "수도") || strings.Contains(arg, "정확히") {
			t.Errorf("prompt text in argument %q", arg)
		}
	}
	if ec, _ := filepath.EvalSymlinks(c.Cwd); !strings.EqualFold(ec, mustEval(folder)) {
		t.Errorf("cwd = %q", c.Cwd)
	}
	// The last agent message is the answer; the lead-in is not.
	if res.Answer != `fake: 한 문장으로. 100% "정확히" & 짧게` || res.SessionID != "01a188b9-9dfe-7b92-b37c-d12a1eca2234" ||
		res.Model != "gpt-6.1-terra" || res.InputTokens != 100 || res.OutputTokens != 9 {
		t.Errorf("result = %+v", res)
	}
	res2, err := a.Ask(context.Background(), core.AskRequest{Folder: folder, Prompt: "더", Tier: core.TierDeep, Effort: "high", Resume: res.SessionID})
	if err != nil {
		t.Fatal(err)
	}
	want = slices.Concat([]string{"--search", "exec", "-s", "read-only", "resume", "--ignore-user-config", "--skip-git-repo-check", "--json",
		"-m", "gpt-6.1-sol", "-c", `model_reasoning_effort="high"`, "-c", `sandbox_mode="read-only"`}, wantDisables(), []string{res.SessionID, "-"})
	if got := calls(t, argsFile)[1].Args; !reflect.DeepEqual(got, want) {
		t.Errorf("follow-up args = %q\nwant %q", got, want)
	}
	if res2.SessionID != res.SessionID {
		t.Errorf("follow-up thread = %s", res2.SessionID)
	}
}

func mustEval(p string) string {
	e, _ := filepath.EvalSymlinks(p)
	return e
}

func TestAskErrors(t *testing.T) {
	a, argsFile, folder := fakeAdapter(t, map[string]core.AskModel{core.TierQuick: {Model: "gpt-6-luna-mini"}})
	if m := a.AskTiers()[core.TierQuick]; m.Model != "gpt-6-luna-mini" || m.Effort != "medium" {
		t.Errorf("tier override = %+v", m)
	}
	ask := func(req core.AskRequest) error {
		req.Folder, req.Prompt, req.Tier = folder, "q", core.TierStandard
		_, err := a.Ask(context.Background(), req)
		return err
	}
	t.Setenv("ASKSEMBLE_FAKE_MODE", "badmodel")
	err := ask(core.AskRequest{Model: "x"})
	var ae *core.ActionError
	if core.ErrorCode(err) != core.ErrCodeBadModel || !strings.Contains(err.Error(), "not supported") {
		t.Errorf("bad model: %v", err)
	}
	_ = ae
	t.Setenv("ASKSEMBLE_FAKE_MODE", "noresume")
	if err := ask(core.AskRequest{Resume: "01a188b9-9dfe-7b92-b37c-d12a1eca2235"}); core.ErrorCode(err) != core.ErrCodeNotFound {
		t.Errorf("unknown thread: %v", err)
	}
	t.Setenv("ASKSEMBLE_FAKE_MODE", "nothing")
	if err := ask(core.AskRequest{}); core.ErrorCode(err) != core.ErrCodeAskFailed {
		t.Errorf("no turn: %v", err)
	}
	t.Setenv("ASKSEMBLE_FAKE_MODE", "")
	n := len(calls(t, argsFile))
	for name, req := range map[string]core.AskRequest{
		"thread not a uuid": {Resume: "../../x"},
		"model as a flag":   {Model: "--dangerously-bypass-approvals-and-sandbox"},
		"model with quote":  {Model: `x"`},
		"unknown effort":    {Effort: "max"},
	} {
		if err := ask(req); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	if got := len(calls(t, argsFile)); got != n {
		t.Errorf("refused asks ran codex %d times", got-n)
	}
	missing := New(Options{Path: filepath.Join(t.TempDir(), "codex.exe")})
	if _, err := missing.Ask(context.Background(), core.AskRequest{Folder: folder, Prompt: "q"}); core.ErrorCode(err) != core.ErrCodeAgentUnavailable {
		t.Errorf("missing codex: %v", err)
	}
}

func TestCheckArgs(t *testing.T) {
	good := askArgs("gpt-6.1-terra", "medium", "")
	follow := askArgs("gpt-6.1-terra", "medium", testThread)
	for _, args := range [][]string{good, follow, askArgs("gpt-6.1-terra", "", ""), askArgs("gpt-6.1-terra", "", testThread)} {
		if err := checkArgs(args); err != nil {
			t.Fatal(err)
		}
	}
	ins := func(base []string, at int, extra ...string) []string {
		return slices.Insert(slices.Clone(base), at, extra...)
	}
	set := func(base []string, at int, v string) []string { s := slices.Clone(base); s[at] = v; return s }
	last, flast := len(good)-1, len(follow)-1
	first, ffirst := slices.Index(good, "--disable"), slices.Index(follow, "--disable")
	apps, fapps := slices.Index(good, "apps"), slices.Index(follow, "apps")
	if first < 0 || ffirst < 0 || apps != first+7 || fapps != ffirst+7 || follow[flast-1] != testThread {
		t.Fatalf("ask shapes: %q\n%q", good, follow)
	}
	swapped := slices.Clone(good)
	swapped[first+1], swapped[first+3] = swapped[first+3], swapped[first+1]
	for name, args := range map[string][]string{
		"bypass":          ins(good, 2, "--dangerously-bypass-approvals-and-sandbox"),
		"full auto":       ins(good, 2, "--full-auto"),
		"workspace write": {"--search", "exec", "-s", "workspace-write", "--ignore-user-config", "--skip-git-repo-check", "--json", "-m", "gpt-6.1-terra", "-"},
		"approval":        ins(good, 4, "-a", "never"),
		"profile":         ins(good, 4, "--profile", "work"),
		"config key":      ins(good, last, "-c", `approval_policy="never"`),
		"user config":     {"--search", "exec", "-s", "read-only", "--skip-git-repo-check", "--json", "-m", "gpt-6.1-terra", "-"},
		"prompt argument": append(slices.Clone(good[:last]), "hello"),
		"add dir":         ins(good, 4, "--add-dir", `C:\`),

		// The P2-1 shapes, before the features were turned off.
		"old shape": {"--search", "exec", "-s", "read-only", "--ignore-user-config", "--skip-git-repo-check", "--json",
			"-m", "gpt-6.1-terra", "-c", `model_reasoning_effort="medium"`, "-"},
		"old follow-up": {"--search", "exec", "-s", "read-only", "resume", "--ignore-user-config", "--skip-git-repo-check", "--json",
			"-m", "gpt-6.1-terra", "-c", `model_reasoning_effort="medium"`, "-c", `sandbox_mode="read-only"`, testThread, "-"},
		// A feature left on, swapped, turned on or added.
		"last feature missing":     slices.Concat(good[:last-2], []string{"-"}),
		"apps missing":             slices.Delete(slices.Clone(good), apps-1, apps+1),
		"apps missing, follow-up":  slices.Delete(slices.Clone(follow), fapps-1, fapps+1),
		"apps replaced":            set(good, apps, "memories"),
		"apps replaced, follow-up": set(follow, fapps, "memories"),
		"apps enabled":             set(good, apps-1, "--enable"),
		"enable added":             ins(good, last, "--enable", "apps"),
		"enable added, follow-up":  ins(follow, flast-1, "--enable", "apps"),
		"apps on through config":   ins(good, last, "-c", "features.apps=true"),
		"extra disable":            ins(good, last, "--disable", "memories"),
		"apps disabled twice":      ins(good, last, "--disable", "apps"),
		"disable with equals":      slices.Concat(good[:apps-1], []string{"--disable=apps"}, good[apps+1:]),
		"features swapped":         swapped,
		"no disables, padded":      slices.Concat(good[:first], []string{"--disable", "apps"}, good[first:first+2*len(featuresOff)-2], []string{"-"}),
		"web search off":           ins(good, last, "-c", `web_search="disabled"`),
		"no search":                good[1:],
		"ephemeral":                ins(good, first, "--ephemeral"),
		"output schema":            ins(good, first, "--output-schema", `C:\x.json`),
		// The disables in another place.
		"disables after the thread": slices.Concat(follow[:ffirst], []string{testThread}, disableArgs(), []string{"-"}),
		"disables after the prompt": slices.Concat(good[:first], []string{"-"}, disableArgs()),
		"disables before exec":      slices.Concat([]string{"--search"}, disableArgs(), good[1:first], []string{"-"}),
		"disables before search":    slices.Concat(disableArgs(), good[:first], []string{"-"}),
		"disables before resume":    slices.Concat(follow[:4], disableArgs(), follow[4:ffirst], []string{testThread, "-"}),
		"disables before -c":        slices.Concat(good[:first-2], disableArgs(), good[first-2:first], []string{"-"}),
		"thread after the prompt":   slices.Concat(follow[:flast-1], []string{"-", testThread}),
	} {
		if err := checkArgs(args); err == nil {
			t.Errorf("%s: %q accepted", name, args)
		}
	}
}

// TestAskDisablesTheJudgeFeatures: an ask, new or resumed, turns off the
// same features as a judge (P2-3 S2 decision 4), and keeps its web search.
func TestAskDisablesTheJudgeFeatures(t *testing.T) {
	a := New(Options{Path: filepath.Join(t.TempDir(), "codex.exe"), SchemaDir: t.TempDir()})
	ask, follow := askArgs("gpt-6-luna", "medium", ""), askArgs("gpt-6-luna", "medium", testThread)
	judged := judgeArgs("gpt-6-luna", "medium", a.schemaPath())

	if !slices.Equal(featuresOff, wantOff) {
		t.Errorf("featuresOff = %q\nwant %q", featuresOff, wantOff)
	}
	for _, f := range []string{"apps", "plugins", "hooks", "shell_tool", "unified_exec"} {
		if !slices.Contains(featuresOff, f) {
			t.Errorf("%s is left on", f)
		}
	}
	for _, f := range featuresOff {
		if strings.Contains(f, "web_search") {
			t.Errorf("web search feature %s is turned off", f)
		}
	}
	if !slices.Equal(disableArgs(), wantDisables()) {
		t.Errorf("disableArgs = %q", disableArgs())
	}
	// disableArgs hands out a new slice: changing one changes nothing else.
	d := disableArgs()
	d[1] = "memories"
	if disableArgs()[1] != "shell_tool" || featuresOff[0] != "shell_tool" {
		t.Error("disableArgs shares its slice")
	}

	for name, args := range map[string][]string{"ask": ask, "follow-up": follow, "judge": judged} {
		// Every --disable is one of a run of pairs that equals featuresOff.
		var off []string
		firstOff, lastOff, lastC := -1, -1, -1
		for i, arg := range args {
			switch arg {
			case "--disable":
				if i+1 < len(args) {
					off = append(off, args[i+1])
				}
				if firstOff < 0 {
					firstOff = i
				}
				lastOff = i + 1
			case "-c":
				lastC = i + 1
			}
		}
		if !slices.Equal(off, featuresOff) {
			t.Errorf("%s turns off %q", name, off)
		}
		if firstOff < 0 || !slices.Equal(args[firstOff:lastOff+1], disableArgs()) {
			t.Errorf("%s: the disables are not one run: %q", name, args)
		}
		if lastC < 0 || firstOff < lastC {
			t.Errorf("%s: the disables do not come after the -c options: %q", name, args)
		}
		if args[len(args)-1] != "-" || slices.Contains(args, "--enable") {
			t.Errorf("%s: %q", name, args)
		}
	}
	for name, args := range map[string][]string{"ask": ask, "follow-up": follow} {
		if args[0] != "--search" || args[1] != "exec" {
			t.Errorf("%s does not start with --search exec: %q", name, args)
		}
		for _, arg := range args {
			if strings.Contains(arg, "web_search") || arg == "--ephemeral" || arg == "--output-schema" {
				t.Errorf("%s carries the judge's %q", name, arg)
			}
		}
	}
	// The thread stays second to last, right after the disables, where
	// checkArgs and both fakes read it.
	if follow[len(follow)-2] != testThread || follow[len(follow)-3] != featuresOff[len(featuresOff)-1] || ask[len(ask)-2] != featuresOff[len(featuresOff)-1] {
		t.Errorf("follow-up = %q", follow)
	}
	if judged[0] != "exec" || !slices.Contains(judged, `web_search="disabled"`) || slices.Contains(judged, "--search") {
		t.Errorf("judge = %q", judged)
	}

	// Each checker takes its own shape and refuses the other's.
	for name, args := range map[string][]string{"ask": ask, "follow-up": follow} {
		if err := checkArgs(args); err != nil {
			t.Errorf("ask check refused the %s: %v", name, err)
		}
		if err := a.checkJudgeArgs(args); err == nil {
			t.Errorf("judge check accepted the %s: %q", name, args)
		}
	}
	if err := a.checkJudgeArgs(judged); err != nil {
		t.Errorf("judge check refused the judge: %v", err)
	}
	for name, args := range map[string][]string{
		"judge":             judged,
		"judge with search": slices.Concat([]string{"--search"}, judged),
		"judge as resume":   slices.Concat([]string{"--search"}, judged[:3], []string{"resume"}, judged[3:len(judged)-1], []string{testThread, "-"}),
	} {
		if err := checkArgs(args); err == nil {
			t.Errorf("ask check accepted the %s: %q", name, args)
		}
	}
}

func TestFindExePrefersTheRealExe(t *testing.T) {
	if _, err := findExe(filepath.Join(t.TempDir(), "codex.cmd")); err == nil {
		t.Error("a .cmd setting was accepted")
	}
	// An npm install: only codex.cmd on PATH, the exe under node_modules.
	npm := t.TempDir()
	os.WriteFile(filepath.Join(npm, "codex.cmd"), []byte("@echo off\r\n"), 0o600)
	var exe string
	for _, target := range []string{"aarch64-pc-windows-msvc", "x86_64-pc-windows-msvc"} { // both targets: take this PC's
		vendor := filepath.Join(npm, "node_modules", "@openai", "codex", "vendor", target, "codex")
		os.MkdirAll(vendor, 0o700)
		os.WriteFile(filepath.Join(vendor, "codex.exe"), []byte("MZ"), 0o700)
		if strings.HasPrefix(target, vendorArch()) {
			exe = filepath.Join(vendor, "codex.exe")
		}
	}
	t.Setenv("PATH", npm)
	t.Setenv("PATHEXT", ".COM;.EXE;.BAT;.CMD")
	got, err := findExe("")
	if proc.CheckExe(exe) == nil && (err != nil || got != exe) {
		t.Errorf("findExe = %q, %v; want %q", got, err, exe)
	}
}
