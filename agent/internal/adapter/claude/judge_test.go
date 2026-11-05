package claude

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/asksemble/asksemble/agent/internal/core"
	"github.com/asksemble/asksemble/agent/internal/fakecli"
	"github.com/asksemble/asksemble/agent/internal/proc"
)

// newJudgeAdapter points an adapter at the shared fake claude and returns
// an empty judge folder.
func newJudgeAdapter(t *testing.T) (*Adapter, string) {
	t.Helper()
	exe := fakecli.Use(t, fakecli.RoleClaude)
	folder := filepath.Join(t.TempDir(), "ask", core.JudgeFolderName)
	if err := os.MkdirAll(folder, 0o700); err != nil {
		t.Fatal(err)
	}
	return New(Options{ClaudeDir: t.TempDir(), ClaudePath: exe}), folder
}

func judgeDetail(err error) string {
	var ae *core.ActionError
	if errors.As(err, &ae) {
		return ae.Detail
	}
	return ""
}

// parseJudgeOutput on outputs the fake does not make.
func TestParseJudgeOutput(t *testing.T) {
	const obj = `{"judgments":[]}`
	long := strings.Repeat("요금 한도에 걸렸습니다. ", 100)
	cases := []struct {
		name, stdout, stderr string
		exit                 int
		code, detail, output string
	}{
		{name: "object", stdout: `{"type":"result","subtype":"success","is_error":false,"result":"x","structured_output":` + obj + `}`, output: obj},
		{name: "JSON string", stdout: `{"type":"result","is_error":false,"structured_output":"{\"judgments\":[]}"}`, output: obj},
		{name: "last result line wins", stdout: `{"type":"result","is_error":true,"result":"old"}` + "\r\n" + `{"type":"system"}` + "\n" + `{"type":"result","is_error":false,"structured_output":` + obj + "}\r\n", output: obj},
		{name: "null", stdout: `{"type":"result","is_error":false,"result":"done","structured_output":null}`, code: core.ErrCodeJudgeBadOutput},
		{name: "empty string", stdout: `{"type":"result","is_error":false,"structured_output":"  "}`, code: core.ErrCodeJudgeBadOutput},
		{name: "no result line", stdout: "not json\n", exit: 1, code: core.ErrCodeJudgeFailed, detail: "claude ended with exit code 1 and no result"},
		{name: "no result, stderr", stderr: "Error: something broke\n  at x", exit: 1, code: core.ErrCodeJudgeFailed, detail: "Error: something broke at x"},
		{name: "no result, unknown model", stderr: `[claude-code:unrecognized_model] {"model":"x"}`, exit: 1, code: core.ErrCodeBadModel, detail: "haiku"},
		{name: "no result, judge JSON on stderr", stderr: `{"judgments":[{"id":"a1"}]}`, exit: 1, code: core.ErrCodeJudgeFailed, detail: "claude ended with exit code 1 and no result"},
		{name: "exit code without is_error", stdout: `{"type":"result","is_error":false,"result":` + `"{\"judgments\":[]}","structured_output":` + obj + `}`, stderr: "killed", exit: 3, code: core.ErrCodeJudgeFailed, detail: "killed"},
		{name: "is_error, judge JSON in result, no stderr", stdout: `{"type":"result","is_error":true,"result":"{\"judgments\":[]}"}`, exit: 1, code: core.ErrCodeJudgeFailed, detail: "claude reported an error (exit code 1)"},
		{name: "is_error with output and result text", stdout: `{"type":"result","is_error":true,"result":"some words","structured_output":` + obj + `}`, exit: 1, code: core.ErrCodeJudgeFailed, detail: "claude reported an error (exit code 1)"},
		{name: "is_error, result preferred to stderr", stdout: `{"type":"result","is_error":true,"result":"API Error: 500"}`, stderr: "trace", exit: 1, code: core.ErrCodeJudgeFailed, detail: "API Error: 500"},
		{name: "api 404", stdout: `{"type":"result","is_error":true,"api_error_status":404,"result":"model not found"}`, exit: 1, code: core.ErrCodeBadModel, detail: "haiku"},
		{name: "api 404 on success is no error", stdout: `{"type":"result","is_error":false,"api_error_status":404,"structured_output":` + obj + `}`, output: obj},
		{name: "schema retries", stdout: `{"type":"result","subtype":"error_max_structured_output_retries","is_error":true}`, exit: 1, code: core.ErrCodeJudgeBadOutput},
		{name: "denials first", stdout: `{"type":"result","is_error":true,"subtype":"error_max_structured_output_retries","permission_denials":[{"tool_name":"WebFetch"}],"structured_output":` + obj + `}`, exit: 1, code: core.ErrCodeJudgeUsedTool},
		{name: "denials not a list", stdout: `{"type":"result","is_error":false,"permission_denials":{"tool_name":"Bash"},"structured_output":` + obj + `}`, code: core.ErrCodeJudgeFailed},
		{name: "long message", stdout: `{"type":"result","is_error":true,"result":"` + long + `"}`, exit: 1, code: core.ErrCodeJudgeFailed},
	}
	for _, c := range cases {
		res, err := parseJudgeOutput(proc.Result{Stdout: []byte(c.stdout), Stderr: []byte(c.stderr), ExitCode: c.exit}, "haiku")
		if core.ErrorCode(err) != c.code {
			t.Errorf("%s: err = %v, want %q", c.name, err, c.code)
			continue
		}
		if c.detail != "" && judgeDetail(err) != c.detail {
			t.Errorf("%s: detail = %q, want %q", c.name, judgeDetail(err), c.detail)
		}
		if string(res.Output) != c.output {
			t.Errorf("%s: output = %s, want %s", c.name, res.Output, c.output)
		}
		if d := judgeDetail(err); utf8.RuneCountInString(d) > core.JudgeMessageChars || strings.Contains(d, "judgments") {
			t.Errorf("%s: detail %q", c.name, d)
		}
	}
	if _, err := parseJudgeOutput(proc.Result{Stdout: []byte(`{"type":"result","is_error":true,"result":"` + long + `"}`), ExitCode: 1}, "haiku"); utf8.RuneCountInString(judgeDetail(err)) != core.JudgeMessageChars {
		t.Errorf("long detail has %d characters", utf8.RuneCountInString(judgeDetail(err)))
	}
}

func TestFamily(t *testing.T) {
	a := New(Options{ClaudeDir: t.TempDir(), ClaudePath: "claude.cmd"}) // Family needs no claude
	for model, want := range map[string]string{
		"haiku": "anthropic", "sonnet": "anthropic", "opus": "anthropic", "default": "anthropic",
		"best": "anthropic", "opusplan": "anthropic", "Sonnet": "anthropic", "OPUS": "anthropic",
		"sonnet[1m]": "anthropic", "opus[1M]": "anthropic", "claude-sonnet-5-5": "anthropic",
		"claude-sonnet-5-5[1m]": "anthropic", "Claude-Haiku-4-5-20251001": "anthropic", "claude-opus-4-7": "anthropic",
		"": "", "[1m]": "", "claude": "", "claude-": "", "claude-[1m]": "", "haiku-x": "", "sonnet[2m]": "",
		"gpt-6-luna": "", "deepseek-v4.1-flash": "", "opencode-go/deepseek-v4.1-flash#max": "",
		"anthropic/claude-sonnet-5-5": "", "us.anthropic.claude-sonnet-5-5": "", "gemini-3-pro": "", "unknown": "",
	} {
		if got := a.Family(model); got != want {
			t.Errorf("Family(%q) = %q, want %q", model, got, want)
		}
	}
}

// JudgeMinVersion is the one-off command's tested version, the fake's
// default; claude keeps no conversation and needs no cleaner.
func TestJudgeMinVersion(t *testing.T) {
	a := New(Options{ClaudeDir: t.TempDir(), ClaudePath: "claude.cmd"})
	if !core.VersionAtLeast(JudgeMinVersion, "2.1.314") || JudgeMinVersion != fakecli.Versions[fakecli.RoleClaude] {
		t.Errorf("JudgeMinVersion = %q", JudgeMinVersion)
	}
	var j core.Judger = a
	if _, ok := j.(core.JudgeCleaner); ok {
		t.Error("claude keeps no judge conversation and needs no cleaner")
	}
}
