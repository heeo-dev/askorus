package judge

import "testing"

// Only the three official apps of the table have a company; any other
// name, in any spelling, has none (11-01 user decision).
func TestHandoffFamily(t *testing.T) {
	for app, want := range map[string]string{
		"chatgpt":      "openai",
		"ChatGPT":      "openai",
		"claude":       "anthropic",
		" gemini ":     "google",
		"perplexity":   "",
		"DeepSeek":     "",
		"ChatGPT app":  "",
		"Claude Code":  "",
		"claude.ai":    "",
		"":             "",
		"   ":          "",
		"claude-code?": "",
	} {
		if got := HandoffFamily(app); got != want {
			t.Errorf("HandoffFamily(%q) = %q, want %q", app, got, want)
		}
	}
}
