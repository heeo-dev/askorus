// Package opencode is the OpenCode adapter. Phase 2 uses it for asks,
// `opencode run --standalone` with a one-run agent that may only search
// and read the web (P0-2, P0-7), and for combined answers (P2-3, synth.go
// and judge.go), with a one-run agent that may use no tool. It is the only
// package that runs opencode or knows its formats (principle 2), and it
// never reads OpenCode login files (N10); whether it is signed in comes
// from `opencode models` (login.go, #60). Session listing comes in Phase 3.
package opencode

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/heeo-dev/askorus/agent/internal/core"
	"github.com/heeo-dev/askorus/agent/internal/proc"
)

// Name is the agent name used in session ids ("opencode:ses_…").
const Name = "opencode"

// Notice codes. The app turns them into text (N9).
const (
	NoticeNotFound      = "opencode_not_found"
	NoticeVersionFailed = "opencode_version_failed"
	NoticeScriptOnly    = "opencode_script_only" // only an npm opencode.cmd and no exe next to it
)

const versionTTL = 5 * time.Minute

// askAgent is the agent this adapter hands OpenCode for one run only, in
// OPENCODE_CONFIG_CONTENT; the person's own config files are not touched.
// Everything is denied but the two web tools: with only edit and bash
// denied, a subagent wrote a file (P0-7). OpenCode still lists its skill
// tool, which only reads skill text and falls under "*" (P2-1 probe).
const askAgent = "askorus-ask"

var askConfig = func() string {
	b, _ := json.Marshal(map[string]any{"agent": map[string]any{askAgent: map[string]any{
		"description": "Askorus ask: answer the question only",
		"mode":        "primary",
		"prompt": "You answer one general question sent by the Askorus app to several AIs at once. " +
			"It is not a coding task. Answer only the question; do not add next-step suggestions or extra sections.",
		"permission": map[string]string{"*": "deny", "webfetch": "allow", "websearch": "allow"},
	}}})
	return string(b)
}()

// defaultTiers are the tier models (phase2-plan 4: 2026-10-29, deep
// 2026-11-04). The effort is OpenCode's variant ("model#variant"). Deep was
// Kimi K3.1 Pro until the person found it used too much (11-04); DeepSeek
// V4 Pro, the latest Pro (v4.1 is Flash only), has no low or medium, so
// high, as for Kimi. 11-06 (the person: standard's max was too slow, two
// 3-minute timeouts that day): quick Flash low, standard Flash high.
var defaultTiers = map[string]core.AskModel{
	core.TierQuick:    {Model: "opencode-go/deepseek-v4.1-flash", Effort: "low"},
	core.TierStandard: {Model: "opencode-go/deepseek-v4.1-flash", Effort: "high"},
	core.TierDeep:     {Model: "opencode-go/deepseek-v4-pro", Effort: "high"},
}

// modelEfforts are the variants each known model takes (11-06, short
// questions: Flash answered with low, high and max and refused medium and
// xhigh, "Variant unavailable"; Pro takes high and max only, 11-04).
var modelEfforts = map[string][]string{
	"opencode-go/deepseek-v4.1-flash": {"low", "high", "max"},
	"opencode-go/deepseek-v4-pro":     {"high", "max"},
}

// AskEfforts lists the variants each tier model may take
// (core.EffortLister). A model named with its variant in agent.json is
// looked up without it.
func (a *Adapter) AskEfforts() map[string][]string {
	return core.TierEfforts(a.AskTiers(), func(m string) []string {
		base, _, _ := strings.Cut(m, "#")
		return modelEfforts[base]
	})
}

var (
	// provider/model, optionally "#variant"; never starts with "-".
	modelRe   = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*(/[A-Za-z0-9][A-Za-z0-9._:-]*)+(#[a-z]{1,16})?$`)
	variantRe = regexp.MustCompile(`^[a-z]{1,16}$`)
	sessionRe = regexp.MustCompile(`^ses_[A-Za-z0-9]{20,40}$`)
)

// Options configures the adapter.
type Options struct {
	Path  string                   // empty: opencode.exe on PATH, or the exe an npm opencode.cmd starts
	Tiers map[string]core.AskModel // settings ask.tiers.opencode
	// Follow lets the tier models follow the newest model of their lines
	// (models.go, #64), as codex.Options.Follow.
	Follow bool
}

// Adapter implements core.Adapter and core.Asker for OpenCode.
type Adapter struct {
	exe    string
	exeErr error
	tiers  map[string]core.AskModel

	mu      sync.Mutex
	version string
	verErr  error
	verAt   time.Time

	// The sign-in state (#60, login.go). loginDone is set while a check
	// runs; loginCheck is nil outside tests.
	loginMu    sync.Mutex
	login      loginState
	loginAt    time.Time
	loginDone  chan struct{}
	loginCheck func(context.Context) loginState

	// usage is what is left of the OpenCode Go plan (#20, usage.go), read
	// in the background and kept usageTTL.
	usage *core.UsageCache

	// follow keeps opencode's model list (models.go, #64).
	follow *core.Follower
}

// New finds opencode. A missing opencode is not an error: /v1/info says so.
func New(o Options) *Adapter {
	a := &Adapter{tiers: o.Tiers}
	a.exe, a.exeErr = findExe(o.Path)
	a.usage = &core.UsageCache{TTL: usageTTL, Read: a.readUsage}
	a.follow = &core.Follower{Lines: lines}
	if o.Follow {
		a.follow.List = a.listModels
	}
	return a
}

// npmExes are where npm installs keep the real opencode.exe, next to the
// opencode.cmd that starts it. The .cmd is never run: cmd.exe drops what
// follows a line break and may read the question as commands (P0-7).
var npmExes = []string{
	filepath.Join("node_modules", "@opencode", "cli", "bin", "opencode.exe"),
	filepath.Join("node_modules", "opencode-ai", "node_modules", "opencode-windows-x64", "bin", "opencode.exe"),
	filepath.Join("node_modules", "opencode-windows-x64", "bin", "opencode.exe"),
}

func findExe(override string) (string, error) {
	if override != "" {
		return override, proc.CheckExe(override)
	}
	exe, shim, err := proc.LookExe("opencode")
	if err == nil {
		return exe, nil
	}
	if shim != "" {
		for _, rel := range npmExes {
			p := filepath.Join(filepath.Dir(shim), rel)
			if proc.CheckExe(p) == nil {
				return p, nil
			}
		}
	}
	return "", err
}

// Agent returns "opencode".
func (a *Adapter) Agent() string { return Name }

// Info reports the version and whether the providers of the tier models
// are signed in (login.go); asking is the only capability for now.
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
			a.follow.Check(v)
			a.loginNotices(ctx, &info)
			info.Usage = a.usage.Get()
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
		a.verErr = errors.New("opencode --version: no version number")
	default:
		a.version = string(v)
	}
	a.verAt = time.Now()
	return a.version, a.verErr
}

// ListSessions lists nothing yet: OpenCode sessions come in Phase 3.
func (a *Adapter) ListSessions(context.Context) (core.ListResult, error) {
	return core.ListResult{Sessions: []core.Session{}}, nil
}

// AskTiers returns the tier models, with the settings' overrides: a model
// of the settings stays as written, any other follows its line (#64).
func (a *Adapter) AskTiers() map[string]core.AskModel {
	out := make(map[string]core.AskModel, len(defaultTiers))
	for t, m := range defaultTiers {
		own := false
		if o, ok := a.tiers[t]; ok {
			if o.Model != "" {
				m.Model, m.Effort, own = o.Model, "", true // a variant belongs to its model
			}
			if o.Effort != "" {
				m.Effort = o.Effort
			}
		}
		if !own {
			m.Model = a.follow.Newest(m.Model, m.Effort)
		}
		out[t] = m
	}
	return out
}

// modelArg joins model and variant: a variant named in the model stays
// unless an effort replaces it.
func modelArg(model, effort string) string {
	if effort == "" {
		return model
	}
	base, _, _ := strings.Cut(model, "#")
	return base + "#" + effort
}

func askArgs(model, session string) []string { return askImageArgs(model, session, nil) }

// askImageArgs is askArgs for a question with photos (#14): "--file" and
// the path of each kept photo (core.CheckImage; the agent's own file) at
// the end. opencode attaches the files itself; the ask agent's denied
// tools stay denied.
func askImageArgs(model, session string, images []string) []string {
	args := []string{"run", "--standalone", "--format", "json", "--agent", askAgent, "-m", model}
	if session != "" {
		args = append(args, "--session", session)
	}
	for _, p := range images {
		args = append(args, "--file", p)
	}
	return args
}

// checkArgs accepts exactly what askArgs or askImageArgs makes from valid
// values. The prompt goes on stdin, so no argument carries the question.
func checkArgs(args []string) error {
	model, session := "", ""
	var images []string
	for i := 0; i+1 < len(args); i++ {
		switch args[i] {
		case "-m":
			model = args[i+1]
		case "--session":
			session = args[i+1]
		case "--file":
			images = append(images, args[i+1])
		}
	}
	switch {
	case !modelRe.MatchString(model):
		return fmt.Errorf("opencode ask: model %q refused", model)
	case session != "" && !sessionRe.MatchString(session):
		return fmt.Errorf("opencode ask: session %q refused", session)
	case checkImagePaths(images) != nil:
		return fmt.Errorf("opencode ask: an image path was refused")
	case !slices.Equal(args, askImageArgs(model, session, images)):
		return fmt.Errorf("opencode ask: arguments %q refused", args)
	}
	return nil
}

// checkImagePaths accepts the paths of kept photos only (core.CheckImages),
// each with the type its ending says.
func checkImagePaths(paths []string) error {
	ims := make([]core.Image, len(paths))
	for i, p := range paths {
		for typ, ext := range core.ImageExt {
			if strings.HasSuffix(p, ext) {
				ims[i] = core.Image{Path: p, Type: typ}
			}
		}
		if ims[i].Path == "" {
			return fmt.Errorf("not a kept photo")
		}
	}
	return core.CheckImages(ims)
}

// imagePaths checks a request's photos and returns their paths.
func imagePaths(images []core.Image) ([]string, error) {
	if err := core.CheckImages(images); err != nil {
		return nil, err
	}
	paths := make([]string, len(images))
	for i, im := range images {
		paths[i] = im.Path
	}
	return paths, nil
}

// imageModels are the models known to see photos (2026-11-18 probe, #14):
// DeepSeek V4.1 Flash read the test photos; DeepSeek V4 Pro answered that
// there was no image, without an error. OpenCode gives no error for a
// model that cannot see, so a model not listed here takes no photos.
var imageModels = []string{"opencode-go/deepseek-v4.1-flash"}

// SeesImages reports whether a run of model (with or without "#variant")
// takes photos (core.ImageSeer): model is one of imageModels, or a newer
// model of the line of one, which then runs that one (photoModel, #64).
func (a *Adapter) SeesImages(model string) bool { return a.photoModel(model) != "" }

// ImageModels lists them for the app: imageModels and the tier models
// that run one of them for photos.
func (a *Adapter) ImageModels() core.ImageInfo {
	ms := slices.Clone(imageModels)
	for _, t := range core.Tiers {
		if m := a.AskTiers()[t].Model; !slices.Contains(ms, m) && a.SeesImages(m) {
			ms = append(ms, m)
		}
	}
	return core.ImageInfo{Models: ms}
}

// env is what every run gets. OpenCode takes its folder from PWD, not
// from the process folder (P0-2).
func env(folder string) map[string]string {
	return map[string]string{"PWD": folder, "OPENCODE_CONFIG_CONTENT": askConfig}
}

// Ask answers one question with opencode run (core.Asker).
func (a *Adapter) Ask(ctx context.Context, req core.AskRequest) (core.AskResult, error) {
	if a.exeErr != nil {
		return core.AskResult{}, core.Fail(core.ErrCodeAgentUnavailable, "opencode was not found")
	}
	tier, ok := a.AskTiers()[req.Tier]
	if !ok {
		tier = a.AskTiers()[core.TierStandard]
	}
	model, effort := tier.Model, tier.Effort
	if req.Model != "" {
		model, effort = a.Follow(req.Model), ""
	}
	if req.Effort != "" {
		effort = req.Effort
	}
	if err := askCheck(model, effort); err != nil {
		return core.AskResult{}, err // the agent's own check: no model is left out for it
	}
	out, err := a.ask(ctx, req, model, effort)
	if base, variant := splitVariant(model); refused(err) && a.follow.Refuse(base) {
		// A newer model, or its variant, that OpenCode did not take: once
		// more with the shipped one (a variant named in the model kept), and
		// the newer one is left out until the next list (#64).
		out, err = a.ask(ctx, req, a.follow.Base(base)+variant, effort)
	}
	return out, err
}

// ask runs one ask with model (without its variant, or with one when
// effort is "") and effort.
func (a *Adapter) ask(ctx context.Context, req core.AskRequest, model, effort string) (core.AskResult, error) {
	switch {
	case effort != "" && !variantRe.MatchString(effort):
		return core.AskResult{}, core.Fail(core.ErrCodeBadModel, "variant "+effort)
	case !modelRe.MatchString(modelArg(model, effort)):
		return core.AskResult{}, core.Fail(core.ErrCodeBadModel, model)
	case req.Resume != "" && !sessionRe.MatchString(req.Resume):
		return core.AskResult{}, core.Fail(core.ErrCodeNotFound, "")
	}
	model = modelArg(model, effort)
	if req.Resume != "" {
		// --session with an unknown id quietly starts an empty session
		// under that id, losing the conversation; and only asks may be
		// carried on here. So the session must be one of the ask folder's.
		if err := a.checkSession(ctx, req.Folder, req.Resume); err != nil {
			return core.AskResult{}, err
		}
	}
	if req.OneOff && req.Resume != "" {
		return core.AskResult{}, core.Fail(core.ErrCodeInternal, "a one-off ask never resumes")
	}
	images, err := imagePaths(req.Images)
	if err != nil {
		return core.AskResult{}, core.Fail(core.ErrCodeInternal, "opencode ask: a photo was refused")
	}
	if len(images) > 0 {
		pm := a.photoModel(model)
		if pm == "" {
			// OpenCode would answer without seeing them, and say so to nobody.
			return core.AskResult{}, core.Fail(core.ErrCodeNoImages, model)
		}
		model = pm
	}
	args := askImageArgs(model, req.Resume, images)
	if err := checkArgs(args); err != nil {
		return core.AskResult{}, err
	}
	res, err := proc.Run(ctx, proc.Cmd{Exe: a.exe, Dir: req.Folder, Args: args, Env: env(req.Folder), Stdin: req.Prompt})
	if err := runError(err); err != nil {
		// A new conversation cut off (a timeout, a failure) is deleted: left
		// half done, OpenCode's own service later tries to carry it on and
		// fails without the ask agent (11-06, "Agent not found"). A carried
		// on one keeps its earlier turns.
		if req.Resume == "" {
			a.dropSession(ctx, req.Folder, sessionOf(res.Stdout))
		}
		return core.AskResult{}, err
	}
	out, perr := parseOutput(res, model)
	perr = a.signInError(ctx, perr, model)
	if req.OneOff || (perr != nil && req.Resume == "") {
		// A debate turn keeps no conversation (11-06); a new one that
		// failed (an error event, no answer) is cut off like a timeout.
		a.dropSession(ctx, req.Folder, firstNonEmpty(out.SessionID, sessionOf(res.Stdout)))
		out.SessionID = ""
	}
	return out, perr
}

// sessionOf is the session id the first event that names one gives, ""
// when none does (the output may be cut off).
func sessionOf(stdout []byte) string {
	for _, line := range bytes.Split(stdout, []byte("\n")) {
		var e struct {
			SessionID string `json:"sessionID"`
		}
		if json.Unmarshal(bytes.TrimSpace(line), &e) == nil && sessionRe.MatchString(e.SessionID) {
			return e.SessionID
		}
	}
	return ""
}

// dropSession deletes one conversation this adapter's run made in folder,
// best effort and bounded, also after ctx ended (a timed-out ask).
func (a *Adapter) dropSession(ctx context.Context, folder, id string) {
	if id == "" {
		return
	}
	args := deleteArgs(id)
	if checkSessionArgs(args) != nil {
		return
	}
	dctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), core.JudgeCleanupTimeout)
	defer cancel()
	_, _ = proc.Run(dctx, proc.Cmd{Exe: a.exe, Dir: folder, Args: args, Env: env(folder)})
}

func firstNonEmpty(xs ...string) string {
	for _, x := range xs {
		if x != "" {
			return x
		}
	}
	return ""
}

func runError(err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, proc.ErrTimeout):
		return core.Fail(core.ErrCodeAskTimeout, "")
	case errors.Is(err, fs.ErrNotExist), errors.Is(err, proc.ErrScript):
		return core.Fail(core.ErrCodeAgentUnavailable, "opencode was not found")
	}
	return core.Fail(core.ErrCodeAskFailed, err.Error())
}

// checkSession looks for id among the ask folder's sessions (read only,
// no AI call, about 2-3 s). A one-off conversation, in the judge folder
// inside the ask folder, is never carried on as an ask.
func (a *Adapter) checkSession(ctx context.Context, folder, id string) error {
	list, err := a.listSessions(ctx, folder, env(folder))
	if errors.Is(err, errNoList) {
		return core.Fail(core.ErrCodeAskFailed, "opencode could not list its sessions")
	}
	if err := runError(err); err != nil {
		return err
	}
	judgeFolder := core.JudgeFolder(folder)
	for _, s := range list {
		if s.ID == id && core.InFolder(s.Directory, folder) && !core.InFolder(s.Directory, judgeFolder) {
			return nil
		}
	}
	return core.Fail(core.ErrCodeNotFound, "")
}

// sessionInfo is one item of `opencode session list --format json`.
type sessionInfo struct {
	ID        string `json:"id"`
	Title     string `json:"title"`
	Directory string `json:"directory"`
	Updated   millis `json:"updated"`
}

// millis is a time in Unix milliseconds as session list prints it.
// Anything but a number reads as 0, unknown, so a change of format does
// not stop follow-up asks.
type millis int64

func (m *millis) UnmarshalJSON(b []byte) error {
	var f float64
	if json.Unmarshal(b, &f) != nil {
		f = 0
	}
	*m = millis(f)
	return nil
}

// stale reports whether m is known and over judgeStale before now.
func (m millis) stale(now time.Time) bool {
	return m > 0 && now.Sub(time.UnixMilli(int64(m))) > judgeStale
}

// errNoList is listSessions' error when opencode ran but printed no list.
var errNoList = errors.New("opencode printed no session list")

// listSessions runs `session list` in folder with env and returns every
// session it prints, of any folder: the caller picks its own. Its error
// is errNoList or proc.Run's.
func (a *Adapter) listSessions(ctx context.Context, folder string, env map[string]string) ([]sessionInfo, error) {
	args := listArgs()
	if err := checkSessionArgs(args); err != nil {
		return nil, err
	}
	res, err := proc.Run(ctx, proc.Cmd{Exe: a.exe, Dir: folder, Env: env, Args: args})
	if err != nil {
		return nil, err
	}
	var list []sessionInfo
	if res.ExitCode != 0 || json.Unmarshal(bytes.TrimSpace(res.Stdout), &list) != nil {
		return nil, errNoList
	}
	return list, nil
}

// event is one JSON line of `opencode run --format json`.
type event struct {
	Type      string `json:"type"`
	SessionID string `json:"sessionID"`
	Part      *struct {
		Text   string `json:"text"`
		Tokens *struct {
			Input  int `json:"input"`
			Output int `json:"output"`
			Cache  struct {
				Read  int `json:"read"`
				Write int `json:"write"`
			} `json:"cache"`
		} `json:"tokens"`
	} `json:"part"`
	Error *eventError `json:"error"`
}

// eventError is the error of an "error" event.
type eventError struct {
	Type    string `json:"type"`
	Message string `json:"message"`
	Status  int    `json:"status"` // the provider's HTTP status, when it answered
	// RetryAfterMs: how long until it may be asked again (the free models'
	// limit, 11-18).
	RetryAfterMs int64 `json:"retryAfterMs"`
}

// freeLimit is the error of an ask that reached the free models' usage
// limit (11-18, never signed in: "Free usage limit reached. Connect a
// provider or subscribe to OpenCode Go to keep going, or switch models.",
// status 429, retryAfterMs about 7 hours, the same for every free model,
// up to midnight UTC). Its detail is when they work again, to the minute.
func freeLimit(e *eventError, now time.Time) error {
	at := ""
	if e.RetryAfterMs > 0 {
		at = now.Add(time.Duration(e.RetryAfterMs) * time.Millisecond).UTC().Round(time.Minute).Format(time.RFC3339)
	}
	return core.Fail(core.ErrCodeFreeLimit, at)
}

// parseOutput reads the events. Each text part comes whole. The answer is
// the text of the last step that wrote any (an earlier step may be a lead-in
// before a web search). The last step_finish is sometimes never printed,
// so the process exit, not step_finish, ends the answer.
func parseOutput(res proc.Result, model string) (core.AskResult, error) {
	r := core.AskResult{Model: model}
	var steps [][]string
	var errEvent *eventError
	for _, line := range bytes.Split(res.Stdout, []byte("\n")) {
		line = bytes.TrimSpace(line) // lines end with \r\n on Windows
		var e event
		if len(line) == 0 || line[0] != '{' || json.Unmarshal(line, &e) != nil {
			continue
		}
		if r.SessionID == "" && sessionRe.MatchString(e.SessionID) {
			r.SessionID = e.SessionID
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
		case "error":
			if e.Error != nil && errEvent == nil {
				errEvent = e.Error
			}
		}
	}
	if errEvent != nil || res.ExitCode != 0 {
		code, msg := core.ErrCodeAskFailed, ""
		if errEvent != nil {
			msg = errEvent.Message
			switch {
			case errEvent.Type == "provider.no-route" || strings.HasPrefix(msg, "Model unavailable"):
				code = core.ErrCodeBadModel // or not signed in: Ask tells (signInError)
			case errEvent.Type == "provider.auth" || errEvent.Status == 401:
				return r, core.Fail(core.ErrCodeSignedOut, LoginCommand)
			case errEvent.Type == "provider.free-limit":
				return r, freeLimit(errEvent, time.Now())
			}
		}
		if msg == "" {
			msg = strings.TrimSpace(string(res.Stderr))
		}
		if msg == "" {
			msg = fmt.Sprintf("opencode ended with exit code %d and no answer", res.ExitCode)
		}
		return r, core.Fail(code, clip(msg, 300))
	}
	for i := len(steps) - 1; i >= 0; i-- {
		if len(steps[i]) > 0 {
			r.Answer = strings.Join(steps[i], "\n\n")
			break
		}
	}
	return r, nil
}

// clip collapses whitespace and keeps at most n runes.
func clip(s string, n int) string {
	s = strings.Join(strings.Fields(s), " ")
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	return string([]rune(s)[:n]) + "…"
}
