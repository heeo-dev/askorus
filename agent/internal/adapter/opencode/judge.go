package opencode

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"time"

	"github.com/asksemble/asksemble/agent/internal/core"
	"github.com/asksemble/asksemble/agent/internal/proc"
)

// The one-off command of the judging days (P2-3, spec part 8): one
// `opencode run --standalone` with its own agent that may use no tool. It
// runs in the judge folder, <ask folder>\judge, where OpenCode keeps it as
// a conversation; CleanupJudge deletes it after the comparison. The judges
// went with C6d (#24); the combined answer and refining (synth.go) run
// this command, and this file keeps what they share: the company of a
// model, the event parser and the cleanup.

// JudgeMinVersion is the oldest OpenCode the one-off command was tested
// with (P2-3 version table). The gate compares Info's version with it.
const JudgeMinVersion = "2.2.1"

const (
	// judgeTitle is the title of every one-off conversation (a writer's, a
	// moderator's and the judging days' judges'); CleanupJudge deletes only
	// conversations with it.
	judgeTitle = "asksemble-judge"
	// judgeStale is how long a one-off conversation may stay unchanged
	// before CleanupJudge deletes it although no ref names it: one a crash
	// or a failed delete left behind. A running writer ends within
	// core.SynthTimeout.
	judgeStale = 10 * time.Minute
)

var (
	_ core.Judger       = (*Adapter)(nil)
	_ core.JudgeCleaner = (*Adapter)(nil)
)

// familyRules name the company of a model by its own name, the last part
// of provider/…/model without its #variant. Several providers serve the
// same models ("openrouter/anthropic/claude-x"), so the provider never
// counts.
var familyRules = []struct {
	family string
	match  func(name string) bool
}{
	{"anthropic", func(n string) bool { return strings.HasPrefix(n, "claude") }},
	{"openai", func(n string) bool {
		return strings.HasPrefix(n, "gpt-") || strings.HasPrefix(n, "codex") ||
			(len(n) > 1 && n[0] == 'o' && n[1] >= '0' && n[1] <= '9')
	}},
	{"google", func(n string) bool { return strings.HasPrefix(n, "gemini") }},
	{"deepseek", func(n string) bool { return strings.Contains(n, "deepseek") }},
	{"moonshot", func(n string) bool { return strings.Contains(n, "kimi") || strings.Contains(n, "moonshot") }},
	{"zhipu", func(n string) bool { return strings.Contains(n, "glm") || strings.Contains(n, "zhipu") }},
	{"alibaba", func(n string) bool { return strings.Contains(n, "qwen") }},
}

// Family names the company of a model OpenCode runs (core.Judger). It is
// "" for a model it does not know, and also for a name that fits two
// companies ("deepseek-r1-distill-qwen"): a writer of family "" does not
// write (fail closed).
func (a *Adapter) Family(model string) string {
	base, _, _ := strings.Cut(model, "#")
	name := strings.ToLower(base[strings.LastIndex(base, "/")+1:])
	if name == "" {
		return ""
	}
	found := ""
	for _, r := range familyRules {
		if r.match(name) {
			if found != "" && found != r.family {
				return ""
			}
			found = r.family
		}
	}
	return found
}

func listArgs() []string { return []string{"session", "list", "--format", "json", "--standalone"} }

func deleteArgs(id string) []string { return []string{"session", "delete", id, "--standalone"} }

// checkSessionArgs accepts exactly listArgs, or deleteArgs of one valid
// session id: never a delete of more, or of something that reads as a
// flag.
func checkSessionArgs(args []string) error {
	if slices.Equal(args, listArgs()) {
		return nil
	}
	if len(args) == 4 && sessionRe.MatchString(args[2]) && slices.Equal(args, deleteArgs(args[2])) {
		return nil
	}
	return fmt.Errorf("opencode session: arguments %q refused", args)
}

// judgeFolderOK reports whether folder is an absolute, existing folder. A
// one-off call never runs with an empty PWD, which would put its conversation in
// the agent's own folder, out of CleanupJudge's reach.
func judgeFolderOK(folder string) bool {
	if !filepath.IsAbs(folder) {
		return false
	}
	info, err := os.Stat(folder)
	return err == nil && info.IsDir()
}

// judgeNoAnswer is judge_failed's Detail when OpenCode said nothing usable.
const judgeNoAnswer = "opencode ended with an error and no message"

// parseJudgeOutput reads the events of one one-off run. Cleanup is the
// conversation's id on every path, so it is deleted however the run
// ended. A tool call throws the output away (spec part 10), even a
// denied one. The output is the text of the last step that wrote any,
// its parts joined by a line break; the ```json fence stays, and
// internal/judge's ExtractJSON finds the object.
func parseJudgeOutput(res proc.Result, model string) (core.JudgeResult, error) {
	r := core.JudgeResult{Model: model}
	var steps [][]string
	var errEvent *eventError
	usedTool := false
	for _, line := range bytes.Split(res.Stdout, []byte("\n")) {
		line = bytes.TrimSpace(line) // lines end with \r\n on Windows
		var e event
		if len(line) == 0 || line[0] != '{' || json.Unmarshal(line, &e) != nil {
			continue
		}
		if r.Cleanup == "" && sessionRe.MatchString(e.SessionID) {
			r.Cleanup = e.SessionID
		}
		switch e.Type {
		case "step_start":
			steps = append(steps, nil)
		case "text":
			if e.Part != nil && e.Part.Text != "" {
				if len(steps) == 0 {
					steps = append(steps, nil)
				}
				steps[len(steps)-1] = append(steps[len(steps)-1], e.Part.Text)
			}
		case "step_finish":
			if e.Part != nil && e.Part.Tokens != nil {
				tk := e.Part.Tokens
				r.InputTokens += tk.Input + tk.Cache.Read + tk.Cache.Write
				r.OutputTokens += tk.Output
			}
		case "tool_use":
			usedTool = true
		case "error":
			if e.Error != nil && errEvent == nil {
				errEvent = e.Error
			}
		}
	}
	if usedTool {
		return r, core.Fail(core.ErrCodeJudgeUsedTool, "")
	}
	if errEvent != nil || res.ExitCode != 0 {
		code, msg := core.ErrCodeJudgeFailed, ""
		if errEvent != nil {
			msg = errEvent.Message
			if errEvent.Type == "provider.no-route" || strings.HasPrefix(msg, "Model unavailable") {
				code = core.ErrCodeBadModel
			}
		}
		if strings.TrimSpace(msg) == "" {
			msg = string(res.Stderr)
		}
		// The message is the AI's own words, never the judging days' JSON.
		if strings.TrimSpace(msg) == "" || strings.Contains(msg, `"judgments"`) {
			msg = judgeNoAnswer
		}
		return r, core.Fail(code, clip(msg, core.JudgeMessageChars-1))
	}
	for i := len(steps) - 1; i >= 0; i-- {
		if len(steps[i]) > 0 {
			r.Output = json.RawMessage(strings.Join(steps[i], "\n"))
			return r, nil
		}
	}
	return r, core.Fail(core.ErrCodeJudgeBadOutput, "")
}

// sameFolder reports whether a and b are the same folder; a folder inside
// the other does not count. Windows paths compare without the case of A-Z
// only: any other letter must match exactly, so a lookalike name that
// Unicode case folding alone makes equal (KELVIN SIGN for k, LONG S for
// s) is another folder and its conversations are never deleted.
func sameFolder(a, b string) bool {
	if a == "" || b == "" {
		return false
	}
	a, b = filepath.Clean(a), filepath.Clean(b)
	if runtime.GOOS == "windows" {
		return asciiLower(a) == asciiLower(b)
	}
	return a == b
}

// asciiLower lowers A-Z and leaves every other rune as it is.
func asciiLower(s string) string {
	return strings.Map(func(r rune) rune {
		if 'A' <= r && r <= 'Z' {
			return r + 'a' - 'A'
		}
		return r
	}, s)
}

// Fixed texts of CleanupJudge's errors: never an id or a title.
const (
	cleanupNoList   = "opencode could not list its judge conversations"
	cleanupNoDelete = "opencode could not delete a judge conversation"
	cleanupMismatch = "a judge conversation is not in the judge folder or has another title"
)

// CleanupJudge deletes the one-off conversations refs name, and one-off
// conversations no ref names that stayed unchanged for over judgeStale,
// from folder (core.JudgeCleaner). It deletes only conversations that
// `session list` shows in folder itself, not a folder inside it, with the
// judge title, and runs OpenCode with a writer's environment (synthEnv).
// A ref the list does not show is passed over: OpenCode lists the 100
// newest only. One delete failing does not stop the others. Its errors are judge_failed with a fixed text (no id, no
// title), agent_unavailable or internal.
func (a *Adapter) CleanupJudge(ctx context.Context, folder string, refs []string) error {
	if a.exeErr != nil {
		return core.Fail(core.ErrCodeAgentUnavailable, "opencode was not found")
	}
	if !judgeFolderOK(folder) {
		return core.Fail(core.ErrCodeInternal, "the judge folder is not an absolute existing folder")
	}
	lctx, cancel := context.WithTimeout(ctx, core.JudgeCleanupTimeout)
	list, err := a.listSessions(lctx, folder, synthEnv(folder))
	cancel()
	if err != nil {
		return core.Fail(core.ErrCodeJudgeFailed, cleanupNoList)
	}
	want := map[string]bool{}
	for _, id := range refs {
		if sessionRe.MatchString(id) {
			want[id] = true
		}
	}
	now := time.Now()
	mismatch, failed := false, false
	var del []string
	for _, s := range list {
		if !sessionRe.MatchString(s.ID) || slices.Contains(del, s.ID) {
			continue
		}
		mine := sameFolder(s.Directory, folder) && s.Title == judgeTitle
		switch {
		case want[s.ID] && !mine:
			mismatch = true // a ref of a one-off call, though not where it ran or retitled: left alone
		case mine && (want[s.ID] || s.Updated.stale(now)):
			del = append(del, s.ID)
		}
	}
	for _, id := range del {
		if ctx.Err() != nil {
			failed = true
			break
		}
		args := deleteArgs(id)
		if checkSessionArgs(args) != nil {
			failed = true
			continue
		}
		dctx, cancel := context.WithTimeout(ctx, core.JudgeCleanupTimeout)
		res, err := proc.Run(dctx, proc.Cmd{Exe: a.exe, Dir: folder, Args: args, Env: synthEnv(folder)})
		cancel()
		if err != nil || res.ExitCode != 0 {
			failed = true
		}
	}
	switch {
	case mismatch:
		return core.Fail(core.ErrCodeJudgeFailed, cleanupMismatch)
	case failed:
		return core.Fail(core.ErrCodeJudgeFailed, cleanupNoDelete)
	}
	return nil
}
