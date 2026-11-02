//go:build judgeeval

package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/asksemble/asksemble/agent/internal/config"
	"github.com/asksemble/asksemble/agent/internal/core"
)

// The run folder (plan part 2, store): %LOCALAPPDATA%\Asksemble\judge-eval\<run>,
// outside the repo. requests.jsonl holds one line per finished request,
// with the judges' raw outputs and reasons: content stays here and never
// reaches events.log or the console.

// Files of a run folder.
const (
	fileManifest = "manifest.json"
	fileRequests = "requests.jsonl"
	fileEvents   = "events.log"
	fileStop     = "stop.json"
	fileSet      = "set.json"
	fileReport   = "report.json"
	fileReportMD = "report.md"
)

// Record statuses.
const (
	statusDone   = "done"
	statusFailed = "failed"
)

// manifestFormat is the manifest's format.
const manifestFormat = 1

// manifest describes a run; resume checks a run against it.
type manifest struct {
	Tool            string                `json:"tool"`
	Format          int                   `json:"format"`
	Run             string                `json:"run"`
	Created         string                `json:"created"`
	Set             string                `json:"set"`
	Split           string                `json:"split"`
	Questions       []int                 `json:"questions"`
	Engines         []string              `json:"engines"`
	Tier            string                `json:"tier"`
	Shape           string                `json:"shape"`
	Labels          string                `json:"labels"`
	Rules           string                `json:"rules"`
	MaxUnits        int                   `json:"maxUnits"`
	Parallel        int                   `json:"parallel"`
	Checks          []string              `json:"checks"`
	GoldEvidence    bool                  `json:"goldEvidence,omitempty"`
	PerturbN        int                   `json:"perturbN,omitempty"`
	EngineInfo      map[string]engineInfo `json:"engineInfo"`
	SettingsIgnored bool                  `json:"settingsIgnored"`
	Data            dataHashes            `json:"data"`
	EvalFolder      string                `json:"evalFolder"`
	CodexSchema     string                `json:"codexSchema"`
	Commit          string                `json:"commit"`
	Sessions        []session             `json:"sessions"`
}

type engineInfo struct {
	Model   string `json:"model"`
	Effort  string `json:"effort"`
	Family  string `json:"family"`
	Version string `json:"version"`
}

type dataHashes struct {
	Files   map[string]string `json:"files"`             // file name → sha256
	Perturb string            `json:"perturb,omitempty"` // sha256 of the canonical perturb set
}

type session struct {
	Started  string            `json:"started"`
	Versions map[string]string `json:"versions"`
	Resume   bool              `json:"resume"`
}

// record is one finished request: a line of requests.jsonl.
type record struct {
	Key      string       `json:"key"`
	Set      string       `json:"set"`
	Q        int          `json:"q"`
	Item     string       `json:"item"`
	Engine   string       `json:"engine"`
	From     string       `json:"from"`
	Against  string       `json:"against"`
	Model    string       `json:"model"`
	Effort   string       `json:"effort"`
	CLI      string       `json:"cli"`
	Task     string       `json:"task"` // sha256 of the task's canonical JSON
	Status   string       `json:"status"`
	Code     string       `json:"code"`
	Attempts []attemptRec `json:"attempts"`
	Verdicts []verdictRec `json:"verdicts"`
	Missing  []string     `json:"missing"`
}

type verdictRec struct {
	ID       string   `json:"id"`
	Core     bool     `json:"core"`
	Evidence []string `json:"evidence"`
	Reason   string   `json:"reason"`
	Label    string   `json:"label"`
	Unsure   bool     `json:"unsure"`
}

// finalCodes are the failures a resume does not ask again (without
// -retry-failed). A usage limit is never recorded.
var finalCodes = []string{
	core.ErrCodeJudgeBadOutput, core.ErrCodeJudgeUsedTool, core.ErrCodeJudgeTimeout,
	core.ErrCodeBadModel, core.ErrCodeJudgeFailed, core.ErrCodeInternal,
}

// finished reports whether a resume skips r.
func (r record) finished(retryFailed bool) bool {
	switch r.Status {
	case statusDone:
		return true
	case statusFailed:
		return !retryFailed && slices.Contains(finalCodes, r.Code)
	}
	return false
}

// taskSHA is the sha256 of the task's canonical JSON.
func taskSHA(task core.JudgeTask) string {
	b, _ := json.Marshal(task)
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

// defaultOut is %LOCALAPPDATA%\Asksemble\judge-eval.
func defaultOut() (string, error) {
	base, err := os.UserCacheDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(base, "Asksemble", "judge-eval"), nil
}

// marshal is v as JSON ending in a line break, indented by one space with
// indent, without escaping <, > and & (keys read "claude>codex").
func marshal(v any, indent bool) ([]byte, error) {
	var b bytes.Buffer
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	if indent {
		enc.SetIndent("", " ")
	}
	if err := enc.Encode(v); err != nil {
		return nil, err
	}
	return b.Bytes(), nil
}

// writeJSON writes v as indented JSON, atomically.
func writeJSON(path string, v any) error {
	b, err := marshal(v, true)
	if err != nil {
		return err
	}
	return config.WriteFileAtomic(path, b)
}

func readManifest(dir string) (manifest, error) {
	var m manifest
	b, err := os.ReadFile(filepath.Join(dir, fileManifest))
	if err != nil {
		return m, err
	}
	if err := json.Unmarshal(b, &m); err != nil {
		return m, fmt.Errorf("%s: %w", fileManifest, err)
	}
	if m.Tool != "judge-eval" || m.Format != manifestFormat {
		return m, fmt.Errorf("%s: not a judge-eval run of format %d", fileManifest, manifestFormat)
	}
	return m, nil
}

// loadRecords reads requests.jsonl: the latest line of each key, and the
// keys in the order they first came. A torn last line (a crash while it
// was written) is dropped, and with repair the file is cut back to the
// last good line. Any other bad line is an error.
func loadRecords(path string, repair bool) (map[string]record, []string, error) {
	b, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return map[string]record{}, nil, nil
	}
	if err != nil {
		return nil, nil, err
	}
	latest := map[string]record{}
	var order []string
	good := 0 // bytes up to the end of the last good line
	for pos := 0; pos < len(b); {
		end := bytes.IndexByte(b[pos:], '\n')
		if end < 0 {
			break // a line without its line break is torn
		}
		line, next := b[pos:pos+end], pos+end+1
		if len(bytes.TrimSpace(line)) > 0 {
			var r record
			if err := json.Unmarshal(line, &r); err != nil || r.Key == "" {
				if next == len(b) {
					break // a torn last line
				}
				return nil, nil, fmt.Errorf("%s: a bad line at byte %d", fileRequests, pos)
			}
			if _, ok := latest[r.Key]; !ok {
				order = append(order, r.Key)
			}
			latest[r.Key] = r
		}
		pos, good = next, next
	}
	if good < len(b) && repair {
		if err := os.Truncate(path, int64(good)); err != nil {
			return nil, nil, err
		}
	}
	return latest, order, nil
}

// store appends to a run folder's requests.jsonl and events.log.
type store struct {
	dir    string
	mu     sync.Mutex
	req    *os.File
	events *os.File
}

func openStore(dir string) (*store, error) {
	req, err := os.OpenFile(filepath.Join(dir, fileRequests), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return nil, err
	}
	events, err := os.OpenFile(filepath.Join(dir, fileEvents), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		req.Close()
		return nil, err
	}
	return &store{dir: dir, req: req, events: events}, nil
}

// add appends one record and syncs it.
func (s *store) add(r record) error {
	b, err := marshal(r, false)
	if err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, err := s.req.Write(b); err != nil {
		return err
	}
	return s.req.Sync()
}

// event appends one line of codes, ids and counts to events.log.
func (s *store) event(kind string, fields ...any) {
	var b strings.Builder
	b.WriteString(time.Now().UTC().Format(time.RFC3339))
	b.WriteString(" " + kind)
	for i := 0; i+1 < len(fields); i += 2 {
		fmt.Fprintf(&b, " %v=%v", fields[i], fields[i+1])
	}
	b.WriteByte('\n')
	s.mu.Lock()
	defer s.mu.Unlock()
	io.WriteString(s.events, b.String())
}

func (s *store) close() {
	s.req.Close()
	s.events.Close()
}
