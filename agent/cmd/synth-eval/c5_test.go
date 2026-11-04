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
