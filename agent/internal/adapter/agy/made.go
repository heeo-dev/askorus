package agy

// Pictures Antigravity makes (#14, made pictures; probes of 2026-11-20 and
// 2026-11-21). agy's generate_image tool saves each picture as
// <brain>/<conversation>/<name>_<milliseconds>.jpg, <brain> being
// ~/.gemini/antigravity-cli/brain; the stream says only that the tool ran.
// A name the model gives with "/" or "\" in it is saved with "_" there, at
// the top of the same folder, and the model dropped "../" itself (11-21).
// The adapter hands the tool over only with two agents of its own,
// askorus-draw and askorus-photo-draw (the ask agents with generate_image),
// notes the conversation's folder before the ask and takes what is new at
// its top afterwards (core.FindMade). It reads that one folder only, never
// agy's conversations, settings or sign-in, and leaves every file there.
//
// A conversation keeps the tools of its first run (R-130): one started
// without generate_image makes no picture later, and one started with it
// keeps it. So the adapter notes the conversations it started with a draw
// agent (MakesIn), and a follow-up runs with the draw agent exactly when the
// conversation is one of them.
//
// agy's picture tool has a limit of its own (5 pictures a week on the
// plan of 2026-11-20). When the tool fails with Google's 429, the answer
// says so with the time it refreshes, and Makes reports it until then.

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/heeo-dev/askorus/agent/internal/core"
)

const (
	drawAgent      = "askorus-draw"
	photoDrawAgent = "askorus-photo-draw"
	pictureTool    = "generate_image"
)

// drawInstructions follow the ask's own in the draw agents' files.
const drawInstructions = "When the person asks for a picture, you may make it with generate_image.\n"

// drawAgentFile is askAgentFile with generate_image.
const drawAgentFile = "---\n" +
	"name: " + drawAgent + "\n" +
	"description: Askorus ask that may make pictures, answer the question only\n" +
	"tools: [search_web, generate_image]\n" +
	"mainAgent: true\n" +
	"subagent: false\n" +
	"commandExecutionPolicy: off\n" +
	"---\n" +
	askInstructions + drawInstructions

// photoDrawAgentFile is photoAgentFile with generate_image.
const photoDrawAgentFile = "---\n" +
	"name: " + photoDrawAgent + "\n" +
	"description: Askorus ask with photos that may make pictures, answer the question only\n" +
	"tools: [search_web, view_file, generate_image]\n" +
	"mainAgent: true\n" +
	"subagent: false\n" +
	"commandExecutionPolicy: off\n" +
	"---\n" +
	askInstructions +
	"The photos the person attached are files in the current folder; look at them with view_file. Open no other file.\n" +
	drawInstructions

// withPictures is tools and generate_image.
func withPictures(tools map[string]bool) map[string]bool {
	out := map[string]bool{pictureTool: true}
	for t := range tools {
		out[t] = true
	}
	return out
}

var (
	drawTools      = withPictures(askTools)
	photoDrawTools = withPictures(photoTools)
)

// madeNameRe is the shape of a picture agy saves: the model's name (no
// separator, not starting with a dot), "_" and the milliseconds.
var madeNameRe = regexp.MustCompile(`^[^./\\][^/\\]{0,150}_[0-9]{10,16}\.(jpg|jpeg|png|webp)$`)

// maxDrawConversations bounds the conversations MakesIn remembers.
const maxDrawConversations = 2000

// brainFolder is Options.BrainDir, else agy's own: "" when unknown.
func brainFolder(dir string) string {
	if dir == "" {
		home, err := os.UserHomeDir()
		if err != nil || home == "" {
			return ""
		}
		dir = filepath.Join(home, ".gemini", "antigravity-cli", "brain")
	}
	if !filepath.IsAbs(dir) {
		return ""
	}
	return filepath.Clean(dir)
}

// Makes: agy makes pictures while it is found and not known to be signed
// out (core.PictureMaker); LimitedUntil while its picture limit is known
// to be reached.
func (a *Adapter) Makes() *core.MakesInfo {
	if a.exeErr != nil || a.brainDir == "" {
		return nil
	}
	a.loginMu.Lock()
	st := a.login
	a.loginMu.Unlock()
	if st.known && !st.signedIn {
		return nil
	}
	m := &core.MakesInfo{MaxCount: core.MaxMade, Types: core.MadeTypes}
	a.drawMu.Lock()
	if !a.picLimit.IsZero() && a.now().Before(a.picLimit) {
		m.LimitedUntil = a.picLimit.UTC().Format(time.RFC3339)
	}
	a.drawMu.Unlock()
	return m
}

// MakesIn reports whether the conversation resume was started with a draw
// agent by this agent (core.PictureResumer). The list lives as long as the
// agent: after a restart an older conversation draws with code only.
func (a *Adapter) MakesIn(resume string) bool {
	a.drawMu.Lock()
	defer a.drawMu.Unlock()
	return a.drawConvs[resume]
}

func (a *Adapter) noteDraw(conversation string) {
	if !conversationRe.MatchString(conversation) {
		return
	}
	a.drawMu.Lock()
	defer a.drawMu.Unlock()
	if a.drawConvs == nil {
		a.drawConvs = map[string]bool{}
	}
	if len(a.drawConvs) >= maxDrawConversations {
		clear(a.drawConvs) // forgetting only makes older ones draw with code
	}
	a.drawConvs[conversation] = true
}

func (a *Adapter) noteLimit(until time.Time) {
	a.drawMu.Lock()
	defer a.drawMu.Unlock()
	a.picLimit = until
}

func (a *Adapter) now() time.Time {
	if a.clock != nil {
		return a.clock()
	}
	return time.Now()
}

// pictureLimitRe finds the refresh time in the tool's error.
var pictureLimitRe = regexp.MustCompile(`"quotaResetTimeStamp"\s*:\s*"([0-9]{4}-[0-9]{2}-[0-9]{2}T[0-9:.]+Z)"`)

// pictureLimitOf reads a failed generate_image step's error: whether it is
// the limit, and when it refreshes (zero when the error does not say, or
// says a time that is past or more than two weeks away).
func pictureLimitOf(msg string, now time.Time) (limited bool, until time.Time) {
	low := strings.ToLower(msg)
	if !strings.Contains(low, "429") && !strings.Contains(low, "resource_exhausted") && !strings.Contains(low, "quota_exhausted") {
		return false, time.Time{}
	}
	if m := pictureLimitRe.FindStringSubmatch(msg); m != nil {
		if t, err := time.Parse(time.RFC3339Nano, m[1]); err == nil && t.After(now) && t.Before(now.Add(14*24*time.Hour)) {
			return true, t
		}
	}
	return true, time.Time{}
}

// pictureAsk is one ask's picture plan: the agents to run with, whether to
// collect, and the conversation folder noted before the ask.
type pictureAsk struct {
	collect bool
	dir     string
	before  map[string]bool
}

// collectInto puts what the ask made into out, when it may make pictures,
// and takes the conversation folder's place out of the answer in any case
// (R-161: stripFileLinks takes out links to files, not a plain path).
func (a *Adapter) collectInto(p pictureAsk, out *core.AskResult) {
	if a.brainDir == "" {
		return
	}
	dir := p.dir
	if dir == "" && conversationRe.MatchString(out.SessionID) {
		dir = filepath.Join(a.brainDir, out.SessionID)
	}
	if p.collect && dir != "" {
		out.Made, out.MadeLeftOut = core.FindMade(dir, p.before, madeNameRe)
	}
	if dir != "" {
		out.Answer = core.ScrubMadePaths(out.Answer, dir, out.Made)
	}
	out.Answer = core.ScrubMadePaths(out.Answer, a.brainDir, nil)
}
