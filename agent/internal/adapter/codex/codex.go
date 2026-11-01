// Package codex is the Codex CLI adapter. Phase 2 uses it for asks and
// comparison judging: `codex exec` in the ask folder, read-only, with the
// person's config.toml left out (P0-2: its approvals_reviewer =
// "auto_review" approved a write under read-only) and live web search on
// (P0-7); a judge (judge.go) runs in the judge folder with web search off
// and keeps no session file. Both turn off the features in featuresOff
// (features.go): even with the person's config left out, an exec saw their
// ChatGPT app tools such as Gmail, plugins and hooks (P2-3 S2 decision 4).
// It is the only package that runs codex or knows its formats (principle
// 2), and it never reads Codex login files (N10). Session listing comes in
// Phase 3.
package codex

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/asksemble/asksemble/agent/internal/config"
	"github.com/asksemble/asksemble/agent/internal/core"
	"github.com/asksemble/asksemble/agent/internal/proc"
)

// Name is the agent name used in session ids ("codex:<thread id>").
const Name = "codex"

// Notice codes. The app turns them into text (N9).
const (
	NoticeNotFound      = "codex_not_found"
	NoticeVersionFailed = "codex_version_failed"
	NoticeScriptOnly    = "codex_script_only" // only an npm codex.cmd was found
)

const versionTTL = 5 * time.Minute

// defaultTiers are the 2026-10-29 tier models (phase2-plan 4). Luna has no
// 6.1, so quick uses gpt-6-luna.
var defaultTiers = map[string]core.AskModel{
	core.TierQuick:    {Model: "gpt-6-luna", Effort: "medium"},
	core.TierStandard: {Model: "gpt-6.1-terra", Effort: "medium"},
	core.TierDeep:     {Model: "gpt-6.1-sol", Effort: "medium"},
}

var efforts = map[string]bool{"minimal": true, "low": true, "medium": true, "high": true, "xhigh": true}

var threadRe = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

// Options configures the adapter.
type Options struct {
	Path  string                   // empty: codex.exe on PATH
	Tiers map[string]core.AskModel // settings ask.tiers.codex
	// SchemaDir holds the judge schema file judge-schema-v1.json, which
	// codex reads for --output-schema. Empty: the agent folder,
	// config.Dir() (%APPDATA%\Asksemble). Tests give a temporary folder.
	SchemaDir string
}

// Adapter implements core.Adapter, core.Asker and core.Judger for Codex CLI.
type Adapter struct {
	exe    string
	exeErr error
	tiers  map[string]core.AskModel

	mu      sync.Mutex
	version string
	verErr  error
	verAt   time.Time

	// The judge schema file (judge.go): schemaDir is absolute, or "" when
	// there is no agent folder. schemaMu guards schemaReady, set once this
	// process wrote the file or found it right so later judges only read
	// and compare, and schemaWrites, the writes (for tests).
	schemaDir    string
	schemaMu     sync.Mutex
	schemaReady  bool
	schemaWrites int
}

// New finds codex. A missing codex is not an error: /v1/info reports it.
func New(o Options) *Adapter {
	a := &Adapter{tiers: o.Tiers, schemaDir: o.SchemaDir}
	a.exe, a.exeErr = findExe(o.Path)
	if a.schemaDir == "" {
		a.schemaDir, _ = config.Dir() // none: a judge fails as internal
	}
	if a.schemaDir != "" {
		if abs, err := filepath.Abs(a.schemaDir); err == nil {
			a.schemaDir = abs
		}
	}
	return a
}

// findExe returns the real codex.exe: the setting, codex.exe on PATH, or
// the exe an npm codex.cmd starts. A .cmd itself is never run (P0-7).
func findExe(override string) (string, error) {
	if override != "" {
		return override, proc.CheckExe(override)
	}
	exe, shim, err := proc.LookExe("codex")
	if err == nil {
		return exe, nil
	}
	if shim != "" {
		pattern := filepath.Join(filepath.Dir(shim), "node_modules", "@openai", "codex", "vendor", "*", "codex", "codex.exe")
		if m, _ := filepath.Glob(pattern); len(m) > 0 {
			// A package may carry both Windows targets; take this PC's.
			p := m[0]
			for _, c := range m {
				if strings.HasPrefix(filepath.Base(filepath.Dir(filepath.Dir(c))), vendorArch()) {
					p = c
				}
			}
			return p, proc.CheckExe(p)
		}
	}
	return "", err
}

// vendorArch is how the npm package names this CPU in its vendor folders.
func vendorArch() string {
	if runtime.GOARCH == "arm64" {
		return "aarch64"
	}
	return "x86_64"
}

// Agent returns "codex".
func (a *Adapter) Agent() string { return Name }

// Info reports the version; asking is the only capability for now.
func (a *Adapter) Info(ctx context.Context) core.AdapterInfo {
	info := core.AdapterInfo{Agent: Name, Capabilities: []core.Capability{}}
	switch {
	case errors.Is(a.exeErr, proc.ErrScript):
		info.Notices = append(info.Notices, NoticeScriptOnly)
	case a.exeErr != nil:
		info.Notices = append(info.Notices, NoticeNotFound)
	default:
		if v, err := a.cachedVersion(ctx); err != nil {
			info.Notices = append(info.Notices, NoticeVersionFailed)
		} else {
			info.Version, info.Available = v, true
			info.Capabilities = append(info.Capabilities, core.CapAsk)
		}
	}
	return info
}

var versionRe = regexp.MustCompile(`\d+\.\d+\.\d+`)

func (a *Adapter) cachedVersion(ctx context.Context) (string, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if !a.verAt.IsZero() && time.Since(a.verAt) < versionTTL {
		return a.version, a.verErr
	}
	// The caller may be a phone request that goes away; its cancel must not
	// be cached as a broken AI for versionTTL.
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 15*time.Second)
	defer cancel()
	a.version, a.verErr = "", nil
	res, err := proc.Run(ctx, proc.Cmd{Exe: a.exe, Dir: filepath.Dir(a.exe), Args: []string{"--version"}})
	switch v := versionRe.Find(res.Stdout); {
	case err != nil:
		a.verErr = err
	case res.ExitCode != 0 || v == nil:
		a.verErr = errors.New("codex --version: no version number")
	default:
		a.version = string(v)
	}
	a.verAt = time.Now()
	return a.version, a.verErr
}

// ListSessions lists nothing yet: Codex sessions come in Phase 3.
func (a *Adapter) ListSessions(context.Context) (core.ListResult, error) {
	return core.ListResult{Sessions: []core.Session{}}, nil
}

// AskTiers returns the tier models, with the settings' overrides.
func (a *Adapter) AskTiers() map[string]core.AskModel {
	out := make(map[string]core.AskModel, len(defaultTiers))
	for t, m := range defaultTiers {
		if o, ok := a.tiers[t]; ok {
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

// askArgs builds one ask. --search is a top-level option and goes before
// exec, and turns on the native web search; -s goes after exec and before
// resume, which does not take it, so a follow-up also sets sandbox_mode
// after resume (P2-1 probe). The featuresOff features are turned off
// after the -c options (disableArgs; P2-3 S2 decision 4: app tools such as
// Gmail were visible to an exec with the person's config left out), on a
// new question and on resume alike, which takes --disable among its
// options before the thread id. The thread id stays second to last and
// "-", which reads the prompt from stdin, last:
//
//	--search exec -s read-only --ignore-user-config --skip-git-repo-check --json -m M [-c effort] --disable f… -
//	--search exec -s read-only resume --ignore-user-config --skip-git-repo-check --json -m M [-c effort] -c sandbox_mode="read-only" --disable f… <thread> -
func askArgs(model, effort, resume string) []string {
	args := []string{"--search", "exec", "-s", "read-only"}
	if resume != "" {
		args = append(args, "resume")
	}
	args = append(args, "--ignore-user-config", "--skip-git-repo-check", "--json", "-m", model)
	if effort != "" {
		args = append(args, "-c", `model_reasoning_effort="`+effort+`"`)
	}
	if resume != "" {
		args = append(args, "-c", `sandbox_mode="read-only"`)
	}
	args = append(args, disableArgs()...)
	if resume != "" {
		args = append(args, resume)
	}
	return append(args, "-")
}

// checkArgs accepts exactly what askArgs makes from valid values, so no
// other option (a sandbox or approval change, a profile, a config key, a
// feature left on or turned on with --enable, the judge's
// web_search="disabled" or --ephemeral) reaches codex through an ask. It
// reads the model, the effort and the thread (second to last after
// resume) and compares the whole list with askArgs of those values, so
// every flag, its place and the order of the disabled features must match.
func checkArgs(args []string) error {
	model, effort, resume := "", "", ""
	for i := 0; i+1 < len(args); i++ {
		switch {
		case args[i] == "-m":
			model = args[i+1]
		case args[i] == "-c" && strings.HasPrefix(args[i+1], "model_reasoning_effort="):
			effort = strings.Trim(strings.TrimPrefix(args[i+1], "model_reasoning_effort="), `"`)
		case args[i] == "resume" && i+1 < len(args):
			resume = args[len(args)-2]
		}
	}
	switch {
	case !config.ValidModel(model):
		return fmt.Errorf("codex ask: model %q refused", model)
	case effort != "" && !efforts[effort]:
		return fmt.Errorf("codex ask: effort %q refused", effort)
	case resume != "" && !threadRe.MatchString(resume):
		return fmt.Errorf("codex ask: thread %q refused", resume)
	case !slices.Equal(args, askArgs(model, effort, resume)):
		return fmt.Errorf("codex ask: arguments %q refused", args)
	}
	return nil
}

// Ask answers one question with codex exec (core.Asker).
func (a *Adapter) Ask(ctx context.Context, req core.AskRequest) (core.AskResult, error) {
	if a.exeErr != nil {
		return core.AskResult{}, core.Fail(core.ErrCodeAgentUnavailable, "codex was not found")
	}
	tier, ok := a.AskTiers()[req.Tier]
	if !ok {
		tier = a.AskTiers()[core.TierStandard]
	}
	model, effort := req.Model, req.Effort
	if model == "" {
		model = tier.Model
	}
	if effort == "" {
		effort = tier.Effort
	}
	switch {
	case !config.ValidModel(model):
		return core.AskResult{}, core.Fail(core.ErrCodeBadModel, model)
	case effort != "" && !efforts[effort]:
		return core.AskResult{}, core.Fail(core.ErrCodeBadModel, "effort "+effort)
	case req.Resume != "" && !threadRe.MatchString(req.Resume):
		return core.AskResult{}, core.Fail(core.ErrCodeNotFound, "")
	}
	args := askArgs(model, effort, req.Resume)
	if err := checkArgs(args); err != nil {
		return core.AskResult{}, err
	}
	res, err := proc.Run(ctx, proc.Cmd{Exe: a.exe, Dir: req.Folder, Args: args, Stdin: req.Prompt})
	switch {
	case errors.Is(err, proc.ErrTimeout):
		return core.AskResult{}, core.Fail(core.ErrCodeAskTimeout, "")
	case errors.Is(err, fs.ErrNotExist), errors.Is(err, proc.ErrScript):
		return core.AskResult{}, core.Fail(core.ErrCodeAgentUnavailable, "codex was not found")
	case err != nil:
		return core.AskResult{}, core.Fail(core.ErrCodeAskFailed, err.Error())
	}
	return parseOutput(res, model, req.Resume)
}

// event is one JSONL line of `codex exec --json`.
type event struct {
	Type     string `json:"type"`
	ThreadID string `json:"thread_id"`
	Item     *struct {
		Type string `json:"type"`
		Text string `json:"text"`
	} `json:"item"`
	Usage *struct {
		InputTokens  int `json:"input_tokens"`
		OutputTokens int `json:"output_tokens"`
	} `json:"usage"`
	Message string `json:"message"`
	Error   *struct {
		Message string `json:"message"`
	} `json:"error"`
}

func parseOutput(res proc.Result, model, resume string) (core.AskResult, error) {
	r := core.AskResult{Model: model}
	completed, failed := false, false
	errMsg := ""
	for _, line := range bytes.Split(res.Stdout, []byte("\n")) {
		line = bytes.TrimSpace(line)
		var e event
		if len(line) == 0 || line[0] != '{' || json.Unmarshal(line, &e) != nil {
			continue
		}
		switch e.Type {
		case "thread.started":
			if threadRe.MatchString(e.ThreadID) {
				r.SessionID = e.ThreadID
			}
		case "item.completed":
			// Earlier agent messages are lead-ins ("…확인하겠습니다.");
			// an item of type "error" is only a warning.
			if e.Item != nil && e.Item.Type == "agent_message" {
				r.Answer = e.Item.Text
			}
		case "turn.completed":
			completed = true
			if e.Usage != nil {
				r.InputTokens, r.OutputTokens = e.Usage.InputTokens, e.Usage.OutputTokens
			}
		case "turn.failed":
			failed = true
			if e.Error != nil {
				errMsg = e.Error.Message
			}
		case "error":
			if errMsg == "" {
				errMsg = e.Message
			}
		}
	}
	stderr := strings.TrimSpace(string(res.Stderr))
	switch {
	case completed && !failed && res.ExitCode == 0:
		return r, nil
	case resume != "" && strings.Contains(stderr, "no rollout found"):
		return core.AskResult{}, core.Fail(core.ErrCodeNotFound, "")
	}
	code, msg := core.ErrCodeAskFailed, errMsg
	if inner, c := apiError(errMsg); inner != "" {
		msg = inner
		if c == "model_not_found" {
			code = core.ErrCodeBadModel
		}
	}
	if msg == "" {
		msg = lastLines(stderr)
	}
	if msg == "" {
		msg = fmt.Sprintf("codex ended with exit code %d and no answer", res.ExitCode)
	}
	r.Answer = ""
	return r, core.Fail(code, clip(msg, 300))
}

// apiError reads the API error JSON that codex puts inside its message.
func apiError(s string) (msg, code string) {
	var v struct {
		Error struct {
			Message string `json:"message"`
			Code    string `json:"code"`
		} `json:"error"`
	}
	if json.Unmarshal([]byte(s), &v) != nil {
		return "", ""
	}
	return v.Error.Message, v.Error.Code
}

// lastLines keeps the end of stderr, without codex's timestamped log lines.
func lastLines(s string) string {
	var keep []string
	for _, l := range strings.Split(s, "\n") {
		l = strings.TrimSpace(l)
		if l == "" || (len(l) > 4 && l[4] == '-' && strings.Contains(l, " ERROR ")) {
			continue
		}
		keep = append(keep, l)
	}
	if len(keep) > 3 {
		keep = keep[len(keep)-3:]
	}
	return strings.Join(keep, " ")
}

// clip collapses whitespace and keeps at most n runes.
func clip(s string, n int) string {
	s = strings.Join(strings.Fields(s), " ")
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	return string([]rune(s)[:n]) + "…"
}
