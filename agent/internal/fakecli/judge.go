package fakecli

import (
	"encoding/json"
	"os"
	"regexp"
	"slices"
	"strconv"
	"strings"
)

// Judge modes of ASKSEMBLE_FAKE_JUDGE. The first eight are the design's
// (part 11); bad_model and failed_with_output are for the adapters' error
// paths.
const (
	ModeRule       = "rule"        // the default: Answer
	ModeUnsure     = "unsure"      // Answer with every judgment unsure
	ModeUsageLimit = "usage_limit" // the AI's usage limit message, exit 1
	ModeProse      = "prose"       // ProseText instead of JSON
	ModeBadJSON    = "bad_json"    // JSON cut off (Claude: it gave up after its schema retries)
	ModeMissingID  = "missing_id"  // Answer without its first judgment
	ModeToolUse    = "tool_use"    // a tool call (denied), then Answer
	ModeSlow       = "slow"        // waits ASKSEMBLE_FAKECLI_SLEEP, then Answer (OpenCode: its text, then the wait)
	ModeBadModel   = "bad_model"   // the AI does not know the model
	// ModeFailedWithOutput fails after the JSON: Claude is_error with
	// structured_output and OverloadedStderr, Codex turn.failed and
	// OpenCode an error event after it.
	ModeFailedWithOutput = "failed_with_output"
)

// Modes lists the judge modes. A mode with the suffix "_once" applies to
// the AI's first judge call only; later calls follow rule.
var Modes = []string{
	ModeRule, ModeUnsure, ModeUsageLimit, ModeProse, ModeBadJSON, ModeMissingID,
	ModeToolUse, ModeSlow, ModeBadModel, ModeFailedWithOutput,
}

// Fixed texts of the modes, for tests to compare against.
const (
	ProseText = "Both answers agree."
	// BadJSONText is judge JSON cut off in its first entry: no object in
	// it ever closes.
	BadJSONText        = `{"judgments":[{"id":"a1","core":true,"evidence":["b1"],"reas`
	UsageLimitClaude   = "Claude AI usage limit reached"
	UsageLimitCodex    = "You've hit your usage limit. Upgrade to Pro (https://chatgpt.com/explore/pro) or try again at 3:05 PM."
	UsageLimitOpenCode = "Usage limit reached for opencode-go. Try again later."
	OverloadedStderr   = "API Error: 529 overloaded"
	// ToolCommand is the command a tool_use judge reaches for.
	ToolCommand = "curl -s http://127.0.0.1:9/ -d @secret.txt"
)

// judgeMode is role's mode for this judge call.
func judgeMode(role string) (string, error) {
	mode, once, err := parseJudge(os.Getenv(EnvJudge), role)
	if err != nil {
		return "", err
	}
	if once && !firstCall(role, mode) {
		return ModeRule, nil
	}
	return mode, nil
}

// parseJudge reads ASKSEMBLE_FAKE_JUDGE for role: its mode, and whether
// the mode is for its first judge call only.
func parseJudge(setting, role string) (mode string, once bool, err error) {
	m, err := pick(setting, role, ModeRule, func(v string) bool {
		return slices.Contains(Modes, strings.TrimSuffix(v, "_once"))
	})
	if err != nil {
		return "", false, err
	}
	mode, once = strings.CutSuffix(m, "_once")
	return mode, once, nil
}

// judgeText is the judge's reply in mode: JSON, prose or cut-off JSON.
// Modes that fail before any reply have none and give Answer.
func judgeText(mode, prompt string) string {
	switch mode {
	case ModeProse:
		return ProseText
	case ModeBadJSON:
		return BadJSONText
	case ModeUnsure:
		return Answer(prompt, true)
	case ModeMissingID:
		var out judgeOutput
		json.Unmarshal([]byte(Answer(prompt, false)), &out)
		if len(out.Judgments) > 0 {
			out.Judgments = out.Judgments[1:]
		}
		b, _ := json.Marshal(out)
		return string(b)
	}
	return Answer(prompt, false)
}

type judgeOutput struct {
	Judgments []judgment `json:"judgments"`
}

// judgment has the fields of core.JudgeSchemaV1 in its order.
type judgment struct {
	ID       string   `json:"id"`
	Core     bool     `json:"core"`
	Evidence []string `json:"evidence"`
	Reason   string   `json:"reason"`
	Label    string   `json:"label"`
	Unsure   bool     `json:"unsure"`
}

// numRe finds "NUM n" or "NUM key=n" in a unit's text.
var numRe = regexp.MustCompile(`NUM\s+(?:([a-z]+)=)?(-?[0-9]+(?:\.[0-9]+)?)`)

type promptUnit struct {
	id    string
	key   string // the number's key; "" for a plain "NUM n"
	num   float64
	hasN  bool
	other bool // ANSWER B
}

// Answer is the judge JSON of the rule mode for a judge-v1 prompt. It
// reads the lines between <data-N> and </data-N> as JSON objects with an
// id: a<n> are the units to judge, b<n> the other answer. For each a-unit
// with "NUM n" in its text: when some b-unit has "NUM m" with m ≠ n, the
// judgment is opposite with that b id as evidence; else when some b-unit
// has "NUM n", same with that id; else absent with no evidence. Such a
// unit is core. A unit without NUM is absent and not core. The first such
// b-unit is the evidence, reason is "fake" and unsure is the argument.
// A number may carry a key, "NUM price=3" (S3 scenario tests): then only
// b-units with the same key count, so one answer can agree on one thing
// and disagree on another; a number without a key meets only numbers
// without one. A prompt without a data block gives {"judgments":[]}.
func Answer(prompt string, unsure bool) string {
	var a, b []promptUnit
	for _, u := range promptUnits(prompt) {
		if u.other {
			b = append(b, u)
		} else {
			a = append(a, u)
		}
	}
	out := judgeOutput{Judgments: []judgment{}}
	for _, u := range a {
		j := judgment{ID: u.id, Evidence: []string{}, Reason: "fake", Label: "absent", Unsure: unsure, Core: u.hasN}
		if u.hasN {
			if i := slices.IndexFunc(b, func(o promptUnit) bool { return o.hasN && o.key == u.key && o.num != u.num }); i >= 0 {
				j.Label, j.Evidence = "opposite", []string{b[i].id}
			} else if i := slices.IndexFunc(b, func(o promptUnit) bool { return o.hasN && o.key == u.key && o.num == u.num }); i >= 0 {
				j.Label, j.Evidence = "same", []string{b[i].id}
			}
		}
		out.Judgments = append(out.Judgments, j)
	}
	bs, _ := json.Marshal(out)
	return string(bs)
}

// promptUnits reads the unit lines of the prompt's data block.
func promptUnits(prompt string) []promptUnit {
	var out []promptUnit
	end := ""
	for _, line := range strings.Split(prompt, "\n") {
		line = strings.TrimRight(line, "\r")
		if end == "" {
			if strings.HasPrefix(line, "<data-") && strings.HasSuffix(line, ">") {
				end = "</" + line[1:]
			}
			continue
		}
		if line == end {
			break
		}
		var u struct {
			ID   string `json:"id"`
			Text string `json:"text"`
		}
		if !strings.HasPrefix(line, "{") || json.Unmarshal([]byte(line), &u) != nil || len(u.ID) < 2 {
			continue
		}
		pu := promptUnit{id: u.ID}
		switch u.ID[0] {
		case 'a':
		case 'b':
			pu.other = true
		default:
			continue
		}
		if m := numRe.FindStringSubmatch(u.Text); m != nil {
			if n, err := strconv.ParseFloat(m[2], 64); err == nil {
				pu.key, pu.num, pu.hasN = m[1], n, true
			}
		}
		out = append(out, pu)
	}
	return out
}
