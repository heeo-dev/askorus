package judge

import (
	"bytes"
	"encoding/json"
	"io"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/asksemble/asksemble/agent/internal/core"
)

// Checking a judge's output (spec part 4, 검사). The output is untrusted:
// it is read strictly, only ids, fixed words and booleans leave this
// file, and no error quotes it.

// validFields are the fields of one judgment, in the order of
// core.JudgeSchemaV1. Each is required and no other is allowed.
var validFields = []string{"id", "core", "evidence", "reason", "label", "unsure"}

// validLabels is the label set v1.
var validLabels = []string{LabelSame, LabelOpposite, LabelAbsent}

// validReasonChars bounds the reason kept for judge-eval. The prompt asks
// for at most 20 words.
const validReasonChars = 400

// Fixed messages of judge_bad_output. They never quote the output.
const (
	validMsgTooLong  = "the judge's output is over 1 MiB"
	validMsgNoObject = "the judge's output holds no JSON object"
	validMsgNotJudge = `the judge's JSON is not {"judgments": [...]}`
)

// ExtractJSON finds the judge's JSON object in its output. When a line
// opens a ```json fence it reads from after that line, else from the
// start (or again from the start when the fence holds no object). It
// returns the first balanced top-level {…}, reading strings as strings:
// the span of the first "{" whose "}" comes, or, when a stray "{" never
// closes, the earliest object that does. obj is a part of out. ok is false
// when there is none, or when out is over MaxOutputBytes.
func ExtractJSON(out []byte) (obj []byte, ok bool) {
	if len(out) > MaxOutputBytes {
		return nil, false
	}
	from := validFence(out)
	start, end, ok := validObject(out, from)
	if !ok && from > 0 {
		start, end, ok = validObject(out, 0)
	}
	if !ok {
		return nil, false
	}
	return out[start:end], true
}

// validFence is where the text after the first ```json line starts, or 0.
// The fence must open its line, so a fence inside a JSON string, which
// cannot hold a line break, does not count.
func validFence(out []byte) int {
	for ls := 0; ls < len(out); {
		le := bytes.IndexByte(out[ls:], '\n')
		next := len(out)
		if le >= 0 {
			le += ls
			next = le + 1
		} else {
			le = len(out)
		}
		line := bytes.TrimSpace(out[ls:le])
		if len(line) == len("```json") && bytes.HasPrefix(line, []byte("```")) && bytes.EqualFold(line[3:], []byte("json")) {
			return next
		}
		ls = next
	}
	return 0
}

// validObject scans out from from for a balanced {…}, in one pass.
func validObject(out []byte, from int) (start, end int, ok bool) {
	var open []int // the "{" not closed yet
	inString, escaped := false, false
	start = -1
	for i := from; i < len(out); i++ {
		c := out[i]
		if inString {
			switch {
			case escaped:
				escaped = false
			case c == '\\':
				escaped = true
			case c == '"':
				inString = false
			}
			continue
		}
		switch c {
		case '"':
			inString = len(open) > 0 // quotes in prose around the object are prose
		case '{':
			open = append(open, i)
		case '}':
			if len(open) == 0 {
				continue
			}
			s := open[len(open)-1]
			open = open[:len(open)-1]
			if len(open) == 0 {
				return s, i + 1, true
			}
			if start < 0 || s < start {
				start, end = s, i+1
			}
		}
	}
	return start, end, start >= 0
}

// Validate checks a judge's output against the task it answers (part 4,
// 검사). The object ExtractJSON finds must be {"judgments": [...]}, nothing
// else, and every entry {id, core, evidence, reason, label, unsure}: each
// field once, no other field, booleans, strings, an array of strings for
// evidence and a label of v1 (same, opposite or absent). Each id sent must
// come back in exactly one entry, and a valid one; otherwise it is
// Missing and its entries are dropped, as are entries with ids that were
// not sent. Evidence keeps the other answer's ids, each once, the first
// MaxEvidence; an absent label keeps none. Reason is cut to 400
// characters and stays in Verdict for judge-eval; Judgments leaves it
// behind.
//
// Output that cannot be read at all, or is over MaxOutputBytes, gives an
// *Error with code judge_bad_output. Its message is fixed and never quotes
// the output. Missing ids are no error: the caller asks once more with a
// new nonce. A task of a shape or label set this build does not have
// gives an *Error with code internal.
func Validate(task core.JudgeTask, out []byte) (Validated, error) {
	if !planBuilt(task.Shape, task.Labels) {
		return Validated{}, &Error{Code: planCodeInternal, Message: "this build checks only shape directed with labels v1"}
	}
	if len(out) > MaxOutputBytes {
		return Validated{}, &Error{Code: core.ErrCodeJudgeBadOutput, Message: validMsgTooLong}
	}
	obj, ok := ExtractJSON(out)
	if !ok {
		return Validated{}, &Error{Code: core.ErrCodeJudgeBadOutput, Message: validMsgNoObject}
	}
	entries, ok := validEntries(obj)
	if !ok {
		return Validated{}, &Error{Code: core.ErrCodeJudgeBadOutput, Message: validMsgNotJudge}
	}

	var sent []string
	index := map[string]int{}
	for _, u := range task.Units {
		if _, dup := index[u.ID]; u.ID != "" && !dup {
			index[u.ID] = len(sent)
			sent = append(sent, u.ID)
		}
	}
	other := map[string]bool{}
	for _, u := range task.Other {
		if u.ID != "" {
			other[u.ID] = true
		}
	}

	type found struct {
		n      int // entries with the id
		valid  bool
		v      Verdict
		denied int // evidence ids dropped from v
	}
	got := make([]found, len(sent))
	v := Validated{Verdicts: []Verdict{}, Missing: []string{}}
	for _, raw := range entries {
		e, id, valid, denied := validEntry(raw, other)
		k, ok := index[id]
		if !ok {
			v.Dropped++
			continue
		}
		got[k].n++
		got[k].valid, got[k].v, got[k].denied = valid, e, denied
	}
	for k, id := range sent {
		g := got[k]
		if g.n == 1 && g.valid {
			v.Verdicts = append(v.Verdicts, g.v)
			v.Dropped += g.denied
			continue
		}
		v.Missing = append(v.Missing, id)
		v.Dropped += g.n
	}
	return v, nil
}

// validEntries reads the top object, {"judgments": [...]} with nothing
// else, and returns the entries as they are.
func validEntries(obj []byte) ([]json.RawMessage, bool) {
	dec := json.NewDecoder(bytes.NewReader(obj))
	if tok, err := dec.Token(); err != nil || tok != json.Delim('{') {
		return nil, false
	}
	entries := []json.RawMessage{}
	seen := false
	for dec.More() {
		tok, err := dec.Token()
		if err != nil || tok != "judgments" || seen {
			return nil, false
		}
		seen = true
		if tok, err := dec.Token(); err != nil || tok != json.Delim('[') {
			return nil, false
		}
		for dec.More() {
			var raw json.RawMessage
			if err := dec.Decode(&raw); err != nil {
				return nil, false
			}
			entries = append(entries, raw)
		}
		if tok, err := dec.Token(); err != nil || tok != json.Delim(']') {
			return nil, false
		}
	}
	if tok, err := dec.Token(); err != nil || tok != json.Delim('}') {
		return nil, false
	}
	if _, err := dec.Token(); err != io.EOF {
		return nil, false
	}
	return entries, seen
}

// validEntry reads one entry. id is its id when it has one string id, so
// even an entry that is not valid counts against the id it names. denied
// counts the evidence ids dropped.
func validEntry(raw json.RawMessage, other map[string]bool) (v Verdict, id string, valid bool, denied int) {
	dec := json.NewDecoder(bytes.NewReader(raw))
	if tok, err := dec.Token(); err != nil || tok != json.Delim('{') {
		return Verdict{}, "", false, 0
	}
	valid = true
	have := map[string]bool{}
	var evidence []string
	for dec.More() {
		tok, err := dec.Token()
		key, isKey := tok.(string)
		var val json.RawMessage
		if err != nil || !isKey || dec.Decode(&val) != nil {
			return Verdict{}, "", false, 0
		}
		if have[key] {
			valid = false
			if key == "id" {
				id = ""
			}
			continue
		}
		have[key] = true
		ok := true
		switch key {
		case "id":
			if s, isString := validString(val); isString {
				id, v.ID = s, s
			} else {
				ok = false
			}
		case "core":
			v.Core, ok = validBool(val)
		case "unsure":
			v.Unsure, ok = validBool(val)
		case "reason":
			v.Reason, ok = validString(val)
			v.Reason = validClip(v.Reason, validReasonChars)
		case "label":
			v.Label, ok = validString(val)
			ok = ok && validLabel(v.Label)
		case "evidence":
			evidence, ok = validStrings(val)
		default:
			ok = false
		}
		valid = valid && ok
	}
	if tok, err := dec.Token(); err != nil || tok != json.Delim('}') {
		return Verdict{}, "", false, 0
	}
	for _, f := range validFields {
		valid = valid && have[f]
	}
	if !valid {
		return Verdict{}, id, false, 0
	}
	v.Evidence = []string{}
	seen := map[string]bool{}
	for _, e := range evidence {
		if !other[e] || seen[e] || len(v.Evidence) == MaxEvidence || v.Label == LabelAbsent {
			denied++
			continue
		}
		seen[e] = true
		v.Evidence = append(v.Evidence, e)
	}
	return v, id, true, denied
}

// validString reads a JSON string.
func validString(val json.RawMessage) (string, bool) {
	var s string
	if len(val) == 0 || val[0] != '"' || json.Unmarshal(val, &s) != nil {
		return "", false
	}
	return s, true
}

// validBool reads true or false.
func validBool(val json.RawMessage) (bool, bool) {
	switch string(val) {
	case "true":
		return true, true
	case "false":
		return false, true
	}
	return false, false
}

// validStrings reads an array of strings; null is not one.
func validStrings(val json.RawMessage) ([]string, bool) {
	var items []json.RawMessage
	if len(val) == 0 || val[0] != '[' || json.Unmarshal(val, &items) != nil {
		return nil, false
	}
	out := make([]string, 0, len(items))
	for _, it := range items {
		s, ok := validString(it)
		if !ok {
			return nil, false
		}
		out = append(out, s)
	}
	return out, true
}

// validLabel reports whether l is a label of v1.
func validLabel(l string) bool {
	for _, x := range validLabels {
		if l == x {
			return true
		}
	}
	return false
}

// validClip cuts s to n characters.
func validClip(s string, n int) string {
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	i := 0
	for k := 0; k < n; k++ {
		_, size := utf8.DecodeRuneInString(s[i:])
		i += size
	}
	return s[:i]
}

// Judgments turns the verdicts of request r into judgments on unit ids:
// a<n> is unit n of r.From and b<n> unit n of r.Against, so "<From>:<n>"
// and "<Against>:<n>". Reason stays behind: no judge's words leave the
// agent. Evidence and Flags are empty, not nil; Aggregate sets the flags.
func Judgments(r Request, v Validated) []Judgment {
	out := []Judgment{}
	for _, vd := range v.Verdicts {
		n, ok := validIndex(vd.ID, "a")
		if !ok {
			continue
		}
		evidence := []string{}
		for _, e := range vd.Evidence {
			if m, ok := validIndex(e, "b"); ok {
				evidence = append(evidence, r.Against+":"+strconv.Itoa(m))
			}
		}
		out = append(out, Judgment{
			Unit:     r.From + ":" + strconv.Itoa(n),
			Against:  r.Against,
			Request:  r.ID,
			Judge:    r.Judge,
			Core:     vd.Core,
			Label:    vd.Label,
			Evidence: evidence,
			Unsure:   vd.Unsure,
			Flags:    []string{},
		})
	}
	return out
}

// validIndex reads the n of a task id prefix<n>, as BuildTask writes it.
func validIndex(id, prefix string) (int, bool) {
	digits, ok := strings.CutPrefix(id, prefix)
	if !ok {
		return 0, false
	}
	n, err := strconv.Atoi(digits)
	if err != nil || n < 1 || strconv.Itoa(n) != digits {
		return 0, false
	}
	return n, true
}
