package fakecli

import (
	"encoding/json"
	"fmt"
	"os"
	"slices"
	"strings"
)

// The combined-answer writer (종합 답변, P2-3 S5b). Each fake plays it when
// the adapters' synth command reaches it: Claude's -p whose --json-schema
// is the synth schema, Codex's exec whose --output-schema file is, and
// OpenCode's run with the agent asksemble-synth. ASKSEMBLE_FAKE_SYNTH picks
// what it does, as ASKSEMBLE_FAKE_JUDGE does for a judge.

// EnvSynth is the writer's mode: "prose" for every AI, or per AI
// "claude=usage_limit_once,codex=bad_json"; _once is only that AI's first
// synth call, later ones are ok.
const EnvSynth = "ASKSEMBLE_FAKE_SYNTH"

// SynthAgent is the agent OpenCode's synth command names.
const SynthAgent = "asksemble-synth"

// Synth modes of ASKSEMBLE_FAKE_SYNTH.
const (
	SynthOK           = "ok"            // the default: SynthText, naming the AIs of every DIFFER side
	SynthMissingNames = "missing_names" // SynthMissingNamesText: no AI named
	SynthProse        = "prose"         // SynthProseText instead of JSON
	SynthBadJSON      = "bad_json"      // JSON cut off (Claude: it gave up after its schema retries)
	SynthEmpty        = "empty"         // {"answer":""}
	SynthUsageLimit   = "usage_limit"   // the AI's usage limit message, exit 1
	SynthToolUse      = "tool_use"      // a tool call (denied), then the answer
	SynthSlow         = "slow"          // waits ASKSEMBLE_FAKECLI_SLEEP, then the answer (OpenCode: its text, then the wait)
	SynthBadModel     = "bad_model"     // the AI does not know the model
	// SynthFailedWithOutput fails after the answer: Claude is_error with
	// structured_output and OverloadedStderr, Codex turn.failed and
	// OpenCode an error event after it.
	SynthFailedWithOutput = "failed_with_output"
)

// SynthModes lists the synth modes. A mode with the suffix "_once" applies
// to the AI's first synth call only.
var SynthModes = []string{
	SynthOK, SynthMissingNames, SynthProse, SynthBadJSON, SynthEmpty, SynthUsageLimit,
	SynthToolUse, SynthSlow, SynthBadModel, SynthFailedWithOutput,
}

// Fixed texts of the synth modes, for tests to compare against.
const (
	// SynthLead starts every combined answer the fake writes.
	SynthLead = "Fake combined answer."
	// SynthMissingNamesText names no AI.
	SynthMissingNamesText = SynthLead + "\n\nThe answers differ in places."
	SynthProseText        = "Here is the combined answer, without any JSON."
	// SynthBadJSONText is synth JSON cut off: it never closes.
	SynthBadJSONText = `{"answer":"Fake combined answer, cut o`
	// SynthEmptyText is synth JSON with an empty answer.
	SynthEmptyText = `{"answer":""}`
)

// SynthText is the combined answer of mode ok for a synth-v2 prompt: the
// lead, then one line per DIFFER line of the data block, "- The AIs differ:
// A vs B, C" with the AIs of each side as the prompt names them. A prompt
// without DIFFER lines gives the lead only.
func SynthText(prompt string) string {
	var b strings.Builder
	b.WriteString(SynthLead)
	for i, sides := range synthSides(prompt) {
		if i == 0 {
			b.WriteString("\n")
		}
		names := make([]string, len(sides))
		for j, s := range sides {
			names[j] = strings.Join(s, ", ")
		}
		b.WriteString("\n- The AIs differ: " + strings.Join(names, " vs "))
	}
	return b.String()
}

// synthSides reads the AIs of every side of the prompt's DIFFER lines.
func synthSides(prompt string) [][][]string {
	var out [][][]string
	end := ""
	for _, line := range strings.Split(prompt, "\n") {
		line = strings.TrimRight(line, "\r")
		if end == "" {
			if strings.HasPrefix(line, "<data-") && strings.HasSuffix(line, ">") {
				end = "</" + line[1:]
			}
			continue
		}
		if line == end {
			break
		}
		rest, ok := strings.CutPrefix(line, "DIFFER: ")
		if !ok {
			continue
		}
		var d struct {
			Sides []struct {
				AIs []string `json:"ais"`
			} `json:"sides"`
		}
		if json.Unmarshal([]byte(rest), &d) != nil {
			continue
		}
		var sides [][]string
		for _, s := range d.Sides {
			sides = append(sides, s.AIs)
		}
		out = append(out, sides)
	}
	return out
}

// synthMode is role's mode for this synth call.
func synthMode(role string) (string, error) {
	m, err := pick(os.Getenv(EnvSynth), role, SynthOK, func(v string) bool {
		return slices.Contains(SynthModes, strings.TrimSuffix(v, "_once"))
	})
	if err != nil {
		return "", err
	}
	mode, once := strings.CutSuffix(m, "_once")
	if once && !firstCall(role, "synth-"+mode) {
		return SynthOK, nil
	}
	return mode, nil
}

// synthReply is the writer's reply in mode: JSON, prose, cut-off JSON or
// an empty answer. Modes that fail before any reply have none and give
// the ok reply.
func synthReply(mode, prompt string) string {
	switch mode {
	case SynthProse:
		return SynthProseText
	case SynthBadJSON:
		return SynthBadJSONText
	case SynthEmpty:
		return SynthEmptyText
	}
	text := SynthText(prompt)
	if mode == SynthMissingNames {
		text = SynthMissingNamesText
	}
	b, _ := json.Marshal(map[string]string{"answer": text})
	return string(b)
}

// isSynthSchema reports whether a schema is the synth one: it has the
// property answer, which the judge schema does not.
func isSynthSchema(schema string) bool { return strings.Contains(schema, `"answer"`) }

// claudeSynth prints the result object of a synth call, as claudeJudge
// does for a judge.
func claudeSynth(c *Call) int {
	mode, err := synthMode(RoleClaude)
	if err != nil {
		fmt.Fprintln(os.Stderr, "fakecli:", err)
		return 2
	}
	if mode == SynthSlow {
		sleepSlow()
	}
	model := flagValue(c.Args, "--model")
	out := claudeResult(model)
	text := synthReply(mode, c.Stdin)
	code := 0
	switch mode {
	case SynthUsageLimit:
		out["is_error"], out["result"], code = true, UsageLimitClaude, 1
	case SynthBadModel:
		fmt.Fprintf(os.Stderr, "[claude-code:unrecognized_model] {\"model\":%q}\n", model)
		out["is_error"], out["result"], out["modelUsage"] = true, "There's an issue with the selected model ("+model+").", map[string]any{}
		out["api_error_status"], code = 404, 1
	case SynthProse:
		out["result"] = text // no structured_output
	case SynthBadJSON:
		out["subtype"], out["is_error"], code = "error_max_structured_output_retries", true, 1
	case SynthToolUse:
		out["permission_denials"] = []any{map[string]any{
			"tool_name": "WebSearch", "tool_use_id": "toolu_fake", "tool_input": map[string]string{"query": ToolCommand},
		}}
		out["result"], out["structured_output"] = text, json.RawMessage(text)
	case SynthFailedWithOutput:
		fmt.Fprintln(os.Stderr, OverloadedStderr)
		out["is_error"], out["result"], out["structured_output"], code = true, text, json.RawMessage(text), 1
	default: // ok, missing_names, empty, slow
		out["result"], out["structured_output"] = text, json.RawMessage(text)
	}
	printJSON(out)
	return code
}

// codexSynth prints the events of a synth call, as codexJudge does.
func codexSynth(c *Call) int {
	mode, err := synthMode(RoleCodex)
	if err != nil {
		fmt.Fprintln(os.Stderr, "fakecli:", err)
		return 2
	}
	codexEvent(map[string]any{"type": "thread.started", "thread_id": newUUID()})
	codexEvent(map[string]any{"type": "turn.started"})
	if mode == SynthSlow {
		sleepSlow()
	}
	text := synthReply(mode, c.Stdin)
	switch mode {
	case SynthUsageLimit:
		return codexFail(UsageLimitCodex)
	case SynthBadModel:
		model := flagValue(c.Args, "-m")
		inner, _ := json.Marshal(map[string]any{"type": "error", "status": 400, "error": map[string]string{
			"type": "invalid_request_error", "code": "model_not_found",
			"message": "The '" + model + "' model is not supported when using Codex with a ChatGPT account.",
		}})
		return codexFail(string(inner))
	case SynthToolUse:
		codexEvent(map[string]any{"type": "item.completed", "item": codexItem("web_search", "item_0", map[string]any{"query": ToolCommand})})
	}
	codexEvent(map[string]any{"type": "item.completed", "item": codexItem("reasoning", "item_1", map[string]any{"text": "**Writing the combined answer**"})})
	codexEvent(map[string]any{"type": "item.completed", "item": codexItem("agent_message", "item_2", map[string]any{"text": text})})
	if mode == SynthFailedWithOutput {
		return codexFail("stream disconnected before completion")
	}
	codexEvent(map[string]any{"type": "turn.completed", "usage": map[string]int{"input_tokens": 300, "cached_input_tokens": 0, "output_tokens": 40}})
	return 0
}

// opencodeSynth saves the conversation under the title the command gives,
// as OpenCode does, and prints a step with the synth JSON in a ```json
// fence, as opencodeJudge does.
func opencodeSynth(c *Call) int {
	mode, err := synthMode(RoleOpenCode)
	if err != nil {
		fmt.Fprintln(os.Stderr, "fakecli:", err)
		return 2
	}
	id := newSessionID()
	if err := saveSession(c, id, flagValue(c.Args, "--title")); err != nil {
		fmt.Fprintln(os.Stderr, "fakecli:", err)
		return 2
	}
	opencodeEvent("step_start", id, opencodePart("step-start", nil))
	text := synthReply(mode, c.Stdin)
	if mode != SynthProse {
		text = "```json\n" + text + "\n```"
	}
	switch mode {
	case SynthUsageLimit:
		return opencodeError(id, "provider.rate-limit", UsageLimitOpenCode)
	case SynthBadModel:
		return opencodeError(id, "provider.no-route", "Model unavailable: "+flagValue(c.Args, "-m"))
	case SynthToolUse:
		opencodeEvent("tool_use", id, opencodePart("tool", map[string]any{
			"tool": "websearch", "state": map[string]any{"status": "error", "input": map[string]string{"query": ToolCommand}, "error": "Permission denied: websearch"},
		}))
		opencodeStepFinish(id, "tool-calls")
		opencodeEvent("step_start", id, opencodePart("step-start", nil))
	}
	opencodeEvent("text", id, opencodePart("text", map[string]any{"text": text}))
	if mode == SynthSlow {
		sleepSlow()
	}
	if mode == SynthFailedWithOutput {
		return opencodeError(id, "provider.overloaded", "Provider is overloaded")
	}
	opencodeStepFinish(id, "stop")
	return 0
}
