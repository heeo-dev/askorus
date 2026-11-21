package codex

import (
	"os"
	"path/filepath"
	"regexp"

	"github.com/heeo-dev/askorus/agent/internal/core"
)

// Pictures Codex makes (#14, made pictures, probe of 2026-11-20). With the
// feature image_generation left on (askImageArgs), codex makes a picture
// when the question asks for one and saves it as
// <codex home>/generated_images/<thread>/exec-<uuid>.png; --json says
// nothing of it. The agent notes the thread's folder before the ask and
// takes what is new in it afterwards (core.FindMade). It reads that one
// folder only, never codex's sign-in (auth.json) or anything else there,
// and leaves every file where codex put it.

// madeNameRe is the shape of a picture codex saves.
var madeNameRe = regexp.MustCompile(`^exec-[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}\.(png|jpg|jpeg|webp)$`)

// pictureRoot is the folder codex keeps its pictures in, one folder per
// thread: absolute, or "" when the agent cannot tell where codex lives.
func (a *Adapter) pictureRoot() string {
	home := a.home
	if home == "" {
		home = os.Getenv("CODEX_HOME")
	}
	if home == "" {
		h, err := os.UserHomeDir()
		if err != nil || h == "" {
			return ""
		}
		home = filepath.Join(h, ".codex")
	}
	if !filepath.IsAbs(home) {
		return ""
	}
	return filepath.Join(filepath.Clean(home), "generated_images")
}

// Makes: codex makes pictures while it is found and not known to be signed
// out (core.PictureMaker). The pictures are PNG today; the other types are
// taken too, by their first bytes.
func (a *Adapter) Makes() *core.MakesInfo {
	if a.exeErr != nil || a.pictureRoot() == "" {
		return nil
	}
	a.loginMu.Lock()
	st := a.login
	a.loginMu.Unlock()
	if st.known && !st.signedIn {
		return nil
	}
	return &core.MakesInfo{MaxCount: core.MaxMade, Types: core.MadeTypes}
}

// pictureAsk is one ask's picture folder: on when codex gets its image
// tool, with the thread's folder and what it held before (a follow-up's).
type pictureAsk struct {
	on     bool
	root   string
	dir    string
	before map[string]bool
}

// pictureFolder plans the pictures of an ask: on when the ask may make
// pictures and codex's folder is known.
func (a *Adapter) pictureFolder(want bool, resume string) pictureAsk {
	root := a.pictureRoot()
	if !want || root == "" {
		return pictureAsk{}
	}
	p := pictureAsk{on: true, root: root}
	if resume != "" && threadRe.MatchString(resume) {
		p.dir = filepath.Join(root, resume)
		p.before = core.MadeSnapshot(p.dir)
	}
	return p
}

// collect puts the pictures the ask made into out and takes the folder's
// place out of the answer.
func (p pictureAsk) collect(out *core.AskResult) {
	if !p.on {
		return
	}
	dir := p.dir
	if dir == "" {
		if !threadRe.MatchString(out.SessionID) {
			return
		}
		dir = filepath.Join(p.root, out.SessionID) // a new thread: nothing was there before
	}
	out.Made, out.MadeLeftOut = core.FindMade(dir, p.before, madeNameRe)
	out.Answer = core.ScrubMadePaths(out.Answer, dir, out.Made)
}
