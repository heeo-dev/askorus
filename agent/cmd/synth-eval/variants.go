//go:build syntheval

package main

import (
	"strings"

	"github.com/asksemble/asksemble/agent/internal/judge"
)

// Drafts the quality experiments try (C4, E2 and E3). None is the
// agent's: a draft that wins is moved into internal/judge with its golden
// file, by a decision of the person. The combined-answer drafts v4 and
// v4.1 of E2 went there as synth-v4 on 2026-11-04 (v4.1, word for word).

// synthC5B is the C5 board's draft B (#11, 11-04): synth-v4 with the
// answer cut into marked parts the phone can draw each its own way:
// [[lead]], [[body]], [[differ]] with a topic and a line per side, and
// [[only]]. The parts travel inside the command's {"answer": "..."}: the
// writers' command takes no other schema (its arguments are checked byte
// for byte). Rules 1, 7 and 8 of synth-v4 are kept word for word; 2-6
// become the parts.
func synthC5B(task judge.SynthTask, nonce string) string {
	p := judge.SynthPrompt(task, nonce)
	i := strings.Index(p, c5TaskStart)
	if p == "" || i < 0 || !strings.HasPrefix(p, c5ProductHead) {
		return ""
	}
	return strings.Replace(p[:i], c5ProductHead, "[Asksemble synthesis c5-b-draft]", 1) + c5BTask
}

const (
	c5ProductHead = "[Asksemble synthesis v4]"
	c5TaskStart   = "TASK. Write in the language of the QUESTION"
	c5BTask       = `TASK. Write in the language of the QUESTION, in Markdown, for the person who asked it. A phone shows your answer in parts, each drawn its own way, so write it in the marked parts of rule 2.
1. First read what the person wants: the thing asked, how many items they want if they said so, who it is for, the conditions they gave, and any form or length they asked for. Follow all of it: when they asked for a number of items, a length or a form (such as a table or one line), give exactly that. When they asked for a number of items and the answers give more, different ones, give that number: first the items more than one AI gave, then items taken from each AI in turn, each marked with the AI that gave it; then name the others in one short line.
2. Write these parts in this order, each beginning with its marker at the start of a line of its own:
[[lead]] The answer itself in one or two sentences: the fact, the number, the pick or the conclusion. No preamble.
[[body]] What the person needs beyond the lead, in the shape that fits it: a Markdown table when several items, options or numbers are compared (a row per item); a numbered list for steps; for recommendations, the items with a short reason each (several unless the person asked for one); otherwise short paragraphs or bullets. Use headings only when the body has several distinct parts. State what the answers agree on plainly, as the answer, without saying which AIs said it or that they agree. Mention CHECK entries only when they change what the person should do or believe, as something to verify. Leave the body out when the lead says all the QUESTION needs.
[[differ]] followed on the same line by a short topic: one for every DIFFER entry and every other place where the answers clearly differ on something that matters to the person (entries on one topic share one marker). Under it, one line per side: that side's AIs in double braces, exactly as written in "ais", a colon, then what that side says, with the side's own reason or source if it gives one. Do not decide which side is right, and do not repeat the difference in the body.
[[only]] One line for each detail that only one AI gives and that helps the person: that AI in double braces, a colon, then the detail. Leave the rest out. Anything the person must know to stay safe goes in the body instead.
Leave out a [[differ]] or [[only]] part that would be empty.
3. Never write about the process: not how the answers were compared or written, not "all three answers", "the review", "not verified by the comparison", nor that you do not judge which side is right. Write to the person about their question only.
4. When the QUESTION is not in English, write terms in its language, with the original term in parentheses the first time when it helps, such as "지연 시간(latency)". Write every name of an AI that answered in double braces, exactly as written in "ais", such as {{Claude}}: write a name without braces when it is a product or company the QUESTION is about, and put braces around nothing else. Add nothing that no answer says. Be as long as the QUESTION needs and no longer: a simple question gets a few lines.
OUTPUT. One JSON object and nothing else: {"answer":"<the marked parts of rule 2, in Markdown>"}
`
)

// lightTemplates are E3's light answer formats: refine-v1's kinds and
// forms without the length line, with a recommendation that is not cut to
// one, a table only when several things are compared, and one line that
// keeps the person's own count, length and form. Open adds nothing.
var lightTemplates = map[string]map[string][]string{
	"ko": {
		"fact":         {"첫 문장에 질문에 바로 답하세요.", "근거나 출처를 짧게 덧붙이세요.", "확실하지 않은 부분은 그렇다고 적으세요."},
		"number":       {"첫 문장에 숫자와 단위로 답하세요.", "계산이면 식을 한 줄로 보여 주세요.", "기준(날짜, 조건)을 적으세요."},
		"compare":      {"첫 문장에 결론을 한 줄로 쓰세요.", "여러 항목을 견주면 Markdown 표로 쓰세요.", "끝에 고를 때 볼 점을 짧게 쓰세요."},
		"recommend":    {"첫 문장에 추천을 이름으로 쓰세요. 질문이 개수를 정했으면 그 개수만큼, 정하지 않았으면 몇 가지를 쓰세요.", "추천마다 이유를 짧게 쓰세요.", "다른 선택이 나은 경우가 있으면 한 줄로 쓰세요."},
		"howto":        {"첫 문장에 방법을 한 줄로 요약하세요.", "단계는 번호를 매겨 쓰세요.", "주의할 점이 있으면 마지막에 쓰세요."},
		"explain":      {"첫 문장에 한 줄 정의나 답을 쓰세요.", "핵심을 짧은 문단이나 글머리로 쓰세요.", "도움이 되면 쉬운 예를 하나 드세요."},
		"troubleshoot": {"첫 문장에 가장 가능성 높은 원인을 쓰세요.", "의심할 곳을 가능성 순서로 목록이나 표로 쓰세요.", "직접 확인하는 방법과 안전 주의를 쓰세요."},
	},
	"en": {
		"fact":         {"Answer the question in the first sentence.", "Add the reason or source briefly.", "Say so where you are not sure."},
		"number":       {"Give the number with its unit in the first sentence.", "For a calculation, show it in one line.", "State what it depends on (date, conditions)."},
		"compare":      {"Give the conclusion in one line first.", "When several things are compared, use a Markdown table.", "End with what to look at when choosing."},
		"recommend":    {"Name your recommendations in the first sentence: as many as the question asks for, or a few when it gives no number.", "Give a short reason for each.", "Say in one line when another choice is better, if it is."},
		"howto":        {"Sum up the way in one line first.", "Number the steps.", "End with what to watch out for, if anything."},
		"explain":      {"Give a one-line definition or answer first.", "Give the key points as short paragraphs or bullets.", "Give one simple example when it helps."},
		"troubleshoot": {"Name the most likely cause in the first sentence.", "List what to suspect, most likely first, as a list or table.", "Say how to check it yourself, and any safety warning."},
	},
}

// lightKeep is the line every light format ends with.
var lightKeep = map[string]string{
	"ko": "질문이 정한 개수, 길이, 형식이 있으면 그대로 따르세요.",
	"en": "When the question sets a number, a length or a form, follow it.",
}

// lightTemplate is the light format of spec under the agent's heading, or
// "" for open or a kind it does not know.
func lightTemplate(spec judge.QuestionSpec) string {
	lang := "en"
	if spec.Language == "ko" || strings.HasPrefix(spec.Language, "ko-") {
		lang = "ko"
	}
	lines, ok := lightTemplates[lang][spec.Kind]
	if !ok {
		return ""
	}
	return liveTemplate(lang, append(append([]string{}, lines...), lightKeep[lang]))
}
