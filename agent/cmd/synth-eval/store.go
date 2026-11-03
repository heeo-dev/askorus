//go:build syntheval

package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/asksemble/asksemble/agent/internal/judge"
)

// A run keeps one file per question, q-<id>.json, in its folder under
// %LOCALAPPDATA%\Asksemble\synth-eval: the question, the answers, the
// comparison and the combined answer, outside the repo (the person's own
// questions and the AIs' answers).

// Record is what one question gave.
type Record struct {
	V        int               `json:"v"`   // 1
	Seq      int               `json:"seq"` // the question's place in the run, from 1
	Question Question          `json:"question"`
	Run      RunInfo           `json:"run"`
	At       string            `json:"at"` // when it was asked (RFC 3339)
	Versions map[string]string `json:"versions,omitempty"`
	Refine   *RefineRec        `json:"refine,omitempty"`
	Answers  []AnswerRec       `json:"answers"`
	Compare  *CompareRec       `json:"compare,omitempty"`
	Synth    *SynthRec         `json:"synth,omitempty"`
	// Task is what the writer got: the answers, the judgments' 다른 말 and
	// 확인 필요 items and the spec (C4, E0), so resynth can write again
	// from the same answers and judgments. Runs before E0 have none.
	Task *judge.SynthTask `json:"synthTask,omitempty"`
	// Stop is set when the question did not run through and is asked
	// again on resume: usage_limit, or the pipeline's error code.
	Stop string `json:"stop,omitempty"`
}

// RunInfo is how a run asks.
type RunInfo struct {
	Name   string `json:"name"`
	Tier   string `json:"tier"`
	Writer string `json:"writer"`
	Refine string `json:"refine"` // off, or on: the writer moderates (C2)
	// From is the run whose answers and judgments resynth wrote from; the
	// writer then wrote at SynthTier with the prompt Synth (C4, E0).
	From      string `json:"from,omitempty"`
	SynthTier string `json:"synthTier,omitempty"`
	Synth     string `json:"synth,omitempty"`
	// Form is how a refine-only run refined: fixed (refine-v1 and the
	// agent's templates) or live (the moderator writes the format).
	Form string `json:"form,omitempty"`
	// ModTier is the moderator's tier for refine light or live (C4 E3);
	// the agent's own refining (refine on) is always quick.
	ModTier string `json:"modTier,omitempty"`
}

// writerLabel names who wrote a run's combined answers and how: the
// writer, the tier whose model wrote and the prompt.
func (r RunInfo) writerLabel() string {
	tier, prompt := r.Tier, "product"
	if r.SynthTier != "" {
		tier = r.SynthTier
	}
	if r.Synth != "" {
		prompt = r.Synth
	}
	return r.Writer + " · " + tier + " · " + prompt
}

// RefineRec is the question's refining (C2): the moderator, its time,
// the spec and the template every AI got.
type RefineRec struct {
	Status   string `json:"status"`
	Agent    string `json:"agent,omitempty"`
	Model    string `json:"model,omitempty"`
	Ms       int64  `json:"ms,omitempty"`
	Kind     string `json:"kind,omitempty"`
	Depth    string `json:"depth,omitempty"`
	Language string `json:"language,omitempty"`
	Intent   string `json:"intent,omitempty"`
	Template string `json:"template,omitempty"`
	// Form are the format lines a live moderator wrote (refine -form live).
	Form []string `json:"form,omitempty"`
	// Before is set when the question was refined before any AI was asked
	// (refine light or live, C4 E3): the answers' times then hold none of
	// it. The agent's refining runs inside each AI's part.
	Before bool   `json:"before,omitempty"`
	Error  string `json:"error,omitempty"`
	Detail string `json:"detail,omitempty"`
}

// AnswerRec is one AI's answer.
type AnswerRec struct {
	Agent  string `json:"agent"`
	Model  string `json:"model,omitempty"`
	Status string `json:"status"`
	Ms     int64  `json:"ms,omitempty"`
	In     int    `json:"inputTokens,omitempty"`
	Out    int    `json:"outputTokens,omitempty"`
	Text   string `json:"text,omitempty"`
	Error  string `json:"error,omitempty"`  // code
	Detail string `json:"detail,omitempty"` // the AI's message, at most 300 characters
}

// CompareRec is the comparison: its state, how long the judges took after
// the last answer, the cards' counts and the judge requests.
type CompareRec struct {
	Status   string       `json:"status"`
	Ms       int64        `json:"ms"`
	Counts   judge.Counts `json:"counts"`
	Requests int          `json:"requests"`
	Failed   int          `json:"failed"`
	Error    string       `json:"error,omitempty"`
	Detail   string       `json:"detail,omitempty"`
}

// SynthRec is the combined answer.
type SynthRec struct {
	Status  string `json:"status"`
	Agent   string `json:"agent,omitempty"`
	Model   string `json:"model,omitempty"`
	Ms      int64  `json:"ms,omitempty"`
	In      int    `json:"inputTokens,omitempty"`
	Out     int    `json:"outputTokens,omitempty"`
	Judged  bool   `json:"judged"`
	Unnamed int    `json:"unnamedSides,omitempty"`
	Text    string `json:"text,omitempty"`
	Error   string `json:"error,omitempty"`
	Detail  string `json:"detail,omitempty"`
}

var runNameRe = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,63}$`)

// rootDir is where runs live; tests replace it.
var rootDir = func() (string, error) {
	base := os.Getenv("LOCALAPPDATA")
	if base == "" {
		return "", errors.New("LOCALAPPDATA is not set")
	}
	return filepath.Join(base, "Asksemble", "synth-eval"), nil
}

// runDir is the folder of the run name.
func runDir(name string) (string, error) {
	if !runNameRe.MatchString(name) {
		return "", fmt.Errorf("run name %q: use a-z, 0-9 and -, up to 64", name)
	}
	root, err := rootDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(root, name), nil
}

func recordFile(dir, id string) string { return filepath.Join(dir, "q-"+id+".json") }

// saveRecord writes a record whole: to a temporary file, then renamed.
func saveRecord(dir string, r Record) error {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	b, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return err
	}
	tmp := recordFile(dir, r.Question.ID) + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, recordFile(dir, r.Question.ID))
}

// loadRecords reads a run's records in the run's order.
func loadRecords(dir string) ([]Record, error) {
	names, err := filepath.Glob(filepath.Join(dir, "q-*.json"))
	if err != nil {
		return nil, err
	}
	var out []Record
	for _, n := range names {
		b, err := os.ReadFile(n)
		if err != nil {
			return nil, err
		}
		var r Record
		if err := json.Unmarshal(b, &r); err != nil {
			return nil, fmt.Errorf("%s: %w", filepath.Base(n), err)
		}
		if r.V != 1 {
			return nil, fmt.Errorf("%s: record version %d", filepath.Base(n), r.V)
		}
		out = append(out, r)
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("no records in %s", dir)
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Seq < out[j].Seq })
	return out, nil
}

// done reports whether the run already has the question, run through.
func done(dir, id string) bool {
	b, err := os.ReadFile(recordFile(dir, id))
	if err != nil {
		return false
	}
	var r Record
	return json.Unmarshal(b, &r) == nil && r.V == 1 && r.Stop == ""
}

// clip keeps at most n characters of s.
func clip(s string, n int) string {
	if r := []rune(strings.TrimSpace(s)); len(r) > n {
		return string(r[:n])
	}
	return strings.TrimSpace(s)
}
