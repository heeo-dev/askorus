//go:build syntheval

package main

import (
	"strings"

	"github.com/asksemble/asksemble/agent/internal/judge"
)

// Drafts the quality experiments try (C4, E2 and E3). None is the
// agent's: a draft that wins is moved into internal/judge with its golden
// file, by a decision of the person.

// synthV4 is the combined-answer draft of E2 (#11 and the 11-04
// feedback): the writer reads the person's intent itself (count, length,
// form, conditions) and follows it; the answer comes first, the agreed
// content is stated plainly without attributing it, every DIFFER is still
// named, detail only one AI gives is kept only when it helps, and no
// process talk. The data block is the agent's, so only the instructions
// differ from product; a spec is not used.
func synthV4(task judge.SynthTask, nonce string) string {
	t := task
	t.Spec = nil
	p := judge.SynthPrompt(t, nonce)
	i := strings.Index(p, "TASK. Write in the language of the QUESTION")
	j := strings.LastIndex(p, "OUTPUT. ")
	if p == "" || i < 0 || j < i {
		return ""
	}
	p = p[:i] + synthV4Task + p[j:]
	return strings.Replace(p, "[Asksemble synthesis v2]", "[Asksemble synthesis v4-draft]", 1)
}

const synthV4Task = `TASK. Write in the language of the QUESTION, in Markdown, for the person who asked it.
1. First read what the person wants: the thing asked, how many items they want if they said so, who it is for, the conditions they gave, and any form or length they asked for. Follow all of it: when they asked for a number of items, a length or a form (such as a table or one line), give exactly that.
2. Begin with the answer itself in one or two sentences: the fact, the number, the pick or the conclusion. No preamble.
3. Then give what the person needs, in the shape that fits it: a Markdown table when several items, options or numbers are compared (a row per item); a numbered list for steps; for recommendations, the items with a short reason each (several unless the person asked for one); otherwise short paragraphs or bullets. Use headings only when the answer has several distinct parts.
4. State what the answers agree on plainly, as the answer, without saying which AIs said it or that they agree.
5. For every DIFFER entry, and every other place where the answers clearly differ on something, say that the AIs differ, name the AIs of each side exactly as written in "ais" and what each side says, and do not decide which side is right; you may add a side's own reason or source. Put the differences where they belong in the answer, or together after the main answer when there are several. Mention CHECK entries only when they change what the person should do or believe, as something to verify.
6. Detail that only one AI gives: keep it only when it helps the person, in a short line marked with that AI's name, such as "({{Codex}}만)" in Korean or "({{Codex}} only)" in English; leave the rest out.
7. Never write about the process: not how the answers were compared or written, not "all three answers", "the review", "not verified by the comparison", nor that you do not judge which side is right. Write to the person about their question only.
8. When the QUESTION is not in English, write terms in its language, with the original term in parentheses the first time when it helps, such as "지연 시간(latency)". Write every name of an AI that answered in double braces, exactly as written in "ais", such as {{Claude}}: write a name without braces when it is a product or company the QUESTION is about, and put braces around nothing else. Add nothing that no answer says. Be as long as the QUESTION needs and no longer: a simple question gets a few lines.
`

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
