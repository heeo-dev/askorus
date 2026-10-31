package fakecli

import (
	"encoding/json"
	"fmt"
	"os"
	"slices"
	"strings"
)

// codexMain plays codex: exec with --output-schema is a judge, any other
// exec (also after --search) an ask.
func codexMain(c *Call, schemaErr error) int {
	exec := len(c.Args) > 0 && c.Args[0] == "exec" ||
		len(c.Args) > 1 && c.Args[0] == "--search" && c.Args[1] == "exec"
	switch {
	case exec && slices.Contains(c.Args, "--output-schema"):
		return codexJudge(c, schemaErr)
	case exec:
		return codexAsk(c)
	}
	return unexpected(RoleCodex)
}

// codexEvent prints one JSONL event of `codex exec --json`.
func codexEvent(v any) {
	b, _ := json.Marshal(v)
	fmt.Print(string(b) + "\n")
}

func codexItem(kind, id string, fields map[string]any) map[string]any {
	item := map[string]any{"id": id, "type": kind}
	for k, v := range fields {
		item[k] = v
	}
	return item
}

// codexFail prints how codex 0.164.0 ends a turn that failed: an error
// event and turn.failed with the same message.
func codexFail(msg string) int {
	codexEvent(map[string]any{"type": "error", "message": msg})
	codexEvent(map[string]any{"type": "turn.failed", "error": map[string]string{"message": msg}})
	return 1
}

// codexJudge prints the events of a judge: thread.started, turn.started,
// a reasoning item, the judge JSON as the agent_message and
// turn.completed. A schema file that cannot be read ends it first, as
// codex does.
func codexJudge(c *Call, schemaErr error) int {
	if schemaErr != nil {
		fmt.Fprintf(os.Stderr, "Error: failed to read output schema file %s: %v\n", flagValue(c.Args, "--output-schema"), schemaErr)
		return 1
	}
	mode, err := judgeMode(RoleCodex)
	if err != nil {
		fmt.Fprintln(os.Stderr, "fakecli:", err)
		return 2
	}
	codexEvent(map[string]any{"type": "thread.started", "thread_id": newUUID()})
	codexEvent(map[string]any{"type": "turn.started"})
	if mode == ModeSlow {
		sleepSlow()
	}
	text := judgeText(mode, c.Stdin)
	switch mode {
	case ModeUsageLimit:
		return codexFail(UsageLimitCodex)
	case ModeBadModel:
		model := flagValue(c.Args, "-m")
		inner, _ := json.Marshal(map[string]any{"type": "error", "status": 400, "error": map[string]string{
			"type": "invalid_request_error", "code": "model_not_found",
			"message": "The '" + model + "' model is not supported when using Codex with a ChatGPT account.",
		}})
		codexEvent(map[string]any{"type": "item.completed", "item": codexItem("error", "item_0", map[string]any{"message": "Model metadata for " + model + " not found."})})
		return codexFail(string(inner))
	case ModeToolUse:
		cmd := map[string]any{"command": ToolCommand, "aggregated_output": "", "exit_code": nil, "status": "in_progress"}
		codexEvent(map[string]any{"type": "item.started", "item": codexItem("command_execution", "item_0", cmd)})
		cmd["status"], cmd["exit_code"], cmd["aggregated_output"] = "declined", 1, "rejected by sandbox"
		codexEvent(map[string]any{"type": "item.completed", "item": codexItem("command_execution", "item_0", cmd)})
	}
	codexEvent(map[string]any{"type": "item.completed", "item": codexItem("reasoning", "item_1", map[string]any{"text": "**Comparing the two answers**"})})
	codexEvent(map[string]any{"type": "item.completed", "item": codexItem("agent_message", "item_2", map[string]any{"text": text})})
	if mode == ModeFailedWithOutput {
		return codexFail("stream disconnected before completion")
	}
	codexEvent(map[string]any{"type": "turn.completed", "usage": map[string]int{"input_tokens": 100, "cached_input_tokens": 80, "output_tokens": 9}})
	return 0
}

// codexAsk answers like the codex fake of the adapter's own tests: a
// lead-in, a web search, "fake: " and the prompt's last line. A follow-up
// (exec … resume … <thread> -) keeps its thread.
func codexAsk(c *Call) int {
	thread := newUUID()
	if slices.Contains(c.Args, "resume") && len(c.Args) >= 2 {
		thread = c.Args[len(c.Args)-2]
	}
	lines := strings.Split(strings.TrimSpace(c.Stdin), "\n")
	codexEvent(map[string]any{"type": "thread.started", "thread_id": thread})
	codexEvent(map[string]any{"type": "turn.started"})
	codexEvent(map[string]any{"type": "item.completed", "item": codexItem("agent_message", "item_0", map[string]any{"text": "확인하겠습니다."})})
	codexEvent(map[string]any{"type": "item.completed", "item": codexItem("web_search", "item_1", map[string]any{"query": "q"})})
	codexEvent(map[string]any{"type": "item.completed", "item": codexItem("agent_message", "item_2", map[string]any{"text": "fake: " + lines[len(lines)-1]})})
	codexEvent(map[string]any{"type": "turn.completed", "usage": map[string]int{"input_tokens": 100, "cached_input_tokens": 80, "output_tokens": 9}})
	return 0
}
