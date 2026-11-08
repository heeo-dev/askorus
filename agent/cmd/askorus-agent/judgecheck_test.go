package main

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/heeo-dev/askorus/agent/internal/compares"
	"github.com/heeo-dev/askorus/agent/internal/core"
	"github.com/heeo-dev/askorus/agent/internal/fakecli"
)

// ---- fakes ----------------------------------------------------------------

// fakeAI is an AI program that can ask and name its models' companies, in
// process. It never runs a program.
type fakeAI struct {
	name, family, version string
	avail                 bool
	tiers                 map[string]core.AskModel
}

func newFakeAI(name, family string) *fakeAI {
	return &fakeAI{
		name: name, family: family, version: "1.2.3", avail: true,
		tiers: map[string]core.AskModel{core.TierStandard: {Model: name + "-standard"}},
	}
}

// threeAIs are fakes of the three AI programs; they cannot write combined
// answers.
func threeAIs() []*fakeAI {
	return []*fakeAI{newFakeAI("claude", "anthropic"), newFakeAI("codex", "openai"), newFakeAI("opencode", "deepseek")}
}

func adaptersOf[T core.Adapter](ais []T) []core.Adapter {
	out := make([]core.Adapter, len(ais))
	for i, a := range ais {
		out[i] = a
	}
	return out
}

func (f *fakeAI) Agent() string { return f.name }
func (f *fakeAI) Info(context.Context) core.AdapterInfo {
	return core.AdapterInfo{Agent: f.name, Version: f.version, Available: f.avail, Capabilities: []core.Capability{core.CapAsk}}
}
func (f *fakeAI) ListSessions(context.Context) (core.ListResult, error) {
	return core.ListResult{}, nil
}
func (f *fakeAI) Ask(context.Context, core.AskRequest) (core.AskResult, error) {
	return core.AskResult{}, errors.New("the fake does not answer")
}
func (f *fakeAI) AskTiers() map[string]core.AskModel { return f.tiers }
func (f *fakeAI) Family(model string) string {
	if model == "" {
		return ""
	}
	return f.family
}

// fakeWriterAI is a fakeAI that also writes combined answers: fakecli's
// ok text.
type fakeWriterAI struct{ *fakeAI }

func (f *fakeWriterAI) DisplayName() string { return strings.ToUpper(f.name[:1]) + f.name[1:] }

func (f *fakeWriterAI) Synthesize(ctx context.Context, req core.SynthRequest) (core.SynthResult, error) {
	b, _ := json.Marshal(map[string]string{"answer": fakecli.SynthLead})
	return core.SynthResult{Output: b, Model: req.Model}, nil
}

func threeWriterAIs() []*fakeWriterAI {
	var out []*fakeWriterAI
	for _, a := range threeAIs() {
		out = append(out, &fakeWriterAI{fakeAI: a})
	}
	return out
}

func testGate(t *testing.T) compares.Gate {
	return compares.Gate{
		MinVersions: map[string]string{"claude": "1.0.0", "codex": "1.0.0", "opencode": "1.0.0"},
		Folder:      filepath.Join(t.TempDir(), "ask", core.JudgeFolderName),
	}
}

// ---- check's lines about combined answers ---------------------------------

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
	writeLine   = "combined answers: Claude Code · OpenCode can write them (the question's tier model)"
	writeLineKo = "종합 답변을 쓸 수 있는 AI: Claude Code · OpenCode (질문 단계의 모델)"
	company     = "when a combined answer is written, the writing AI's company receives the question and every answer with the AIs' names"
	companyKo   = "종합 답변을 쓸 때는 쓰는 AI의 회사가 질문과 AI 이름이 붙은 모든 답을 받습니다"
	// Milestone C: refining sends the question to the moderator once more;
	// the line shows only while refining is on (off since 2026-11-04).
	refineInfo   = "when a question is refined, the moderator's company (one of the AIs asked) receives the question once more, with up to 3 earlier questions"
	refineInfoKo = "질문을 정제할 때는 사회자 AI의 회사(질문을 받는 AI 중 하나)가 질문을 한 번 더, 앞 질문 3개까지와 함께 받습니다"
)

// Every line about combined answers, in both languages, each once: who
// writes, a CLI too old to, a tier model of a company the agent cannot
// tell, the version the one-off command was tested with when this PC's
// differs, what the writer's company receives, and last that nothing else
// leaves the PC. No word of judging.
func TestWriterLines(t *testing.T) {
	st := []writerStatus{
		{agent: "claude", name: "Claude Code", version: "2.1.317", min: "2.1.314", model: "claude-standard"},
		{agent: "codex", name: "Codex", version: "0.163.0", min: "0.164.0", why: compares.WhyOld},
		{agent: "opencode", name: "OpenCode", version: "2.2", min: "2.2.0", model: "opencode-go/deepseek-v4.1-flash"},
		{agent: "zed", name: "zed", version: "1.0.0", min: "1.0.0", why: compares.WhyNoFamily, model: "openrouter/x-1"},
		{agent: "nofolder", name: "nofolder", version: "1.0.0", min: "1.0.0", why: compares.WhyNoFolder},
	}
	lines := writerLines(st, false)
	findLine(t, lines, "ok", writeLine, writeLineKo)
	findLine(t, lines, "warn",
		"Codex 0.163.0 is older than 0.164.0, the oldest version its one-off command was tested with; it does not write combined answers until it is updated",
		"Codex 0.163.0 판은 일회성 명령을 시험한 가장 오래된 판(0.164.0)보다 오래되어 업데이트할 때까지 종합 답변을 쓰지 않습니다")
	findLine(t, lines, "warn",
		"the model openrouter/x-1 of zed is of a company the agent cannot tell, so it does not write combined answers",
		"zed의 모델 openrouter/x-1의 회사를 알 수 없어 종합 답변을 쓰지 않습니다")
	findLine(t, lines, "info",
		"the one-off command was tested on 2026-10-31 with Claude Code 2.1.314; this PC has 2.1.317",
		"2026-10-31에 Claude Code 2.1.314 판으로 일회성 명령을 시험했습니다. 이 PC는 2.1.317입니다")
	findLine(t, lines, "info", company, companyKo)
	findLine(t, lines, "ok", noOutside, noOutsideKo)
	noLineTwice(t, lines)
	if len(lines) != 6 {
		t.Errorf("%d lines:\n%s", len(lines), strings.Join(lineTexts(lines), "\n"))
	}
	// 2.2 is 2.2.0: no version line for it.
	if slices.ContainsFunc(lines, func(l checkLine) bool { return strings.Contains(l.text.en, "with OpenCode") }) {
		t.Errorf("a version line for the same version:\n%s", strings.Join(lineTexts(lines), "\n"))
	}
	idx := func(en string) int {
		return slices.IndexFunc(lines, func(l checkLine) bool { return strings.HasPrefix(l.text.en, en) })
	}
	if !(idx("combined answers:") == 0 && idx("the one-off command was tested") < idx("when a combined answer")) {
		t.Errorf("order:\n%s", strings.Join(lineTexts(lines), "\n"))
	}
	if last := lines[len(lines)-1]; last.text.en != noOutside {
		t.Errorf("last line = %q", last.text.en)
	}
	for _, s := range lineTexts(lines) {
		if strings.Contains(s, "%!") || strings.Contains(s, "judg") || strings.Contains(s, "compared") || strings.Contains(s, "판정") || strings.Contains(s, "비교") {
			t.Errorf("format error or a judging word in %q", s)
		}
	}
	// No refine line while refining is off; turned on, it says what
	// refining sends, before the last line.
	if slices.ContainsFunc(lines, func(l checkLine) bool { return l.text.en == refineInfo }) {
		t.Error("the refine line shows while refining is off")
	}
	refineOn = true
	t.Cleanup(func() { refineOn = false })
	on := writerLines(st, false)
	findLine(t, on, "info", refineInfo, refineInfoKo)
	if len(on) != 7 || on[len(on)-1].text.en != noOutside {
		t.Errorf("refining on:\n%s", strings.Join(lineTexts(on), "\n"))
	}
}

// Combined answers off: the off line and that nothing leaves the PC, no
// other.
func TestWriterLinesOff(t *testing.T) {
	st := []writerStatus{
		{agent: "claude", name: "Claude Code", version: "2.1.300", min: "2.1.314", why: compares.WhyOff},
		{agent: "codex", name: "Codex", version: "0.164.0", min: "0.164.0", why: compares.WhyOff},
	}
	lines := writerLines(st, true)
	if len(lines) != 2 {
		t.Fatalf("lines:\n%s", strings.Join(lineTexts(lines), "\n"))
	}
	findLine(t, lines, "info", "combined answers are off (agent.json judge.off)", "종합 답변이 꺼져 있습니다 (agent.json judge.off)")
	findLine(t, lines, "ok", noOutside, noOutsideKo)
}

// Nobody writes: no line of who writes and no company line; a version
// that could not be read shows as "?".
func TestWriterLinesNobody(t *testing.T) {
	lines := writerLines([]writerStatus{{agent: "codex", name: "Codex", min: "0.164.0", why: compares.WhyOld}}, false)
	findLine(t, lines, "warn", "Codex ? is older than 0.164.0", "Codex ? 판은")
	for _, s := range lineTexts(lines) {
		if strings.Contains(s, "combined answers:") || strings.Contains(s, company) {
			t.Errorf("line %q although nobody writes", s)
		}
	}
	if len(lines) != 2 {
		t.Errorf("lines:\n%s", strings.Join(lineTexts(lines), "\n"))
	}
	if lines := writerLines(nil, false); len(lines) != 1 || lines[0].text.en != noOutside {
		t.Errorf("no AI: %v", lineTexts(lines))
	}
}

// writerStatuses asks the gate at the standard tier: an AI that writes
// carries its tier model, one whose model is of an unknown company that
// model, an AI that cannot write is cannot_synthesize, an unavailable AI
// is left out (check warned about it already), and judge.off is off for
// every AI.
func TestWriterStatuses(t *testing.T) {
	ais := threeWriterAIs()
	ais[0].version = "2.1.316"
	ais[1].avail = false
	ais[2].family = "" // the tier model's company cannot be told
	plain := newFakeAI("gemini", "google")
	g := testGate(t)
	g.MinVersions = map[string]string{"claude": "2.1.314", "codex": "0.164.0", "opencode": "1.0.0", "gemini": "1.0.0"}
	st := writerStatuses(context.Background(), append(adaptersOf(ais), plain), g)
	if len(st) != 3 {
		t.Fatalf("statuses = %+v", st)
	}
	c, o, p := st[0], st[1], st[2]
	if c.agent != "claude" || c.name != "Claude Code" || c.why != "" || c.version != "2.1.316" || c.min != "2.1.314" || c.model != "claude-standard" {
		t.Errorf("claude = %+v", c)
	}
	if o.agent != "opencode" || o.name != "OpenCode" || o.why != compares.WhyNoFamily || o.model != "opencode-standard" {
		t.Errorf("opencode = %+v", o)
	}
	if p.agent != "gemini" || p.name != "gemini" || p.why != compares.WhyCannotWrite {
		t.Errorf("gemini = %+v", p)
	}
	lines := writerLines(st, false)
	findLine(t, lines, "ok", "combined answers: Claude Code can write them", "종합 답변을 쓸 수 있는 AI: Claude Code (")
	findLine(t, lines, "warn", "the model opencode-standard of OpenCode", "OpenCode의 모델 opencode-standard")
	noLineTwice(t, lines)

	g.Settings.Off = true
	for _, s := range writerStatuses(context.Background(), adaptersOf(ais), g) {
		if s.why != compares.WhyOff {
			t.Errorf("off: %+v", s)
		}
	}
}

func TestDisplayName(t *testing.T) {
	for agent, want := range map[string]string{"claude": "Claude Code", "codex": "Codex", "opencode": "OpenCode", "gemini": "gemini"} {
		if got := displayName(agent); got != want {
			t.Errorf("displayName(%q) = %q", agent, got)
		}
	}
}
