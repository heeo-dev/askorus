package claude

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"
	"unicode/utf8"
)

// Transcript is what the session list needs from one ~/.claude/projects
// file. It holds metadata only, plus the first words of the last prompt.
type Transcript struct {
	SessionID  string
	Title      string
	Cwd        string
	LastActive time.Time
	Model      string
	RemoteURL  string
	LastPrompt string
	Entrypoint string
}

// searchPromptRunes is how much of the last prompt is kept for searching.
const searchPromptRunes = 200

var uuidRe = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

// ValidUUID reports whether s is a lowercase Claude session uuid.
func ValidUUID(s string) bool { return uuidRe.MatchString(s) }

// fileState is the cached parse of one transcript. Transcripts only grow, so
// when a file gets longer only the new part is read.
type fileState struct {
	size   int64
	mtime  time.Time
	offset int64 // end of the last complete line that was parsed

	customTitle, agentName, aiTitle string
	cwd, model, remoteURL           string
	lastPrompt, entrypoint          string
	lastTS                          time.Time
	sideTitle                       string // <uuid>/custom-title.json
	sideMtime                       time.Time
}

// Transcripts scans a Claude projects directory with a per-file cache.
type Transcripts struct {
	Root string // usually ~/.claude/projects

	mu    sync.Mutex
	cache map[string]*fileState
}

// NewTranscripts returns a scanner for root.
func NewTranscripts(root string) *Transcripts {
	return &Transcripts{Root: root, cache: map[string]*fileState{}}
}

// Scan returns one Transcript per session file. Orphaned copies
// (<uuid>.orphaned-*.jsonl), empty files and files whose name is not a
// session uuid are skipped. Only the projects folder is read (N10).
func (t *Transcripts) Scan() ([]Transcript, error) {
	t.mu.Lock()
	defer t.mu.Unlock()

	projects, err := os.ReadDir(t.Root)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	var out []Transcript
	for _, p := range projects {
		if !p.IsDir() {
			continue
		}
		dir := filepath.Join(t.Root, p.Name())
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, e := range entries {
			name := e.Name()
			id, ok := strings.CutSuffix(name, ".jsonl")
			if !ok || e.IsDir() || !ValidUUID(id) {
				continue
			}
			path := filepath.Join(dir, name)
			st, err := t.load(path)
			if err != nil || st == nil {
				continue
			}
			seen[path] = true
			t.loadSideTitle(st, filepath.Join(dir, id, "custom-title.json"))
			out = append(out, st.transcript(id))
		}
	}
	for path := range t.cache {
		if !seen[path] {
			delete(t.cache, path)
		}
	}
	return out, nil
}

// load returns the up-to-date state for path, or nil for an empty file.
func (t *Transcripts) load(path string) (*fileState, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if info.Size() == 0 {
		delete(t.cache, path)
		return nil, nil
	}
	st := t.cache[path]
	switch {
	case st != nil && st.size == info.Size() && st.mtime.Equal(info.ModTime()):
		return st, nil
	case st != nil && info.Size() > st.size:
		// grown: read only the new part
	default:
		st = &fileState{}
	}
	if err := st.readFrom(path); err != nil {
		delete(t.cache, path)
		return nil, err
	}
	st.size, st.mtime = info.Size(), info.ModTime()
	t.cache[path] = st
	return st, nil
}

func (st *fileState) readFrom(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	if _, err := f.Seek(st.offset, io.SeekStart); err != nil {
		return err
	}
	r := bufio.NewReaderSize(f, 256<<10)
	for {
		line, err := r.ReadBytes('\n')
		if len(line) > 0 {
			complete := line[len(line)-1] == '\n'
			// A line still being written is applied if it already parses;
			// every field is "last value wins", so reading it again later
			// gives the same result.
			st.apply(line)
			if complete {
				st.offset += int64(len(line))
			}
		}
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
	}
}

// lineHead is the part of a transcript line the list needs. Unknown types
// and fields are ignored, so new Claude Code versions keep working.
type lineHead struct {
	Type        string `json:"type"`
	Subtype     string `json:"subtype"`
	CustomTitle string `json:"customTitle"`
	AgentName   string `json:"agentName"`
	AITitle     string `json:"aiTitle"`
	LastPrompt  string `json:"lastPrompt"`
	Cwd         string `json:"cwd"`
	Timestamp   string `json:"timestamp"`
	URL         string `json:"url"`
	IsSidechain bool   `json:"isSidechain"`
	Entrypoint  string `json:"entrypoint"`
}

type assistantModel struct {
	Message struct {
		Model string `json:"model"`
	} `json:"message"`
}

func (st *fileState) apply(line []byte) {
	line = bytes.TrimSpace(line)
	if len(line) == 0 || line[0] != '{' {
		return
	}
	var h lineHead
	if json.Unmarshal(line, &h) != nil {
		return
	}
	switch h.Type {
	case "custom-title":
		st.customTitle = keep(st.customTitle, h.CustomTitle)
	case "agent-name":
		st.agentName = keep(st.agentName, h.AgentName)
	case "ai-title":
		st.aiTitle = keep(st.aiTitle, h.AITitle)
	case "last-prompt":
		st.lastPrompt = keep(st.lastPrompt, clip(h.LastPrompt, searchPromptRunes))
	case "user", "assistant", "system":
		if h.IsSidechain {
			return
		}
		st.cwd = keep(st.cwd, h.Cwd)
		st.entrypoint = keep(st.entrypoint, h.Entrypoint)
		if ts, err := time.Parse(time.RFC3339Nano, h.Timestamp); err == nil && ts.After(st.lastTS) {
			st.lastTS = ts
		}
		if h.Type == "system" && h.Subtype == "bridge_status" && isRemoteURL(h.URL) {
			st.remoteURL = h.URL
		}
		if h.Type == "assistant" {
			var m assistantModel
			if json.Unmarshal(line, &m) == nil && m.Message.Model != "" && !strings.HasPrefix(m.Message.Model, "<") {
				st.model = m.Message.Model
			}
		}
	}
}

func (t *Transcripts) loadSideTitle(st *fileState, path string) {
	info, err := os.Stat(path)
	if err != nil {
		st.sideTitle, st.sideMtime = "", time.Time{}
		return
	}
	if info.ModTime().Equal(st.sideMtime) || info.Size() > 64<<10 {
		return
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return
	}
	var v struct {
		CustomTitle string `json:"customTitle"`
	}
	if json.Unmarshal(b, &v) == nil {
		st.sideTitle, st.sideMtime = strings.TrimSpace(v.CustomTitle), info.ModTime()
	}
}

func (st *fileState) transcript(id string) Transcript {
	return Transcript{
		SessionID:  id,
		Title:      firstNonEmpty(st.customTitle, st.sideTitle, st.agentName, st.aiTitle),
		Cwd:        st.cwd,
		LastActive: firstTime(st.lastTS, st.mtime),
		Model:      st.model,
		RemoteURL:  st.remoteURL,
		LastPrompt: st.lastPrompt,
		Entrypoint: st.entrypoint,
	}
}

// isRemoteURL accepts only the official Remote Control session links, so a
// crafted transcript cannot make the app open another site.
func isRemoteURL(u string) bool {
	rest, ok := strings.CutPrefix(u, "https://claude.ai/code/session_")
	if !ok || rest == "" || len(rest) > 128 {
		return false
	}
	for _, r := range rest {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '_' || r == '-') {
			return false
		}
	}
	return true
}

func keep(old, v string) string {
	if v = strings.TrimSpace(v); v != "" {
		return v
	}
	return old
}

func firstNonEmpty(vs ...string) string {
	for _, v := range vs {
		if v != "" {
			return v
		}
	}
	return ""
}

func firstTime(ts ...time.Time) time.Time {
	for _, t := range ts {
		if !t.IsZero() {
			return t.UTC()
		}
	}
	return time.Time{}
}

// clip collapses whitespace and keeps at most n runes.
func clip(s string, n int) string {
	s = strings.Join(strings.Fields(s), " ")
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	r := []rune(s)
	return string(r[:n]) + "…"
}
