package claude

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/asksemble/asksemble/agent/internal/core"
)

// Lines shaped like Claude Code's transcripts (types and blocks as seen in
// a real rc-test transcript on 10-28; the text is made up).
func userLine(content any, extra map[string]any) string {
	m := map[string]any{"type": "user", "timestamp": "2026-10-28T10:00:00Z", "message": map[string]any{"role": "user", "content": content}}
	for k, v := range extra {
		m[k] = v
	}
	b, _ := json.Marshal(m)
	return string(b)
}

func assistantLine(blocks ...map[string]any) string {
	b, _ := json.Marshal(map[string]any{"type": "assistant", "timestamp": "2026-10-28T10:00:05Z", "message": map[string]any{"role": "assistant", "content": blocks}})
	return string(b)
}

func textBlock(s string) map[string]any { return map[string]any{"type": "text", "text": s} }
func thinking(s string) map[string]any  { return map[string]any{"type": "thinking", "thinking": s} }
func toolUse(name string) map[string]any {
	return map[string]any{"type": "tool_use", "name": name, "input": map[string]any{"command": "secret-ish input"}}
}

func writePreviewFile(t *testing.T, lines ...string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "s.jsonl")
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestPreviewLastQuestionAndAnswer(t *testing.T) {
	path := writePreviewFile(t,
		`{"type":"custom-title","customTitle":"t"}`,
		userLine("first question", nil),
		assistantLine(textBlock("first answer")),
		userLine("second question", nil),
		assistantLine(thinking("private reasoning"), textBlock("Let me look.")),
		assistantLine(toolUse("Bash")),
		userLine([]map[string]any{{"type": "tool_result", "content": "tool output"}}, map[string]any{"toolUseResult": map[string]any{}}),
		assistantLine(toolUse("Read")),
		userLine("<command-name>/compact</command-name>", nil),
		userLine("<task-notification>\n<task-id>b1</task-id>\n</task-notification>", nil),
		userLine("caveat", map[string]any{"isMeta": true}),
		userLine("subagent prompt", map[string]any{"isSidechain": true}),
		assistantLine(textBlock("Done: it was the cache.")),
		`{"type":"assistant","message":{"content":[{"type":"text","text":"half a li`, // still being written
	)
	p, err := readPreview(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Turns) != 2 || p.Truncated {
		t.Fatalf("preview = %+v", p)
	}
	q, a := p.Turns[0], p.Turns[1]
	if q.Role != "user" || q.Text != "second question" || q.At.IsZero() {
		t.Errorf("question = %+v", q)
	}
	if a.Role != "assistant" || a.Text != "Let me look.\n\nDone: it was the cache." || a.Tools != 2 {
		t.Errorf("answer = %+v", a)
	}
	if want := []core.ToolCount{{Kind: "read", Count: 1}, {Kind: "run", Count: 1}}; !slices.Equal(a.ToolKinds, want) {
		t.Errorf("tool kinds = %+v, want %+v", a.ToolKinds, want)
	}
	b, _ := json.Marshal(p)
	for _, never := range []string{"private reasoning", "tool output", "secret-ish input", "subagent prompt", "/compact", "caveat", "task-id"} {
		if strings.Contains(string(b), never) {
			t.Errorf("preview sends %q: %s", never, b)
		}
	}
}

// Tools are grouped by kind, most used first; unknown and MCP tools are
// "other". Only names are looked at, never their input.
func TestPreviewToolKinds(t *testing.T) {
	path := writePreviewFile(t,
		userLine("fix it", nil),
		assistantLine(toolUse("Grep"), toolUse("Read"), toolUse("Read")),
		assistantLine(toolUse("Edit"), toolUse("Bash"), toolUse("Bash"), toolUse("Bash")),
		assistantLine(toolUse("mcp__claude-in-chrome__navigate"), toolUse("TodoWrite"), textBlock("done")),
	)
	p, err := readPreview(path)
	if err != nil {
		t.Fatal(err)
	}
	want := []core.ToolCount{{Kind: "run", Count: 3}, {Kind: "read", Count: 2}, {Kind: "other", Count: 2}, {Kind: "edit", Count: 1}, {Kind: "search", Count: 1}}
	if a := p.Turns[1]; a.Tools != 9 || !slices.Equal(a.ToolKinds, want) {
		t.Fatalf("answer = %+v, want kinds %+v", a, want)
	}
}

func TestPreviewTextBlocksQuestionAndNoAnswerYet(t *testing.T) {
	path := writePreviewFile(t,
		userLine("old", nil),
		assistantLine(textBlock("old answer")),
		userLine([]map[string]any{{"type": "image", "source": map[string]any{}}, {"type": "text", "text": "what is in this picture?"}}, nil),
	)
	p, err := readPreview(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Turns) != 1 || p.Turns[0].Text != "what is in this picture?" {
		t.Fatalf("preview = %+v, want only the new question (Claude has not answered yet)", p)
	}
}

func TestPreviewClipsLongTexts(t *testing.T) {
	long := strings.Repeat("가", previewAnswerRunes+50)
	path := writePreviewFile(t, userLine(strings.Repeat("q", previewQuestionRunes+1), nil), assistantLine(textBlock(long)))
	p, err := readPreview(path)
	if err != nil {
		t.Fatal(err)
	}
	if !p.Truncated || len([]rune(p.Turns[0].Text)) != previewQuestionRunes+1 || len([]rune(p.Turns[1].Text)) != previewAnswerRunes+1 {
		t.Fatalf("lengths %d %d truncated %v", len([]rune(p.Turns[0].Text)), len([]rune(p.Turns[1].Text)), p.Truncated)
	}
	if !strings.HasSuffix(p.Turns[1].Text, "…") {
		t.Error("a cut text does not end with …")
	}
}

// A big transcript is read from its last megabyte only.
func TestPreviewReadsOnlyTheTail(t *testing.T) {
	filler := assistantLine(textBlock(strings.Repeat("x", 4000)))
	lines := []string{userLine("very old question", nil)}
	for i := 0; i < 400; i++ { // about 1.6 MB
		lines = append(lines, filler)
	}
	lines = append(lines, userLine("latest question", nil), assistantLine(textBlock("latest answer")))
	p, err := readPreview(writePreviewFile(t, lines...))
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Turns) != 2 || p.Turns[0].Text != "latest question" || p.Turns[1].Text != "latest answer" {
		t.Fatalf("preview = %+v", p.Turns)
	}

	// No question inside the last megabyte: nothing rather than half a turn.
	lines = append([]string{userLine("only question", nil)}, lines[1:401]...)
	p, err = readPreview(writePreviewFile(t, lines...))
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Turns) != 0 {
		t.Fatalf("preview = %+v, want no turns", p.Turns)
	}
}

func TestPreviewFindsTheSessionFile(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "D--work-app")
	os.MkdirAll(dir, 0o700)
	os.WriteFile(filepath.Join(dir, id1+".jsonl"), []byte(userLine("hello", nil)+"\n"+assistantLine(textBlock("hi"))+"\n"), 0o600)
	a := &Adapter{transcripts: NewTranscripts(root)}
	p, err := a.Preview(context.Background(), id1)
	if err != nil || len(p.Turns) != 2 {
		t.Fatalf("preview = %+v, %v", p, err)
	}
	if _, err := a.Preview(context.Background(), "22222222-2222-4222-8222-222222222222"); core.ErrorCode(err) != core.ErrCodeNotFound {
		t.Errorf("unknown session: %v, want not_found", err)
	}
	if _, err := a.Preview(context.Background(), "../secret"); core.ErrorCode(err) != core.ErrCodeNotFound {
		t.Errorf("bad id: %v, want not_found", err)
	}
}
