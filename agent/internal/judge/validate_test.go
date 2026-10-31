package judge

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"slices"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/asksemble/asksemble/agent/internal/core"
)

// validTask is a task with units a1..a3 to judge and b1..b4 to cite, each
// with a context line among them.
func validTask() core.JudgeTask {
	return core.JudgeTask{
		Question: "q",
		Units:    []core.JudgeUnit{{Context: "## h"}, {ID: "a1", Text: "x"}, {ID: "a2", Text: "y"}, {ID: "a3", Kind: UnitRow, Text: "z", Context: "c"}},
		Other:    []core.JudgeUnit{{ID: "b1", Text: "x"}, {Context: "## h"}, {ID: "b2", Text: "y"}, {ID: "b3", Text: "z"}, {ID: "b4", Text: "w"}},
		Shape:    ShapeDirected,
		Labels:   LabelsV1,
	}
}

// validJ writes one entry: core and unsure as given, evidence as a JSON
// array.
func validJ(id string, core bool, evidence, label string, unsure bool) string {
	return fmt.Sprintf(`{"id":%q,"core":%v,"evidence":%s,"reason":"compared the years","label":%q,"unsure":%v}`, id, core, evidence, label, unsure)
}

// validOut wraps entries as a judge's object.
func validOut(entries ...string) string {
	return `{"judgments":[` + strings.Join(entries, ",") + `]}`
}

// validV is a verdict with the test reason.
func validV(id string, core bool, label string, unsure bool, evidence ...string) Verdict {
	if evidence == nil {
		evidence = []string{}
	}
	return Verdict{ID: id, Core: core, Evidence: evidence, Label: label, Unsure: unsure, Reason: "compared the years"}
}

var (
	validGood = []string{
		validJ("a1", true, `["b2"]`, "same", false),
		validJ("a2", true, `["b1","b3"]`, "opposite", true),
		validJ("a3", false, `[]`, "absent", false),
	}
	validGoodVerdicts = []Verdict{
		validV("a1", true, LabelSame, false, "b2"),
		validV("a2", true, LabelOpposite, true, "b1", "b3"),
		validV("a3", false, LabelAbsent, false),
	}
)

func TestExtractJSON(t *testing.T) {
	obj := validOut(validGood...)
	for _, tc := range []struct {
		name, out, want string // want "" for none
	}{
		{"only the object", obj, obj},
		{"prose around", "Here are the judgments:\n" + obj + "\nHope this helps {really}.", obj},
		{"json fence", "```json\n" + obj + "\n```", obj},
		{"JSON fence with braces before it", "I judged {a1} and {a2}:\n  ```JSON  \r\n" + obj + "\r\n```\n", obj},
		{"plain fence", "```\n" + obj + "\n```", obj},
		{"fence holding no object", `{"a":1}` + "\n```json\nnothing here\n```", `{"a":1}`},
		{"braces and quotes in strings", `{"r":"a } b { c \" } \\","s":"{"}`, `{"r":"a } b { c \" } \\","s":"{"}`},
		{"a backslash ending a string", `{"r":"\\"} {"s":1}`, `{"r":"\\"}`},
		{"stray brace before", "Note: { is open. " + obj, obj},
		{"stray closing brace before", "} oops " + obj, obj},
		{"two objects", `{"a":1} {"b":2}`, `{"a":1}`},
		{"truncated: the earliest whole object", `{"judgments":[{"id":"a1"},{"id":"a2"`, `{"id":"a1"}`},
		{"a fence in a string is no fence", `{"r":"see` + "```json" + `"} {"x":1}`, `{"r":"see` + "```json" + `"}`},
		{"no object", "I cannot judge these answers.", ""},
		{"never closed", `{"judgments":[`, ""},
		{"empty", "", ""},
		{"an array of objects", `[{"id":"a1"}]`, `{"id":"a1"}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := ExtractJSON([]byte(tc.out))
			if ok != (tc.want != "") || string(got) != tc.want {
				t.Errorf("ExtractJSON = %q, %v; want %q", got, ok, tc.want)
			}
		})
	}

	// 1 MiB is the most a judge may write.
	pad := strings.Repeat(" ", MaxOutputBytes-len(obj))
	if got, ok := ExtractJSON([]byte(pad + obj)); !ok || string(got) != obj {
		t.Errorf("1 MiB: %v", ok)
	}
	if _, ok := ExtractJSON([]byte(pad + obj + " ")); ok {
		t.Errorf("over 1 MiB was read")
	}
	// A long run of open braces is read in one pass.
	if _, ok := ExtractJSON(bytes.Repeat([]byte("{"), MaxOutputBytes)); ok {
		t.Errorf("open braces gave an object")
	}
}

func TestValidate(t *testing.T) {
	g := validGood
	for _, tc := range []struct {
		name     string
		out      string
		verdicts []Verdict // nil: validGoodVerdicts without the missing ones
		missing  []string
		dropped  int
	}{
		{name: "all good", out: validOut(g...)},
		{name: "in a fence among prose", out: "Sure.\n```json\n" + validOut(g...) + "\n```\nDone."},
		{name: "out of order", out: validOut(g[2], g[0], g[1])},
		{name: "pretty-printed", out: "{\n  \"judgments\" : [\n    " + strings.Join(g, ",\n    ") + "\n  ]\n}\n"},
		{name: "escaped id", out: validOut(strings.Replace(g[0], `"a1"`, `"\u0061\u0031"`, 1), g[1], g[2])},
		// Keys are read exactly, as encoding/json into a struct would not:
		// it matches them in any case and keeps the last of two.
		{name: "a key in capitals", out: validOut(strings.Replace(g[0], `"id"`, `"ID"`, 1), g[1], g[2]), missing: []string{"a1"}, dropped: 1},
		{name: "an extra key in capitals", out: validOut(strings.Replace(g[0], `"label":"same"`, `"label":"same","Label":"opposite"`, 1), g[1], g[2]), missing: []string{"a1"}, dropped: 1},
		{name: "a key twice, once escaped", out: validOut(strings.Replace(g[0], `"label":"same"`, `"label":"same","\u006cabel":"opposite"`, 1), g[1], g[2]), missing: []string{"a1"}, dropped: 1},
		{name: "a missing id", out: validOut(g[0], g[2]), missing: []string{"a2"}},
		{name: "no judgments", out: `{"judgments":[]}`, verdicts: []Verdict{}, missing: []string{"a1", "a2", "a3"}},
		{name: "ids not sent", out: validOut(append(slices.Clone(g), validJ("a4", true, `[]`, "absent", false), validJ("b1", true, `[]`, "absent", false), validJ(" a1", true, `[]`, "absent", false), validJ("A1", true, `[]`, "absent", false))...), dropped: 4},
		{name: "a context line is no id", out: validOut(append(slices.Clone(g), validJ("", true, `[]`, "absent", false))...), dropped: 1},
		{name: "an id twice, both the same", out: validOut(g[0], g[1], g[2], g[0]), missing: []string{"a1"}, dropped: 2},
		{name: "an id twice, one not valid", out: validOut(g[0], g[1], g[2], strings.Replace(g[0], `"same"`, `"maybe"`, 1)), missing: []string{"a1"}, dropped: 2},
		{name: "label in capitals", out: validOut(strings.Replace(g[0], `"same"`, `"Same"`, 1), g[1], g[2]), missing: []string{"a1"}, dropped: 1},
		{name: "label unclear is not v1", out: validOut(strings.Replace(g[0], `"same"`, `"unclear"`, 1), g[1], g[2]), missing: []string{"a1"}, dropped: 1},
		{name: "core as a string", out: validOut(strings.Replace(g[0], `"core":true`, `"core":"true"`, 1), g[1], g[2]), missing: []string{"a1"}, dropped: 1},
		{name: "core as a number", out: validOut(strings.Replace(g[0], `"core":true`, `"core":1`, 1), g[1], g[2]), missing: []string{"a1"}, dropped: 1},
		{name: "unsure null", out: validOut(strings.Replace(g[0], `"unsure":false`, `"unsure":null`, 1), g[1], g[2]), missing: []string{"a1"}, dropped: 1},
		{name: "evidence null", out: validOut(strings.Replace(g[0], `["b2"]`, `null`, 1), g[1], g[2]), missing: []string{"a1"}, dropped: 1},
		{name: "evidence a string", out: validOut(strings.Replace(g[0], `["b2"]`, `"b2"`, 1), g[1], g[2]), missing: []string{"a1"}, dropped: 1},
		{name: "evidence of numbers", out: validOut(strings.Replace(g[0], `["b2"]`, `[2]`, 1), g[1], g[2]), missing: []string{"a1"}, dropped: 1},
		{name: "reason a number", out: validOut(strings.Replace(g[0], `"compared the years"`, `7`, 1), g[1], g[2]), missing: []string{"a1"}, dropped: 1},
		{name: "id a number", out: validOut(strings.Replace(g[0], `"a1"`, `1`, 1), g[1], g[2]), missing: []string{"a1"}, dropped: 1},
		{name: "an unknown field", out: validOut(strings.Replace(g[0], `{"id"`, `{"confidence":0.9,"id"`, 1), g[1], g[2]), missing: []string{"a1"}, dropped: 1},
		{name: "a field twice", out: validOut(strings.Replace(g[0], `"label":"same"`, `"label":"same","label":"opposite"`, 1), g[1], g[2]), missing: []string{"a1"}, dropped: 1},
		{name: "two ids in one entry count for neither", out: validOut(strings.Replace(g[0], `"core"`, `"id":"a2","core"`, 1), g[1], g[2]), missing: []string{"a1"}, dropped: 1},
		{name: "no reason", out: validOut(strings.Replace(g[0], `"reason":"compared the years",`, ``, 1), g[1], g[2]), missing: []string{"a1"}, dropped: 1},
		{name: "entries not objects", out: validOut(append([]string{`"a1"`, `1`, `null`, `[]`, `true`}, g...)...), dropped: 5},
		{
			name: "evidence: the other answer's, once, the first three",
			out:  validOut(strings.Replace(g[0], `["b2"]`, `["b2","b2","a1","b9","B1","b1"," b3","b3","b4"]`, 1), g[1], g[2]),
			verdicts: []Verdict{
				validV("a1", true, LabelSame, false, "b2", "b1", "b3"), validGoodVerdicts[1], validGoodVerdicts[2],
			},
			dropped: 6,
		},
		{
			name:     "absent keeps no evidence",
			out:      validOut(g[0], g[1], strings.Replace(g[2], `[]`, `["b1","b2"]`, 1)),
			verdicts: validGoodVerdicts,
			dropped:  2,
		},
		{
			name: "opposite without evidence stays",
			out:  validOut(g[0], strings.Replace(g[1], `["b1","b3"]`, `[]`, 1), g[2]),
			verdicts: []Verdict{
				validGoodVerdicts[0], validV("a2", true, LabelOpposite, true), validGoodVerdicts[2],
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			v, err := Validate(validTask(), []byte(tc.out))
			if err != nil {
				t.Fatal(err)
			}
			wantV := tc.verdicts
			if wantV == nil {
				for _, x := range validGoodVerdicts {
					if !slices.Contains(tc.missing, x.ID) {
						wantV = append(wantV, x)
					}
				}
			}
			wantM := tc.missing
			if wantM == nil {
				wantM = []string{}
			}
			if !reflect.DeepEqual(v.Verdicts, wantV) || !reflect.DeepEqual(v.Missing, wantM) || v.Dropped != tc.dropped {
				t.Errorf("Validate =\n%+v\nwant verdicts\n%+v\nmissing %q, dropped %d", v, wantV, wantM, tc.dropped)
			}
			validCheck(t, validTask(), v)
		})
	}

	t.Run("reason cut to 400 characters", func(t *testing.T) {
		long := strings.Repeat("이유", 300)
		out := validOut(strings.Replace(g[0], "compared the years", long, 1), g[1], g[2])
		v, err := Validate(validTask(), []byte(out))
		if err != nil || len(v.Verdicts) != 3 || v.Verdicts[0].Reason != long[:len("이")*400] {
			t.Errorf("reason %q, %v", v.Verdicts[0].Reason, err)
		}
	})

	t.Run("nothing sent, nothing asked", func(t *testing.T) {
		task := validTask()
		task.Units = []core.JudgeUnit{{Context: "only a heading"}}
		v, err := Validate(task, []byte(validOut(g[0])))
		if err != nil || len(v.Verdicts) != 0 || len(v.Missing) != 0 || v.Dropped != 1 {
			t.Errorf("Validate = %+v, %v", v, err)
		}
	})
}

func TestValidateBadOutput(t *testing.T) {
	g := validGood
	deep := `{"judgments":[` + strings.Repeat("[", 20000) + strings.Repeat("]", 20000) + `]}`
	for _, tc := range []struct {
		name, out, msg string
	}{
		{"prose only", "SECRET-MARKER: a1 is the same as b2.", validMsgNoObject},
		{"empty", "", validMsgNoObject},
		{"over 1 MiB", validOut(g...) + strings.Repeat(" SECRET-MARKER", MaxOutputBytes/14+1), validMsgTooLong},
		{"another top field", `{"judgments":[],"note":"SECRET-MARKER"}`, validMsgNotJudge},
		{"judgments twice", `{"judgments":[],"judgments":[]}`, validMsgNotJudge},
		{"judgments in capitals", `{"Judgments":[]}`, validMsgNotJudge},
		{"judgments null", `{"judgments":null}`, validMsgNotJudge},
		{"judgments an object", `{"judgments":{"a1":"same"}}`, validMsgNotJudge},
		{"no judgments", `{"SECRET-MARKER":1}`, validMsgNotJudge},
		{"empty object", `{}`, validMsgNotJudge},
		{"an entry as the object", `[` + g[0] + `]`, validMsgNotJudge},
		{"a trailing comma", `{"judgments":[` + g[0] + `,]}`, validMsgNotJudge},
		{"a comment", `{"judgments":[` + g[0] + ` /* SECRET-MARKER */]}`, validMsgNotJudge},
		{"single quotes", `{'judgments':[]}`, validMsgNotJudge},
		{"truncated", `{"judgments":[` + g[0] + `,{"id":"a2"`, validMsgNotJudge},
		{"nested too deep", deep, validMsgNotJudge},
	} {
		t.Run(tc.name, func(t *testing.T) {
			v, err := Validate(validTask(), []byte(tc.out))
			var je *Error
			if !errors.As(err, &je) || je.Code != core.ErrCodeJudgeBadOutput || je.Message != tc.msg {
				t.Fatalf("Validate = %+v, %#v; want judge_bad_output %q", v, err, tc.msg)
			}
			if err.Error() != core.ErrCodeJudgeBadOutput || strings.Contains(je.Message, "SECRET") || v.Verdicts != nil {
				t.Errorf("error %q, message %q, verdicts %v", err.Error(), je.Message, v.Verdicts)
			}
		})
	}

	// A task of another shape or label set is not this build's.
	for _, change := range []func(*core.JudgeTask){
		func(t *core.JudgeTask) { t.Shape = ShapePair },
		func(t *core.JudgeTask) { t.Labels = LabelsV1Four },
	} {
		task := validTask()
		change(&task)
		var je *Error
		if _, err := Validate(task, []byte(validOut(g...))); !errors.As(err, &je) || je.Code != planCodeInternal {
			t.Errorf("task %s/%s: %v", task.Shape, task.Labels, err)
		}
	}
	// An empty shape and label set are the product's.
	task := validTask()
	task.Shape, task.Labels = "", ""
	if _, err := Validate(task, []byte(validOut(g...))); err != nil {
		t.Errorf("empty shape and labels: %v", err)
	}
}

// TestValidatePromptExample checks that the example the prompt shows is
// output Validate takes.
func TestValidatePromptExample(t *testing.T) {
	i := strings.Index(promptTail, "OUTPUT. ")
	example := promptTail[i:]
	task := core.JudgeTask{Units: []core.JudgeUnit{{ID: "a1", Text: "x"}}, Other: []core.JudgeUnit{{ID: "b1"}, {ID: "b2"}}}
	v, err := Validate(task, []byte(example))
	want := []Verdict{{ID: "a1", Core: true, Evidence: []string{"b2"}, Label: LabelSame, Reason: "…"}}
	if err != nil || !reflect.DeepEqual(v.Verdicts, want) || len(v.Missing) != 0 || v.Dropped != 0 {
		t.Errorf("the prompt's example: %+v, %v", v, err)
	}
}

func TestJudgments(t *testing.T) {
	r := Request{ID: "r3", From: "claude", Against: "opencode", Judge: "codex", Status: RequestRunning}
	v := Validated{Verdicts: []Verdict{
		{ID: "a1", Core: true, Evidence: []string{"b2", "b10"}, Label: LabelSame, Reason: "SECRET reason"},
		{ID: "a12", Label: LabelAbsent, Unsure: true, Evidence: []string{}, Reason: "x"},
		{ID: "b3", Label: LabelSame}, // the pair shape's, not this build's
		{ID: "a01", Label: LabelSame},
	}, Missing: []string{"a2"}, Dropped: 4}
	got := Judgments(r, v)
	want := []Judgment{
		{Unit: "claude:1", Against: "opencode", Request: "r3", Judge: "codex", Core: true, Label: LabelSame, Evidence: []string{"opencode:2", "opencode:10"}, Flags: []string{}},
		{Unit: "claude:12", Against: "opencode", Request: "r3", Judge: "codex", Label: LabelAbsent, Unsure: true, Evidence: []string{}, Flags: []string{}},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Judgments =\n%+v\nwant\n%+v", got, want)
	}
	raw, _ := json.Marshal(got)
	if strings.Contains(string(raw), "SECRET") || strings.Contains(string(raw), "reason") || strings.Contains(string(raw), "null") {
		t.Errorf("JSON = %s", raw)
	}
	if got := Judgments(r, Validated{}); got == nil || len(got) != 0 {
		t.Errorf("no verdicts = %#v", got)
	}

	// From the prompt to judgments: a task built for request r validates a
	// judge's ids back onto r's units.
	s, req := promptSpec()
	task := BuildTask(s, req)
	out := validOut(validJ("a1", true, `["b1"]`, "same", false), validJ("a2", true, `["b2"]`, "opposite", false), validJ("a3", false, `["b2"]`, "same", true), validJ("a4", false, `[]`, "absent", false))
	val, err := Validate(task, []byte(out))
	if err != nil || len(val.Missing) != 0 {
		t.Fatalf("Validate = %+v, %v", val, err)
	}
	js := Judgments(req, val)
	if len(js) != 4 || js[1].Unit != "claude:2" || !slices.Equal(js[1].Evidence, []string{"codex:2"}) || js[1].Label != LabelOpposite || js[3].Unit != "claude:4" {
		t.Errorf("judgments = %+v", js)
	}
}

// validCheck checks the rules every result of Validate keeps.
func validCheck(t *testing.T, task core.JudgeTask, v Validated) {
	t.Helper()
	var sent []string
	for _, u := range task.Units {
		if u.ID != "" {
			sent = append(sent, u.ID)
		}
	}
	other := map[string]bool{}
	for _, u := range task.Other {
		if u.ID != "" {
			other[u.ID] = true
		}
	}
	if v.Verdicts == nil || v.Missing == nil || v.Dropped < 0 {
		t.Fatalf("result %+v", v)
	}
	if len(v.Verdicts)+len(v.Missing) != len(sent) {
		t.Fatalf("%d verdicts and %d missing for %d ids", len(v.Verdicts), len(v.Missing), len(sent))
	}
	vi, mi := 0, 0
	for _, id := range sent {
		switch {
		case vi < len(v.Verdicts) && v.Verdicts[vi].ID == id:
			vi++
		case mi < len(v.Missing) && v.Missing[mi] == id:
			mi++
		default:
			t.Fatalf("id %s is neither the next verdict nor the next missing: %+v", id, v)
		}
	}
	for _, x := range v.Verdicts {
		if !validLabel(x.Label) || len(x.Evidence) > MaxEvidence || x.Evidence == nil {
			t.Errorf("verdict %+v", x)
		}
		if x.Label == LabelAbsent && len(x.Evidence) > 0 {
			t.Errorf("absent with evidence: %+v", x)
		}
		seen := map[string]bool{}
		for _, e := range x.Evidence {
			if !other[e] || seen[e] {
				t.Errorf("evidence %q in %+v", e, x)
			}
			seen[e] = true
		}
		if utf8.RuneCountInString(x.Reason) > validReasonChars || !utf8.ValidString(x.Reason) {
			t.Errorf("reason of %d characters", utf8.RuneCountInString(x.Reason))
		}
	}
}

// FuzzValidate feeds Validate any output: it never panics, an error is
// always judge_bad_output with a fixed message, and what it accepts keeps
// the rules. Writing an accepted result back out gives the same result.
func FuzzValidate(f *testing.F) {
	g := validGood
	for _, seed := range []string{
		validOut(g...),
		"```json\n" + validOut(g...) + "\n```",
		"prose {" + validOut(g[1:]...) + " more",
		validOut(g[0], g[0], strings.Replace(g[1], `"b1"`, `"b9"`, 1)),
		validOut(strings.Replace(g[0], `"core"`, `"x":{"y":[1,{"z":null}]},"core"`, 1)),
		`{"judgments":[{"id":"a1","core":true,"evidence":["b1","b2","b3","b4","b1"],"reason":"\ud800","label":"same","unsure":false}]}`,
		`{"judgments":[{"id":"a1"`,
		`}}}{{{"judgments":[]}`,
		`{"judgments":[],"x":1}`,
		"\xff\xfe{\"judgments\":[\"\\\"}]}",
		``,
	} {
		f.Add([]byte(seed))
	}
	task := validTask()
	messages := []string{validMsgTooLong, validMsgNoObject, validMsgNotJudge}
	f.Fuzz(func(t *testing.T, out []byte) {
		if obj, ok := ExtractJSON(out); ok {
			if len(obj) < 2 || obj[0] != '{' || obj[len(obj)-1] != '}' || !bytes.Contains(out, obj) {
				t.Fatalf("ExtractJSON = %q", obj)
			}
		}
		v, err := Validate(task, out)
		if err != nil {
			var je *Error
			if !errors.As(err, &je) || je.Code != core.ErrCodeJudgeBadOutput || !slices.Contains(messages, je.Message) {
				t.Fatalf("error %#v", err)
			}
			return
		}
		validCheck(t, task, v)

		// Written back out, the result reads the same, with nothing dropped.
		type entry struct {
			ID       string   `json:"id"`
			Core     bool     `json:"core"`
			Evidence []string `json:"evidence"`
			Reason   string   `json:"reason"`
			Label    string   `json:"label"`
			Unsure   bool     `json:"unsure"`
		}
		var back struct {
			Judgments []entry `json:"judgments"`
		}
		back.Judgments = []entry{}
		for _, x := range v.Verdicts {
			back.Judgments = append(back.Judgments, entry{ID: x.ID, Core: x.Core, Evidence: x.Evidence, Reason: x.Reason, Label: x.Label, Unsure: x.Unsure})
		}
		raw, err := json.Marshal(back)
		if err != nil {
			t.Fatal(err)
		}
		again, err := Validate(task, raw)
		if err != nil || !reflect.DeepEqual(again.Verdicts, v.Verdicts) || !reflect.DeepEqual(again.Missing, v.Missing) || again.Dropped != 0 {
			t.Fatalf("written back: %+v, %v\nfirst: %+v", again, err, v)
		}
	})
}
