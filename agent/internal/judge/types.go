package judge

// Ids and offsets. An answer's id is the name of the AI that wrote it
// ("claude") for a PC answer, or the id the phone gave a pasted one. A
// unit's id is "<answer id>:<n>" ("claude:3"), n counting from 1 in
// reading order. Offsets the phone gets (Unit.Start, UnitMath.Start,
// NumberSpan.Start) count UTF-16 code units, as JavaScript strings do;
// offsets that stay in Go (Quantity, MathSpan) count bytes.

// Unit kinds.
const (
	UnitSentence = "sentence" // a sentence of a paragraph
	UnitItem     = "item"     // a list item, or one sentence of it
	UnitRow      = "row"      // a table body row, its cells joined by " | "
	UnitQuote    = "quote"    // a sentence of a block quote
)

// Labels: how the other answer, read as a whole, relates to a unit.
const (
	LabelSame     = "same"     // it says the same thing
	LabelOpposite = "opposite" // it says something that cannot be true together with the unit
	LabelAbsent   = "absent"   // it says nothing either way
	// LabelUnclear is the fourth label of the v1four set, which only
	// judge-eval uses. It counts as unsure, with no usable relation.
	LabelUnclear = "unclear"
)

// Shapes and label sets of a comparison (core.JudgeTask.Shape and
// Labels). The product uses ShapeDirected and LabelsV1; the others are for
// judge-eval only.
const (
	ShapeDirected = "directed" // one request per direction: X's units against Y
	ShapePair     = "pair"     // both directions of a pair in one request
	LabelsV1      = "v1"       // same, opposite or absent, plus unsure (core.JudgeSchemaV1)
	LabelsV1Four  = "v1four"   // same, opposite, absent or unclear, without unsure
)

// Rules names the judge prompt and its rules, as /v1/info shows them.
const Rules = "judge-v1"

// Card kinds (Item.Kind). A shown unit is on one card at most.
const (
	CardDiffer  = "differ"  // 다른 말: the AIs said different things
	CardCheck   = "check"   // 확인 필요: no opposite, but an enabled check fired
	CardAgree   = "agree"   // 같은 말: two or more AIs said the same, none against
	CardOnlyOne = "onlyOne" // 한 AI만: only one AI said it
)

// CardOrder is the order of the cards on the phone (결정 2).
var CardOrder = []string{CardDiffer, CardCheck, CardAgree, CardOnlyOne}

// Checks are code checks on a judgment. Each only raises a hand and never
// changes a label. Every check is computed and kept in Judgment.Flags; the
// enabled ones (agent.json judge.checks) put a unit on the 확인 필요 card.
const (
	CheckUnsure         = "unsure"         // the judge was not sure (or said unclear)
	CheckNoEvidence     = "noEvidence"     // same or opposite, but no evidence
	CheckCrossDirection = "crossDirection" // the two directions disagree: u same as v, v opposite to u
	CheckNumberMismatch = "numberMismatch" // same, but the numbers of a kind do not fit
	CheckNumbersAbsent  = "numbersAbsent"  // absent, but the other answer has other numbers of the kind
)

// AllChecks lists every check, for checking the settings.
var AllChecks = []string{CheckUnsure, CheckNoEvidence, CheckCrossDirection, CheckNumberMismatch, CheckNumbersAbsent}

// DefaultChecks are the checks enabled unless agent.json judge.checks names
// others: unsure and the two number checks (S6, 11-01 user decision 2).
// With them an unsure same or absent goes to 확인 필요 only when its number
// check fires (see Aggregate).
var DefaultChecks = []string{CheckUnsure, CheckNumberMismatch, CheckNumbersAbsent}

// Request statuses.
const (
	RequestQueued  = "queued" // waiting for a judge process slot
	RequestRunning = "running"
	RequestDone    = "done"
	RequestFailed  = "failed" // Error says why; a no_judge record is failed from the start
)

// Where an answer came from (Answer.Via).
const (
	ViaPC      = "pc"      // an AI program on the PC answered
	ViaHandoff = "handoff" // pasted from an official app (P2-4)
)

// Answer statuses.
const (
	AnswerReady   = "ready"   // the answer is here, split into units
	AnswerMissing = "missing" // the AI gave no answer; it is not compared
)

// Unit states (UnitState.State) besides the card kinds.
const (
	StateHidden      = "hidden"      // compared, but no judge called it core and it is in no opposite
	StateNotCompared = "notCompared" // no usable judgment, because its requests failed
	StateNotJudged   = "notJudged"   // after the first maxUnits units of its answer (agent.json judge.maxUnits)
	StateWaiting     = "waiting"     // its card waits until every request has ended
)

// Kinds of quantities (Quantity.Kind). Quantities of different kinds are
// never compared.
const (
	NumDuration = "duration" // 8분 20초, 3 hours
	NumDate     = "date"     // 2026년 9월 18일, 9월 18일, Sep 18
	NumYear     = "year"     // 1998년, in 2026
	NumPercent  = "percent"  // 30%, 30 percent
	NumMoney    = "money"    // 3만 원, $10
	NumCount    = "count"    // 970만 명, 3개
	NumRatio    = "ratio"    // 1.5배, 10 times
	NumMeasure  = "measure"  // a number with a unit: 300 mg, 1.5 km
	NumVersion  = "version"  // 1.99.1
)

// Limits.
const (
	MaxUnits        = 40       // most units judged per answer that agent.json judge.maxUnits accepts; the rest are only evidence
	DefaultMaxUnits = 30       // units judged per answer when judge.maxUnits is not set (S6 rule 4: p90 request over 30 s)
	MaxAnswerBytes  = 64 << 10 // of an answer that are split
	MaxUnitChars    = 600      // characters of a unit before it is cut at "; " or ", "
	MaxEvidence     = 3        // evidence ids kept per judgment
	MaxOutputBytes  = 1 << 20  // of a judge's output; more is refused
	MaxEarlier      = 3        // earlier questions a judge gets
	MaxEarlierBytes = 4 << 10  // of one earlier question, in UTF-8
)

// Unit is one piece of an answer that is judged: a sentence, a list item,
// a table row or a sentence of a quote. Text is the AI's own words, the
// raw span with only Markdown marks taken out, so the phone shows nothing
// a judge wrote. An ordered item's first unit keeps its number ("1.
// 서울"); a bullet's does not. Fields may be added inside /v1, never
// removed (principle 4).
type Unit struct {
	ID      string `json:"id"`                // "<answer id>:<n>"
	N       int    `json:"n"`                 // 1, 2, … in reading order
	Kind    string `json:"kind"`              // UnitSentence, UnitItem, UnitRow or UnitQuote
	Text    string `json:"text"`              // Strip(Kind, raw span)
	Context string `json:"context,omitempty"` // a row's table header (first 300 characters), a nested item's parent's Text (first 120)
	Start   int    `json:"start"`             // UTF-16 offsets of the raw span in the answer text
	End     int    `json:"end"`
	Judged  bool   `json:"judged"` // false after the first maxUnits units: then it is only evidence
	// Math are the places in Text that the answer drew as math, in order:
	// the inline math the splitter read in the raw span. The phone draws
	// math there only and never looks for "$" in Text itself, where code
	// ("`$HOME`") and escapes ("\$5") have lost their marks. None: no key.
	Math []UnitMath `json:"math,omitempty"`
	// Before are the context lines between the previous unit and this one:
	// headings, and code or formulas that are not judged. Judges get them;
	// the phone does not.
	Before []string `json:"-"`
}

// Answer is one AI's answer in a comparison. Fields may be added inside
// /v1, never removed (principle 4).
type Answer struct {
	ID     string `json:"id"`     // the AI's name for a PC answer, or the phone's id for a pasted one
	Agent  string `json:"agent"`  // the AI program or app that wrote it: claude, codex, chatgpt, …
	Via    string `json:"via"`    // ViaPC or ViaHandoff
	Family string `json:"family"` // its company: the adapter's Family(model), or HandoffFamily(Agent)
	Model  string `json:"model,omitempty"`
	Status string `json:"status"` // AnswerReady or AnswerMissing
	// NotJudged counts the units after the first maxUnits, so the phone can
	// say only the first ones were compared.
	NotJudged int    `json:"notJudged"`
	Units     []Unit `json:"units"`
	// Tail are the context lines after the last unit, from SplitAnswer:
	// code or a formula that ends the answer, or all of an answer that is
	// only code. Judges get them; the phone does not.
	Tail []string `json:"-"`
	// Text is the answer exactly as the AI gave it. It is split here and
	// never sent back: the phone has it already.
	Text string `json:"-"`
}

// JudgeAI is an AI that may judge in a comparison: a PC AI that announces
// judge, is available and is in the comparison's judges list (결정 5: only
// AIs turned on for the question).
type JudgeAI struct {
	Agent  string // the AI program, e.g. "codex"
	Family string // the company of its judge model, from its adapter's Family
	Model  string // the judge model
	Effort string // the judge effort
	// AskCode is how this AI's own answer to the question ended when it
	// gave none: ask_timeout, ask_empty or ask_failed. It is empty when the
	// AI answered or was not asked. A judge-only AI whose ask failed with
	// ask_failed (often a usage limit) is picked last.
	AskCode string
}

// Spec is the input of one comparison: the question, the answers to
// compare and the AIs that may judge them.
type Spec struct {
	Question string    // the question as the person wrote it
	Earlier  []string  // earlier questions every judge already got, oldest first; at most MaxEarlier of MaxEarlierBytes
	Answers  []Answer  // in the turn's order, split into units
	Judges   []JudgeAI // in order of preference
	Shape    string    // ShapeDirected when empty (ShapePair in judge-eval)
	Labels   string    // LabelsV1 when empty (LabelsV1Four in judge-eval)
}

// Request is one judge call of a comparison: Judge judges the units of
// answer From against answer Against. Fields may be added inside /v1,
// never removed (principle 4).
type Request struct {
	ID           string `json:"id"`              // r1, r2, … in plan order
	From         string `json:"from"`            // the answer whose units are judged
	Against      string `json:"against"`         // the answer they are compared with
	Judge        string `json:"judge"`           // the judging AI; "" in a no_judge record
	JudgeOnly    bool   `json:"judgeOnly"`       // the judge wrote no answer of the comparison: '(판정만)'
	Model        string `json:"model,omitempty"` // the judge model
	Status       string `json:"status"`          // RequestQueued, RequestRunning, RequestDone or RequestFailed
	Attempt      int    `json:"attempt"`         // how many times it ran: 2 after the retry on bad output
	Ms           int64  `json:"ms,omitempty"`
	InputTokens  int    `json:"inputTokens,omitempty"`
	OutputTokens int    `json:"outputTokens,omitempty"`
	Error        *Error `json:"error,omitempty"`
}

// Error is why a request or a comparison failed. Message is for the
// person: for judge_failed the AI's own text, otherwise a fixed sentence.
// Error() gives the code only, so a message never reaches a log through an
// error value.
type Error struct {
	Code    string `json:"code"`
	Message string `json:"message,omitempty"`
}

func (e *Error) Error() string { return e.Code }

// Verdict is one entry of a judge's output after Validate, still in the
// request's own ids (a3, b1).
type Verdict struct {
	ID       string   // the judged unit: a<n>, or b<n> in the pair shape
	Core     bool     // the unit itself answers the question
	Evidence []string // ids of the other answer, best first, at most MaxEvidence
	Label    string   // LabelSame, LabelOpposite or LabelAbsent (LabelUnclear in v1four)
	Unsure   bool
	// Reason is the judge's few words on what it compared. Only judge-eval
	// keeps it: Judgments leaves it behind, so it never reaches the API,
	// the screen or a log.
	Reason string
}

// Validated is a judge's output after Validate.
type Validated struct {
	Verdicts []Verdict // in the order the ids were sent
	// Missing are ids that were sent but got no valid entry. The request is
	// asked again once, with a new nonce; ids still missing stay unjudged.
	Missing []string
	// Dropped counts what was thrown away: entries with unknown or repeated
	// ids, and evidence ids not of the other answer, repeated or past
	// MaxEvidence.
	Dropped int
}

// Judgment is one judge's verdict on one unit against one other answer,
// in unit ids. A comparison keeps every judgment, so the cards can be made
// again under other rules without judging again. There is no reason: a
// judge's words never reach the phone. Fields may be added inside /v1,
// never removed (principle 4).
type Judgment struct {
	Unit     string   `json:"unit"`     // the judged unit
	Against  string   `json:"against"`  // the answer it was compared with
	Request  string   `json:"request"`  // the request that judged it
	Judge    string   `json:"judge"`    // the judging AI
	Core     bool     `json:"core"`     // the judge says the unit itself answers the question
	Label    string   `json:"label"`    // LabelSame, LabelOpposite or LabelAbsent
	Evidence []string `json:"evidence"` // units of Against, best first, at most MaxEvidence; empty, not nil
	Unsure   bool     `json:"unsure"`
	Flags    []string `json:"flags"` // the checks that fired, enabled or not, set by Aggregate; empty, not nil
}

// Item is one entry on a card. Fields may be added inside /v1, never
// removed (principle 4).
type Item struct {
	// Key stays while the cards fill in: "d:" and the first opposite
	// judgment of the item's group for differ ("d:claude:3~codex:2"), "c:",
	// "a:" or "o:" and the representative unit's id for the others. When
	// items merge, the oldest key stays.
	Key  string `json:"key"`
	Kind string `json:"kind"` // CardDiffer, CardCheck, CardAgree or CardOnlyOne
	// Positions are the sides: two or more for differ, one for the others.
	Positions     []Position `json:"positions"`
	NumbersDiffer bool       `json:"numbersDiffer,omitempty"` // differ: the sides' numbers do not fit
	Check         []string   `json:"check"`                   // enabled checks that fired on the item's judgments; empty, not nil
	Missing       []string   `json:"missing"`                 // answers it could not be compared with, as their requests failed; empty, not nil
}

// Position is one side of an item: units of one or more answers that say
// the same thing.
type Position struct {
	Agents      []string     `json:"agents"`                // the answers on this side, by answer id
	Units       []string     `json:"units"`                 // their units, the representative first
	NumberSpans []NumberSpan `json:"numberSpans,omitempty"` // differ: the numbers to draw bold
}

// NumberSpan is a number in a unit's text that the other side does not
// share. The phone draws it bold and knows nothing about numbers.
type NumberSpan struct {
	Unit  string `json:"unit"`
	Start int    `json:"start"` // UTF-16 offsets into the unit's Text
	End   int    `json:"end"`
}

// Counts are the numbers on the chips, and the units on no card.
type Counts struct {
	Differ      int `json:"differ"` // items on each card
	Check       int `json:"check"`
	Agree       int `json:"agree"`
	OnlyOne     int `json:"onlyOne"`
	NotCompared int `json:"notCompared"` // units no judge compared, as their requests failed
	NotJudged   int `json:"notJudged"`   // units after the first maxUnits of their answer
}

// AggregateInput is what Aggregate makes the cards from.
type AggregateInput struct {
	Answers   []Answer   // in the turn's order, with their units
	Requests  []Request  // every request, failed ones and no_judge records included
	Judgments []Judgment // every judgment so far, in the order they came
	Checks    []string   // the enabled checks, nil for none; the agent passes DefaultChecks unless the settings name others
	Ended     bool       // every request has ended, so every card is made, not only differ
	// Prev are the items of the last call, followed by those of the last
	// call with Ended, whose keys stay. A retry runs its calls with Ended
	// false, which make only differ items, so the caller keeps both: the
	// other cards' keys then survive the retry.
	Prev []Item
}

// Summary is what Aggregate makes: the 정리 (Summary) tab.
type Summary struct {
	// Judgments are the usable judgments with their flags set; judgments
	// by the author's or the against answer's AI are left out.
	Judgments []Judgment
	Items     []Item      // in CardOrder, each card in its own order
	Units     []UnitState // every unit, in answer order and then unit order
	Counts    Counts
}

// UnitState is where one unit ended up. judge-eval reads it to count what
// each labelled claim became.
type UnitState struct {
	Unit  string `json:"unit"`
	State string `json:"state"`          // a card kind, or StateHidden, StateNotCompared, StateNotJudged or StateWaiting
	Item  string `json:"item,omitempty"` // the key of the item that shows it
}

// Quantity is one number in a text, with what it counts. Its value (an
// interval, a limit or a range, and a hedge) is private to numbers.go;
// compare quantities with Compatible, Mismatch and Unmatched.
type Quantity struct {
	Kind  string // NumDuration, NumDate, …
	Text  string // as written, e.g. "약 8분 20초"
	Start int    // byte offsets of Text in the text given to Numbers
	End   int
	num   numValue
}

// UnitMath is a place in a unit's Text that the answer drew as math:
// "$…$", "$$…$$", "\(…\)" or "\[…\]", marks included, as written in the
// answer with its white space collapsed (in a table row, an escaped "\|"
// is "|", as GFM reads it). Fields may be added inside /v1,
// never removed (principle 4).
type UnitMath struct {
	Start   int  `json:"start"` // UTF-16 offsets into the unit's Text
	End     int  `json:"end"`
	Display bool `json:"display"` // "$$…$$" or "\[…\]" inside a line
}

// MathSpan is math written inside a line: "$…$", "$$…$$", "\(…\)" or
// "\[…\]".
type MathSpan struct {
	Start, End int  // byte offsets of the span, its marks included
	Display    bool // "$$…$$" or "\[…\]"
}
