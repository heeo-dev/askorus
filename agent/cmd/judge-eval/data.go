//go:build judgeeval

package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
)

// The P0-6 set and the eval files of S4 (docs/judge, plan part 1). The
// loader checks what the files claim about themselves: the split covers
// every question once and its counts recompute, the groups follow their
// rule and the evidence names the right pairs. An evidence sentence that
// cannot be mapped to units is no error: fit.go records it.

// Files of the set. The pairs file is named by eval-split.json, the answers
// file by the pairs file.
const (
	fileQuestions = "questions.json"
	fileSplit     = "eval-split.json"
	fileGroups    = "eval-groups.json"
	fileEvidence  = "eval-evidence.json"
)

// ais are the AIs of the P0-6 set, in its answer order: Plan pairs them in
// this order, and every comparison lists its answers so.
var ais = []string{"claude", "codex", "opencode"}

// Split and group names.
const (
	splitDev  = "dev"
	splitTest = "test"
	splitAll  = "all"

	groupHard      = "hard"
	groupBoundary  = "boundary"
	groupClear     = "clear"
	groupSynthetic = "synthetic"
)

type question struct {
	ID    int    `json:"id"`
	Group string `json:"group"`
	Lang  string `json:"lang"`
	Text  string `json:"text"`
	Aim   string `json:"aim"`
}

type answerRec struct {
	Q      int    `json:"id"`
	AI     string `json:"ai"`
	Model  string `json:"model"`
	Answer string `json:"answer"`
}

type claimRec struct {
	Q    int    `json:"q"`
	AI   string `json:"ai"`
	N    int    `json:"n"`
	Text string `json:"text"`
}

type pairRec struct {
	ID      string `json:"id"`
	Q       int    `json:"q"`
	From    string `json:"from"`
	N       int    `json:"n"`
	Against string `json:"against"`
	Draft   string `json:"draft"`
	Doubt   bool   `json:"doubt"`
	Review  bool   `json:"review"`
	Label   string `json:"label"`
	Check   string `json:"check"`
	Hard    bool   `json:"hard"`
}

type splitCounts struct {
	Questions int `json:"questions"`
	English   int `json:"english"`
	Claims    int `json:"claims"`
	Pairs     int `json:"pairs"`
	Opposite  int `json:"opposite"`
	Hard      int `json:"hard"`
}

type splitFile struct {
	Set    string                 `json:"set"`
	Made   string                 `json:"made"`
	Pairs  string                 `json:"pairs"`
	Dev    []int                  `json:"dev"`
	Test   []int                  `json:"test"`
	Counts map[string]splitCounts `json:"counts"`
}

type groupCounts struct {
	Hard     int `json:"hard"`
	Boundary int `json:"boundary"`
	Clear    int `json:"clear"`
}

type groupsFile struct {
	Set      string       `json:"set"`
	Made     string       `json:"made"`
	Pairs    string       `json:"pairs"`
	Counts   *groupCounts `json:"counts"`
	Hard     []string     `json:"hard"`
	Boundary []string     `json:"boundary"`
}

type evidenceEntry struct {
	ID       string   `json:"id"`
	Label    string   `json:"label"`
	Evidence []string `json:"evidence"`
}

// evidenceFile is eval-evidence.json. Its plan format has the key "pairs"
// twice: the pairs file's name, then the entries. UnmarshalJSON reads the
// top level key by key, so either order works, and so does a file that
// names the pairs file under "pairsFile" instead.
type evidenceFile struct {
	Set     string
	Made    string
	Pairs   string // the pairs file
	Answers string
	Entries []evidenceEntry
}

func (e *evidenceFile) UnmarshalJSON(b []byte) error {
	dec := json.NewDecoder(bytes.NewReader(b))
	if t, err := dec.Token(); err != nil || t != json.Delim('{') {
		return errors.New("not a JSON object")
	}
	for dec.More() {
		t, err := dec.Token()
		if err != nil {
			return err
		}
		key, _ := t.(string)
		var raw json.RawMessage
		if err := dec.Decode(&raw); err != nil {
			return err
		}
		var dst any
		switch {
		case key == "set":
			dst = &e.Set
		case key == "made":
			dst = &e.Made
		case key == "answers":
			dst = &e.Answers
		case key == "pairsFile", key == "pairs" && len(raw) > 0 && raw[0] == '"':
			dst = &e.Pairs
		case key == "pairs":
			dst = &e.Entries
		default:
			continue
		}
		if err := json.Unmarshal(raw, dst); err != nil {
			return fmt.Errorf("%s: %w", key, err)
		}
	}
	return nil
}

// dataSet is the loaded set.
type dataSet struct {
	dir       string
	names     map[string]string // logical name (questions, answers, …) → file name
	hashes    map[string]string // file name → sha256, lower-case hex
	questions []question        // in file order
	qByID     map[int]question
	answers   map[string]answerRec // ansKey
	claims    []claimRec
	claimByID map[string]claimRec // claimKey
	pairs     []pairRec
	pairByID  map[string]pairRec
	split     splitFile
	splitOf   map[int]string
	groups    groupsFile
	boundary  map[string]bool
	evidence  evidenceFile
	evByID    map[string]evidenceEntry
}

func ansKey(q int, ai string) string          { return strconv.Itoa(q) + "-" + ai }
func claimKey(q int, ai string, n int) string { return ansKey(q, ai) + strconv.Itoa(n) }
func pairID(q int, from string, n int, against string) string {
	return claimKey(q, from, n) + "-" + against
}

// findData walks up from the working folder to docs/judge/questions.json.
func findData() (string, error) {
	wd, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for d := wd; ; {
		p := filepath.Join(d, "docs", "judge")
		if _, err := os.Stat(filepath.Join(p, fileQuestions)); err == nil {
			return p, nil
		}
		up := filepath.Dir(d)
		if up == d {
			return "", errors.New("docs/judge/questions.json not found above the working folder; give -data")
		}
		d = up
	}
}

// readJSON reads one file of the set and keeps its hash.
func (d *dataSet) readJSON(name string, v any) error {
	if name == "" || filepath.Base(name) != name {
		return fmt.Errorf("data: bad file name %q", name)
	}
	b, err := os.ReadFile(filepath.Join(d.dir, name))
	if err != nil {
		return fmt.Errorf("data: %w", err)
	}
	sum := sha256.Sum256(b)
	d.hashes[name] = hex.EncodeToString(sum[:])
	if err := json.Unmarshal(b, v); err != nil {
		return fmt.Errorf("data: %s: %w", name, err)
	}
	return nil
}

// loadData reads and checks the set in dir.
func loadData(dir string) (*dataSet, error) {
	d := &dataSet{dir: dir, names: map[string]string{}, hashes: map[string]string{}}
	var qf struct {
		Questions []question `json:"questions"`
	}
	if err := d.readJSON(fileQuestions, &qf); err != nil {
		return nil, err
	}
	d.names["questions"] = fileQuestions
	if err := d.readJSON(fileSplit, &d.split); err != nil {
		return nil, err
	}
	d.names["eval-split"] = fileSplit
	var pf struct {
		Answers string     `json:"answers"`
		Claims  []claimRec `json:"claims"`
		Pairs   []pairRec  `json:"pairs"`
	}
	if err := d.readJSON(d.split.Pairs, &pf); err != nil {
		return nil, err
	}
	d.names["pairs"] = d.split.Pairs
	var af struct {
		Answers []answerRec `json:"answers"`
	}
	if err := d.readJSON(pf.Answers, &af); err != nil {
		return nil, err
	}
	d.names["answers"] = pf.Answers
	if err := d.readJSON(fileGroups, &d.groups); err != nil {
		return nil, err
	}
	d.names["eval-groups"] = fileGroups
	if err := d.readJSON(fileEvidence, &d.evidence); err != nil {
		return nil, err
	}
	d.names["eval-evidence"] = fileEvidence

	if err := d.setBase(qf.Questions, af.Answers, pf.Claims, pf.Pairs); err != nil {
		return nil, err
	}
	if d.groups.Pairs != d.split.Pairs || (d.evidence.Pairs != "" && d.evidence.Pairs != d.split.Pairs) ||
		(d.evidence.Answers != "" && d.evidence.Answers != pf.Answers) {
		return nil, errors.New("data: the eval files name different pairs or answers files")
	}
	for _, check := range []func() error{d.checkSplit, d.checkGroups, d.checkEvidence} {
		if err := check(); err != nil {
			return nil, err
		}
	}
	return d, nil
}

// setBase indexes the questions, answers, claims and pairs, and checks
// they fit together.
func (d *dataSet) setBase(qs []question, as []answerRec, cs []claimRec, ps []pairRec) error {
	d.questions, d.qByID = qs, map[int]question{}
	for _, q := range qs {
		if q.ID <= 0 || d.qByID[q.ID].ID != 0 || (q.Lang != "ko" && q.Lang != "en") || strings.TrimSpace(q.Text) == "" {
			return fmt.Errorf("data: question %d is bad or repeats", q.ID)
		}
		d.qByID[q.ID] = q
	}
	d.answers = map[string]answerRec{}
	for _, a := range as {
		k := ansKey(a.Q, a.AI)
		if _, ok := d.qByID[a.Q]; !ok || !slices.Contains(ais, a.AI) || strings.TrimSpace(a.Answer) == "" {
			return fmt.Errorf("data: answer %s is bad", k)
		}
		if _, dup := d.answers[k]; dup {
			return fmt.Errorf("data: answer %s repeats", k)
		}
		d.answers[k] = a
	}
	for _, q := range qs {
		for _, ai := range ais {
			if _, ok := d.answers[ansKey(q.ID, ai)]; !ok {
				return fmt.Errorf("data: no answer %s", ansKey(q.ID, ai))
			}
		}
	}
	d.claims, d.claimByID = cs, map[string]claimRec{}
	for _, c := range cs {
		k := claimKey(c.Q, c.AI, c.N)
		if _, ok := d.answers[ansKey(c.Q, c.AI)]; !ok || c.N <= 0 || strings.TrimSpace(c.Text) == "" {
			return fmt.Errorf("data: claim %s is bad", k)
		}
		if _, dup := d.claimByID[k]; dup {
			return fmt.Errorf("data: claim %s repeats", k)
		}
		d.claimByID[k] = c
	}
	d.pairs, d.pairByID = ps, map[string]pairRec{}
	for _, p := range ps {
		if p.ID != pairID(p.Q, p.From, p.N, p.Against) || p.From == p.Against || !slices.Contains(ais, p.Against) {
			return fmt.Errorf("data: pair %q is bad", p.ID)
		}
		if _, ok := d.claimByID[claimKey(p.Q, p.From, p.N)]; !ok {
			return fmt.Errorf("data: pair %s has no claim", p.ID)
		}
		switch p.Label {
		case "same", "opposite", "absent":
		default:
			return fmt.Errorf("data: pair %s has label %q", p.ID, p.Label)
		}
		if _, dup := d.pairByID[p.ID]; dup {
			return fmt.Errorf("data: pair %s repeats", p.ID)
		}
		d.pairByID[p.ID] = p
	}
	return nil
}

// countSplit recomputes the counts of the questions ids.
func (d *dataSet) countSplit(ids []int) splitCounts {
	in := map[int]bool{}
	var c splitCounts
	for _, q := range ids {
		in[q] = true
		c.Questions++
		if d.qByID[q].Lang == "en" {
			c.English++
		}
	}
	for _, cl := range d.claims {
		if in[cl.Q] {
			c.Claims++
		}
	}
	for _, p := range d.pairs {
		if in[p.Q] {
			c.Pairs++
			if p.Label == "opposite" {
				c.Opposite++
			}
			if p.Hard {
				c.Hard++
			}
		}
	}
	return c
}

func (d *dataSet) checkSplit() error {
	d.splitOf = map[int]string{}
	for name, ids := range map[string][]int{splitDev: d.split.Dev, splitTest: d.split.Test} {
		for _, q := range ids {
			if _, ok := d.qByID[q]; !ok {
				return fmt.Errorf("data: %s: split names question %d, which is not in the set", fileSplit, q)
			}
			if d.splitOf[q] != "" {
				return fmt.Errorf("data: %s: question %d is in the split twice", fileSplit, q)
			}
			d.splitOf[q] = name
		}
	}
	for _, q := range d.questions {
		if d.splitOf[q.ID] == "" {
			return fmt.Errorf("data: %s: question %d is in neither half", fileSplit, q.ID)
		}
	}
	for name, ids := range map[string][]int{splitDev: d.split.Dev, splitTest: d.split.Test} {
		want, ok := d.split.Counts[name]
		if !ok {
			return fmt.Errorf("data: %s: no counts for %s", fileSplit, name)
		}
		if got := d.countSplit(ids); got != want {
			return fmt.Errorf("data: %s: %s counts %+v, the file says %+v", fileSplit, name, got, want)
		}
	}
	return nil
}

// boundaryRule is the boundary group's rule (plan deviation 4): hard, or
// doubt, or review with a label other than same.
func boundaryRule(p pairRec) bool { return p.Hard || p.Doubt || (p.Review && p.Label != "same") }

func (d *dataSet) checkGroups() error {
	var hard, boundary []string
	for _, p := range d.pairs {
		if p.Hard {
			hard = append(hard, p.ID)
		}
		if boundaryRule(p) {
			boundary = append(boundary, p.ID)
		}
	}
	switch {
	case d.groups.Counts == nil:
		return fmt.Errorf("data: %s: no counts", fileGroups)
	case !slices.Equal(d.groups.Hard, hard):
		return fmt.Errorf("data: %s: hard is not the %d pairs with hard true", fileGroups, len(hard))
	case !slices.Equal(d.groups.Boundary, boundary):
		return fmt.Errorf("data: %s: boundary is not the %d pairs of its rule", fileGroups, len(boundary))
	}
	want := groupCounts{Hard: len(hard), Boundary: len(boundary), Clear: len(d.pairs) - len(boundary)}
	if *d.groups.Counts != want {
		return fmt.Errorf("data: %s: counts %+v, the pairs give %+v", fileGroups, *d.groups.Counts, want)
	}
	d.boundary = map[string]bool{}
	for _, id := range boundary {
		d.boundary[id] = true
	}
	return nil
}

// pairGroups are the groups of a natural pair: hard ones are in boundary
// too.
func (d *dataSet) pairGroups(p pairRec) []string {
	switch {
	case p.Hard:
		return []string{groupHard, groupBoundary}
	case d.boundary[p.ID]:
		return []string{groupBoundary}
	}
	return []string{groupClear}
}

// sameEvidencePicks are the same pairs that get evidence gold, one per
// question in question order (plan deviation 6): among the question's
// clear pairs with label same, the one whose id has the smallest SHA-256.
func (d *dataSet) sameEvidencePicks() []string {
	var out []string
	for _, q := range d.questions {
		best, bestSum := "", ""
		for _, p := range d.pairs {
			if p.Q != q.ID || p.Label != "same" || d.boundary[p.ID] {
				continue
			}
			sum := sha256.Sum256([]byte(p.ID))
			if h := hex.EncodeToString(sum[:]); best == "" || h < bestSum {
				best, bestSum = p.ID, h
			}
		}
		if best != "" {
			out = append(out, best)
		}
	}
	return out
}

func (d *dataSet) checkEvidence() error {
	var want []string
	for _, p := range d.pairs {
		if p.Label == "opposite" {
			want = append(want, p.ID)
		}
	}
	want = append(want, d.sameEvidencePicks()...)
	var got []string
	d.evByID = map[string]evidenceEntry{}
	for _, e := range d.evidence.Entries {
		p, ok := d.pairByID[e.ID]
		switch {
		case !ok:
			return fmt.Errorf("data: %s: no pair %q", fileEvidence, e.ID)
		case e.Label != p.Label:
			return fmt.Errorf("data: %s: %s has label %q, the pair %q", fileEvidence, e.ID, e.Label, p.Label)
		case len(e.Evidence) < 1 || len(e.Evidence) > 3:
			return fmt.Errorf("data: %s: %s has %d evidence entries, not 1 to 3", fileEvidence, e.ID, len(e.Evidence))
		}
		for i, s := range e.Evidence {
			if strings.TrimSpace(s) == "" || slices.Contains(e.Evidence[:i], s) {
				return fmt.Errorf("data: %s: %s has an empty or repeated entry", fileEvidence, e.ID)
			}
		}
		if _, dup := d.evByID[e.ID]; dup {
			return fmt.Errorf("data: %s: %s repeats", fileEvidence, e.ID)
		}
		d.evByID[e.ID] = e
		got = append(got, e.ID)
	}
	if !slices.Equal(got, want) {
		return fmt.Errorf("data: %s: %d entries; want the %d opposite pairs in file order, then the %d same picks", fileEvidence, len(got), len(want)-len(d.sameEvidencePicks()), len(d.sameEvidencePicks()))
	}
	return nil
}

// questionsOf are the questions of a split, ascending.
func (d *dataSet) questionsOf(split string) []int {
	var out []int
	for _, q := range d.questions {
		if split == splitAll || d.splitOf[q.ID] == split {
			out = append(out, q.ID)
		}
	}
	slices.Sort(out)
	return out
}

// claimsOf are the claims of answer (q, ai), by n.
func (d *dataSet) claimsOf(q int, ai string) []claimRec {
	var out []claimRec
	for _, c := range d.claims {
		if c.Q == q && c.AI == ai {
			out = append(out, c)
		}
	}
	slices.SortFunc(out, func(a, b claimRec) int { return a.N - b.N })
	return out
}

// fileHashes are the set's files and their hashes, for the manifest.
func (d *dataSet) fileHashes() map[string]string {
	out := map[string]string{}
	for _, name := range d.names {
		out[name] = d.hashes[name]
	}
	return out
}
