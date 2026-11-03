package opencode

import (
	"bytes"
	"context"
	"encoding/json"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/asksemble/asksemble/agent/internal/core"
	"github.com/asksemble/asksemble/agent/internal/fakecli"
	"github.com/asksemble/asksemble/agent/internal/judge"
)

const testSynthModel = "opencode-go/deepseek-v4.1-flash"

func synthTask() judge.SynthTask {
	return judge.SynthTask{
		Question: "첫 배는 몇 시인가요?",
		Answers:  []judge.SynthAnswer{{AI: "Claude", Text: "7시입니다."}, {AI: "OpenCode", Text: "8시입니다."}},
		Differ: []judge.SynthDiffer{{Sides: []judge.SynthSide{
			{AIs: []string{"Claude"}, Says: []string{"7시입니다."}}, {AIs: []string{"OpenCode"}, Says: []string{"8시입니다."}},
		}}},
		Compared: true,
	}
}

func synthRequest(t *testing.T, folder string) core.SynthRequest {
	t.Helper()
	tk := synthTask()
	p := judge.SynthPrompt(tk, judge.SynthNonce(tk))
	if p == "" {
		t.Fatal("judge.SynthPrompt refused the task")
	}
	return core.SynthRequest{Folder: folder, Prompt: p, Model: testSynthModel, Effort: "max"}
}

func TestSynthArgsEnvAndStdin(t *testing.T) {
	a, _, jf := judgeSetup(t)
	req := synthRequest(t, jf)
	res, err := a.Synthesize(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	cs := fakecli.Calls(t)
	if len(cs) != 1 {
		t.Fatalf("%d calls, want 1", len(cs))
	}
	c := cs[0]
	want := []string{"run", "--standalone", "--format", "json", "--agent", "asksemble-synth", "-m", testSynthModel + "#max", "--title", "asksemble-judge"}
	if !slices.Equal(c.Args, want) {
		t.Errorf("args = %q, want %q", c.Args, want)
	}
	if c.Stdin != req.Prompt || c.Env["PWD"] != jf || !sameFolder(c.Dir, jf) {
		t.Errorf("stdin same %v, PWD %q, dir %q; want folder %q", c.Stdin == req.Prompt, c.Env["PWD"], c.Dir, jf)
	}
	for _, arg := range c.Args {
		if strings.Contains(arg, "첫 배") || strings.Contains(arg, "Claude") || strings.ContainsAny(arg, "\r\n") {
			t.Errorf("argument %q carries the content", arg)
		}
	}
	// The writer agent may use no tool at all, the web tools neither; the
	// only other agent of the run is the refine agent, locked the same way
	// (질문 정제, milestone C).
	agents := agentConfig(t, c.Env["OPENCODE_CONFIG_CONTENT"])
	ag, ok := agents["asksemble-synth"]
	rf, ok2 := agents["asksemble-refine"]
	if len(agents) != 2 || !ok || !ok2 {
		t.Fatalf("agents = %v", agents)
	}
	rperm, _ := json.Marshal(rf["permission"])
	if string(rperm) != `{"*":"deny"}` || rf["mode"] != "primary" || len(rf) != 4 {
		t.Errorf("refine agent = %v", rf)
	}
	perm, _ := json.Marshal(ag["permission"])
	prompt, _ := ag["prompt"].(string)
	if string(perm) != `{"*":"deny"}` || ag["mode"] != "primary" || len(ag) != 4 ||
		prompt != "You write the combined answer for the Asksemble app. Reply only with the JSON the message asks for, and use no tools." {
		t.Errorf("synth agent = %v", ag)
	}
	if synthConfig == judgeConfig || synthConfig == askConfig {
		t.Error("the synth agent is not its own")
	}
	if !bytes.HasPrefix(res.Output, []byte("```json\n")) || res.Model != testSynthModel+"#max" || res.InputTokens != 120 || res.OutputTokens != 5 {
		t.Errorf("result = %+v", res)
	}
	text, err := judge.ValidateSynth(res.Output)
	if err != nil || judge.UnnamedSides(synthTask(), text) != 0 {
		t.Errorf("ValidateSynth = %q, %v", text, err)
	}
	// The conversation it left has the judge title in the judge folder,
	// and its id is the cleanup ref: CleanupJudge deletes it as a judge's.
	ss := fakecli.Sessions(t)
	if len(ss) != 1 || res.Cleanup != ss[0].ID || ss[0].Title != judgeTitle || !sameFolder(ss[0].Directory, jf) {
		t.Fatalf("cleanup %q, sessions %+v", res.Cleanup, ss)
	}
	if err := a.CleanupJudge(context.Background(), jf, []string{res.Cleanup}); err != nil {
		t.Fatal(err)
	}
	if ss := fakecli.Sessions(t); len(ss) != 0 {
		t.Errorf("left after cleanup: %+v", ss)
	}
	// Without an effort the model runs as named, its own variant kept.
	for _, m := range []struct{ model, effort, want string }{
		{testSynthModel, "", testSynthModel},
		{testSynthModel + "#high", "", testSynthModel + "#high"},
		{testSynthModel + "#high", "max", testSynthModel + "#max"},
	} {
		r := req
		r.Model, r.Effort = m.model, m.effort
		if _, err := a.Synthesize(context.Background(), r); err != nil {
			t.Fatal(err)
		}
		cs := fakecli.Calls(t)
		if got := cs[len(cs)-1].Args[7]; got != m.want {
			t.Errorf("%s %q: -m %q, want %q", m.model, m.effort, got, m.want)
		}
	}
}

func TestCheckSynthArgs(t *testing.T) {
	good := synthArgs(testSynthModel)
	if err := checkSynthArgs(good); err != nil {
		t.Fatalf("own args refused: %v", err)
	}
	set := func(i int, v string) []string { s := slices.Clone(good); s[i] = v; return s }
	for name, args := range map[string][]string{
		"judge agent":     set(5, judgeAgent),
		"ask agent":       set(5, askAgent),
		"other title":     set(9, "asksemble-synth"),
		"no title":        good[:8],
		"model as a flag": synthArgs("--port"),
		"prompt argument": append(slices.Clone(good), "첫 배"),
		"session":         append(slices.Clone(good), "--session", "ses_abcdefghijklmnopqrstuvwxyz"),
		"judge args":      judgeArgs(testSynthModel),
		"ask args":        askArgs(testSynthModel, ""),
	} {
		if err := checkSynthArgs(args); err == nil {
			t.Errorf("%s: %q accepted", name, args)
		}
	}
	if err := checkJudgeArgs(good); err == nil {
		t.Error("the judge check accepted synth arguments")
	}
	if err := checkArgs(good); err == nil {
		t.Error("the ask check accepted synth arguments")
	}
}

func TestSynthModes(t *testing.T) {
	for _, c := range []struct {
		mode   string
		code   string // "" when Synthesize returns the output
		valid  bool   // ValidateSynth accepts the output
		detail string
	}{
		{fakecli.SynthOK, "", true, ""},
		{fakecli.SynthMissingNames, "", true, ""},
		{fakecli.SynthProse, "", false, ""},
		{fakecli.SynthBadJSON, "", false, ""},
		{fakecli.SynthEmpty, "", false, ""},
		{fakecli.SynthToolUse, core.ErrCodeJudgeUsedTool, false, ""},
		{fakecli.SynthUsageLimit, core.ErrCodeJudgeFailed, false, fakecli.UsageLimitOpenCode},
		{fakecli.SynthBadModel, core.ErrCodeBadModel, false, "Model unavailable: " + testSynthModel},
		{fakecli.SynthFailedWithOutput, core.ErrCodeJudgeFailed, false, "Provider is overloaded"},
	} {
		t.Run(c.mode, func(t *testing.T) {
			a, _, jf := judgeSetup(t)
			t.Setenv(fakecli.EnvSynth, c.mode)
			req := synthRequest(t, jf)
			req.Effort = ""
			res, err := a.Synthesize(context.Background(), req)
			// Every run left a conversation and names it whatever the error.
			ss := fakecli.Sessions(t)
			if len(ss) != 1 || res.Cleanup != ss[0].ID || ss[0].Title != judgeTitle {
				t.Errorf("cleanup %q, sessions %+v", res.Cleanup, ss)
			}
			if core.ErrorCode(err) != c.code {
				t.Fatalf("err = %v, want code %q", err, c.code)
			}
			if c.code != "" {
				if len(res.Output) != 0 || !strings.Contains(detail(err), c.detail) || strings.Contains(err.Error(), fakecli.SynthLead) {
					t.Errorf("output %q, detail %q", res.Output, detail(err))
				}
				return
			}
			if _, verr := judge.ValidateSynth(res.Output); (verr == nil) != c.valid {
				t.Errorf("ValidateSynth = %v", verr)
			}
		})
	}
}

func TestSynthTimeoutAndGuards(t *testing.T) {
	if synthTimeout != core.SynthTimeout {
		t.Fatalf("synthTimeout = %v", synthTimeout)
	}
	a, _, jf := judgeSetup(t)
	req := synthRequest(t, jf)
	t.Setenv(fakecli.EnvSynth, fakecli.SynthSlow)
	t.Setenv(fakecli.EnvSleep, "20s")
	old := synthTimeout
	synthTimeout = 2 * time.Second
	t.Cleanup(func() { synthTimeout = old })
	start := time.Now()
	res, err := a.Synthesize(context.Background(), req)
	if core.ErrorCode(err) != core.ErrCodeJudgeTimeout || time.Since(start) > 15*time.Second || len(res.Output) != 0 {
		t.Errorf("own limit: %v after %s, output %q", err, time.Since(start), res.Output)
	}
	// A writer ended after it named its conversation still returns it.
	if ss := fakecli.Sessions(t); len(ss) != 1 || res.Cleanup != ss[0].ID {
		t.Errorf("cleanup %q, sessions %+v", res.Cleanup, ss)
	}
	t.Setenv(fakecli.EnvSynth, "")
	n := len(fakecli.Calls(t))
	for name, c := range map[string]struct {
		req  func(core.SynthRequest) core.SynthRequest
		code string
	}{
		"empty prompt":    {func(r core.SynthRequest) core.SynthRequest { r.Prompt = ""; return r }, core.ErrCodeInternal},
		"no folder":       {func(r core.SynthRequest) core.SynthRequest { r.Folder = ""; return r }, core.ErrCodeInternal},
		"missing folder":  {func(r core.SynthRequest) core.SynthRequest { r.Folder = filepath.Join(jf, "gone"); return r }, core.ErrCodeInternal},
		"no model":        {func(r core.SynthRequest) core.SynthRequest { r.Model = ""; return r }, core.ErrCodeBadModel},
		"model as a flag": {func(r core.SynthRequest) core.SynthRequest { r.Model = "--port"; return r }, core.ErrCodeBadModel},
		"bad variant":     {func(r core.SynthRequest) core.SynthRequest { r.Effort = "MAX"; return r }, core.ErrCodeBadModel},
	} {
		if _, err := a.Synthesize(context.Background(), c.req(req)); core.ErrorCode(err) != c.code {
			t.Errorf("%s: %v, want %s", name, err, c.code)
		}
	}
	ended, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := a.Synthesize(ended, req); core.ErrorCode(err) != core.ErrCodeJudgeTimeout {
		t.Errorf("ended context: %v", err)
	}
	if got := len(fakecli.Calls(t)); got != n {
		t.Errorf("refused requests ran opencode %d times", got-n)
	}
	missing := New(Options{Path: filepath.Join(t.TempDir(), "opencode.exe")})
	if _, err := missing.Synthesize(context.Background(), req); core.ErrorCode(err) != core.ErrCodeAgentUnavailable {
		t.Errorf("missing opencode: %v", err)
	}
	var named core.Named = a
	if named.DisplayName() != "OpenCode" {
		t.Errorf("DisplayName = %q", named.DisplayName())
	}
}
