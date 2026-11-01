package main

import (
	"context"
	"strings"

	"github.com/asksemble/asksemble/agent/internal/adapter/claude"
	"github.com/asksemble/asksemble/agent/internal/adapter/codex"
	"github.com/asksemble/asksemble/agent/internal/adapter/opencode"
	"github.com/asksemble/asksemble/agent/internal/compares"
	"github.com/asksemble/asksemble/agent/internal/core"
)

// What check says about comparison judging (P2-3, spec part 8: check).

// judgeTestedOn is the day the judge commands were tested with each CLI's
// JudgeMinVersion. Stage 1 of 판정 검증 (S6) replaces the tested-version
// line with the verification's date and versions (S3 decision 12).
const judgeTestedOn = "2026-10-31"

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

// judgeStatus is how one available AI stands with judging.
type judgeStatus struct {
	agent   string
	name    string // displayName(agent)
	version string // the CLI's version, as Info read it
	min     string // its adapter's JudgeMinVersion; "" when none is listed
	why     string // "" when it judges, else a compares.Why* reason
	// info is the model it judges with: set when it judges, and for
	// unknown_company the model whose company is unknown.
	info *core.JudgeInfo
}

// checkLine is one line check prints: its level (ok, warn, bad or info)
// and the text.
type checkLine struct {
	level string
	text  msg
}

// judgeStatuses asks the gate about every adapter. AIs that are not
// available are left out: check has warned about them already.
func judgeStatuses(ctx context.Context, adapters []core.Adapter, g compares.Gate) []judgeStatus {
	infos, _ := compares.Announce(ctx, adapters, g)
	var out []judgeStatus
	for i, a := range adapters {
		info := infos[i]
		if !info.Available {
			continue
		}
		st := judgeStatus{agent: a.Agent(), name: displayName(a.Agent()), version: info.Version, min: g.MinVersions[a.Agent()]}
		e, why := g.Engine(a, info)
		st.why = why
		switch {
		case why == "" && info.Judge != nil:
			ji := *info.Judge
			st.info = &ji
		case why == compares.WhyNoFamily:
			st.info = &core.JudgeInfo{Model: e.Model, Effort: e.Effort}
		}
		out = append(out, st)
	}
	return out
}

// judgeLines are check's lines about comparison judging, in English and
// Korean: off; or which AI judges with which model, the CLIs too old to
// judge, judge models of a company the agent cannot tell, the version the
// judge command was tested with when this PC's differs, and where the
// question and the answers go. The last line, that the agent itself sends
// nothing outside this PC, is always there.
func judgeLines(st []judgeStatus, off bool) []checkLine {
	var lines []checkLine
	add := func(level string, x msg) { lines = append(lines, checkLine{level, x}) }
	if off {
		add("info", m("comparison judging is off (agent.json judge.off)", "비교 판정이 꺼져 있습니다 (agent.json judge.off)"))
	} else {
		var judging []string
		for _, s := range st {
			if s.why == "" && s.info != nil {
				judging = append(judging, s.name+" "+s.info.Model)
			}
		}
		if len(judging) > 0 {
			list := strings.Join(judging, " · ")
			add("ok", m("comparison judging: %s", "비교 판정: %s", list))
		}
		for _, s := range st {
			switch {
			case s.why == compares.WhyOld:
				have, min := orUnknown(s.version), orUnknown(s.min)
				add("warn", m("%s %s is older than %s, the oldest version the judge was tested with; it does not judge until it is updated",
					"%s %s 판은 판정을 시험한 가장 오래된 판(%s)보다 오래되어 업데이트할 때까지 판정하지 않습니다", s.name, have, min))
			case s.why == compares.WhyNoFamily:
				model := ""
				if s.info != nil {
					model = s.info.Model
				}
				add("warn", m("the judge model %[2]s set for %[1]s is of a company the agent cannot tell, so it does not judge",
					"%[1]s에 설정한 판정 모델 %[2]s의 회사를 알 수 없어 판정하지 않습니다", s.name, model))
			}
		}
		for _, s := range st {
			if s.why == "" && s.info != nil && s.min != "" && !sameVersion(s.version, s.min) {
				add("info", m("the judge command was tested on %s with %s %s; this PC has %s",
					"%[1]s에 %[2]s %[3]s 판으로 판정 명령을 시험했습니다. 이 PC는 %[4]s입니다", judgeTestedOn, s.name, s.min, s.version))
			}
		}
		if len(judging) > 0 {
			add("info", m("when answers are compared, the judging AI's company receives the question and the other AIs' answers",
				"비교할 때는 판정하는 AI의 회사가 질문과 다른 AI들의 답을 받습니다"))
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
