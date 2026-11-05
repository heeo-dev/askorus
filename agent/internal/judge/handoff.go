package judge

import "strings"

// handoffApps maps the official apps whose answers can be pasted (P2-4)
// to their companies.
var handoffApps = map[string]string{
	"chatgpt": "openai",
	"claude":  "anthropic", // the Claude app
	"gemini":  "google",
}

// HandoffFamily is the company of an official app whose answer was pasted
// (P2-4): chatgpt is openai, claude (the Claude app) anthropic and gemini
// google. Any other name, "ChatGPT app" or "Claude Code" too, is of
// unknown company: "", which a comparison refuses (11-01 user decision;
// the P2-4 API takes only the table's names). PC answers get their family
// from their adapter instead.
func HandoffFamily(app string) string {
	return handoffApps[strings.ToLower(strings.TrimSpace(app))]
}
