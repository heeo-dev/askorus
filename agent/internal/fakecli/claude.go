package fakecli

import (
	"encoding/json"
	"fmt"
	"os"
	"slices"
	"strings"
)

// claudeMain plays claude: -p with the synth schema as --json-schema is
// a combined-answer writer, -p with any other --json-schema a judge, -p
// without it an ask, and `agents --json --all` lists no running session.
func claudeMain(c *Call) int {
	switch {
	case len(c.Args) > 0 && c.Args[0] == "-p" && isSynthSchema(flagValue(c.Args, "--json-schema")):
		return claudeSynth(c)
	case len(c.Args) > 0 && c.Args[0] == "-p" && slices.Contains(c.Args, "--json-schema"):
		return claudeJudge(c)
	case len(c.Args) > 0 && c.Args[0] == "-p":
		return claudeAsk(c)
	case strings.Join(c.Args, " ") == "agents --json --all":
		fmt.Println("[]")
		return 0
	}
	return unexpected(RoleClaude)
}

// claudeResult is the start of the one object `claude -p --output-format
// json` prints.
func claudeResult(model string) map[string]any {
	return map[string]any{
		"type": "result", "subtype": "success", "is_error": false,
		"duration_ms": 1200, "num_turns": 1, "session_id": newUUID(), "total_cost_usd": 0.001,
		"usage":              map[string]int{"input_tokens": 2, "cache_creation_input_tokens": 3, "cache_read_input_tokens": 10, "output_tokens": 7},
		"modelUsage":         map[string]any{"claude-" + model + "-fake": map[string]float64{"costUSD": 0.001}},
		"permission_denials": []any{},
	}
}

func printJSON(v any) {
	b, _ := json.Marshal(v)
	fmt.Println(string(b))
}

// claudeJudge prints the result object of a judge. structured_output is
// the judge JSON as an object; result carries the same text.
func claudeJudge(c *Call) int {
	mode, err := judgeMode(RoleClaude)
	if err != nil {
		fmt.Fprintln(os.Stderr, "fakecli:", err)
		return 2
	}
	if mode == ModeSlow {
		sleepSlow()
	}
	model := flagValue(c.Args, "--model")
	out := claudeResult(model)
	text := judgeText(mode, c.Stdin)
	code := 0
	switch mode {
	case ModeUsageLimit:
		out["is_error"], out["result"], code = true, UsageLimitClaude, 1
	case ModeBadModel:
		fmt.Fprintf(os.Stderr, "[claude-code:unrecognized_model] {\"model\":%q}\n", model)
		out["is_error"], out["result"], out["modelUsage"] = true, "There's an issue with the selected model ("+model+").", map[string]any{}
		out["api_error_status"], code = 404, 1
	case ModeProse:
		out["result"] = text // no structured_output
	case ModeBadJSON:
		out["subtype"], out["is_error"], code = "error_max_structured_output_retries", true, 1
	case ModeToolUse:
		out["permission_denials"] = []any{map[string]any{
			"tool_name": "Bash", "tool_use_id": "toolu_fake", "tool_input": map[string]string{"command": ToolCommand},
		}}
		out["result"], out["structured_output"] = text, json.RawMessage(text)
	case ModeFailedWithOutput:
		fmt.Fprintln(os.Stderr, OverloadedStderr)
		out["is_error"], out["result"], out["structured_output"], code = true, text, json.RawMessage(text), 1
	default: // rule, unsure, missing_id, slow
		out["result"], out["structured_output"] = text, json.RawMessage(text)
	}
	printJSON(out)
	return code
}

// claudeAsk answers like the claude fake of the adapter's own tests:
// "fake answer: " and the prompt's last line (or the answers file's
// text), in the session --resume names or a new one. Ask mode fail is the
// usage limit, is_error with exit 1; empty is a result with no text.
func claudeAsk(c *Call) int {
	mode, err := askMode(RoleClaude)
	if err != nil {
		fmt.Fprintln(os.Stderr, "fakecli:", err)
		return 2
	}
	out := claudeResult(flagValue(c.Args, "--model"))
	if id := flagValue(c.Args, "--resume"); id != "" {
		out["session_id"] = id
	}
	switch mode {
	case AskFail:
		out["subtype"], out["is_error"], out["result"] = "error_during_execution", true, UsageLimitClaude
		printJSON(out)
		return 1
	case AskEmpty:
		out["result"] = ""
	default:
		text, err := askText(RoleClaude, "fake answer: ", c.Stdin)
		if err != nil {
			fmt.Fprintln(os.Stderr, "fakecli:", err)
			return 2
		}
		out["result"] = text
	}
	printJSON(out)
	return 0
}
