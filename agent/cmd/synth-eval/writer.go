//go:build syntheval

package main

import (
	"context"
	"strings"
)

// The quality experiments (C4, E0) make their own writer calls, outside a
// comparison: resynth writes a combined answer again, refine asks the
// moderator only, score asks a strong model which of two combined answers
// is better. Each is one call of a writer's locked-down command
// (core.Synthesizer: no tools, no web, nothing kept, prompt on stdin), as
// the agent makes it.

// writeReq is one writer call.
type writeReq struct {
	Agent  string // claude, codex or opencode
	Tier   string // the ask tier whose model writes
	Refine bool   // the moderator's command (refine-v1 schema), not the combined answer's
	Prompt string
}

// writeRes is what the call gave; Output is the AI's reply, unchecked.
type writeRes struct {
	Agent, Model string
	Ms           int64
	In, Out      int
	Output       []byte
}

// writer makes writer calls: the agent's adapters (agent.go), or a fake in
// tests.
type writer interface {
	Write(ctx context.Context, r writeReq) (writeRes, error)
}

// newWriter makes the agent's writer; tests replace it.
var newWriter = func(ctx context.Context) (writer, func(), error) {
	p, err := newAgentPipeline(ctx)
	if err != nil {
		return nil, nil, err
	}
	return p, p.Close, nil
}

// limitedText reports whether an AI's message names a usage limit.
func limitedText(msg string) bool {
	msg = strings.ToLower(msg)
	for _, w := range usageLimitWords {
		if strings.Contains(msg, w) {
			return true
		}
	}
	return false
}

// validWriter reports whether name is an AI that writes.
func validWriter(name string) bool {
	return name == "claude" || name == "codex" || name == "opencode"
}
