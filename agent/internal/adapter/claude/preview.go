package claude

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/asksemble/asksemble/agent/internal/core"
)

// Recent conversation preview (F8): the last question in a transcript and
// the answer after it. Only the end of the file is read, and only text the
// person typed or Claude wrote; thinking and tool input or output stay on
// the PC (tool calls are only counted).

const (
	previewTailBytes     = 1 << 20 // the biggest transcript here is about 38 MB
	previewQuestionRunes = 300
	previewAnswerRunes   = 600
	previewSeparator     = "\n\n"
	previewUserRole      = "user"
	previewAssistantRole = "assistant"
)

// commandLineRe matches what Claude Code writes as a user line for slash
// commands, shell escapes and background task notices; the person did not
// type those, so they are not questions.
var commandLineRe = regexp.MustCompile(`^<((command|local-command|bash)-[a-z-]+|task-notification)>`)

// Preview returns the last question and its answer for session id.
func (a *Adapter) Preview(_ context.Context, id string) (core.Preview, error) {
	if !ValidUUID(id) {
		return core.Preview{}, core.Fail(core.ErrCodeNotFound, "")
	}
	path, ok := a.transcripts.Find(id)
	if !ok {
		return core.Preview{}, core.Fail(core.ErrCodeNotFound, "")
	}
	return readPreview(path)
}

// Find returns the transcript file of session id, the newest one if a
// session somehow has two. Only the projects folder is looked at (N10).
func (t *Transcripts) Find(id string) (string, bool) {
	if !ValidUUID(id) {
		return "", false
	}
	projects, err := os.ReadDir(t.Root)
	if err != nil {
		return "", false
	}
	best, bestTime := "", time.Time{}
	for _, p := range projects {
		if !p.IsDir() {
			continue
		}
		path := filepath.Join(t.Root, p.Name(), id+".jsonl")
		info, err := os.Stat(path)
		if err != nil || info.IsDir() || info.Size() == 0 {
			continue
		}
		if best == "" || info.ModTime().After(bestTime) {
			best, bestTime = path, info.ModTime()
		}
	}
	return best, best != ""
}

// previewLine is the part of a transcript line the preview needs.
type previewLine struct {
	Type        string `json:"type"`
	IsMeta      bool   `json:"isMeta"`
	IsSidechain bool   `json:"isSidechain"`
	Timestamp   string `json:"timestamp"`
	Message     struct {
		Content json.RawMessage `json:"content"`
	} `json:"message"`
}

type contentBlock struct {
	Type string `json:"type"`
	Text string `json:"text"`
	Name string `json:"name"` // tool_use: the tool, never its input
}

// toolKinds maps Claude Code tool names to the preview's kinds; anything
// else (including MCP tools) is "other".
var toolKinds = map[string]string{
	"Read": core.ToolRead, "NotebookRead": core.ToolRead,
	"Edit": core.ToolEdit, "MultiEdit": core.ToolEdit, "Write": core.ToolEdit, "NotebookEdit": core.ToolEdit,
	"Bash": core.ToolRun, "PowerShell": core.ToolRun, "BashOutput": core.ToolRun, "KillShell": core.ToolRun,
	"Grep": core.ToolSearch, "Glob": core.ToolSearch, "LS": core.ToolSearch,
	"WebFetch": core.ToolWeb, "WebSearch": core.ToolWeb,
	"Task": core.ToolAgent, "Agent": core.ToolAgent,
}

// kindOrder breaks ties between kinds used equally often.
var kindOrder = []string{core.ToolRead, core.ToolEdit, core.ToolRun, core.ToolSearch, core.ToolWeb, core.ToolAgent, core.ToolOther}

func toolCounts(byKind map[string]int) []core.ToolCount {
	var out []core.ToolCount
	for _, k := range kindOrder {
		if n := byKind[k]; n > 0 {
			out = append(out, core.ToolCount{Kind: k, Count: n})
		}
	}
	slices.SortStableFunc(out, func(a, b core.ToolCount) int { return b.Count - a.Count })
	return out
}

func readPreview(path string) (core.Preview, error) {
	f, err := os.Open(path)
	if err != nil {
		return core.Preview{}, core.Fail(core.ErrCodeNotFound, "")
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return core.Preview{}, err
	}
	start := max(info.Size()-previewTailBytes, 0)
	if _, err := f.Seek(start, io.SeekStart); err != nil {
		return core.Preview{}, err
	}
	data, err := io.ReadAll(io.LimitReader(f, previewTailBytes))
	if err != nil {
		return core.Preview{}, err
	}
	if start > 0 {
		// The first line is cut in the middle.
		if i := bytes.IndexByte(data, '\n'); i >= 0 {
			data = data[i+1:]
		} else {
			data = nil
		}
	}

	var question *core.Turn
	var answer []string
	var answerAt time.Time
	tools, byKind := 0, map[string]int{}
	for _, raw := range bytes.Split(data, []byte{'\n'}) {
		raw = bytes.TrimSpace(raw)
		if len(raw) == 0 || raw[0] != '{' {
			continue
		}
		var l previewLine
		if json.Unmarshal(raw, &l) != nil || l.IsSidechain {
			continue // a line still being written that does not parse yet, or a subagent's
		}
		at, _ := time.Parse(time.RFC3339Nano, l.Timestamp)
		switch l.Type {
		case previewUserRole:
			if l.IsMeta {
				continue
			}
			if text, ok := userText(l.Message.Content); ok {
				question = &core.Turn{Role: previewUserRole, Text: text, At: at}
				answer, answerAt, tools, byKind = nil, time.Time{}, 0, map[string]int{}
			}
		case previewAssistantRole:
			if question == nil {
				continue
			}
			var blocks []contentBlock
			if json.Unmarshal(l.Message.Content, &blocks) != nil {
				continue
			}
			for _, b := range blocks {
				switch b.Type {
				case "text":
					if t := strings.TrimSpace(b.Text); t != "" {
						answer = append(answer, t)
						answerAt = at
					}
				case "tool_use":
					tools++
					kind, ok := toolKinds[b.Name]
					if !ok {
						kind = core.ToolOther
					}
					byKind[kind]++
				}
			}
		}
	}

	p := core.Preview{Turns: []core.Turn{}}
	if question == nil {
		return p, nil
	}
	q := *question
	q.Text, p.Truncated = clipPreview(q.Text, previewQuestionRunes)
	p.Turns = append(p.Turns, q)
	if len(answer) > 0 || tools > 0 {
		text, cut := clipPreview(strings.Join(answer, previewSeparator), previewAnswerRunes)
		p.Truncated = p.Truncated || cut
		p.Turns = append(p.Turns, core.Turn{Role: previewAssistantRole, Text: text, At: answerAt, Tools: tools, ToolKinds: toolCounts(byKind)})
	}
	return p, nil
}

// userText returns what the person typed: a plain string, or the text
// blocks of a message (one with an image, say). Tool results, slash
// command records and shell escapes are not questions.
func userText(content json.RawMessage) (string, bool) {
	var s string
	if json.Unmarshal(content, &s) == nil {
		s = strings.TrimSpace(s)
		if s == "" || commandLineRe.MatchString(s) {
			return "", false
		}
		return s, true
	}
	var blocks []contentBlock
	if json.Unmarshal(content, &blocks) != nil {
		return "", false
	}
	var parts []string
	for _, b := range blocks {
		switch b.Type {
		case "tool_result":
			return "", false
		case "text":
			if t := strings.TrimSpace(b.Text); t != "" && !commandLineRe.MatchString(t) {
				parts = append(parts, t)
			}
		}
	}
	if len(parts) == 0 {
		return "", false
	}
	return strings.Join(parts, previewSeparator), true
}

// clipPreview keeps the first n runes and says whether it cut.
func clipPreview(s string, n int) (string, bool) {
	r := []rune(s)
	if len(r) <= n {
		return s, false
	}
	return strings.TrimRight(string(r[:n]), " \n") + "…", true
}
