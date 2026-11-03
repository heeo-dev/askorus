package fakecli

import (
	"cmp"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"os"
	"slices"
	"strings"
	"time"
)

// The judge agent and title the OpenCode adapter uses (spec part 8).
const (
	JudgeAgent = "asksemble-judge"
	JudgeTitle = "asksemble-judge"
)

// ProjectID is the projectId of the sessions the fake saves.
const ProjectID = "fakeproject0000000000000000000000000000"

// opencodeMain plays opencode: run with --agent asksemble-judge is a
// judge, run with --agent asksemble-synth a combined-answer writer (and
// asksemble-refine a moderator refining a question), any
// other run an ask; `session list --format json --standalone`
// prints every saved session, whatever its folder, and `session delete
// <id> --standalone` removes one.
func opencodeMain(c *Call) int {
	a := c.Args
	switch {
	case len(a) > 0 && a[0] == "run" && flagValue(a, "--agent") == JudgeAgent:
		return opencodeJudge(c)
	case len(a) > 0 && a[0] == "run" && (flagValue(a, "--agent") == SynthAgent || flagValue(a, "--agent") == RefineAgent):
		return opencodeSynth(c)
	case len(a) > 0 && a[0] == "run":
		return opencodeAsk(c)
	case slices.Equal(a, []string{"session", "list", "--format", "json", "--standalone"}):
		return opencodeList()
	case len(a) == 4 && a[0] == "session" && a[1] == "delete" && a[3] == "--standalone":
		return opencodeDelete(a[2])
	}
	return unexpected(RoleOpenCode)
}

// failing reports whether ASKSEMBLE_FAKECLI_FAIL names what.
func failing(what string) bool {
	return slices.Contains(strings.Split(os.Getenv(EnvFail), ","), what)
}

const sessionChars = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789"

// newSessionID is an id like OpenCode's: ses_ and 26 letters and digits.
func newSessionID() string {
	b := make([]byte, 26)
	rand.Read(b)
	for i := range b {
		b[i] = sessionChars[int(b[i])%len(sessionChars)]
	}
	return "ses_" + string(b)
}

// saveSession keeps a new session with OpenCode's folder, PWD.
func saveSession(c *Call, id, title string) error {
	dir := c.Env["PWD"]
	if dir == "" {
		dir = c.Dir
	}
	now := time.Now().UnixMilli()
	return updateSessions(func(ss []Session) ([]Session, error) {
		if slices.ContainsFunc(ss, func(s Session) bool { return s.ID == id }) {
			return ss, nil
		}
		return append(ss, Session{ID: id, Title: title, Updated: now, Created: now, ProjectID: ProjectID, Directory: dir}), nil
	})
}

// opencodeEvent prints one JSON line of `opencode run --format json`.
func opencodeEvent(kind, session string, fields map[string]any) {
	ev := map[string]any{"type": kind, "timestamp": time.Now().UnixMilli(), "sessionID": session}
	for k, v := range fields {
		ev[k] = v
	}
	b, _ := json.Marshal(ev)
	fmt.Print(string(b) + "\r\n") // OpenCode ends its lines with \r\n on Windows
}

func opencodePart(kind string, fields map[string]any) map[string]any {
	part := map[string]any{"type": kind}
	for k, v := range fields {
		part[k] = v
	}
	return map[string]any{"part": part}
}

func opencodeError(session, kind, msg string) int {
	opencodeEvent("error", session, map[string]any{"error": map[string]string{"type": kind, "message": msg}})
	return 1
}

func opencodeStepFinish(session, reason string) {
	opencodeEvent("step_finish", session, opencodePart("step-finish", map[string]any{
		"reason": reason, "tokens": map[string]any{"input": 100, "output": 5, "cache": map[string]int{"read": 20, "write": 0}},
	}))
}

// opencodeJudge saves the conversation, as OpenCode does, and prints a
// step with the judge JSON in a ```json fence. Mode slow waits after the
// text, before the step ends, so a judge ended by its time limit has
// already named its session and printed the judge JSON, which the adapter
// must not use.
func opencodeJudge(c *Call) int {
	mode, err := judgeMode(RoleOpenCode)
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
	text := judgeText(mode, c.Stdin)
	if mode != ModeProse {
		text = "```json\n" + text + "\n```"
	}
	switch mode {
	case ModeUsageLimit:
		return opencodeError(id, "provider.rate-limit", UsageLimitOpenCode)
	case ModeBadModel:
		return opencodeError(id, "provider.no-route", "Model unavailable: "+flagValue(c.Args, "-m"))
	case ModeToolUse:
		opencodeEvent("tool_use", id, opencodePart("tool", map[string]any{
			"tool": "bash", "state": map[string]any{"status": "error", "input": map[string]string{"command": ToolCommand}, "error": "Permission denied: bash"},
		}))
		opencodeStepFinish(id, "tool-calls")
		opencodeEvent("step_start", id, opencodePart("step-start", nil))
	}
	opencodeEvent("text", id, opencodePart("text", map[string]any{"text": text}))
	if mode == ModeSlow {
		sleepSlow()
	}
	if mode == ModeFailedWithOutput {
		return opencodeError(id, "provider.overloaded", "Provider is overloaded")
	}
	opencodeStepFinish(id, "stop")
	return 0
}

// AskTitle is the title of the ask conversations the fake saves.
const AskTitle = "fake ask"

// opencodeAsk answers like the opencode fake of the adapter's own tests:
// a lead-in step with a web search, then "fake: " and the prompt's last
// line (or the answers file's text), in the session --session names or a
// new one. Ask mode fail is an error event after the session is saved;
// empty does the web search and says nothing.
func opencodeAsk(c *Call) int {
	mode, err := askMode(RoleOpenCode)
	if err != nil {
		fmt.Fprintln(os.Stderr, "fakecli:", err)
		return 2
	}
	text, err := askText(RoleOpenCode, "fake: ", c.Stdin)
	if err != nil {
		fmt.Fprintln(os.Stderr, "fakecli:", err)
		return 2
	}
	id := flagValue(c.Args, "--session")
	if id == "" {
		id = newSessionID()
	}
	if err := saveSession(c, id, AskTitle); err != nil {
		fmt.Fprintln(os.Stderr, "fakecli:", err)
		return 2
	}
	opencodeEvent("step_start", id, opencodePart("step-start", nil))
	if mode == AskFail {
		return opencodeError(id, "provider.rate-limit", UsageLimitOpenCode)
	}
	if mode != AskEmpty {
		opencodeEvent("text", id, opencodePart("text", map[string]any{"text": "검색해 볼게요."}))
	}
	opencodeEvent("tool_use", id, opencodePart("tool", map[string]any{"tool": "websearch", "state": map[string]string{"status": "completed"}}))
	opencodeStepFinish(id, "tool-calls")
	if mode == AskEmpty {
		return 0
	}
	opencodeEvent("step_start", id, opencodePart("step-start", nil))
	opencodeEvent("text", id, opencodePart("text", map[string]any{"text": text}))
	return 0 // the last step_finish is sometimes never printed; neither here
}

// opencodeList prints every saved session, newest first, whatever its
// folder: the adapter must pick its own.
func opencodeList() int {
	switch {
	case failing("list"):
		fmt.Fprintln(os.Stderr, "Error: database is locked")
		return 1
	case failing("listjunk"):
		fmt.Println("Database is locked")
		return 0
	}
	dir := os.Getenv(EnvState)
	var ss []Session
	err := withLock(dir, func() error {
		var err error
		ss, err = loadSessions(dir)
		return err
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, "fakecli:", err)
		return 2
	}
	slices.SortStableFunc(ss, func(a, b Session) int { return cmp.Compare(b.Updated, a.Updated) })
	b, _ := json.MarshalIndent(append([]Session{}, ss...), "", "  ")
	fmt.Println(string(b))
	return 0
}

// opencodeDelete removes one saved session.
func opencodeDelete(id string) int {
	if failing("delete") || failing("delete="+id) {
		fmt.Fprintln(os.Stderr, "Error: database is locked")
		return 1
	}
	found := false
	err := updateSessions(func(ss []Session) ([]Session, error) {
		n := len(ss)
		ss = slices.DeleteFunc(ss, func(s Session) bool { return s.ID == id })
		found = len(ss) < n
		return ss, nil
	})
	switch {
	case err != nil:
		fmt.Fprintln(os.Stderr, "fakecli:", err)
		return 2
	case !found:
		fmt.Fprintf(os.Stderr, "Error: Session not found: %s\n", id)
		return 1
	}
	fmt.Printf("Session %s deleted\n", id)
	return 0
}
