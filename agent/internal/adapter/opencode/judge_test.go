package opencode

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/asksemble/asksemble/agent/internal/core"
	"github.com/asksemble/asksemble/agent/internal/fakecli"
	"github.com/asksemble/asksemble/agent/internal/proc"
)

const testDefaultModel = "opencode-go/deepseek-v4.1-flash#high"

// judgeSetup makes this test binary the fake opencode (internal/fakecli)
// and returns the adapter, an ask folder and its judge folder, where the
// one-off calls run.
func judgeSetup(t *testing.T) (a *Adapter, ask, jf string) {
	t.Helper()
	exe := fakecli.Use(t, fakecli.RoleOpenCode)
	ask = filepath.Join(t.TempDir(), "ask")
	jf = core.JudgeFolder(ask)
	if err := os.MkdirAll(jf, 0o700); err != nil {
		t.Fatal(err)
	}
	return New(Options{Path: exe}), ask, jf
}

func procResult(stdout []byte, stderr string, exit int) proc.Result {
	return proc.Result{Stdout: stdout, Stderr: []byte(stderr), ExitCode: exit}
}

func detail(err error) string {
	var ae *core.ActionError
	if errors.As(err, &ae) {
		return ae.Detail
	}
	return ""
}

// agentConfig reads OPENCODE_CONFIG_CONTENT's agents.
func agentConfig(t *testing.T, content string) map[string]map[string]any {
	t.Helper()
	var cfg struct {
		Agent map[string]map[string]any `json:"agent"`
	}
	if err := json.Unmarshal([]byte(content), &cfg); err != nil {
		t.Fatalf("config %q: %v", content, err)
	}
	return cfg.Agent
}

func sessionIDs(ss []fakecli.Session) []string {
	var out []string
	for _, s := range ss {
		out = append(out, s.ID)
	}
	slices.Sort(out)
	return out
}

// An ask after a combined answer still gets the ask agent with the web
// tools: the two configs never mix.
func TestAskAfterWriter(t *testing.T) {
	a, ask, jf := judgeSetup(t)
	if _, err := a.Synthesize(context.Background(), synthRequest(t, jf)); err != nil {
		t.Fatal(err)
	}
	res, err := a.Ask(context.Background(), core.AskRequest{Folder: ask, Prompt: "q", Tier: core.TierStandard})
	if err != nil {
		t.Fatal(err)
	}
	cs := fakecli.Calls(t)
	if len(cs) != 2 {
		t.Fatalf("%d calls, want 2", len(cs))
	}
	j, c := cs[0], cs[1]
	if !slices.Equal(c.Args, askArgs("opencode-go/deepseek-v4.1-flash#max", "")) || c.Env["PWD"] != ask || res.Answer != "fake: q" {
		t.Errorf("ask call %q in %q, answer %q", c.Args, c.Env["PWD"], res.Answer)
	}
	if c.Env["OPENCODE_CONFIG_CONTENT"] != askConfig || j.Env["OPENCODE_CONFIG_CONTENT"] != synthConfig || askConfig == synthConfig {
		t.Errorf("configs: ask %s, writer %s", c.Env["OPENCODE_CONFIG_CONTENT"], j.Env["OPENCODE_CONFIG_CONTENT"])
	}
	agents := agentConfig(t, c.Env["OPENCODE_CONFIG_CONTENT"])
	perm, _ := json.Marshal(agents["asksemble-ask"]["permission"])
	if len(agents) != 1 || string(perm) != `{"*":"deny","webfetch":"allow","websearch":"allow"}` {
		t.Errorf("ask agents = %v", agents)
	}
}

func TestParseJudgeOutput(t *testing.T) {
	const sid = "ses_e773ef84effedagCyC2bkvl1W9"
	ev := func(lines ...string) []byte { return []byte(strings.Join(lines, "\r\n") + "\r\n") }
	stepStart := `{"type":"step_start","sessionID":"` + sid + `","part":{"type":"step-start"}}`
	finish := `{"type":"step_finish","sessionID":"` + sid + `","part":{"type":"step-finish","tokens":{"input":10,"output":2,"cache":{"read":3,"write":1}}}}`
	text := func(s string) string {
		b, _ := json.Marshal(map[string]any{"type": "text", "sessionID": sid, "part": map[string]string{"type": "text", "text": s}})
		return string(b)
	}
	errorEvent := func(kind, msg string) string {
		b, _ := json.Marshal(map[string]any{"type": "error", "sessionID": sid, "error": map[string]string{"type": kind, "message": msg}})
		return string(b)
	}
	tool := `{"type":"tool_use","sessionID":"` + sid + `","part":{"type":"tool","tool":"read","state":{"status":"error"}}}`
	long := strings.Repeat("너무 깁니다 ", 200)

	for _, c := range []struct {
		name    string
		stdout  []byte
		stderr  string
		exit    int
		code    string
		output  string
		detail  string
		cleanup string
	}{
		{name: "the last step's parts", stdout: ev(stepStart, text("먼저 볼게요."), finish, stepStart, text("```json"), text(`{"judgments":[]}`+"\n```"), finish),
			output: "```json\n{\"judgments\":[]}\n```", cleanup: sid},
		{name: "a step without text after the answer", stdout: ev(stepStart, text(`{"judgments":[]}`), finish, stepStart, finish),
			output: `{"judgments":[]}`, cleanup: sid},
		{name: "junk lines and a bad id first", stdout: ev("INFO starting", "{not json", `{"type":"step_start","sessionID":"ses_../x"}`, stepStart, text("{}")),
			output: "{}", cleanup: sid},
		{name: "no text", stdout: ev(stepStart, finish), code: core.ErrCodeJudgeBadOutput, cleanup: sid},
		{name: "nothing", code: core.ErrCodeJudgeBadOutput},
		{name: "a tool, then an error", stdout: ev(stepStart, tool, errorEvent("provider.overloaded", "busy")), exit: 1, code: core.ErrCodeJudgeUsedTool, cleanup: sid},
		{name: "an error event", stdout: ev(stepStart, text(`{"judgments":[]}`), errorEvent("provider.overloaded", "Provider is overloaded")),
			exit: 1, code: core.ErrCodeJudgeFailed, detail: "Provider is overloaded", cleanup: sid},
		{name: "model unavailable", stdout: ev(errorEvent("unknown", "Model unavailable: x/y")), exit: 1, code: core.ErrCodeBadModel, detail: "Model unavailable: x/y", cleanup: sid},
		{name: "exit 1, stderr", stdout: ev(stepStart), stderr: "Error: boom\n", exit: 1, code: core.ErrCodeJudgeFailed, detail: "Error: boom", cleanup: sid},
		{name: "exit 1, nothing said", exit: 1, code: core.ErrCodeJudgeFailed, detail: judgeNoAnswer},
		{name: "judge JSON in stderr", stderr: `{"judgments":[{"id":"a1"}]}`, exit: 1, code: core.ErrCodeJudgeFailed, detail: judgeNoAnswer},
		{name: "judge JSON in the error", stdout: ev(errorEvent("x", `bad {"judgments":[]}`)), exit: 1, code: core.ErrCodeJudgeFailed, detail: judgeNoAnswer, cleanup: sid},
		{name: "a long message", stdout: ev(errorEvent("x", long)), exit: 1, code: core.ErrCodeJudgeFailed, detail: "너무 깁니다", cleanup: sid},
	} {
		r, err := parseJudgeOutput(procResult(c.stdout, c.stderr, c.exit), "m/x")
		switch {
		case core.ErrorCode(err) != c.code:
			t.Errorf("%s: err %v, want code %q", c.name, err, c.code)
		case string(r.Output) != c.output || r.Cleanup != c.cleanup || r.Model != "m/x":
			t.Errorf("%s: output %q, cleanup %q, model %q", c.name, r.Output, r.Cleanup, r.Model)
		case !strings.Contains(detail(err), c.detail) || utf8.RuneCountInString(detail(err)) > core.JudgeMessageChars:
			t.Errorf("%s: detail %q (%d characters)", c.name, detail(err), utf8.RuneCountInString(detail(err)))
		}
	}
	r, _ := parseJudgeOutput(procResult(ev(stepStart, text("{}"), finish, stepStart, finish), "", 0), "m/x")
	if r.InputTokens != 28 || r.OutputTokens != 4 {
		t.Errorf("tokens %d/%d, want 28/4", r.InputTokens, r.OutputTokens)
	}
}

func TestCleanupJudge(t *testing.T) {
	sid := func(tag string) string { return "ses_" + tag + strings.Repeat("0", 26-len(tag)) }
	now := time.Now()
	sess := func(tag, title, dir string, ago time.Duration) fakecli.Session {
		ms := now.Add(-ago).UnixMilli()
		return fakecli.Session{ID: sid(tag), Title: title, Directory: dir, Updated: ms, Created: ms, ProjectID: fakecli.ProjectID}
	}
	ctx := context.Background()

	t.Run("deletes only its own", func(t *testing.T) {
		a, ask, jf := judgeSetup(t)
		otherSpelling := jf + string(filepath.Separator)
		if runtime.GOOS == "windows" {
			otherSpelling = strings.ToUpper(jf)
		}
		unknownAge := sess("unknownage", judgeTitle, jf, 0)
		unknownAge.Updated = 0
		// A folder whose name has KELVIN SIGN for the k of ask: Unicode
		// case folding makes it the judge folder, NTFS does not.
		lookalike := filepath.Join(filepath.Dir(ask), "asK", core.JudgeFolderName)
		if err := os.MkdirAll(lookalike, 0o700); err != nil {
			t.Fatal(err)
		}
		if li, err1 := os.Stat(lookalike); err1 == nil {
			if ji, err2 := os.Stat(jf); err2 == nil && os.SameFile(li, ji) {
				t.Fatalf("%q is the judge folder on this file system", lookalike)
			}
		}
		fakecli.SetSessions(t, []fakecli.Session{
			sess("ref", judgeTitle, jf, time.Minute),                         // deleted: a ref
			sess("stale", judgeTitle, jf, 11*time.Minute),                    // deleted: left behind
			sess("stalespelling", judgeTitle, otherSpelling, 11*time.Minute), // deleted: the same folder
			sess("fresh", judgeTitle, jf, time.Minute),                       // kept: may be another comparison's, still running
			unknownAge, // kept
			sess("othertitle", "asksemble-judge-x", jf, time.Hour),              // kept
			sess("subfolder", judgeTitle, filepath.Join(jf, "live"), time.Hour), // kept: a live test's
			sess("ask", "fake ask", ask, time.Hour),                             // kept: an ask
			sess("askfolder", judgeTitle, ask, time.Hour),                       // kept: not in the judge folder
			sess("lookalike", judgeTitle, lookalike, time.Hour),                 // kept: another folder
		})
		refs := []string{sid("ref"), sid("notlisted"), "ses_../../x", "", "--all", sid("ref") + " "}
		if err := a.CleanupJudge(ctx, jf, refs); err != nil {
			t.Fatal(err)
		}
		cs := fakecli.Calls(t)
		if len(cs) == 0 || !slices.Equal(cs[0].Args, listArgs()) {
			t.Fatalf("calls %+v", cs)
		}
		var deleted []string
		for _, c := range cs {
			if c.Env["PWD"] != jf || c.Env["OPENCODE_CONFIG_CONTENT"] != synthConfig || !sameFolder(c.Dir, jf) {
				t.Errorf("call %q ran with PWD %q in %q", c.Args, c.Env["PWD"], c.Dir)
			}
			if c.Args[1] == "delete" {
				if !slices.Equal(c.Args, deleteArgs(c.Args[2])) {
					t.Errorf("delete %q", c.Args)
				}
				deleted = append(deleted, c.Args[2])
			}
		}
		slices.Sort(deleted)
		if want := []string{sid("ref"), sid("stale"), sid("stalespelling")}; !slices.Equal(deleted, want) {
			t.Errorf("deleted %q, want %q", deleted, want)
		}
		want := []string{sid("ask"), sid("askfolder"), sid("fresh"), sid("lookalike"), sid("othertitle"), sid("subfolder"), sid("unknownage")}
		if got := sessionIDs(fakecli.Sessions(t)); !slices.Equal(got, want) {
			t.Errorf("left %q, want %q", got, want)
		}
	})

	t.Run("after real writers", func(t *testing.T) {
		a, _, jf := judgeSetup(t)
		t.Setenv(fakecli.EnvSynth, "tool_use_once")
		var refs []string
		for range 2 {
			res, _ := a.Synthesize(ctx, synthRequest(t, jf))
			refs = append(refs, res.Cleanup)
		}
		if err := a.CleanupJudge(ctx, jf, refs); err != nil {
			t.Fatal(err)
		}
		if ss := fakecli.Sessions(t); len(ss) != 0 {
			t.Errorf("left %+v", ss)
		}
	})

	for _, c := range []struct {
		name     string
		fail     string
		sessions []fakecli.Session
		refs     []string
		msg      string
		left     []string
	}{
		{name: "list fails", fail: "list", msg: cleanupNoList, left: []string{sid("one")}},
		{name: "list is no JSON", fail: "listjunk", msg: cleanupNoList, left: []string{sid("one")}},
		{name: "a delete fails", fail: "delete=" + sid("one"), msg: cleanupNoDelete, left: []string{sid("one")}},
		{name: "a ref retitled", sessions: []fakecli.Session{sess("retitled", "my notes", "", time.Hour)},
			refs: []string{sid("retitled")}, msg: cleanupMismatch, left: []string{sid("retitled")}},
		{name: "a ref elsewhere", sessions: []fakecli.Session{sess("elsewhere", judgeTitle, "sub", time.Hour)},
			refs: []string{sid("elsewhere")}, msg: cleanupMismatch, left: []string{sid("elsewhere")}},
	} {
		t.Run(c.name, func(t *testing.T) {
			a, _, jf := judgeSetup(t)
			ss := []fakecli.Session{sess("one", judgeTitle, jf, time.Minute), sess("two", judgeTitle, jf, time.Minute)}
			for _, s := range c.sessions {
				if s.Directory == "" {
					s.Directory = jf
				} else {
					s.Directory = filepath.Join(jf, s.Directory)
				}
				ss = append(ss, s)
			}
			fakecli.SetSessions(t, ss)
			t.Setenv(fakecli.EnvFail, c.fail)
			err := a.CleanupJudge(ctx, jf, append([]string{sid("one"), sid("two")}, c.refs...))
			if core.ErrorCode(err) != core.ErrCodeJudgeFailed || detail(err) != c.msg || strings.Contains(err.Error(), "ses_") {
				t.Errorf("err = %v, want %q", err, c.msg)
			}
			if c.fail == "list" || c.fail == "listjunk" {
				c.left = append(c.left, sid("two"))
				for _, call := range fakecli.Calls(t) {
					if call.Args[1] == "delete" {
						t.Errorf("deleted after a failed list: %q", call.Args)
					}
				}
			}
			slices.Sort(c.left)
			if got := sessionIDs(fakecli.Sessions(t)); !slices.Equal(got, c.left) {
				t.Errorf("left %q, want %q", got, c.left)
			}
		})
	}

	t.Run("refused", func(t *testing.T) {
		a, _, jf := judgeSetup(t)
		for name, folder := range map[string]string{"empty": "", "relative": "judge", "missing": filepath.Join(jf, "gone")} {
			if err := a.CleanupJudge(ctx, folder, nil); core.ErrorCode(err) != core.ErrCodeInternal {
				t.Errorf("%s folder: %v", name, err)
			}
		}
		missing := New(Options{Path: filepath.Join(t.TempDir(), "opencode.exe")})
		if err := missing.CleanupJudge(ctx, jf, nil); core.ErrorCode(err) != core.ErrCodeAgentUnavailable {
			t.Errorf("missing opencode: %v", err)
		}
		if cs := fakecli.Calls(t); len(cs) != 0 {
			t.Errorf("ran %+v", cs)
		}
	})
}

// sameFolder folds only A-Z on Windows: a name that Unicode case folding
// alone makes the judge folder's is another folder, and is never cleaned.
func TestSameFolder(t *testing.T) {
	win := runtime.GOOS == "windows"
	jf := filepath.Join(t.TempDir(), "ask", core.JudgeFolderName)
	for _, c := range []struct {
		name, other string
		same        bool
	}{
		{"itself", jf, true},
		{"trailing separator", jf + string(filepath.Separator), true},
		{"upper case", strings.ToUpper(jf), win},
		{"KELVIN SIGN for k", filepath.Join(filepath.Dir(filepath.Dir(jf)), "as\u212A", core.JudgeFolderName), false},
		{"LONG S for s", filepath.Join(filepath.Dir(filepath.Dir(jf)), "a\u017Fk", core.JudgeFolderName), false},
		{"inside it", filepath.Join(jf, "live"), false},
		{"empty", "", false},
	} {
		if got := sameFolder(c.other, jf); got != c.same {
			t.Errorf("%s: sameFolder(%q) = %v, want %v", c.name, c.other, got, c.same)
		}
	}
	if win && !sameFolder(`C:\Users\Ab\ask\judge`, `c:\users\aB\ASK\JUDGE`) {
		t.Error("A-Z is not folded")
	}
}

// A one-off conversation (the judge title, in the judge folder) is never
// carried on as an ask, though the judge folder is inside the ask folder.
func TestFollowUpRefusesJudgeSession(t *testing.T) {
	a, ask, jf := judgeSetup(t)
	sid := func(tag string) string { return "ses_" + tag + strings.Repeat("0", 26-len(tag)) }
	upper := jf
	if runtime.GOOS == "windows" {
		upper = strings.ToUpper(jf)
	}
	fakecli.SetSessions(t, []fakecli.Session{
		{ID: sid("judge"), Title: judgeTitle, Directory: jf},
		{ID: sid("judgecase"), Title: judgeTitle, Directory: upper},
		{ID: sid("judgelive"), Title: judgeTitle, Directory: filepath.Join(jf, "live")},
		{ID: sid("ask"), Title: "fake ask", Directory: ask},
		{ID: sid("sibling"), Title: "fake ask", Directory: filepath.Join(ask, "judge-x")},
	})
	for _, tag := range []string{"judge", "judgecase", "judgelive"} {
		_, err := a.Ask(context.Background(), core.AskRequest{Folder: ask, Prompt: "q", Tier: core.TierStandard, Resume: sid(tag)})
		if core.ErrorCode(err) != core.ErrCodeNotFound {
			t.Errorf("%s: %v", tag, err)
		}
	}
	for _, c := range fakecli.Calls(t) {
		if c.Args[0] == "run" {
			t.Errorf("ran %q", c.Args)
		}
	}
	for _, tag := range []string{"ask", "sibling"} {
		if _, err := a.Ask(context.Background(), core.AskRequest{Folder: ask, Prompt: "q", Tier: core.TierStandard, Resume: sid(tag)}); err != nil {
			t.Errorf("%s: %v", tag, err)
		}
		cs := fakecli.Calls(t)
		if got := cs[len(cs)-1].Args; got[0] != "run" || got[len(got)-1] != sid(tag) {
			t.Errorf("%s: last call %q", tag, got)
		}
	}
}

func TestCheckSessionArgs(t *testing.T) {
	id := "ses_e773ef84effedagCyC2bkvl1W9"
	for _, args := range [][]string{listArgs(), deleteArgs(id)} {
		if err := checkSessionArgs(args); err != nil {
			t.Errorf("%q: %v", args, err)
		}
	}
	for name, args := range map[string][]string{
		"list, no standalone":    {"session", "list", "--format", "json"},
		"list, more":             append(listArgs(), "-n", "1000"),
		"delete a flag":          deleteArgs("--all"),
		"delete a path":          deleteArgs("ses_../../e773ef84effedagCyC2"),
		"delete nothing":         deleteArgs(""),
		"delete, no standalone":  {"session", "delete", id},
		"delete two":             {"session", "delete", id, id, "--standalone"},
		"delete, standalone 1st": {"session", "delete", "--standalone", id},
		"a run":                  synthArgs(testDefaultModel),
	} {
		if err := checkSessionArgs(args); err == nil {
			t.Errorf("%s: %q accepted", name, args)
		}
	}
}

func TestFamily(t *testing.T) {
	a := New(Options{Path: filepath.Join(t.TempDir(), "opencode.exe")})
	for model, want := range map[string]string{
		"opencode-go/deepseek-v4.1-flash":                  "deepseek",
		"opencode-go/deepseek-v4.1-flash#max":              "deepseek",
		"deepseek-chat":                                    "deepseek",
		"opencode-go/kimi-k3.1-pro":                        "moonshot",
		"moonshotai/Kimi-K2#high":                          "moonshot",
		"openrouter/moonshotai/moonshot-v1":                "moonshot",
		"opencode-go/glm-5.2":                              "zhipu",
		"zai/GLM-4.6":                                      "zhipu",
		"zhipuai/zhipu-x":                                  "zhipu",
		"openrouter/qwen/qwen3-max":                        "alibaba",
		"anthropic/claude-sonnet-5-5":                      "anthropic",
		"openrouter/anthropic/claude-x#high":               "anthropic",
		"openai/gpt-6.1-terra":                             "openai",
		"openai/gpt-oss-120b":                              "openai",
		"openai/o3":                                        "openai",
		"openai/o4-mini#high":                              "openai",
		"openai/codex-mini":                                "openai",
		"google/gemini-3-pro":                              "google",
		"openrouter/google/Gemini-2.5-Flash":               "google",
		"opencode/big-pickle":                              "",
		"x-ai/grok-4":                                      "",
		"meta/llama-4":                                     "",
		"openai/omni-x":                                    "",
		"openrouter/anthropic":                             "", // a provider, not a model
		"openrouter/deepseek/deepseek-r1-distill-qwen-32b": "", // two companies
		"opencode-go/":                                     "",
		"#max":                                             "",
		"":                                                 "",
	} {
		if got := a.Family(model); got != want {
			t.Errorf("Family(%q) = %q, want %q", model, got, want)
		}
	}
}

// JudgeMinVersion is the one-off command's tested version; the fake plays
// at least it.
func TestJudgeMinVersion(t *testing.T) {
	if JudgeMinVersion != "2.2.1" || !core.VersionAtLeast(fakecli.Versions[fakecli.RoleOpenCode], JudgeMinVersion) {
		t.Errorf("JudgeMinVersion %q", JudgeMinVersion)
	}
}
