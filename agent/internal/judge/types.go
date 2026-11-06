package judge

// What a comparison holds since judging went (C6, #19, 2026-11-05; the code
// with C6d, #24): the question, the answers and the AIs the earlier
// questions may go to. Some names keep the old "judge" words, as the API's
// do: /v1 only adds fields (principle 4).

// Where an answer came from (Answer.Via).
const (
	ViaPC      = "pc"      // an AI program on the PC answered
	ViaHandoff = "handoff" // pasted from an official app (P2-4)
)

// Answer statuses.
const (
	AnswerReady   = "ready"   // the answer is here
	AnswerMissing = "missing" // the AI gave no answer
)

// Limits.
const (
	MaxAnswerBytes  = 64 << 10 // of an answer a comparison takes
	MaxEarlier      = 3        // earlier questions the writer gets
	MaxEarlierBytes = 4 << 10  // of one earlier question, in UTF-8
)

// Answer is one answer of a comparison. Fields may be added inside /v1,
// never removed (principle 4): Units and NotJudged were the judging's and
// are always empty and 0 now.
type Answer struct {
	ID        string     `json:"id"`     // the AI's name for a PC answer, or the phone's id for a pasted one
	Agent     string     `json:"agent"`  // the AI program or app that wrote it: claude, codex, chatgpt, …
	Via       string     `json:"via"`    // ViaPC or ViaHandoff
	Family    string     `json:"family"` // its company: the adapter's Family(model), or HandoffFamily(Agent)
	Model     string     `json:"model,omitempty"`
	Status    string     `json:"status"` // AnswerReady or AnswerMissing
	NotJudged int        `json:"notJudged"`
	Units     []struct{} `json:"units"`
	// Text is the answer exactly as the AI gave it. It stays in the agent:
	// the phone has it already.
	Text string `json:"-"`
}

// Spec is the input of one comparison: the question, the answers and the
// AIs the earlier questions may go to.
type Spec struct {
	Question string   // the question as the person wrote it
	Earlier  []string // earlier questions of a follow-up, oldest first; at most MaxEarlier of MaxEarlierBytes
	Answers  []Answer // in the turn's order
	// Listed are the AIs the phone listed with the earlier questions (the
	// request's judges): each of them got every one of them. A writer gets
	// the earlier questions only when it is listed.
	Listed []string
	// Conversation is the conversation record a follow-up came with (#29);
	// nil for a first question or a phone that sends none.
	Conversation *Conversation
}

// Error is why a comparison or its combined answer failed. Message is for
// the person: a writer's judge_failed carries the AI's own text, every
// other code a fixed sentence. Error() gives the code only, so a message
// never reaches a log through an error value.
type Error struct {
	Code    string `json:"code"`
	Message string `json:"message,omitempty"`
}

func (e *Error) Error() string { return e.Code }

// Counts were the numbers on the judging's chips. A comparison still
// shows them, always 0 (principle 4).
type Counts struct {
	Differ      int `json:"differ"`
	Check       int `json:"check"`
	Agree       int `json:"agree"`
	OnlyOne     int `json:"onlyOne"`
	NotCompared int `json:"notCompared"`
	NotJudged   int `json:"notJudged"`
}
