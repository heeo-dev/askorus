package agy

// Photos with a question (#14, #20 decision 5). agy has no way to take a
// photo with the question: its stream-json input refuses image blocks
// (2026-11-18). It reads a photo that lies in the folder it runs in with
// its own file tool. So a question with photos runs in a folder of its
// own, made for that one run, that holds only that question's photos and
// the photo agent's file, and the deny rules leave out read_file(*): agy
// reads in the folder it runs in without asking, and any read outside it
// needs an approval that a run without a terminal cannot get, so it is
// refused (2026-11-18: an absolute path, ../, ..\, a listing and agy's own
// settings were all refused, 9 runs). That refusal is agy's default, not a
// rule of ours, and LT-17 tries it on every run of the live tests. Each
// CLI gets photos through the narrowest way it has (decision 5); this is
// agy's.

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/heeo-dev/askorus/agent/internal/core"
)

// PhotoFolderName is the folder under the ask folder (and the judge
// folder) whose subfolders hold one run's photos each.
const PhotoFolderName = "agy-photos"

const photoAgent = "askorus-photo"

// photoAgentFile is askAgentFile with view_file: the model looks at the
// photos with it. Reads outside the folder are refused (see above).
const photoAgentFile = "---\n" +
	"name: " + photoAgent + "\n" +
	"description: Askorus ask with photos, answer the question only\n" +
	"tools: [search_web, view_file]\n" +
	"mainAgent: true\n" +
	"subagent: false\n" +
	"commandExecutionPolicy: off\n" +
	"---\n" +
	askInstructions +
	"The photos the person attached are files in the current folder; look at them with view_file. Open no other file.\n"

// photoSettings are the deny rules of a question with photos: every one of
// askSettings but read_file(*).
var photoSettings = lockSettings(false, false)

// photoTools are the tools a run with photos may use: agy names its file
// tool read_file in the steps it prints.
var photoTools = map[string]bool{"search_web": true, "manage_task": true, "read_file": true, "view_file": true}

// photoFolder makes a new folder for one run under base/agy-photos, puts
// copies of the photos in it as photo-<n><ending> with the agent file of
// name, and returns the folder, the photos' names and a cleanup that
// removes the folder. The photos are the agent's own kept files
// (core.CheckImages); nothing of the phone's names is used.
func photoFolder(base, name, content string, images []core.Image) (dir string, names []string, cleanup func(), err error) {
	if err := core.CheckImages(images); err != nil || len(images) == 0 {
		return "", nil, nil, core.Fail(core.ErrCodeInternal, "agy: a photo was refused")
	}
	if info, err := os.Stat(base); !filepath.IsAbs(base) || err != nil || !info.IsDir() {
		return "", nil, nil, core.Fail(core.ErrCodeInternal, "agy: the ask folder is not an absolute existing folder")
	}
	root := filepath.Join(base, PhotoFolderName)
	if err := os.MkdirAll(root, 0o700); err != nil {
		return "", nil, nil, core.Fail(core.ErrCodeInternal, "agy: the photo folder could not be made")
	}
	raw := make([]byte, 12)
	if _, err := rand.Read(raw); err != nil {
		return "", nil, nil, core.Fail(core.ErrCodeInternal, "agy: no random name")
	}
	dir = filepath.Join(root, hex.EncodeToString(raw))
	if err := os.Mkdir(dir, 0o700); err != nil {
		return "", nil, nil, core.Fail(core.ErrCodeInternal, "agy: the photo folder could not be made")
	}
	cleanup = func() { os.RemoveAll(dir) }
	fail := func(msg string) (string, []string, func(), error) {
		cleanup()
		return "", nil, nil, core.Fail(core.ErrCodeInternal, msg)
	}
	for i, im := range images {
		n := fmt.Sprintf("photo-%d%s", i+1, core.ImageExt[im.Type])
		if err := copyPhoto(im.Path, filepath.Join(dir, n)); err != nil {
			return fail("agy: a photo could not be copied")
		}
		names = append(names, n)
	}
	p := filepath.Join(dir, ".agents", "agents", name+".md")
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		return fail("agy: the agent folder could not be made")
	}
	if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
		return fail("agy: the agent file could not be written")
	}
	return dir, names, cleanup, nil
}

// copyPhoto copies a kept photo, a regular file of at most MaxImageBytes.
func copyPhoto(src, dst string) error {
	info, err := os.Lstat(src)
	if err != nil || !info.Mode().IsRegular() || info.Size() > core.MaxImageBytes {
		return fmt.Errorf("not a kept photo")
	}
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	_, err = io.Copy(out, io.LimitReader(in, core.MaxImageBytes))
	if cerr := out.Close(); err == nil {
		err = cerr
	}
	return err
}

// photoLine is what the prompt starts with for a run with photos: their
// names in the folder, so the model knows what to open.
func photoLine(names []string) string {
	return "[Photos attached to this question, in the current folder: " + strings.Join(names, ", ") + "]\n\n"
}

// SeesImages: the Gemini models agy runs for an ask all see photos
// (core.ImageSeer).
func (a *Adapter) SeesImages(model string) bool { return modelRe.MatchString(model) }

// ImageModels says so to the app.
func (a *Adapter) ImageModels() core.ImageInfo { return core.ImageInfo{All: true} }
