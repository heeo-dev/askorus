package claude

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
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

// askWebTools is the only value --tools and --allowedTools may take.
const askWebTools = "WebSearch,WebFetch"

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
func askArgs(model, effort, resume string, oneOff bool) []string {
	args := []string{"-p", "--output-format", "json", "--model", model}
	if effort != "" {
		args = append(args, "--effort", effort)
	}
	args = append(args, "--tools", askWebTools, "--allowedTools", askWebTools, "--strict-mcp-config")
	switch {
	case resume != "":
		args = append(args, "--resume", resume)
	case oneOff:
		args = append(args, "--no-session-persistence")
	}
	return args
}

// checkAskArgs accepts exactly the shape askArgs makes, so nothing else
// (a permission flag, another tool, an MCP config, a prompt argument) can
// reach claude through an ask.
func checkAskArgs(args []string) error {
	if len(args) == 0 || args[0] != "-p" {
		return fmt.Errorf("%w: an ask starts with -p", ErrArgRejected)
	}
	seen := map[string]bool{}
	for i := 1; i < len(args); i++ {
		a := args[i]
		if seen[a] {
			return fmt.Errorf("%w: %s twice", ErrArgRejected, a)
		}
		seen[a] = true
		if a == "--strict-mcp-config" || a == "--no-session-persistence" {
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
			ok = v == "json"
		case "--model":
			ok = config.ValidModel(v)
		case "--effort":
			ok = config.ValidEffort(v)
		case "--tools", "--allowedTools":
			ok = v == askWebTools
		case "--resume":
			ok = ValidUUID(v)
		}
		if !ok {
			return fmt.Errorf("%w: %s %q", ErrArgRejected, a, v)
		}
	}
	for _, need := range []string{"--model", "--tools", "--allowedTools", "--strict-mcp-config"} {
		if !seen[need] {
			return fmt.Errorf("%w: an ask needs %s", ErrArgRejected, need)
		}
	}
	if seen["--no-session-persistence"] && seen["--resume"] {
		return fmt.Errorf("%w: a one-off ask never resumes", ErrArgRejected)
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
	args := askArgs(model, effort, req.Resume, req.OneOff)
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
	res, err := proc.Run(ctx, proc.Cmd{Exe: a.runner.Path(), Dir: req.Folder, Args: args, Stdin: req.Prompt})
	switch {
	case errors.Is(err, proc.ErrTimeout):
		return core.AskResult{}, core.Fail(core.ErrCodeAskTimeout, "")
	case errors.Is(err, fs.ErrNotExist), errors.Is(err, proc.ErrScript):
		return core.AskResult{}, core.Fail(core.ErrCodeAgentUnavailable, "claude was not found")
	case err != nil:
		return core.AskResult{}, core.Fail(core.ErrCodeAskFailed, err.Error())
	}
	out, err := parseAskOutput(res, model)
	if req.OneOff {
		out.SessionID = "" // nothing was kept to carry on
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
		code := core.ErrCodeAskFailed
		if strings.Contains(stderr, "[claude-code:unrecognized_model]") || (out.APIErrorStatus != nil && *out.APIErrorStatus == 404) {
			code = core.ErrCodeBadModel
		}
		return r, core.Fail(code, clip(firstNonEmpty(out.Result, stderr), 300))
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
