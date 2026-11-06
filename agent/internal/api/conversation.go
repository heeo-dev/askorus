package api

import (
	"strings"
	"unicode/utf8"

	"github.com/asksemble/asksemble/agent/internal/judge"
)

// The conversation record (#29, 11-06): the phone builds it from a
// conversation's earlier turns and sends it as context with a follow-up
// (POST /v1/runs), a comparison made again (POST /v1/comparisons) and a
// debate (POST /v1/debates). The agent only puts it in prompts as data:
// nothing of it is kept, and logs carry its counts only.

// RequestContext is a request's context: the conversation's latest earlier
// questions (oldest first; the last is the previous turn's), the points
// its debates settled, and the previous turn's final answer. Unknown
// fields are ignored (principle 4).
type RequestContext struct {
	Earlier []string        `json:"earlier,omitempty"`
	Settled []judge.Settled `json:"settled,omitempty"`
	Last    string          `json:"last,omitempty"`
}

// ContextInfo is /v1/info's context: the agent takes a context with these
// limits. The phone sends one only when it is there.
type ContextInfo struct {
	MaxSettled   int `json:"maxSettled"`
	MaxLastBytes int `json:"maxLastBytes"`
}

func contextInfo() *ContextInfo {
	return &ContextInfo{MaxSettled: judge.MaxSettled, MaxLastBytes: judge.MaxLastAnswerBytes}
}

// maxContextBody is what a context may add to a request's body: its
// limits, twice for JSON escapes, and room for the names.
const maxContextBody = 2*(judge.MaxEarlier*judge.MaxEarlierBytes+judge.MaxSettled*(judge.MaxSettledTopic+judge.MaxSettledLine)+judge.MaxLastAnswerBytes) + 4<<10

const msgBadContext = "context must be {earlier, settled, last}: at most 3 earlier questions of 4 KiB, at most 12 settled points {topic, result, line} with a topic of 512 B, a result agreed, conditional, differ or kept and a line of 1 KiB, and a last answer of 8 KiB, all text"

// settledResults are the results a settled point may have: a debate's
// outcomes.
var settledResults = map[string]bool{
	judge.OutcomeAgreed: true, judge.OutcomeConditional: true, judge.OutcomeDiffer: true, judge.OutcomeKept: true,
}

// conversationOf checks c and gives the record the prompts take: nil for
// no context or one without a settled point or a last answer, ok=false
// when c is over a limit or not text. The previous question is the last
// earlier one.
func conversationOf(c *RequestContext) (conv *judge.Conversation, ok bool) {
	if c == nil {
		return nil, true
	}
	text := func(s string, max int) bool {
		return len(s) <= max && utf8.ValidString(s) && !strings.ContainsRune(s, 0)
	}
	if len(c.Earlier) > judge.MaxEarlier || len(c.Settled) > judge.MaxSettled || !text(c.Last, judge.MaxLastAnswerBytes) {
		return nil, false
	}
	out := &judge.Conversation{Last: strings.TrimSpace(c.Last)}
	for _, e := range c.Earlier {
		if !text(e, judge.MaxEarlierBytes) {
			return nil, false
		}
		if e = strings.TrimSpace(e); e != "" {
			out.Earlier = append(out.Earlier, e)
		}
	}
	for _, s := range c.Settled {
		if !text(s.Topic, judge.MaxSettledTopic) || !text(s.Line, judge.MaxSettledLine) || !settledResults[s.Result] {
			return nil, false
		}
		s.Topic, s.Line = strings.TrimSpace(s.Topic), strings.TrimSpace(s.Line)
		if s.Topic != "" && s.Line != "" {
			out.Settled = append(out.Settled, s)
		}
	}
	if out.Last != "" && len(out.Earlier) > 0 {
		out.LastQuestion = out.Earlier[len(out.Earlier)-1]
	}
	if len(out.Settled) == 0 && out.Last == "" {
		return nil, true // earlier questions alone carry no conclusion
	}
	return out, true
}
