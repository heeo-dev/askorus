package fakecli

// The fake Antigravity CLI (#20): `agy --version`, `agy models`,
// `agy -p /usage --output-format json`, and a run whose question comes on
// stdin as one stream-json message (--input-format stream-json): an ask,
// or a combined-answer writer when --json-schema is given. It prints the
// stream-json events agy 1.5.10 printed on 2026-11-18: init, step_update
// and result. Like agy, it refuses an --agent that has no agent file in
// .agents/agents of the folder it runs in, and reports the permission mode
// --settings sets (agy's own default is always-proceed on the PC it was
// tried on).

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"github.com/heeo-dev/askorus/agent/internal/uploads"
)

// EnvAgy is the fake agy's own run mode, beside ASKORUS_FAKE_ASK and
// ASKORUS_FAKE_SYNTH: one of AgyModes.
const EnvAgy = "ASKORUS_FAKE_AGY"

// Run modes of ASKORUS_FAKE_AGY.
const (
	AgyNormal     = ""           // the default
	AgyRetry      = "retry"      // out of capacity once, then the answer: status ERROR with the retry note
	AgyCapacity   = "capacity"   // out of capacity on every attempt: no answer, exit 1
	AgyBadTool    = "badtool"    // reaches for run_command before answering
	AgyWrongAgent = "wrongagent" // the start event names another agent
	AgyDenied     = "denied"     // a file read auto-denied: no answer, the stderr notice
	AgyNoResume   = "noresume"   // --conversation unknown: a warning and a new conversation
	AgyLinks      = "links"      // the answer links a file by its path
	AgyNoInit     = "noinit"     // no start event
	AgyOpenMode   = "always"     // the start event reports always-proceed
)

// AgyModes lists them.
var AgyModes = []string{AgyNormal, AgyRetry, AgyCapacity, AgyBadTool, AgyWrongAgent, AgyDenied, AgyNoResume, AgyLinks, AgyNoInit, AgyOpenMode}

// What agy prints, for tests to compare against. CapacityAgy is agy's own
// words of 2026-11-18; the usage limit and the sign-in are not seen yet
// and follow agy's AGY_ERROR shape.
const (
	CapacityAgy   = "API error (attempt 1): UNAVAILABLE (code 503): No capacity available for model gemini-3.8-flash-medium on the server"
	UsageLimitAgy = "API error: RESOURCE_EXHAUSTED (code 429): You have reached your weekly limit for Gemini models."
	SignedOutAgy  = "API error: UNAUTHENTICATED (code 401): Request had invalid authentication credentials. Sign in again."
	DeniedAgy     = `jetski: no output produced — a tool required the "read_file" permission that headless mode cannot prompt for, so it was auto-denied.`
	// AgyLinkPath is the path the links mode writes.
	AgyLinkPath = "C:/Users/someone/AppData/Local/Askorus/ask/agy/photo-1.jpg"
	// AgyResetTime is when the fake's weekly limits refresh.
	AgyResetTime = "2026-11-25T07:21:31Z"
)

// AgyModels are what `agy models` lists, as on 2026-11-18.
var AgyModels = []string{
	"gemini-3.8-flash-high\tGemini 3.8 Flash (High)",
	"gemini-3.8-flash-medium\tGemini 3.8 Flash (Medium)",
	"gemini-3.8-flash-low\tGemini 3.8 Flash (Low)",
	"claude-sonnet-4-6\tClaude Sonnet 4.6 (Thinking)",
	"claude-opus-4-6-thinking\tClaude Opus 4.6 (Thinking)",
	"gpt-oss-120b-medium\tGPT-OSS 120B (Medium)",
}

func agyMode() (string, error) {
	m := os.Getenv(EnvAgy)
	if !slices.Contains(AgyModes, m) {
		return "", fmt.Errorf("unknown agy mode %q", m)
	}
	return m, nil
}

// agyMain plays agy after the call was recorded.
func agyMain(c *Call) int {
	if flagValue(c.Args, "--input-format") == "stream-json" {
		return agyRun(c)
	}
	return unexpected(RoleAgy)
}

// agyModels prints `agy models`; not recorded.
func agyModels() int {
	fmt.Println("Fetching available models...")
	for _, m := range AgyModels {
		fmt.Println(m)
	}
	return 0
}

// agyUsage prints `agy -p /usage --output-format json`, or the sign-in
// failure when the fake is signed out; not recorded.
func agyUsage(args []string) int {
	if len(args) < 4 || flagValue(args, "--output-format") != "json" {
		return unexpected(RoleAgy)
	}
	mode, err := signInMode(RoleAgy)
	if err != nil {
		fmt.Fprintln(os.Stderr, "fakecli:", err)
		return 2
	}
	if mode == SignedOut {
		printJSON(map[string]any{"conversation_id": "", "status": "ERROR", "response": "", "error": SignedOutAgy, "finished_reason": "error"})
		agyErrorLine(SignedOutAgy, "UNAUTHENTICATED")
		return 1
	}
	left := 0.94
	if m, _ := askMode(RoleAgy); m == AskFail {
		left = 0
	}
	printJSON(map[string]any{
		"conversation_id": "", "status": "SUCCESS", "response": "Gemini Models\tWeekly Limit Remaining\n",
		"command": map[string]any{"name": "usage", "data": map[string]any{"groups": []any{
			map[string]any{"name": "Gemini Models", "buckets": []any{map[string]any{"id": "gemini-weekly", "window": "weekly", "remaining_fraction": left, "reset_time": AgyResetTime}}},
			map[string]any{"name": "Claude and GPT models", "buckets": []any{map[string]any{"id": "3p-weekly", "window": "weekly", "remaining_fraction": 1, "reset_time": AgyResetTime}}},
		}}},
	})
	return 0
}

func agyErrorLine(msg, status string) {
	b, _ := json.Marshal(map[string]any{"short_error": msg, "status": status, "retryable": false})
	fmt.Fprintln(os.Stderr, "error: "+msg)
	fmt.Fprintln(os.Stderr, "AGY_ERROR: "+string(b))
}

// agyStream reads the stream-json input: one user message whose content is
// text blocks only (agy refuses image blocks), with the stream output. It
// returns the text and how many photos lie in the folder for the photo
// agent (photo-<n>.<ext> files, each an image).
func agyStream(args []string, stdin string) (int, string, error) {
	if flagValue(args, "--output-format") != "stream-json" {
		return 0, "", errors.New("stream-json input needs stream-json output")
	}
	lines := strings.Split(strings.TrimRight(stdin, "\n"), "\n")
	if len(lines) != 1 {
		return 0, "", fmt.Errorf("want one stream-json message, got %d lines", len(lines))
	}
	var m struct {
		Event   string `json:"event"`
		Message struct {
			Role    string `json:"role"`
			Content []struct {
				Type string `json:"type"`
				Text string `json:"text"`
			} `json:"content"`
		} `json:"message"`
	}
	if err := json.Unmarshal([]byte(lines[0]), &m); err != nil {
		return 0, "", fmt.Errorf("stream-json message: %w", err)
	}
	if m.Event != "user" || m.Message.Role != "user" || len(m.Message.Content) != 1 || m.Message.Content[0].Type != "text" {
		return 0, "", errors.New("stream-json message: want one user message with one text block")
	}
	photos := 0
	if agent := flagValue(args, "--agent"); strings.HasSuffix(agent, "-photo") {
		// The photo agents look at photo-<n>.<ext> in the folder they run in.
		dir, _ := os.Getwd()
		entries, err := os.ReadDir(dir)
		if err != nil {
			return 0, "", err
		}
		for _, e := range entries {
			if !agyPhotoRe.MatchString(e.Name()) {
				continue
			}
			b, err := os.ReadFile(filepath.Join(dir, e.Name()))
			if err != nil || uploads.Sniff(b) == "" {
				return 0, "", errors.New("photo: " + e.Name() + " is not an image")
			}
			photos++
		}
	}
	return photos, m.Message.Content[0].Text, nil
}

// agyPhotoRe is the name the adapter gives a photo in a run's folder.
var agyPhotoRe = regexp.MustCompile(`^photo-[1-4]\.(jpg|png|webp)$`)

// agyEvent prints one stream-json event.
func agyEvent(v any) { printJSON(v) }

func agyStep(conv string, i int, kind, state string, extra map[string]any) {
	s := map[string]any{"conversation_id": conv, "step_index": i, "state": state, "step_type": kind}
	for k, v := range extra {
		s[k] = v
	}
	agyEvent(map[string]any{"event": "step_update", "step_update": s})
}

// agyResult prints the result event.
func agyResult(conv, status, response, errMsg string, structured json.RawMessage) {
	r := map[string]any{"conversation_id": conv, "status": status, "response": response, "num_turns": 1,
		"usage": map[string]int{"input_tokens": 2100, "output_tokens": 40, "thinking_tokens": 30}}
	if errMsg != "" {
		r["error"] = errMsg
		r["finished_reason"] = "error"
	}
	if structured != nil {
		r["structured_output"] = structured
	}
	agyEvent(map[string]any{"event": "result", "result": r})
}

// agyRun plays one run: the agent file must exist, the start event reports
// the agent and the permission mode, an ask may search the web, a writer
// (--json-schema) calls finish.
func agyRun(c *Call) int {
	mode, err := agyMode()
	if err != nil {
		fmt.Fprintln(os.Stderr, "fakecli:", err)
		return 2
	}
	agent := flagValue(c.Args, "--agent")
	if agent == "" || !agyHasAgentFile(c.Dir, agent) {
		msg := fmt.Sprintf("unknown agent %q (no custom agents are available)", agent)
		agyResult("", "ERROR", "", msg, nil)
		fmt.Fprintln(os.Stderr, "error: "+msg)
		return 1
	}
	if m := flagValue(c.Args, "--model"); !slices.ContainsFunc(AgyModels, func(l string) bool { return strings.HasPrefix(l, m+"\t") }) {
		msg := fmt.Sprintf("invalid model selection (--model %q --effort \"\"): model %s is not recognized as a known model or custom model in settings", m, m)
		agyResult("", "ERROR", "", msg, nil)
		fmt.Fprintln(os.Stderr, "error: "+msg)
		return 1
	}
	var settings struct {
		ToolPermission string `json:"toolPermission"`
	}
	json.Unmarshal([]byte(flagValue(c.Args, "--settings")), &settings)
	permission := settings.ToolPermission
	if permission == "" || mode == AgyOpenMode {
		permission = "always-proceed"
	}
	conv := newUUID()
	if id := flagValue(c.Args, "--conversation"); id != "" {
		if mode == AgyNoResume {
			fmt.Fprintf(os.Stderr, "warning: conversation %q not found\n", id)
		} else {
			conv = id
		}
	}
	if mode != AgyNoInit {
		shown := agent
		if mode == AgyWrongAgent {
			shown = "default"
		}
		agyEvent(map[string]any{"event": "init", "conversation_id": conv, "init": map[string]any{
			"cwd": c.Dir, "agent": shown, "tools": []string{"run_command", "search_web", "view_file"}, "permission_mode": permission,
		}})
	}
	agyStep(conv, 0, "user_input", "DONE", nil)
	if schema := flagValue(c.Args, "--json-schema"); schema != "" {
		return agySynth(c, conv)
	}
	ask, err := askMode(RoleAgy)
	if err != nil {
		fmt.Fprintln(os.Stderr, "fakecli:", err)
		return 2
	}
	switch {
	case ask == AskSignedOut:
		agyResult(conv, "ERROR", "", SignedOutAgy, nil)
		agyErrorLine(SignedOutAgy, "UNAUTHENTICATED")
		return 1
	case ask == AskFail:
		agyResult(conv, "ERROR", "", UsageLimitAgy, nil)
		agyErrorLine(UsageLimitAgy, "RESOURCE_EXHAUSTED")
		return 1
	case mode == AgyCapacity:
		agyStep(conv, 1, "error_message", "DONE", nil)
		msg := strings.Replace(CapacityAgy, "attempt 1", "attempt 3", 1)
		agyResult(conv, "ERROR", "", msg, nil)
		agyErrorLine(msg, "")
		return 3
	case mode == AgyDenied:
		agyStep(conv, 1, "tool", "DONE", map[string]any{"tool_name": "search_web"})
		agyResult(conv, "SUCCESS", "", "", nil)
		fmt.Fprintln(os.Stderr, DeniedAgy)
		return 0
	}
	text, err := askText(RoleAgy, "fake agy: ", c.Prompt)
	if err != nil {
		fmt.Fprintln(os.Stderr, "fakecli:", err)
		return 2
	}
	text += photoNote(c)
	if mode == AgyLinks {
		text = "See [photo-1.jpg](file:///" + AgyLinkPath + ") and file:///" + AgyLinkPath + ". " + text
	}
	i := 1
	if mode == AgyRetry {
		agyStep(conv, i, "error_message", "DONE", nil)
		i++
	}
	if mode == AgyBadTool {
		agyStep(conv, i, "tool", "DONE", map[string]any{"tool_name": "run_command"})
		i++
	}
	if agyAgentHasTool(c.Dir, agent, "search_web") {
		agyStep(conv, i, "tool", "DONE", map[string]any{"tool_name": "search_web"})
		i++
	}
	if ask == AskEmpty {
		text = ""
	}
	agyStep(conv, i, "agent_response", "ACTIVE", map[string]any{"text_delta": text})
	agyStep(conv, i, "agent_response", "DONE", map[string]any{"text_delta": "\n"})
	if mode == AgyRetry {
		agyResult(conv, "ERROR", text+"\n", CapacityAgy, nil)
		return 0
	}
	agyResult(conv, "SUCCESS", text+"\n", "", nil)
	return 0
}

// agySynth plays a combined-answer writer (or a moderator refining a
// question): finish carries the structured output, as agy's --json-schema
// does.
func agySynth(c *Call, conv string) int {
	mode, err := synthMode(RoleAgy)
	if err != nil {
		fmt.Fprintln(os.Stderr, "fakecli:", err)
		return 2
	}
	if mode == SynthSlow {
		sleepSlow()
	}
	switch mode {
	case SynthUsageLimit:
		agyResult(conv, "ERROR", "", UsageLimitAgy, nil)
		agyErrorLine(UsageLimitAgy, "RESOURCE_EXHAUSTED")
		return 1
	case SynthBadModel:
		msg := "invalid model selection: model is not recognized as a known model or custom model in settings"
		agyResult("", "ERROR", "", msg, nil)
		return 1
	case SynthToolUse:
		agyStep(conv, 1, "tool", "DONE", map[string]any{"tool_name": "run_command"})
	}
	reply := synthReply(mode, c.Prompt)
	var structured json.RawMessage
	if json.Valid([]byte(reply)) && strings.HasPrefix(reply, "{") {
		structured = json.RawMessage(reply)
		agyStep(conv, 2, "finish", "DONE", nil)
	}
	agyStep(conv, 3, "agent_response", "DONE", map[string]any{"text_delta": reply})
	if mode == SynthFailedWithOutput {
		agyResult(conv, "ERROR", reply, "stream disconnected before completion", structured)
		return 1
	}
	agyResult(conv, "SUCCESS", reply, "", structured)
	return 0
}

// agyHasAgentFile reports whether dir has the agent file of name.
func agyHasAgentFile(dir, name string) bool {
	if strings.ContainsAny(name, `/\`) || name == "" {
		return false
	}
	info, err := os.Stat(filepath.Join(dir, ".agents", "agents", name+".md"))
	return err == nil && info.Mode().IsRegular()
}

// agyAgentHasTool reports whether the agent file of name lists tool.
func agyAgentHasTool(dir, name, tool string) bool {
	b, err := os.ReadFile(filepath.Join(dir, ".agents", "agents", name+".md"))
	if err != nil {
		return false
	}
	for _, l := range strings.Split(string(b), "\n") {
		if v, ok := strings.CutPrefix(l, "tools:"); ok {
			for _, t := range strings.Split(strings.Trim(strings.TrimSpace(v), "[]"), ",") {
				if strings.TrimSpace(t) == tool {
					return true
				}
			}
		}
	}
	return false
}
