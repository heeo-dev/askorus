package main

import (
	"context"
	"slices"
	"strings"

	"github.com/heeo-dev/askorus/agent/internal/adapter/claude"
	"github.com/heeo-dev/askorus/agent/internal/adapter/codex"
	"github.com/heeo-dev/askorus/agent/internal/adapter/opencode"
	"github.com/heeo-dev/askorus/agent/internal/compares"
	"github.com/heeo-dev/askorus/agent/internal/core"
)

// What check says about combined answers (종합 답변): which AIs write them,
// the CLIs too old to, and where the question and the answers go. The
// comparison judging it spoke of until C6 went with C6d (#24).

// oneOffTestedOn is the day the locked-down one-off commands (judging
// then, the combined answer and refining now) were tested with each CLI's
// JudgeMinVersion.
const oneOffTestedOn = "2026-10-31"

// displayNames are the AI programs' names on the PC's terminal.
var displayNames = map[string]string{
	claude.Name:   "Claude Code",
	codex.Name:    "Codex",
	opencode.Name: "OpenCode",
}

// displayName is agent's program name, or agent itself for one the agent
// has no name for (an official app of a pasted answer).
func displayName(agent string) string {
	if n, ok := displayNames[agent]; ok {
		return n
	}
	return agent
}

// writerStatus is how one available AI stands with combined answers.
type writerStatus struct {
	agent   string
	name    string // displayName(agent)
	version string // the CLI's version, as Info read it
	min     string // the oldest version its one-off command was tested with; "" when none is listed
	// why is "" when it writes combined answers, else a compares.Why*
	// reason (at the standard tier).
	why string
	// model is its standard tier's model, for unknown_company.
	model string
}

// checkLine is one line check prints: its level (ok, warn, bad or info)
// and the text.
type checkLine struct {
	level string
	text  msg
}

// writerStatuses asks the gate about every adapter, at the standard tier.
// AIs that are not available are left out: check has warned about them
// already.
func writerStatuses(ctx context.Context, adapters []core.Adapter, g compares.Gate) []writerStatus {
	infos, _ := compares.Announce(ctx, adapters, g)
	var out []writerStatus
	for i, a := range adapters {
		info := infos[i]
		if !info.Available {
			continue
		}
		w, why := g.Writer(a, info, core.TierStandard)
		st := writerStatus{agent: a.Agent(), name: displayName(a.Agent()), version: info.Version, min: g.MinVersions[a.Agent()], why: why, model: w.Model}
		if why == "" && !slices.Contains(info.Capabilities, core.CapSynthesize) {
			st.why = compares.WhyCannotWrite
		}
		out = append(out, st)
	}
	return out
}

// writerLines are check's lines about combined answers, in English and
// Korean: off (agent.json judge.off); or which AIs write them, the CLIs too
// old to, tier models of a company the agent cannot tell, the version the
// one-off command was tested with when this PC's version differs, and
// where the question and the answers go. The last line, that the agent
// itself sends nothing outside this PC, is always there.
func writerLines(st []writerStatus, off bool) []checkLine {
	var lines []checkLine
	add := func(level string, x msg) { lines = append(lines, checkLine{level, x}) }
	if off {
		add("info", m("combined answers are off (agent.json judge.off)", "종합 답변이 꺼져 있습니다 (agent.json judge.off)"))
	} else {
		var writing []string
		for _, s := range st {
			if s.why == "" {
				writing = append(writing, s.name)
			}
		}
		if len(writing) > 0 {
			add("ok", m("combined answers: %s can write them (the question's tier model)",
				"종합 답변을 쓸 수 있는 AI: %s (질문 단계의 모델)", strings.Join(writing, " · ")))
		}
		for _, s := range st {
			switch s.why {
			case compares.WhyOld:
				add("warn", m("%s %s is older than %s, the oldest version its one-off command was tested with; it does not write combined answers until it is updated",
					"%s %s 판은 일회성 명령을 시험한 가장 오래된 판(%s)보다 오래되어 업데이트할 때까지 종합 답변을 쓰지 않습니다", s.name, orUnknown(s.version), orUnknown(s.min)))
			case compares.WhyNoFamily:
				add("warn", m("the model %[2]s of %[1]s is of a company the agent cannot tell, so it does not write combined answers",
					"%[1]s의 모델 %[2]s의 회사를 알 수 없어 종합 답변을 쓰지 않습니다", s.name, s.model))
			}
		}
		for _, s := range st {
			if s.why == "" && s.min != "" && !sameVersion(s.version, s.min) {
				add("info", m("the one-off command was tested on %s with %s %s; this PC has %s",
					"%[1]s에 %[2]s %[3]s 판으로 일회성 명령을 시험했습니다. 이 PC는 %[4]s입니다", oneOffTestedOn, s.name, s.min, s.version))
			}
		}
		if len(writing) > 0 {
			add("info", m("when a combined answer is written, the writing AI's company receives the question and every answer with the AIs' names",
				"종합 답변을 쓸 때는 쓰는 AI의 회사가 질문과 AI 이름이 붙은 모든 답을 받습니다"))
			if refineOn {
				add("info", m("when a question is refined, the moderator's company (one of the AIs asked) receives the question once more, with up to 3 earlier questions",
					"질문을 정제할 때는 사회자 AI의 회사(질문을 받는 AI 중 하나)가 질문을 한 번 더, 앞 질문 3개까지와 함께 받습니다"))
			}
		}
	}
	add("ok", m("the agent itself sends no request outside this PC", "에이전트가 직접 밖으로 보내는 요청은 없습니다"))
	return lines
}

// sameVersion reports whether two version strings name the same version
// (core.VersionAtLeast both ways), so "2.2" is "2.2.0".
func sameVersion(a, b string) bool {
	return core.VersionAtLeast(a, b) && core.VersionAtLeast(b, a)
}

func orUnknown(s string) string {
	if s == "" {
		return "?"
	}
	return s
}
