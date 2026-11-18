package agy

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"github.com/heeo-dev/askorus/agent/internal/config"
	"github.com/heeo-dev/askorus/agent/internal/core"
	"github.com/heeo-dev/askorus/agent/internal/proc"
)

// FolderName is the folder under the ask folder (and under the judge
// folder) that agy runs in: the agent files live there, and no other AI's
// ask sees them.
const FolderName = "agy"

// The agents the adapter hands agy, each in an agent file of its own.
const (
	askAgent = "askorus-ask"
)

// lockSettings are the deny rules of a run (--settings: that run only,
// never saved). agy weighs deny before ask before allow, so an allow rule
// the person keeps for their own work opens none of these. The mode and
// the tool permission are the person's own otherwise (Auto and
// always-proceed on the PC of 2026-11-18): request-review asks before
// every tool an agent file leaves in, and a run without a terminal cannot
// be asked, so such a call is refused too (soft-denied). denyReads also
// denies every file read: every run but a question with photos, whose
// photos lie in the folder it runs in. slash keeps slash commands on, for
// /usage only.
func lockSettings(denyReads, slash bool) string {
	deny := []string{"command(*)", "unsandboxed(*)"}
	if denyReads {
		deny = append(deny, "read_file(*)")
	}
	deny = append(deny, "write_file(*)", "execute_url(*)", "download_url(*)", "mcp(*)")
	type perms struct {
		Allow []string `json:"allow"`
		Deny  []string `json:"deny"`
	}
	b, _ := json.Marshal(struct {
		AgentMode               string `json:"agentMode"`
		ToolPermission          string `json:"toolPermission"`
		AllowNonWorkspaceAccess bool   `json:"allowNonWorkspaceAccess"`
		DisableSlashCommands    bool   `json:"disableSlashCommands"`
		Permissions             perms  `json:"permissions"`
	}{ToolPermission: permissionMode, DisableSlashCommands: !slash, Permissions: perms{Allow: []string{}, Deny: deny}})
	return string(b)
}

// askSettings are an ask's: every read denied, no slash commands.
var askSettings = lockSettings(true, false)

// askInstructions are the body of the ask's agent files: agy's own
// system prompt is left out for it. The search line keeps a question agy
// cannot answer from searching until the run's time limit (2026-11-18:
// asked to read a page, it searched 19 times for 240 seconds).
const askInstructions = "You answer one general question sent by the Askorus app to several AIs at once. It is not a coding task. " +
	"Answer only the question; do not add next-step suggestions or extra sections. " +
	"Search the web only when the question needs it, a few times at most, then answer with what you found. " +
	"Never write file paths or links to files in the answer.\n"

// askAgentFile is the ask's agent file. Its tool list is the first lock:
// agy hands the model these tools and no other (manage_task, its own to-do
// list, always comes along). commandExecutionPolicy off and subagent false
// keep it from running commands or being started as a helper.
const askAgentFile = "---\n" +
	"name: " + askAgent + "\n" +
	"description: Askorus ask, answer the question only\n" +
	"tools: [search_web]\n" +
	"mainAgent: true\n" +
	"subagent: false\n" +
	"commandExecutionPolicy: off\n" +
	"---\n" +
	askInstructions

// permissionMode is the mode the start event must report: what
// askSettings sets.
const permissionMode = "request-review"

// askTools are the tools an ask's run may use. Any other is a tool the
// agent file did not hand over, and the answer is thrown away.
var askTools = map[string]bool{"search_web": true, "manage_task": true}

// askArgs builds one ask without photos; runArgs any ask. The question is
// not among them: it goes on stdin (stdinMessage).
//
//	--agent A --model M [--conversation C] --input-format stream-json --output-format stream-json --disable-slash-commands --settings S
func askArgs(model, conversation string) []string {
	return runArgs(askAgent, askSettings, model, conversation)
}

func runArgs(agent, settings, model, conversation string) []string {
	args := []string{"--agent", agent, "--model", model}
	if conversation != "" {
		args = append(args, "--conversation", conversation)
	}
	return append(args, "--input-format", "stream-json", "--output-format", "stream-json",
		"--disable-slash-commands", "--settings", settings)
}

// askAgentSettings pairs each ask agent with its deny rules.
var askAgentSettings = map[string]string{askAgent: askSettings, photoAgent: photoSettings}

// checkArgs accepts exactly what runArgs makes for one of the ask agents
// with its own deny rules, a valid model and conversation: no other option
// (a mode, --dangerously-skip-permissions, a project, another agent or
// other settings) reaches agy through an ask.
func checkArgs(args []string) error {
	agent, model, conversation := "", "", ""
	if len(args) > 3 {
		agent, model = args[1], args[3]
	}
	if len(args) > 5 && args[4] == "--conversation" {
		conversation = args[5]
	}
	settings, ok := askAgentSettings[agent]
	switch {
	case !ok:
		return fmt.Errorf("agy ask: agent %q refused", agent)
	case !modelRe.MatchString(model):
		return fmt.Errorf("agy ask: model %q refused", model)
	case conversation != "" && !conversationRe.MatchString(conversation):
		return fmt.Errorf("agy ask: conversation %q refused", conversation)
	case !slices.Equal(args, runArgs(agent, settings, model, conversation)):
		return fmt.Errorf("agy ask: arguments %q refused", args)
	}
	return nil
}

// stdinMessage is the one stream-json message an ask sends: a user
// message whose only block is the prompt's text.
func stdinMessage(prompt string) string {
	b, _ := json.Marshal(map[string]any{
		"event": "user",
		"message": map[string]any{
			"role":    "user",
			"content": []map[string]string{{"type": "text", "text": prompt}},
		},
	})
	return string(b) + "\n"
}

// agentFile makes sure dir/.agents/agents/<name>.md holds content and
// returns dir. It writes the file once (when missing or different), then
// checks before every run that it still holds exactly that: a file changed
// under a running agent is internal, never run.
func (a *Adapter) agentFile(dir, name, content string) (string, error) {
	p := filepath.Join(dir, ".agents", "agents", name+".md")
	want := []byte(content)
	if a.shadowed(name) {
		return "", core.Fail(core.ErrCodeInternal, "agy: an agent of the same name is in agy's own config folder")
	}
	a.fileMu.Lock()
	defer a.fileMu.Unlock()
	if !a.ready[p] {
		if b, err := os.ReadFile(p); err != nil || !bytes.Equal(b, want) {
			if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
				return "", core.Fail(core.ErrCodeInternal, "agy: the agent folder could not be made")
			}
			if err := config.WriteFileAtomic(p, want); err != nil {
				return "", core.Fail(core.ErrCodeInternal, "agy: the agent file could not be written")
			}
			a.writes++
		}
		a.ready[p] = true
	}
	if b, err := os.ReadFile(p); err != nil || !bytes.Equal(b, want) {
		return "", core.Fail(core.ErrCodeInternal, "agy: the agent file changed; restart the agent")
	}
	return dir, nil
}

// askFolder is the folder an ask runs in, with the ask's agent file.
func (a *Adapter) askFolder(folder string) (string, error) {
	if info, err := os.Stat(folder); !filepath.IsAbs(folder) || err != nil || !info.IsDir() {
		return "", core.Fail(core.ErrCodeInternal, "agy: the ask folder is not an absolute existing folder")
	}
	return a.agentFile(filepath.Join(folder, FolderName), askAgent, askAgentFile)
}

// Ask answers one question (core.Asker).
func (a *Adapter) Ask(ctx context.Context, req core.AskRequest) (core.AskResult, error) {
	if a.exeErr != nil {
		return core.AskResult{}, core.Fail(core.ErrCodeAgentUnavailable, "agy was not found")
	}
	tier, ok := a.AskTiers()[req.Tier]
	if !ok {
		tier = a.AskTiers()[core.TierStandard]
	}
	model, effort := req.Model, req.Effort
	if model == "" {
		model, effort = tier.Model, tier.Effort
		if req.Effort != "" {
			effort = req.Effort
		}
	}
	switch {
	case !modelRe.MatchString(model):
		return core.AskResult{}, core.Fail(core.ErrCodeBadModel, model)
	case effort != "" && !efforts[effort]:
		return core.AskResult{}, core.Fail(core.ErrCodeBadModel, "effort "+effort)
	case req.Resume != "" && !conversationRe.MatchString(req.Resume):
		return core.AskResult{}, core.Fail(core.ErrCodeNotFound, "")
	case req.OneOff && req.Resume != "":
		return core.AskResult{}, core.Fail(core.ErrCodeInternal, "a one-off ask never resumes")
	case len(req.Images) > 0 && !a.SeesImages(model):
		return core.AskResult{}, core.Fail(core.ErrCodeNoImages, "")
	case len(req.Images) > 0 && req.Resume != "":
		// R-130: the conversation keeps its first agent's tools; it would say it sees no photo.
		return core.AskResult{}, &core.ActionError{Code: core.ErrCodeNoImages, Hint: core.HintNewOnly}
	}
	m := cliModel(model, effort)
	agent, settings, tools, prompt := askAgent, askSettings, askTools, req.Prompt
	var dir string
	if len(req.Images) > 0 {
		if a.shadowed(photoAgent) {
			return core.AskResult{}, core.Fail(core.ErrCodeInternal, "agy: an agent of the same name is in agy's own config folder")
		}
		d, names, cleanup, err := photoFolder(req.Folder, photoAgent, photoAgentFile, req.Images)
		if err != nil {
			return core.AskResult{}, err
		}
		defer cleanup()
		dir, agent, settings, tools, prompt = d, photoAgent, photoSettings, photoTools, photoLine(names)+req.Prompt
	} else {
		d, err := a.askFolder(req.Folder)
		if err != nil {
			return core.AskResult{}, err
		}
		dir = d
	}
	args := runArgs(agent, settings, m, req.Resume)
	if err := checkArgs(args); err != nil {
		return core.AskResult{}, core.Fail(core.ErrCodeBadModel, m)
	}
	res, err := proc.Run(ctx, proc.Cmd{Exe: a.exe, Dir: dir, Args: args, Stdin: stdinMessage(prompt)})
	switch {
	case errors.Is(err, proc.ErrTimeout):
		// The output so far says whether agy was waiting to try again (its
		// last step an error, after any answer): Google had no capacity.
		if r := readRun(res.Stdout, tools); r.lastErr >= 0 && r.lastErr > r.lastDone {
			return core.AskResult{}, &core.ActionError{Code: core.ErrCodeAskTimeout, Hint: core.HintServerBusy}
		}
		return core.AskResult{}, core.Fail(core.ErrCodeAskTimeout, "")
	case errors.Is(err, fs.ErrNotExist), errors.Is(err, proc.ErrScript):
		return core.AskResult{}, core.Fail(core.ErrCodeAgentUnavailable, "agy was not found")
	case err != nil:
		return core.AskResult{}, core.Fail(core.ErrCodeAskFailed, err.Error())
	}
	out, err := parseAsk(res, agent, tools, m, req.Resume)
	if req.OneOff {
		out.SessionID = "" // agy keeps the conversation, but nothing carries it on
	}
	return out, a.afterAsk(ctx, err)
}

// event is one line of agy's stream-json output.
type event struct {
	Event string `json:"event"`
	Init  *struct {
		Agent          string `json:"agent"`
		PermissionMode string `json:"permission_mode"`
	} `json:"init"`
	Step *struct {
		StepType string `json:"step_type"`
		State    string `json:"state"`
		ToolName string `json:"tool_name"`
	} `json:"step_update"`
	Result *result `json:"result"`
}

// result is the run's result event.
type result struct {
	ConversationID   string          `json:"conversation_id"`
	Status           string          `json:"status"`
	Response         string          `json:"response"`
	Error            string          `json:"error"`
	StructuredOutput json.RawMessage `json:"structured_output"`
	Usage            *struct {
		InputTokens  int `json:"input_tokens"`
		OutputTokens int `json:"output_tokens"`
	} `json:"usage"`
}

// run is what readRun found in one run's output.
type run struct {
	sawInit  bool
	agent    string
	mode     string
	badTool  string // the first tool outside the allowed ones
	lastErr  int    // the place of the last error_message step, -1 if none
	lastDone int    // the place of the last finished answer step, -1 if none
	result   *result
}

// readRun reads the stream-json lines. Places count the step events in
// order.
func readRun(stdout []byte, tools map[string]bool) run {
	r := run{lastErr: -1, lastDone: -1}
	n := 0
	for _, line := range bytes.Split(stdout, []byte("\n")) {
		line = bytes.TrimSpace(line)
		var e event
		if len(line) == 0 || line[0] != '{' || json.Unmarshal(line, &e) != nil {
			continue
		}
		switch {
		case e.Event == "init" && e.Init != nil:
			r.sawInit, r.agent, r.mode = true, e.Init.Agent, e.Init.PermissionMode
		case e.Event == "step_update" && e.Step != nil:
			n++
			switch e.Step.StepType {
			case "tool":
				if !tools[e.Step.ToolName] && r.badTool == "" {
					r.badTool = e.Step.ToolName
					if r.badTool == "" {
						r.badTool = "?"
					}
				}
			case "error_message":
				r.lastErr = n
			case "agent_response", "finish":
				if e.Step.State == "DONE" {
					r.lastDone = n
				}
			}
		case e.Event == "result" && e.Result != nil:
			r.result = e.Result
		}
	}
	return r
}

// retried reports whether a result that says ERROR is a run agy finished
// after trying again: when a model is out of capacity (503), agy tries
// again, and a turn that then answered keeps status ERROR and the first
// attempt's error (2026-11-18, 4 of 17 runs). It counts as finished only
// when the error is that retry note, the last step is a finished answer
// after the last error step, and there is an answer.
func (r run) retried(haveAnswer bool) bool {
	return r.result != nil && r.result.Status == "ERROR" && strings.HasPrefix(r.result.Error, "API error (attempt ") &&
		haveAnswer && r.lastDone > r.lastErr
}

// fileLinkRe matches a Markdown link to a file ("[photo-1.jpg](file:///C:/…)")
// and bareFileRe a bare file URL, in any case and with one slash or
// three ("FILE:///…", "file:/C:/…"): agy links the files it read, and
// their paths name the person's user folder (R-127).
var (
	fileLinkRe = regexp.MustCompile(`(?i)\[([^\]\n]*)\]\(<?file:[^)\n]*>?\)`)
	bareFileRe = regexp.MustCompile(`(?i)<?\bfile:/*[^\s)>\]"']+>?`)
	// winPathRe is a Windows path as link text ("C:\Users\…\photo-1.jpg").
	winPathRe = regexp.MustCompile(`(?i)^[a-z]:[\\/]`)
)

// baseName is the last part of a path or file URL.
func baseName(u string) string {
	u = strings.Trim(u, "<> ")
	u = strings.ReplaceAll(u, `\`, "/")
	if i := strings.LastIndex(u, "/"); i >= 0 {
		u = u[i+1:]
	}
	return u
}

// stripFileLinks keeps a file link's text and drops its target (the text
// itself, when it is a path, down to the file's name), and turns a bare
// file URL into the file's name, so no path of the PC reaches the phone or
// another AI.
func stripFileLinks(s string) string {
	s = fileLinkRe.ReplaceAllStringFunc(s, func(m string) string {
		text := fileLinkRe.FindStringSubmatch(m)[1]
		if strings.Contains(strings.ToLower(text), "file:") || winPathRe.MatchString(text) || strings.HasPrefix(text, "/") {
			return baseName(text)
		}
		return text
	})
	return bareFileRe.ReplaceAllStringFunc(s, baseName)
}

// stripOutput applies stripFileLinks to every string of a writer's JSON,
// which goes on to the phone and to the debate's AIs as it is (R-127).
func stripOutput(raw []byte) json.RawMessage {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var v any
	if dec.Decode(&v) != nil {
		return json.RawMessage(raw)
	}
	var walk func(any) any
	walk = func(x any) any {
		switch t := x.(type) {
		case string:
			return stripFileLinks(t)
		case []any:
			for i := range t {
				t[i] = walk(t[i])
			}
		case map[string]any:
			for k := range t {
				t[k] = walk(t[k])
			}
		}
		return x
	}
	out, err := json.Marshal(walk(v))
	if err != nil {
		return json.RawMessage(raw)
	}
	return out
}

// agyError is the line agy prints on stderr when a run ends on an error.
type agyError struct {
	ShortError string `json:"short_error"`
	Status     string `json:"status"`
}

// stderrInfo reads agy's stderr: the AGY_ERROR line, whether a call was
// refused for a permission no terminal could give (auto-denied), and
// whether --conversation named a conversation agy does not have.
func stderrInfo(stderr []byte) (ae agyError, denied, noConversation bool, rest string) {
	var keep []string
	for _, l := range strings.Split(string(stderr), "\n") {
		l = strings.TrimSpace(l)
		switch {
		case l == "":
		case strings.HasPrefix(l, "AGY_ERROR:"):
			json.Unmarshal([]byte(strings.TrimSpace(strings.TrimPrefix(l, "AGY_ERROR:"))), &ae)
		case strings.Contains(l, "auto-denied"):
			denied = true
		case strings.HasPrefix(l, "warning: conversation ") && strings.HasSuffix(l, " not found"):
			noConversation = true
		default:
			keep = append(keep, l)
		}
	}
	if len(keep) > 3 {
		keep = keep[len(keep)-3:]
	}
	return ae, denied, noConversation, strings.Join(keep, " ")
}

// parseAsk turns one ask's output into its answer, in this order: no start
// event or a start event that is not the agent and mode the adapter set
// (the lock did not hold) throws the run away; so does a tool outside
// tools; a finished run is its answer (or ask_empty); anything else is the
// error agy gave.
func parseAsk(res proc.Result, agent string, tools map[string]bool, model, resume string) (core.AskResult, error) {
	r := readRun(res.Stdout, tools)
	ae, denied, noConversation, rest := stderrInfo(res.Stderr)
	out := core.AskResult{Model: model}
	if r.result != nil {
		if u := r.result.Usage; u != nil {
			out.InputTokens, out.OutputTokens = u.InputTokens, u.OutputTokens
		}
	}
	answer := ""
	if r.result != nil {
		answer = strings.TrimSpace(r.result.Response)
	}
	switch {
	case r.sawInit && (r.agent != agent || r.mode != permissionMode):
		return core.AskResult{}, core.Fail(core.ErrCodeInternal, "agy did not run with Askorus's ask agent and settings")
	case r.badTool != "":
		return core.AskResult{}, core.Fail(core.ErrCodeAskFailed, "agy reached for a tool an ask may not use ("+clip(r.badTool, 40)+")")
	case r.result != nil && (r.result.Status == "SUCCESS" || r.retried(answer != "")):
		if !r.sawInit {
			return core.AskResult{}, core.Fail(core.ErrCodeInternal, "agy did not report its agent")
		}
		if resume != "" && (noConversation || r.result.ConversationID != resume) {
			return core.AskResult{}, core.Fail(core.ErrCodeNotFound, "")
		}
		if answer == "" {
			if denied {
				return core.AskResult{}, core.Fail(core.ErrCodeAskFailed, "agy stopped: it tried to open a file it may not read")
			}
			return core.AskResult{}, core.Fail(core.ErrCodeAskEmpty, "")
		}
		if conversationRe.MatchString(r.result.ConversationID) {
			out.SessionID = r.result.ConversationID
		}
		out.Answer = stripFileLinks(answer)
		return out, nil
	}
	msg := ae.ShortError
	if r.result != nil && r.result.Error != "" {
		msg = r.result.Error
	}
	if msg == "" {
		msg = rest
	}
	return core.AskResult{}, failure(msg, ae.Status, res.ExitCode)
}

// failure turns agy's error into an ask error.
func failure(msg, status string, exitCode int) error {
	low := strings.ToLower(msg)
	switch {
	case strings.Contains(low, "invalid model selection") || strings.Contains(low, "not recognized as a known model"):
		return core.Fail(core.ErrCodeBadModel, clip(msg, 300))
	case signedOut(status, low):
		return core.Fail(core.ErrCodeSignedOut, LoginCommand)
	case usageLimit(status, low):
		return core.Fail(core.ErrCodeUsageLimit, "")
	case strings.TrimSpace(msg) == "":
		return core.Fail(core.ErrCodeAskFailed, fmt.Sprintf("agy ended with exit code %d and no answer", exitCode))
	}
	return core.Fail(core.ErrCodeAskFailed, clip(msg, 300))
}

// afterAsk forgets the kept sign-in state when an ask failed for the
// sign-in or the usage limit, and gives a usage limit the time it ends.
func (a *Adapter) afterAsk(ctx context.Context, err error) error {
	switch core.ErrorCode(err) {
	case core.ErrCodeSignedOut:
		a.forgetLogin()
	case core.ErrCodeUsageLimit:
		a.forgetLogin()
		return core.Fail(core.ErrCodeUsageLimit, a.resetAt(ctx))
	}
	return err
}
