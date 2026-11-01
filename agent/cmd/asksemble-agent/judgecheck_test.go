package main

import (
	"context"
	"slices"
	"strings"
	"testing"

	"github.com/asksemble/asksemble/agent/internal/compares"
	"github.com/asksemble/asksemble/agent/internal/config"
	"github.com/asksemble/asksemble/agent/internal/core"
)

// lineTexts joins each line's level and both languages, for searching.
func lineTexts(lines []checkLine) []string {
	var out []string
	for _, l := range lines {
		out = append(out, l.level+" | "+l.text.en+" | "+l.text.ko)
	}
	return out
}

// findLine is the line whose English text has en, and whose Korean text
// has ko.
func findLine(t *testing.T, lines []checkLine, level, en, ko string) {
	t.Helper()
	for _, l := range lines {
		if strings.Contains(l.text.en, en) && strings.Contains(l.text.ko, ko) {
			if l.level != level {
				t.Errorf("%q is %s, want %s", en, l.level, level)
			}
			return
		}
	}
	t.Errorf("no line with %q / %q in\n%s", en, ko, strings.Join(lineTexts(lines), "\n"))
}

// noLineTwice: no English or Korean text repeats.
func noLineTwice(t *testing.T, lines []checkLine) {
	t.Helper()
	seen := map[string]bool{}
	for _, l := range lines {
		for _, s := range []string{l.text.en, l.text.ko} {
			if s == "" || seen[s] {
				t.Errorf("line %q is empty or twice", s)
			}
			seen[s] = true
		}
	}
}

const (
	noOutside   = "the agent itself sends no request outside this PC"
	noOutsideKo = "에이전트가 직접 밖으로 보내는 요청은 없습니다"
	company     = "the judging AI's company receives the question and the other AIs' answers"
	companyKo   = "판정하는 AI의 회사가 질문과 다른 AI들의 답을 받습니다"
)

// Every judge line of check, in both languages, each once.
func TestJudgeLines(t *testing.T) {
	st := []judgeStatus{
		{agent: "claude", name: "Claude Code", version: "2.1.316", min: "2.1.314", info: &core.JudgeInfo{Model: "haiku", Effort: "medium", Family: "anthropic"}},
		{agent: "codex", name: "Codex", version: "0.163.0", min: "0.164.0", why: compares.WhyOld, info: nil},
		{agent: "opencode", name: "OpenCode", version: "2.2", min: "2.2.0", info: &core.JudgeInfo{Model: "opencode-go/deepseek-v4.1-flash", Effort: "high", Family: "deepseek"}},
		{agent: "zed", name: "zed", version: "1.0.0", min: "1.0.0", why: compares.WhyNoFamily, info: &core.JudgeInfo{Model: "openrouter/x-1"}},
		{agent: "nofolder", name: "nofolder", version: "1.0.0", min: "1.0.0", why: compares.WhyNoFolder},
	}
	lines := judgeLines(st, false)
	findLine(t, lines, "ok", "comparison judging: Claude Code haiku · OpenCode opencode-go/deepseek-v4.1-flash", "비교 판정: Claude Code haiku · OpenCode opencode-go/deepseek-v4.1-flash")
	findLine(t, lines, "warn",
		"Codex 0.163.0 is older than 0.164.0, the oldest version the judge was tested with; it does not judge until it is updated",
		"Codex 0.163.0 판은 판정을 시험한 가장 오래된 판(0.164.0)보다 오래되어 업데이트할 때까지 판정하지 않습니다")
	findLine(t, lines, "warn",
		"the judge model openrouter/x-1 set for zed is of a company the agent cannot tell, so it does not judge",
		"zed에 설정한 판정 모델 openrouter/x-1의 회사를 알 수 없어 판정하지 않습니다")
	findLine(t, lines, "info",
		"the judge command was tested on 2026-10-31 with Claude Code 2.1.314; this PC has 2.1.316",
		"2026-10-31에 Claude Code 2.1.314 판으로 판정 명령을 시험했습니다. 이 PC는 2.1.316입니다")
	findLine(t, lines, "info", company, companyKo)
	findLine(t, lines, "ok", noOutside, noOutsideKo)
	noLineTwice(t, lines)
	if len(lines) != 6 {
		t.Errorf("%d lines:\n%s", len(lines), strings.Join(lineTexts(lines), "\n"))
	}
	// 2.2 is 2.2.0: no tested-version line for it.
	if slices.ContainsFunc(lines, func(l checkLine) bool { return strings.Contains(l.text.en, "with OpenCode") }) {
		t.Error("a tested-version line for the same version")
	}
	if last := lines[len(lines)-1]; last.text.en != noOutside {
		t.Errorf("last line = %q", last.text.en)
	}
	for _, s := range lineTexts(lines) {
		if strings.Contains(s, "%!") {
			t.Errorf("format error in %q", s)
		}
	}
}

// Judging off: the off line and that nothing leaves the PC, no other.
func TestJudgeLinesOff(t *testing.T) {
	st := []judgeStatus{
		{agent: "claude", name: "Claude Code", version: "2.1.300", min: "2.1.314", why: compares.WhyOff},
		{agent: "codex", name: "Codex", version: "0.164.0", min: "0.164.0", why: compares.WhyOld},
	}
	lines := judgeLines(st, true)
	if len(lines) != 2 {
		t.Fatalf("lines:\n%s", strings.Join(lineTexts(lines), "\n"))
	}
	findLine(t, lines, "info", "comparison judging is off (agent.json judge.off)", "비교 판정이 꺼져 있습니다 (agent.json judge.off)")
	findLine(t, lines, "ok", noOutside, noOutsideKo)
}

// Nobody judges: no judging line and no company line; a version that
// could not be read shows as "?".
func TestJudgeLinesNobody(t *testing.T) {
	lines := judgeLines([]judgeStatus{{agent: "codex", name: "Codex", min: "0.164.0", why: compares.WhyOld}}, false)
	findLine(t, lines, "warn", "Codex ? is older than 0.164.0", "Codex ? 판은")
	for _, s := range lineTexts(lines) {
		if strings.Contains(s, "comparison judging") || strings.Contains(s, company) {
			t.Errorf("line %q although nobody judges", s)
		}
	}
	if len(lines) != 2 {
		t.Errorf("lines:\n%s", strings.Join(lineTexts(lines), "\n"))
	}
	if lines := judgeLines(nil, false); len(lines) != 1 || lines[0].text.en != noOutside {
		t.Errorf("no AI: %v", lineTexts(lines))
	}
}

// judgeStatuses asks the gate: an AI that judges carries its model, one
// whose override is of an unknown company the model it was given, and an
// unavailable AI is left out (check warned about it already).
func TestJudgeStatuses(t *testing.T) {
	ais := threeAIs()
	ais[0].version = "2.1.316"
	ais[1].avail = false
	ais[2].family = "" // the override's company cannot be told
	g := testGate(t)
	g.MinVersions = map[string]string{"claude": "2.1.314", "codex": "0.164.0", "opencode": "2.2.1"}
	g.Settings = config.JudgeConfig{Models: map[string]core.AskModel{"opencode": {Model: "openrouter/x-1"}}}
	ais[2].version = "2.2.1"
	st := judgeStatuses(context.Background(), adaptersOf(ais), g)
	if len(st) != 2 {
		t.Fatalf("statuses = %+v", st)
	}
	c, o := st[0], st[1]
	if c.agent != "claude" || c.name != "Claude Code" || c.why != "" || c.version != "2.1.316" || c.min != "2.1.314" ||
		c.info == nil || *c.info != (core.JudgeInfo{Model: "haiku", Effort: "medium", Family: "anthropic"}) {
		t.Errorf("claude = %+v %+v", c, c.info)
	}
	if o.agent != "opencode" || o.name != "OpenCode" || o.why != compares.WhyNoFamily || o.info == nil || o.info.Model != "openrouter/x-1" {
		t.Errorf("opencode = %+v %+v", o, o.info)
	}
	lines := judgeLines(st, false)
	findLine(t, lines, "ok", "comparison judging: Claude Code haiku", "비교 판정: Claude Code haiku")
	findLine(t, lines, "warn", "the judge model openrouter/x-1 set for OpenCode", "OpenCode에 설정한 판정 모델 openrouter/x-1")
	noLineTwice(t, lines)
}

func TestDisplayName(t *testing.T) {
	for agent, want := range map[string]string{"claude": "Claude Code", "codex": "Codex", "opencode": "OpenCode", "gemini": "gemini"} {
		if got := displayName(agent); got != want {
			t.Errorf("displayName(%q) = %q", agent, got)
		}
	}
}
