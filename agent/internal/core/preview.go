package core

import (
	"context"
	"time"
)

// Previewer reads the end of a session's conversation, read only (F8). It
// is the first API that sends conversation text to the phone: the answer
// is never logged or cached (Cache-Control: no-store).
type Previewer interface {
	Preview(ctx context.Context, nativeID string) (Preview, error)
}

// Preview is the last question and its answer. Fields may be added inside
// /v1, never removed (principle 4).
type Preview struct {
	Turns     []Turn `json:"turns"`
	Truncated bool   `json:"truncated"` // a text was cut to its limit
}

// Turn is one side of the conversation.
type Turn struct {
	Role  string    `json:"role"` // "user" or "assistant"
	Text  string    `json:"text"`
	At    time.Time `json:"at,omitzero"`
	Tools int       `json:"tools,omitempty"` // tool calls in the answer; their content is not sent
	// ToolKinds groups Tools by kind, most used first. Kinds are codes the
	// app turns into words: read, edit, run, search, web, agent, other.
	ToolKinds []ToolCount `json:"toolKinds,omitempty"`
}

// ToolCount is how many tool calls of one kind an answer made.
type ToolCount struct {
	Kind  string `json:"kind"`
	Count int    `json:"count"`
}

// Tool kinds in a preview. Adapters map their own tool names to these.
const (
	ToolRead   = "read"
	ToolEdit   = "edit"
	ToolRun    = "run"
	ToolSearch = "search"
	ToolWeb    = "web"
	ToolAgent  = "agent"
	ToolOther  = "other"
)
