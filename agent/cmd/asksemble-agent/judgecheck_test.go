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
		{agent: "claude", name: "Claude Code", version: "2.1.317", min: "2.1.314", verified: "2.1.316", info: &core.JudgeInfo{Model: "haiku", Effort: "medium", Family: "anthropic"}},
		{agent: "codex", name: "Codex", version: "0.163.0", min: "0.164.0", verified: "0.164.0", why: compares.WhyOld, info: nil},
		{agent: "opencode", name: "OpenCode", version: "2.2", min: "2.2.0", verified: "2.2.0", info: &core.JudgeInfo{Model: "opencode-go/deepseek-v4.1-flash", Effort: "high", Family: "deepseek"}},
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
	// S6: the line names the verification, not the day the command was
	// tested (11-01 user decision 4).
	findLine(t, lines, "info",
		"the judging was verified on 2026-11-01 with Claude Code 2.1.316; this PC has 2.1.317",
		"2026-11-01에 Claude Code 2.1.316 판으로 판정을 검증했습니다. 이 PC는 2.1.317입니다")
	findLine(t, lines, "info", company, companyKo)
	findLine(t, lines, "ok", noOutside, noOutsideKo)
	noLineTwice(t, lines)
	if len(lines) != 6 {
		t.Errorf("%d lines:\n%s", len(lines), strings.Join(lineTexts(lines), "\n"))
	}
	// 2.2 is 2.2.0: no version line for it, and none names the tested day.
	if slices.ContainsFunc(lines, func(l checkLine) bool {
		return strings.Contains(l.text.en, "with OpenCode") || strings.Contains(l.text.en, "tested on")
	}) {
		t.Errorf("a version line for the same version, or the tested day:\n%s", strings.Join(lineTexts(lines), "\n"))
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

// The verification's version decides, not the tested one: the version it
// was verified with gives no line although it is newer than the tested one,
// and an AI the verification did not cover keeps the tested-version line.
func TestJudgeLinesVerified(t *testing.T) {
	st := []judgeStatus{
		{agent: "claude", name: "Claude Code", version: "2.1.316", min: "2.1.314", verified: "2.1.316", info: &core.JudgeInfo{Model: "haiku", Family: "anthropic"}},
		{agent: "gemini", name: "gemini", version: "1.1.0", min: "1.0.0", info: &core.JudgeInfo{Model: "gemini-x", Family: "google"}},
	}
	lines := judgeLines(st, false)
	findLine(t, lines, "info",
		"the judge command was tested on 2026-10-31 with gemini 1.0.0; this PC has 1.1.0",
		"2026-10-31에 gemini 1.0.0 판으로 판정 명령을 시험했습니다. 이 PC는 1.1.0입니다")
	for _, s := range lineTexts(lines) {
		if strings.Contains(s, "Claude Code 2.1.31") || strings.Contains(s, "verified on") {
			t.Errorf("line %q for a verified version", s)
		}
	}
	noLineTwice(t, lines)
	// The real verification covers the three AIs by their adapter names.
	for agent, v := range map[string]string{"claude": "2.1.316", "codex": "0.164.0", "opencode": "2.2.1"} {
		if got := compares.JudgingVerified.Versions[agent]; got != v {
			t.Errorf("verified %s = %q, want %q", agent, got, v)
		}
	}
	if compares.JudgingVerified.Date != "2026-11-01" || len(compares.JudgingVerified.Versions) != 3 {
		t.Errorf("verified = %+v", compares.JudgingVerified)
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
	if c.agent != "claude" || c.name != "Claude Code" || c.why != "" || c.version != "2.1.316" || c.min != "2.1.314" || c.verified != "2.1.316" ||
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

const (
	writeLine   = "combined answers: Claude Code · OpenCode can write them (the question's tier model)"
	writeLineKo = "종합 답변을 쓸 수 있는 AI: Claude Code · OpenCode (질문 단계의 모델)"
	writeInfo   = "when a combined answer is written, the writing AI's company receives the question, every answer with the AIs' names and the comparison's results"
	writeInfoKo = "종합 답변을 쓸 때는 쓰는 AI의 회사가 질문, AI 이름이 붙은 모든 답, 비교 결과를 받습니다"
	// Milestone C: refining sends the question to the moderator once more.
	refineInfo   = "when a question is refined, the moderator's company (one of the AIs asked) receives the question once more, with up to 3 earlier questions"
	refineInfoKo = "질문을 정제할 때는 사회자 AI의 회사(질문을 받는 AI 중 하나)가 질문을 한 번 더, 앞 질문 3개까지와 함께 받습니다"
)

// The combined-answer lines: which AIs write, and what the writing AI's
// company receives; the judge lines stay as they were.
func TestJudgeLinesSynthesis(t *testing.T) {
	st := []judgeStatus{
		{agent: "claude", name: "Claude Code", version: "2.1.314", min: "2.1.314", writes: true, info: &core.JudgeInfo{Model: "haiku", Effort: "medium", Family: "anthropic"}},
		{agent: "codex", name: "Codex", version: "0.164.0", min: "0.164.0", info: &core.JudgeInfo{Model: "gpt-6-luna", Family: "openai"}},
		{agent: "opencode", name: "OpenCode", version: "2.2.1", min: "2.2.1", writes: true, info: &core.JudgeInfo{Model: "opencode-go/deepseek-v4.1-flash", Family: "deepseek"}},
		{agent: "old", name: "old", version: "0.1.0", min: "1.0.0", why: compares.WhyOld, writes: true},
	}
	lines := judgeLines(st, false)
	findLine(t, lines, "ok", writeLine, writeLineKo)
	findLine(t, lines, "info", writeInfo, writeInfoKo)
	findLine(t, lines, "info", refineInfo, refineInfoKo)
	findLine(t, lines, "ok", "comparison judging: Claude Code haiku · Codex gpt-6-luna · OpenCode opencode-go/deepseek-v4.1-flash", "비교 판정: Claude Code haiku")
	findLine(t, lines, "info", company, companyKo)
	noLineTwice(t, lines)
	if len(lines) != 7 || lines[len(lines)-1].text.en != noOutside {
		t.Errorf("%d lines:\n%s", len(lines), strings.Join(lineTexts(lines), "\n"))
	}
	// The writer lines follow the judge lines they go with.
	idx := func(en string) int {
		return slices.IndexFunc(lines, func(l checkLine) bool { return strings.HasPrefix(l.text.en, en) })
	}
	if !(idx("comparison judging") < idx("combined answers") && idx("when answers are compared") < idx("when a combined answer")) {
		t.Errorf("order:\n%s", strings.Join(lineTexts(lines), "\n"))
	}
	// Nobody writes, or judging off: no combined-answer line.
	for i := range st {
		st[i].writes = false
	}
	for _, off := range []bool{false, true} {
		for _, l := range lineTexts(judgeLines(st, off)) {
			if strings.Contains(l, "combined answer") || strings.Contains(l, "종합 답변") || strings.Contains(l, "refined") || strings.Contains(l, "정제") {
				t.Errorf("off %v: %q", off, l)
			}
		}
	}
}

// judgeStatuses reads writes from the capability Announce gives.
func TestJudgeStatusesWrites(t *testing.T) {
	writers := threeWriterAIs()
	plain := threeAIs()
	ads := []core.Adapter{writers[0], plain[1], writers[2]}
	g := testGate(t)
	st := judgeStatuses(context.Background(), ads, g)
	if len(st) != 3 || !st[0].writes || st[1].writes || !st[2].writes {
		t.Fatalf("statuses = %+v", st)
	}
	findLine(t, judgeLines(st, false), "ok", writeLine, writeLineKo)
	g.Settings.Off = true
	for _, s := range judgeStatuses(context.Background(), ads, g) {
		if s.writes {
			t.Errorf("off: %+v", s)
		}
	}
}
