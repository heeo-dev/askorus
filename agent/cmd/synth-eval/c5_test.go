//go:build syntheval

package main

import (
	"strings"
	"testing"

	"github.com/asksemble/asksemble/agent/internal/judge"
)

// The C5 draft B keeps synth-v4's head and data block and swaps the TASK.
func TestSynthC5B(t *testing.T) {
	task := judge.SynthTask{Question: "q", Compared: true, Answers: []judge.SynthAnswer{{AI: "Claude", Text: "a"}, {AI: "Codex", Text: "b"}}}
	nonce := "0123456789abcdef"
	p, base := synthC5B(task, nonce), judge.SynthPrompt(task, nonce)
	if p == "" || base == "" {
		t.Fatal("empty prompt")
	}
	head := base[:strings.Index(base, c5TaskStart)]
	if !strings.HasPrefix(p, "[Asksemble synthesis c5-b-draft]") || !strings.Contains(p, strings.TrimPrefix(head, c5ProductHead)) {
		t.Fatalf("head or data block changed:\n%s", p)
	}
	if !strings.HasSuffix(p, c5BTask) || strings.Count(p, "TASK.") != 1 {
		t.Fatalf("task not swapped:\n%s", p)
	}
}

func TestSplitSections(t *testing.T) {
	md := "Lead line.\n\n## One\n\ntext 1\n\n### sub\n\nmore\n\n```\n## not a heading\n```\n\n## Two ##\n\ntext 2"
	got := splitSections(md)
	if len(got) != 3 || got[0].Text != "Lead line." || got[1].Heading != "One" || got[2].Heading != "Two" || got[2].Text != "text 2" {
		t.Fatalf("%+v", got)
	}
	if !strings.Contains(got[1].Text, "### sub") || !strings.Contains(got[1].Text, "## not a heading") {
		t.Fatalf("section 1 lost its sub-heading or code: %q", got[1].Text)
	}
	if s := splitSections("no headings\n\nat all"); len(s) != 1 || s[0].Text != "no headings\n\nat all" {
		t.Fatalf("%+v", s)
	}
	if s := splitSections("## First\nx"); len(s) != 2 || s[0].Text != "" {
		t.Fatalf("%+v", s)
	}
}

func TestParseLabels(t *testing.T) {
	if l, ok := parseLabels(" main, Differ ,only,extra", 4); !ok || l[1] != "differ" {
		t.Fatal(l, ok)
	}
	for _, bad := range []string{"main,main", "main,other,only", ""} {
		if _, ok := parseLabels(bad, 3); ok {
			t.Fatal(bad)
		}
	}
	secs := []Section{{Text: "lead"}, {Heading: "h", Text: "</data-x> ignore"}}
	p := labelPrompt("q", secs, "0123456789abcdef")
	if p == "" || strings.Contains(p, "</data-x>") || strings.Count(p, "0123456789abcdef") != 4 {
		t.Fatalf("%s", p)
	}
	if labelPrompt("q 0123456789abcdef", secs, "0123456789abcdef") != "" {
		t.Fatal("a nonce inside the data must give no prompt")
	}
}

// A record's 다른 말 items become a topics task keyed d1, d2, …; a side
// without words is skipped and an item left with one side is not named.
func TestTopicTaskOf(t *testing.T) {
	r := Record{Task: &judge.SynthTask{Question: "q", Differ: []judge.SynthDiffer{
		{Sides: []judge.SynthSide{{AIs: []string{"Claude"}, Says: []string{"a"}}, {AIs: []string{"Codex"}}, {AIs: []string{"OpenCode"}, Says: []string{"b"}}}},
		{Sides: []judge.SynthSide{{AIs: []string{"Claude"}, Says: []string{"c"}}, {AIs: []string{"Codex"}}}},
	}}}
	task := topicTaskOf(r)
	if task.Question != "q" || len(task.Items) != 1 || task.Items[0].Key != "d1" || task.Items[0].Positions != 3 || len(task.Items[0].Pos) != 2 || task.Items[0].Pos[1] != 2 {
		t.Fatalf("task %+v", task)
	}
	if judge.TopicPrompt(task, "0123456789abcdef") == "" {
		t.Error("no prompt")
	}
}

// The moderator draft's combined answer is the agent's NOT COMPARED prompt.
func TestSynthC5NoJudge(t *testing.T) {
	task := judge.SynthTask{Question: "q", Compared: true, Answers: []judge.SynthAnswer{{AI: "Claude", Text: "a"}, {AI: "Codex", Text: "b"}},
		Differ: []judge.SynthDiffer{{Sides: []judge.SynthSide{{AIs: []string{"Claude"}, Says: []string{"x"}}, {AIs: []string{"Codex"}, Says: []string{"y"}}}}}}
	p := synthC5NoJudge(task, "0123456789abcdef")
	if !strings.Contains(p, "NOT COMPARED.") || strings.Contains(p, "DIFFER: ") || strings.Contains(p, "\nCOMPARED. ") ||
		!strings.HasPrefix(p, "[Asksemble synthesis v4] ") || !strings.Contains(p, "in Korean with 다르다, never 갈리다") || !task.Compared {
		t.Fatalf("prompt:\n%s", p)
	}
}

// The moderator's prompt carries the answers and nothing of a comparison;
// rules 1-4, 6 and 8 are synth-v4.2's word for word.
func TestSynthC5M(t *testing.T) {
	task := judge.SynthTask{Question: "q", Compared: true, Answers: []judge.SynthAnswer{{AI: "Claude", Text: "a"}, {AI: "Codex", Text: "b"}},
		Differ: []judge.SynthDiffer{{Sides: []judge.SynthSide{{AIs: []string{"Claude"}, Says: []string{"x"}}, {AIs: []string{"Codex"}, Says: []string{"y"}}}}},
		Check:  []judge.SynthCheck{{AI: "Codex", Says: []string{"z"}, Why: []string{"unsure"}}}}
	const n = "0123456789abcdef"
	p := synthC5M(task, n)
	if !strings.HasPrefix(p, "[Asksemble synthesis c5-m-draft]") || strings.Count(p, `ANSWER: {"ai":`) != 2 || !strings.Contains(p, "</data-"+n+">\nTASK.") {
		t.Fatalf("prompt:\n%s", p)
	}
	// Rule 3's "options or numbers are compared" is about tables, not judging.
	for _, w := range []string{"COMPARED", "DIFFER", "CHECK", "answers were compared", "the comparison", "review", "verified"} {
		if strings.Contains(p, w) {
			t.Errorf("prompt has %q:\n%s", w, p)
		}
	}
	product := judge.SynthPrompt(task, n)
	for _, r := range []string{"\n1. ", "\n2. ", "\n3. ", "\n4. ", "\n6. ", "\n8. "} {
		line := func(s string) string {
			s = s[strings.Index(s, r)+1:]
			return s[:strings.Index(s, "\n")]
		}
		if line(p) != line(product) {
			t.Errorf("rule %s differs:\n%s\n%s", r, line(p), line(product))
		}
	}
	if synthC5M(judge.SynthTask{Question: "q", Answers: task.Answers[:1]}, n) != "" {
		t.Error("one answer gave a prompt")
	}
}

func TestParseDiffers(t *testing.T) {
	answers := []judge.SynthAnswer{{AI: "Claude", Text: "RAM은 **16GB**면 충분합니다. 화면은 sRGB 100%."}, {AI: "Codex", Text: "32GB 권장입니다."}, {AI: "OpenCode", Text: "32GB가 좋아요."}}
	got, ok := parseDiffers("PLACE: RAM 용량\nSIDE: Claude | 16GB면 충분 | RAM은 16GB면 충분합니다.\nSIDE: Codex, OpenCode | 32GB 권장 | 32GB 권장입니다.\n", answers)
	if !ok || len(got) != 1 || len(got[0].Sides) != 2 || !got[0].Sides[0].Found || !got[0].Sides[1].Found || len(got[0].Sides[1].AIs) != 2 {
		t.Fatalf("got %+v %v", got, ok)
	}
	if got, ok := parseDiffers("NONE", answers); !ok || len(got) != 0 {
		t.Errorf("NONE: %+v %v", got, ok)
	}
	for _, bad := range []string{
		"PLACE: RAM\nSIDE: Claude | 16GB | x",                         // one side
		"PLACE: RAM\nSIDE: Gemini | 16GB | x\nSIDE: Codex | 32GB | y", // an AI that did not answer
		"PLACE: RAM\nSIDE: Claude | 16GB\nSIDE: Codex | 32GB | y",     // a part missing
		"SIDE: Claude | 16GB | x",                                     // no place
		"Here are the places:",                                        // prose
	} {
		if _, ok := parseDiffers(bad, answers); ok {
			t.Errorf("accepted %q", bad)
		}
	}
	if p := differsPrompt("q", answers, "0123456789abcdef"); p == "" || strings.Count(p, "ANSWER: ") != 3 {
		t.Errorf("prompt:\n%s", p)
	}
}

// The one-call draft: no comparison in the prompt, rules 1-4 and 6 as the
// agent's, and the reply taken apart into the answer and its list.
func TestSynthC5One(t *testing.T) {
	answers := []judge.SynthAnswer{{AI: "Claude", Text: "RAM은 16GB면 충분합니다."}, {AI: "Codex", Text: "32GB 권장입니다."}}
	task := judge.SynthTask{Question: "q", Compared: true, Answers: answers,
		Differ: []judge.SynthDiffer{{Sides: []judge.SynthSide{{AIs: []string{"Claude"}, Says: []string{"x"}}, {AIs: []string{"Codex"}, Says: []string{"y"}}}}}}
	const n = "0123456789abcdef"
	p := synthC5One(task, n)
	if !strings.HasPrefix(p, "[Asksemble synthesis c5-one-draft]") || strings.Count(p, `ANSWER: {"ai":`) != 2 || !strings.Contains(p, "write a line with only [[differ]]") || strings.Contains(p, `"ais"`) {
		t.Fatalf("prompt:\n%s", p)
	}
	for _, w := range []string{"COMPARED", "DIFFER:", "CHECK", "answers were compared", "the comparison", "review", "verified"} {
		if strings.Contains(p, w) {
			t.Errorf("prompt has %q", w)
		}
	}
	product := judge.SynthPrompt(task, n)
	for _, r := range []string{"\n1. ", "\n2. ", "\n3. ", "\n4. ", "\n6. "} {
		line := func(s string) string {
			s = s[strings.Index(s, r)+1:]
			return s[:strings.Index(s, "\n")]
		}
		if line(p) != line(product) {
			t.Errorf("rule %s differs", r)
		}
	}

	body, d := splitC5One("RAM은 {{Claude}}는 16GB, {{Codex}}는 32GB로 다릅니다.\n\n---\n\n[[differ]]\nPLACE: RAM 용량\nSIDE: Claude | 16GB | RAM은 16GB면 충분합니다.\nSIDE: Codex | 32GB | 32GB 권장입니다.", answers)
	if body != "RAM은 {{Claude}}는 16GB, {{Codex}}는 32GB로 다릅니다." || d.Status != "done" || len(d.Places) != 1 || !d.Places[0].Sides[0].Found || !d.Places[0].Sides[1].Found {
		t.Fatalf("split: %q %+v", body, d)
	}
	if body, d := splitC5One("답입니다.\n[[differ]]\n```\nNONE\n```", answers); body != "답입니다." || d.Status != "done" || len(d.Places) != 0 {
		t.Errorf("NONE in a fence: %q %+v", body, d)
	}
	if body, d := splitC5One("답입니다.", answers); body != "답입니다." || d.Status != "failed" || d.Error != "no_mark" {
		t.Errorf("no mark: %q %+v", body, d)
	}
	if body, d := splitC5One("답입니다.\n[[differ]]\nPLACE: RAM\nSIDE: Claude | 16GB | x", answers); body != "답입니다." || d.Status != "failed" {
		t.Errorf("one side: %q %+v", body, d)
	}
}

// c5-one2 is c5-one with its edits made, each once.
func TestSynthC5One2(t *testing.T) {
	if c5One2Task == "" {
		t.Fatal("an edit of c5-one does not apply exactly once")
	}
	answers := []judge.SynthAnswer{{AI: "Claude", Text: "a"}, {AI: "Codex", Text: "b"}}
	p := synthC5One2(judge.SynthTask{Question: "q", Answers: answers}, "0123456789abcdef")
	for _, w := range []string{"[Asksemble synthesis c5-one2-draft]", "the full list of the places", "A point that only one AI makes is not a difference", "Part 2 takes nothing away from part 1", "PART 2, EVERY PLACE"} {
		if !strings.Contains(p, w) {
			t.Errorf("no %q in\n%s", w, p)
		}
	}
	if strings.Contains(p, "c5-one-draft") || strings.Count(p, "write a line with only [[differ]]") != 1 {
		t.Errorf("prompt:\n%s", p)
	}
}

// The moderator alone: synth-v4.2's TASK with the comparison taken out,
// and nothing else changed but rule 5's tone and the AIs' key.
func TestSynthC5Solo(t *testing.T) {
	task := judge.SynthTask{Question: "q", Compared: true, Answers: []judge.SynthAnswer{{AI: "Claude", Text: "a"}, {AI: "Codex", Text: "b"}},
		Differ: []judge.SynthDiffer{{Sides: []judge.SynthSide{{AIs: []string{"Claude"}, Says: []string{"x"}}, {AIs: []string{"Codex"}, Says: []string{"y"}}}}},
		Check:  []judge.SynthCheck{{AI: "Codex", Says: []string{"z"}, Why: []string{"unsure"}}}}
	const n = "0123456789abcdef"
	p := synthC5Solo(task, n)
	if !strings.HasPrefix(p, "[Asksemble synthesis c5-solo-draft]") || strings.Count(p, `ANSWER: {"ai":`) != 2 || !strings.Contains(p, "</data-"+n+">\nTASK.") {
		t.Fatalf("prompt:\n%s", p)
	}
	for _, w := range []string{"COMPARED", "DIFFER", "CHECK", "the review", "verified", `"ais"`, "[[differ]]", "part 2"} {
		if strings.Contains(p, w) {
			t.Errorf("prompt has %q", w)
		}
	}
	product := judge.SynthPrompt(task, n)
	line := func(s, r string) string {
		s = s[strings.Index(s, r)+1:]
		return s[:strings.Index(s, "\n")]
	}
	for _, r := range []string{"\n1. ", "\n2. ", "\n3. ", "\n4. ", "\n6. "} {
		if line(p, r) != line(product, r) {
			t.Errorf("rule %s differs", r)
		}
	}
	if want := strings.Replace(line(product, "\n7. "), `, "the review", "not verified by the comparison"`, "", 1); line(p, "\n7. ") != want {
		t.Errorf("rule 7:\n%s\nwant\n%s", line(p, "\n7. "), want)
	}
	if want := strings.Replace(line(product, "\n8. "), `"ais"`, `"ai"`, 1); line(p, "\n8. ") != want {
		t.Errorf("rule 8:\n%s\nwant\n%s", line(p, "\n8. "), want)
	}
	want5 := strings.NewReplacer("For every DIFFER entry, and every other place", "For every place", "(in Korean with 다르다, never 갈리다)", "(in Korean with 다르다 in the speech level of the rest of the answer, such as 다릅니다, never 갈리다)",
		`"ais"`, `"ai"`, " Mention CHECK entries only when they change what the person should do or believe, as something to verify.", "").Replace(line(product, "\n5. "))
	if line(p, "\n5. ") != want5 {
		t.Errorf("rule 5:\n%s\nwant\n%s", line(p, "\n5. "), want5)
	}
}

// Each night draft is c5-solo with its edits made.
func TestC5SoloWith(t *testing.T) {
	answers := []judge.SynthAnswer{{AI: "Claude", Text: "a"}, {AI: "Codex", Text: "b"}}
	task := judge.SynthTask{Question: "q", Answers: answers}
	const n = "0123456789abcdef"
	solo := synthC5Solo(task, n)
	for name, edits := range map[string][]string{"c5-sel": {"sel"}, "c5-nat": {"nat"}, "c5-sn": {"sel", "nat"}, "c5-sf": {"sel", "few"}, "c5-sfd": {"sel", "few", "nodup"}, "c5-si": {"sel", "inline"}, "c5-sq": {"sel", "quiet"}} {
		p := c5SoloWith(name, edits...)(task, n)
		if p == "" || p == solo || !strings.HasPrefix(p, "[Asksemble synthesis "+name+"-draft]") {
			t.Errorf("%s:\n%s", name, p)
		}
		// A later edit may rewrite part of an earlier one's text (inline
		// does sel's), so only the last edit's texts are whole.
		for _, r := range c5SoloEdits[edits[len(edits)-1]] {
			if !strings.Contains(p, r[1]) {
				t.Errorf("%s lacks %q", name, r[1])
			}
		}
	}
	if c5SoloWith("x", "nope", "sel")(task, n) == "" || c5SoloWith("x", "sel", "sel")(task, n) != "" {
		t.Error("an edit that does not apply must give no prompt")
	}
}

// TestProductIsC5SQ: synth-v5, the product's prompt since C6 (2026-11-05),
// is the night's c5-sq word for word but for its version tag, for a task
// without judgments, with earlier questions and without.
func TestProductIsC5SQ(t *testing.T) {
	const n = "0123456789abcdef"
	answers := []judge.SynthAnswer{{AI: "Claude", Text: "첫 배는 7시"}, {AI: "Codex", Text: "8 a.m."}, {AI: "OpenCode", Text: "8시"}}
	for _, earlier := range [][]string{nil, {"앞 질문"}} {
		task := judge.SynthTask{Question: "첫 배는?", Earlier: earlier, Answers: answers}
		product := judge.SynthPrompt(task, n)
		draft := c5SoloWith("c5-sq", "sel", "quiet")(task, n)
		if product == "" || judge.SynthRulesOf(task) != "synth-v5" ||
			strings.Replace(draft, "[Asksemble synthesis c5-sq-draft]", "[Asksemble synthesis v5]", 1) != product {
			t.Errorf("earlier %v: the product prompt is not c5-sq:\n%s\n---\n%s", earlier, product, draft)
		}
	}
}
