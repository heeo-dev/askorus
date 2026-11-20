package main

import (
	"context"
	"slices"
	"strings"

	"github.com/heeo-dev/askorus/agent/internal/core"
)

// modelLines are check's lines on the newest models (#64): with the lists
// read, each AI whose tiers moved to a newer model than the agent ships
// with says which; with agent.json ask.noFollow one line says the tiers
// stay. Nothing while every tier uses a shipped model.
func modelLines(ctx context.Context, askOnly []core.Adapter, noFollow bool) []checkLine {
	if noFollow {
		return []checkLine{{"info", m("the ask models stay as the agent ships them (agent.json ask.noFollow)",
			"묻기 모델은 에이전트에 정해 둔 그대로입니다 (agent.json ask.noFollow)")}}
	}
	waitModels(ctx, askOnly, modelsWait)
	var lines []checkLine
	for _, a := range askOnly {
		f, ok := a.(core.Following)
		if !ok {
			continue
		}
		n := f.Newer()
		if len(n) == 0 {
			continue
		}
		var pairs []string
		for old, now := range n {
			pairs = append(pairs, old+" → "+now)
		}
		slices.Sort(pairs)
		p := strings.Join(pairs, ", ")
		lines = append(lines, checkLine{"info", m("%s asks use newer models: %s", "%s 묻기가 새 모델을 씁니다: %s", displayName(a.Agent()), p)})
	}
	return lines
}
