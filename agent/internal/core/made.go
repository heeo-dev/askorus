package core

// Pictures an AI makes while it answers (#14, made pictures, decisions of
// 2026-11-21). Codex and Antigravity make them with a tool of their own and
// save them in a folder of their own, one per conversation; neither says so
// in what it prints. So the adapter notes which files the conversation's
// folder holds before the ask (MadeSnapshot) and, after it, takes the files
// that are new there (FindMade): only the folder's top level, regular files
// (never a link), a name of the AI's own shape, a JPEG, PNG or WebP by its
// first bytes, at most MaxMadeBytes, whose real place is inside the folder.
// The originals stay where the AI put them: Askorus moves, changes and
// deletes nothing of the AI's.

import (
	"bytes"
	"encoding/binary"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"
)

const (
	// MaxMade is how many made pictures one answer brings (decision 2).
	MaxMade = 4
	// MaxMadeBytes bounds one made picture (a Codex PNG is up to about 3 MiB).
	MaxMadeBytes = 8 << 20
)

// MadeTypes are the picture types an answer may bring, as /v1/info shows them.
var MadeTypes = []string{ImagePNG, ImageJPEG, ImageWebP}

// MadeFile is one picture an AI made, in the AI's own folder, found and
// checked by FindMade.
type MadeFile struct {
	Path string
	Type string
}

// MadePicture is one made picture as a run's part tells the phone: the id
// the agent gave its copy (GET /v1/runs/{run}/images/{id}), its type, size
// and pixels (0 when the agent could not read them).
type MadePicture struct {
	ID     string `json:"id"`
	Type   string `json:"type"`
	Bytes  int64  `json:"bytes"`
	Width  int    `json:"width,omitempty"`
	Height int    `json:"height,omitempty"`
}

// PictureLimit says that the AI's tool for pictures reached its limit in
// this answer, and when it makes pictures again (RFC 3339, UTC; "" when the
// AI did not say).
type PictureLimit struct {
	Until string `json:"until,omitempty"`
}

// MakesInfo is AskInfo.makes: the AI makes pictures when a question asks
// for one, at most MaxCount an answer, of Types. LimitedUntil is set while
// the agent knows that the AI's picture limit is reached (RFC 3339, UTC).
type MakesInfo struct {
	MaxCount     int      `json:"maxCount"`
	Types        []string `json:"types"`
	LimitedUntil string   `json:"limitedUntil,omitempty"`
}

// PictureMaker is implemented by askers that can make pictures. Makes is
// nil while it cannot (not signed in, a CLI too old); an ask's Pictures
// field is honoured only then.
type PictureMaker interface {
	Makes() *MakesInfo
}

// PictureResumer is implemented by a PictureMaker whose conversation keeps
// the tools of its first ask (Antigravity, R-130): MakesIn reports whether
// the conversation resume, a native id, was started with the picture tool.
type PictureResumer interface {
	MakesIn(resume string) bool
}

// PicturePlan decides, for one ask of a question to asker a, whether the
// AI gets its picture tool and which picture guide goes after the preamble.
// want: the phone shows made pictures and the person has not turned them
// off for this AI. The AI makes pictures when it is a PictureMaker that can
// now, and, in a follow-up, when the conversation has the tool. While its
// picture limit is known to be reached it keeps the tool (the conversation
// keeps it for later) and is guided to draw.
func PicturePlan(a any, want bool, resume string) (pictures bool, guide string) {
	pm, ok := a.(PictureMaker)
	if !want || !ok {
		return false, GuideDraw
	}
	m := pm.Makes()
	if m == nil {
		return false, GuideDraw
	}
	if r, ok := a.(PictureResumer); ok && resume != "" && !r.MakesIn(resume) {
		return false, GuideDraw
	}
	if m.LimitedUntil != "" {
		return true, GuideDraw
	}
	return true, GuideMake
}

// MadeSnapshot is the set of file names at the top of dir now; nil when dir
// cannot be read (it does not exist yet, as before a conversation's first
// picture: every file found later is then new).
func MadeSnapshot(dir string) map[string]bool {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	seen := make(map[string]bool, len(entries))
	for _, e := range entries {
		seen[e.Name()] = true
	}
	return seen
}

// FindMade gives the pictures at the top of dir that were not there in
// before (MadeSnapshot), whose names name accepts, oldest first, at most
// MaxMade, and how many more there were. Anything that is not a checked
// picture is left out without a word.
func FindMade(dir string, before map[string]bool, name *regexp.Regexp) (found []MadeFile, leftOut int) {
	real, err := filepath.EvalSymlinks(dir)
	if err != nil {
		return nil, 0
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, 0
	}
	type cand struct {
		f  MadeFile
		at time.Time
	}
	var cands []cand
	for _, e := range entries {
		n := e.Name()
		if before[n] || !name.MatchString(n) || !e.Type().IsRegular() {
			continue
		}
		p := filepath.Join(dir, n)
		info, err := os.Lstat(p)
		if err != nil || !info.Mode().IsRegular() || info.Size() == 0 || info.Size() > MaxMadeBytes {
			continue
		}
		// Its real place, so a link swapped in after the listing is caught.
		rp, err := filepath.EvalSymlinks(p)
		if err != nil || !strings.EqualFold(filepath.Dir(rp), real) {
			continue
		}
		typ := sniffFile(p)
		if typ == "" {
			continue
		}
		cands = append(cands, cand{MadeFile{Path: p, Type: typ}, info.ModTime()})
	}
	slices.SortStableFunc(cands, func(a, b cand) int {
		if c := a.at.Compare(b.at); c != 0 {
			return c
		}
		return strings.Compare(a.f.Path, b.f.Path)
	})
	for i, c := range cands {
		if i < MaxMade {
			found = append(found, c.f)
		}
	}
	return found, max(0, len(cands)-MaxMade)
}

func sniffFile(p string) string {
	f, err := os.Open(p)
	if err != nil {
		return ""
	}
	defer f.Close()
	head := make([]byte, 16)
	n, _ := io.ReadFull(f, head)
	return SniffImage(head[:n])
}

// SniffImage names the image type of data by its first bytes, or "".
func SniffImage(data []byte) string {
	switch {
	case len(data) >= 3 && data[0] == 0xFF && data[1] == 0xD8 && data[2] == 0xFF:
		return ImageJPEG
	case len(data) >= 8 && bytes.Equal(data[:8], []byte{0x89, 'P', 'N', 'G', 0x0D, 0x0A, 0x1A, 0x0A}):
		return ImagePNG
	case len(data) >= 12 && string(data[:4]) == "RIFF" && string(data[8:12]) == "WEBP":
		return ImageWebP
	}
	return ""
}

// ImageSize reads the pixels of a PNG, JPEG or WebP from its first bytes;
// 0, 0 when it cannot tell.
func ImageSize(data []byte) (width, height int) {
	switch SniffImage(data) {
	case ImagePNG:
		if len(data) >= 24 && string(data[12:16]) == "IHDR" {
			return int(binary.BigEndian.Uint32(data[16:20])), int(binary.BigEndian.Uint32(data[20:24]))
		}
	case ImageJPEG:
		for i := 2; i+9 < len(data); {
			if data[i] != 0xFF {
				i++
				continue
			}
			m := data[i+1]
			if m == 0xD8 || m == 0x01 || (m >= 0xD0 && m <= 0xD7) || m == 0xFF {
				i++
				continue
			}
			if m >= 0xC0 && m <= 0xCF && m != 0xC4 && m != 0xC8 && m != 0xCC {
				return int(binary.BigEndian.Uint16(data[i+7 : i+9])), int(binary.BigEndian.Uint16(data[i+5 : i+7]))
			}
			i += 2 + int(binary.BigEndian.Uint16(data[i+2:i+4]))
		}
	case ImageWebP:
		if len(data) < 30 {
			return 0, 0
		}
		switch string(data[12:16]) {
		case "VP8X":
			w := 1 + (int(data[24]) | int(data[25])<<8 | int(data[26])<<16)
			h := 1 + (int(data[27]) | int(data[28])<<8 | int(data[29])<<16)
			return w, h
		case "VP8L":
			b := data[21:25]
			w := 1 + (int(b[0]) | (int(b[1])&0x3F)<<8)
			h := 1 + (int(b[1])>>6 | int(b[2])<<2 | (int(b[3])&0x0F)<<10)
			return w, h
		case "VP8 ":
			return int(binary.LittleEndian.Uint16(data[26:28]) & 0x3FFF), int(binary.LittleEndian.Uint16(data[28:30]) & 0x3FFF)
		}
	}
	return 0, 0
}

// ScrubMadePaths takes the made pictures' places out of an answer: an AI
// asked where it saved a picture wrote the whole path, with the person's
// user folder in it. Each picture's path becomes its name in the app
// (picture-1.png, …) and the folder's path "pictures", in either kind of
// slash and any case.
func ScrubMadePaths(answer, dir string, files []MadeFile) string {
	if answer == "" || dir == "" {
		return answer
	}
	for i, f := range files {
		answer = replaceFold(answer, f.Path, madeName(i, f.Type))
	}
	return replaceFold(answer, dir, "pictures")
}

func madeName(i int, typ string) string {
	return "picture-" + itoa(i+1) + ImageExt[typ]
}

func itoa(n int) string {
	if n < 10 {
		return string(rune('0' + n))
	}
	return itoa(n/10) + string(rune('0'+n%10))
}

// replaceFold replaces old in s, ignoring case and the kind of slash, the
// way Windows and macOS read a path.
func replaceFold(s, old, repl string) string {
	if old == "" {
		return s
	}
	norm := func(r rune) rune {
		if r == '\\' {
			return '/'
		}
		return r
	}
	ls := strings.ToLower(strings.Map(norm, s))
	lo := strings.ToLower(strings.Map(norm, old))
	if len(ls) != len(s) || len(lo) != len(old) {
		return s // a letter changed length when lowered: leave it as it is
	}
	var b strings.Builder
	for i := 0; ; {
		j := strings.Index(ls[i:], lo)
		if j < 0 {
			b.WriteString(s[i:])
			return b.String()
		}
		b.WriteString(s[i : i+j])
		b.WriteString(repl)
		i += j + len(lo)
	}
}
