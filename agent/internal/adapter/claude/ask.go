package claude

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"sort"
	"strings"

	"github.com/heeo-dev/askorus/agent/internal/config"
	"github.com/heeo-dev/askorus/agent/internal/core"
	"github.com/heeo-dev/askorus/agent/internal/proc"
)

// Asks (mode ask, Phase 2) run `claude -p` in the ask folder with the
// prompt on stdin. Only the two web tools exist and only they are allowed
// (P0-7, 2026-10-30 decision: an allow rule on those two alone is not a
// permission bypass, because no other tool exists). --strict-mcp-config
// loads no MCP servers: without it the claude.ai connectors (Claude Docs
// with create, update and delete) were offered to every ask and stopped
// only by the lack of an allow rule (P2-1 probe, 2026-10-30).

//
// Claude Code also reads files by itself: an "@path" in the prompt (at the
// start or after a space) is attached to the conversation before the model
// runs, whatever --tools says. Without a guard a question, or text inside
// another AI's answer, could pull any file of the PC into the answer (R-93,
// found 2026-11-10 with the real claude: a file, a folder's listing, also
// from inside quoted JSON, also in the combined-answer command). Two guards,
// each enough alone in that test:
//   - --settings noFileReads: a deny rule for Read. It only takes away; a
//     deny rule wins over any allow rule of the user's settings.
//   - plainAt: the prompt's "@" can no longer start a mention.

// askWebTools is the only value --tools and --allowedTools may take.
const askWebTools = "WebSearch,WebFetch"

// noFileReads is the only value --settings may take: Claude Code refuses
// every file read, so an "@path" in a prompt attaches nothing.
const noFileReads = `{"permissions":{"deny":["Read"]}}`

// plainAt returns the prompt with a zero-width space after every "@" that
// could start a file mention, so Claude Code attaches no file for it. An
// "@" right after an ASCII letter or digit (an e-mail address) stays as it
// is: Claude Code starts no mention inside a word.
func plainAt(prompt string) string {
	if !strings.Contains(prompt, "@") {
		return prompt
	}
	var b strings.Builder
	b.Grow(len(prompt) + 8)
	prev := rune(' ')
	for _, r := range prompt {
		b.WriteRune(r)
		if r == '@' && !(prev >= 'a' && prev <= 'z' || prev >= 'A' && prev <= 'Z' || prev >= '0' && prev <= '9') {
			b.WriteRune('​')
		}
		prev = r
	}
	return b.String()
}

// defaultAskTiers are the 2026-10-29 tier models (phase2-plan 4).
var defaultAskTiers = map[string]core.AskModel{
	core.TierQuick:    {Model: "haiku", Effort: "medium"},
	core.TierStandard: {Model: "sonnet", Effort: "medium"},
	core.TierDeep:     {Model: "opus", Effort: "medium"},
}

// askEfforts are Claude Code's --effort levels; every model takes them
// (11-06: haiku answered with xhigh and max too).
var askEfforts = []string{"low", "medium", "high", "xhigh", "max"}

// AskEfforts lists the efforts each tier model may take (core.EffortLister).
func (a *Adapter) AskEfforts() map[string][]string {
	return core.TierEfforts(a.AskTiers(), func(string) []string { return askEfforts })
}

// AskTiers returns the tier models, with the settings' overrides.
func (a *Adapter) AskTiers() map[string]core.AskModel {
	out := make(map[string]core.AskModel, len(defaultAskTiers))
	for t, m := range defaultAskTiers {
		if o, ok := a.askTiers[t]; ok {
			if o.Model != "" {
				m.Model = o.Model
			}
			if o.Effort != "" {
				m.Effort = o.Effort
			}
		}
		out[t] = m
	}
	return out
}

// askArgs builds the command line of one ask. The prompt is not in it. A
// one-off ask (a debate turn) keeps no conversation and never resumes.
// Since 0.5.0 every ask runs as a question with photos did (#14): the
// input is one stream-json message on stdin and the output the event
// stream, which carries the plan's usage (#20, usage.go).
func askArgs(model, effort, resume string, oneOff bool) []string {
	return askArgsFor(model, effort, resume, oneOff, true)
}

// askImageArgs is askArgs for a question with photos (#14): the same
// command, but the input is one JSON message on stdin (the prompt and the
// photos together) and the output is the event stream, whose last result
// line has the shape of the one JSON object. No tool, rule or setting
// changes: the photos are in the message, so no file is read.
func askImageArgs(model, effort, resume string, oneOff bool) []string {
	return askArgsFor(model, effort, resume, oneOff, true)
}

func askArgsFor(model, effort, resume string, oneOff, images bool) []string {
	format := "json"
	if images {
		format = streamJSON
	}
	args := []string{"-p", "--output-format", format, "--model", model}
	if effort != "" {
		args = append(args, "--effort", effort)
	}
	args = append(args, "--tools", askWebTools, "--allowedTools", askWebTools, "--strict-mcp-config", "--settings", noFileReads)
	switch {
	case resume != "":
		args = append(args, "--resume", resume)
	case oneOff:
		args = append(args, "--no-session-persistence")
	}
	if images {
		// Claude Code prints the event stream in print mode only with --verbose.
		args = append(args, "--input-format", streamJSON, "--verbose")
	}
	return args
}

// streamJSON is the only value --input-format may take, and the other
// value of --output-format, the two always together.
const streamJSON = "stream-json"

// imageMessage is the one line of stdin of a question with photos: a user
// message with the prompt and each photo as base64.
type imageMessage struct {
	Type    string `json:"type"`
	Message struct {
		Role    string       `json:"role"`
		Content []imageBlock `json:"content"`
	} `json:"message"`
}

type imageBlock struct {
	Type   string       `json:"type"`
	Text   string       `json:"text,omitempty"`
	Source *imageSource `json:"source,omitempty"`
}

type imageSource struct {
	Type      string `json:"type"`
	MediaType string `json:"media_type"`
	Data      string `json:"data"`
}

// imageStdin builds the stdin of a question with photos: prompt (already
// through plainAt) and the photos, read from the files CheckImages
// accepted. A photo that is gone or larger than core.MaxImageBytes is an
// error; nothing of a path goes into the message.
func imageStdin(prompt string, images []core.Image) (string, error) {
	if err := core.CheckImages(images); err != nil {
		return "", err
	}
	var m imageMessage
	m.Type, m.Message.Role = "user", "user"
	m.Message.Content = append(m.Message.Content, imageBlock{Type: "text", Text: prompt})
	for _, im := range images {
		info, err := os.Stat(im.Path)
		if err != nil || !info.Mode().IsRegular() || info.Size() > core.MaxImageBytes {
			return "", errors.New("a photo of the question is no longer on the PC")
		}
		data, err := os.ReadFile(im.Path)
		if err != nil || len(data) > core.MaxImageBytes {
			return "", errors.New("a photo of the question could not be read")
		}
		m.Message.Content = append(m.Message.Content, imageBlock{Type: "image",
			Source: &imageSource{Type: "base64", MediaType: im.Type, Data: base64.StdEncoding.EncodeToString(data)}})
	}
	b, err := json.Marshal(m)
	if err != nil {
		return "", err
	}
	return string(b) + "\n", nil
}

// SeesImages: every Claude model sees photos (core.ImageSeer).
func (a *Adapter) SeesImages(string) bool { return true }

// ImageModels says so to the app.
func (a *Adapter) ImageModels() core.ImageInfo { return core.ImageInfo{All: true} }

// checkAskArgs accepts exactly the shape askArgs makes, so nothing else
// (a permission flag, another tool, an MCP config, other settings, a prompt
// argument) can reach claude through an ask, and no ask goes without the
// file-read deny rule.
func checkAskArgs(args []string) error {
	if len(args) == 0 || args[0] != "-p" {
		return fmt.Errorf("%w: an ask starts with -p", ErrArgRejected)
	}
	seen := map[string]bool{}
	stream := false // --output-format stream-json: a question with photos
	for i := 1; i < len(args); i++ {
		a := args[i]
		if seen[a] {
			return fmt.Errorf("%w: %s twice", ErrArgRejected, a)
		}
		seen[a] = true
		if a == "--strict-mcp-config" || a == "--no-session-persistence" || a == "--verbose" {
			continue
		}
		if i+1 >= len(args) {
			return fmt.Errorf("%w: %s needs a value", ErrArgRejected, a)
		}
		v := args[i+1]
		i++
		var ok bool
		switch a {
		case "--output-format":
			ok = v == "json" || v == streamJSON
			stream = v == streamJSON
		case "--input-format":
			ok = v == streamJSON
		case "--model":
			ok = config.ValidModel(v)
		case "--effort":
			ok = config.ValidEffort(v)
		case "--tools", "--allowedTools":
			ok = v == askWebTools
		case "--settings":
			ok = v == noFileReads
		case "--resume":
			ok = ValidUUID(v)
		}
		if !ok {
			return fmt.Errorf("%w: %s %q", ErrArgRejected, a, v)
		}
	}
	for _, need := range []string{"--model", "--tools", "--allowedTools", "--strict-mcp-config", "--settings"} {
		if !seen[need] {
			return fmt.Errorf("%w: an ask needs %s", ErrArgRejected, need)
		}
	}
	if seen["--no-session-persistence"] && seen["--resume"] {
		return fmt.Errorf("%w: a one-off ask never resumes", ErrArgRejected)
	}
	// The stream output, the stream input and --verbose come together (a
	// question with photos) or not at all.
	if seen["--input-format"] != stream || seen["--verbose"] != stream {
		return fmt.Errorf("%w: the stream formats and --verbose go together", ErrArgRejected)
	}
	return nil
}

// askOutput is the one JSON object `claude -p --output-format json` prints.
type askOutput struct {
	Type           string `json:"type"`
	IsError        bool   `json:"is_error"` // subtype says "success" even on errors
	Result         string `json:"result"`
	SessionID      string `json:"session_id"`
	TerminalReason string `json:"terminal_reason"`
	APIErrorStatus *int   `json:"api_error_status"`
	// Usage is this run's; total_cost_usd and modelUsage add up the whole
	// resumed session.
	Usage struct {
		InputTokens         int `json:"input_tokens"`
		CacheCreationTokens int `json:"cache_creation_input_tokens"`
		CacheReadTokens     int `json:"cache_read_input_tokens"`
		OutputTokens        int `json:"output_tokens"`
	} `json:"usage"`
	ModelUsage map[string]json.RawMessage `json:"modelUsage"`
}

// Ask answers one question with claude -p (core.Asker).
func (a *Adapter) Ask(ctx context.Context, req core.AskRequest) (core.AskResult, error) {
	if a.runner == nil {
		return core.AskResult{}, core.Fail(core.ErrCodeAgentUnavailable, "claude was not found")
	}
	tier, ok := a.AskTiers()[req.Tier]
	if !ok {
		tier = a.AskTiers()[core.TierStandard]
	}
	model, effort := firstNonEmpty(req.Model, tier.Model), firstNonEmpty(req.Effort, tier.Effort)
	if !config.ValidModel(model) {
		return core.AskResult{}, core.Fail(core.ErrCodeBadModel, model)
	}
	if effort != "" && !config.ValidEffort(effort) {
		return core.AskResult{}, core.Fail(core.ErrCodeBadModel, "effort "+effort)
	}
	if req.Resume != "" && !ValidUUID(req.Resume) {
		return core.AskResult{}, core.Fail(core.ErrCodeNotFound, "")
	}
	if req.OneOff && req.Resume != "" {
		return core.AskResult{}, core.Fail(core.ErrCodeInternal, "a one-off ask never resumes")
	}
	// The prompt, and the photos when there are some, go on stdin as one
	// stream-json message; no path reaches the command.
	args := askArgs(model, effort, req.Resume, req.OneOff)
	stdin, err := imageStdin(plainAt(req.Prompt), req.Images)
	if err != nil {
		return core.AskResult{}, core.Fail(core.ErrCodeAskFailed, err.Error())
	}
	if err := checkAskArgs(args); err != nil {
		return core.AskResult{}, err
	}
	if req.Resume != "" {
		// Never resume while a process still runs the session (claude would
		// fork a copy): not during an open or stop from the phone, and not
		// while it is open as a remote or terminal session.
		unlock, err := a.lock(req.Resume)
		if err != nil {
			return core.AskResult{}, err
		}
		defer unlock()
		if _, running := a.live()[req.Resume]; running {
			return core.AskResult{}, core.Fail(core.ErrCodeBusy, "")
		}
	}
	res, err := proc.Run(ctx, proc.Cmd{Exe: a.runner.Path(), Dir: req.Folder, Args: args, Stdin: stdin})
	switch {
	case errors.Is(err, proc.ErrTimeout):
		return core.AskResult{}, core.Fail(core.ErrCodeAskTimeout, "")
	case errors.Is(err, fs.ErrNotExist), errors.Is(err, proc.ErrScript):
		return core.AskResult{}, core.Fail(core.ErrCodeAgentUnavailable, "claude was not found")
	case err != nil:
		return core.AskResult{}, core.Fail(core.ErrCodeAskFailed, err.Error())
	}
	a.noteUsage(res.Stdout)
	out, err := parseAskOutput(res, model)
	if req.OneOff {
		out.SessionID = "" // nothing was kept to carry on
	}
	if core.ErrorCode(err) == core.ErrCodeSignedOut {
		a.forgetLogin() // the next info asks claude again
		err = a.withSignInHint(err)
	}
	return out, err
}

func parseAskOutput(res proc.Result, asked string) (core.AskResult, error) {
	var out askOutput
	found := false
	lines := bytes.Split(bytes.TrimSpace(res.Stdout), []byte("\n"))
	for i := len(lines) - 1; i >= 0 && !found; i-- {
		line := bytes.TrimSpace(lines[i])
		if len(line) > 0 && line[0] == '{' && json.Unmarshal(line, &out) == nil && out.Type == "result" {
			found = true
		}
	}
	stderr := strings.TrimSpace(string(res.Stderr))
	if !found {
		switch {
		case strings.Contains(stderr, "No conversation found"):
			return core.AskResult{}, core.Fail(core.ErrCodeNotFound, "")
		case signedOut(stderr, nil):
			return core.AskResult{}, core.Fail(core.ErrCodeSignedOut, LoginCommand)
		case stderr != "":
			return core.AskResult{}, core.Fail(core.ErrCodeAskFailed, clip(stderr, 300))
		}
		return core.AskResult{}, core.Fail(core.ErrCodeAskFailed, fmt.Sprintf("claude ended with exit code %d and no answer", res.ExitCode))
	}
	r := core.AskResult{
		SessionID:    out.SessionID,
		Model:        answeringModel(out.ModelUsage, asked),
		InputTokens:  out.Usage.InputTokens + out.Usage.CacheCreationTokens + out.Usage.CacheReadTokens,
		OutputTokens: out.Usage.OutputTokens,
	}
	if out.SessionID != "" && !ValidUUID(out.SessionID) {
		r.SessionID = ""
	}
	if out.IsError || res.ExitCode != 0 {
		msg := firstNonEmpty(out.Result, stderr)
		code := core.ErrCodeAskFailed
		switch {
		case strings.Contains(stderr, "[claude-code:unrecognized_model]") || (out.APIErrorStatus != nil && *out.APIErrorStatus == 404):
			code = core.ErrCodeBadModel
		case signedOut(msg+"\n"+stderr, out.APIErrorStatus):
			return r, core.Fail(core.ErrCodeSignedOut, LoginCommand)
		}
		return r, core.Fail(code, clip(msg, 300))
	}
	r.Answer = out.Result
	return r, nil
}

// answeringModel names the model from modelUsage. A resumed session lists
// every model it used; the one matching the asked alias wins.
func answeringModel(usage map[string]json.RawMessage, asked string) string {
	names := make([]string, 0, len(usage))
	for n := range usage {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		if strings.Contains(n, asked) {
			return n
		}
	}
	if len(names) > 0 {
		return names[0]
	}
	return asked
}
