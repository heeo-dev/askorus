//go:build syntheval

package main

import (
	"strings"
	"testing"

	"github.com/asksemble/asksemble/agent/internal/judge"
)

// The moderator alone: the product prompt's head and data block with
// c5-solo's TASK after them. Its rules 1-4, 7 and 8 are the product's
// (synth-v5) word for word; synth-v5 is c5-solo with sel and quiet, which
// change only rules 5 and 6 (TestProductIsC5SQ).
func TestSynthC5Solo(t *testing.T) {
	task := judge.SynthTask{Question: "q", Answers: []judge.SynthAnswer{{AI: "Claude", Text: "a"}, {AI: "Codex", Text: "b"}}}
	const n = "0123456789abcdef"
	p := synthC5Solo(task, n)
	product := judge.SynthPrompt(task, n)
	end := "</data-" + n + ">\n"
	if p == "" || product == "" || !strings.HasPrefix(p, "[Asksemble synthesis c5-solo-draft]") || strings.Count(p, `ANSWER: {"ai":`) != 2 ||
		strings.Replace(p, "[Asksemble synthesis c5-solo-draft]", c5ProductHead, 1) != product[:strings.Index(product, end)+len(end)]+c5SoloTask {
		t.Fatalf("prompt:\n%s", p)
	}
	for _, w := range []string{"COMPARED", "DIFFER", "CHECK", "the review", "verified", `"ais"`, "[[differ]]", "part 2"} {
		if strings.Contains(p, w) {
			t.Errorf("prompt has %q", w)
		}
	}
	line := func(s, r string) string {
		s = s[strings.Index(s, r)+1:]
		return s[:strings.Index(s, "\n")]
	}
	for _, r := range []string{"\n1. ", "\n2. ", "\n3. ", "\n4. ", "\n7. ", "\n8. "} {
		if line(p, r) != line(product, r) {
			t.Errorf("rule %s differs:\n%s\n%s", r, line(p, r), line(product, r))
		}
	}
	if synthC5Solo(judge.SynthTask{Question: "q", Answers: task.Answers[:1]}, n) != "" {
		t.Error("one answer gave a prompt")
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
// is the night's c5-sq word for word but for its version tag, with earlier
// questions and without.
func TestProductIsC5SQ(t *testing.T) {
	const n = "0123456789abcdef"
	answers := []judge.SynthAnswer{{AI: "Claude", Text: "첫 배는 7시"}, {AI: "Codex", Text: "8 a.m."}, {AI: "OpenCode", Text: "8시"}}
	for _, earlier := range [][]string{nil, {"앞 질문"}} {
		task := judge.SynthTask{Question: "첫 배는?", Earlier: earlier, Answers: answers}
		product := judge.SynthPrompt(task, n)
		draft := c5SoloWith("c5-sq", "sel", "quiet")(task, n)
		if product == "" || judge.SynthRules != "synth-v5" ||
			strings.Replace(draft, "[Asksemble synthesis c5-sq-draft]", "[Asksemble synthesis v5]", 1) != product {
			t.Errorf("earlier %v: the product prompt is not c5-sq:\n%s\n---\n%s", earlier, product, draft)
		}
	}
}
