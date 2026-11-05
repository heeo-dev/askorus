//go:build syntheval

package main

import (
	"strings"

	"github.com/asksemble/asksemble/agent/internal/judge"
)

// Drafts the quality experiments try (C4, E2 and E3). None is the
// agent's: a draft that wins is moved into internal/judge with its golden
// file, by a decision of the person. The combined-answer drafts v4 and
// v4.1 of E2 went there as synth-v4 on 2026-11-04 (v4.1, word for word),
// and c5-sq as synth-v5 on 2026-11-05 (C6, #19; TestProductIsC5SQ).

// c5ProductHead is the version tag the product prompt starts with,
// synth-v5's; the drafts swap it for their own.
const c5ProductHead = "[Asksemble synthesis v5.1]"

// synthC5Solo is the moderator alone with no list under the answer (#19,
// 11-05, the person's call: a list of every difference is not better, many
// are of no interest to the person; the combined answer says the ones that
// matter, in a part of their own when it fits). It keeps the product
// prompt's head and data block (the question and the answers) and puts
// its own TASK after them: synth-v4.2's TASK with the comparison taken
// out (no DIFFER or CHECK entries and no COMPARED or NOT COMPARED line);
// rule 5 without its DIFFER and CHECK words, 다르다 in the answer's speech
// level; rule 7 without "the review" and "not verified by the
// comparison"; the AIs named as the data does, "ai". Rules 1-4 and 6 are
// synth-v4.2's, word for word.
func synthC5Solo(task judge.SynthTask, nonce string) string {
	p := judge.SynthPrompt(task, nonce)
	end := "</data-" + nonce + ">\n"
	i := strings.LastIndex(p, end)
	if p == "" || i < 0 || !strings.HasPrefix(p, c5ProductHead) {
		return ""
	}
	return strings.Replace(p[:i+len(end)], c5ProductHead, "[Asksemble synthesis c5-solo-draft]", 1) + c5SoloTask
}

const c5SoloTask = `TASK. Write in the language of the QUESTION, in Markdown, for the person who asked it.
1. First read what the person wants: the thing asked, how many items they want if they said so, who it is for, the conditions they gave, and any form or length they asked for. Follow all of it: when they asked for a number of items, a length or a form (such as a table or one line), give exactly that. When they asked for a number of items and the answers give more, different ones, give that number: first the items more than one AI gave, then items taken from each AI in turn, each marked with the AI that gave it; then name the others in one short line.
2. Begin with the answer itself in one or two sentences: the fact, the number, the pick or the conclusion. No preamble.
3. Then give what the person needs, in the shape that fits it: a Markdown table when several items, options or numbers are compared (a row per item); a numbered list for steps; for recommendations, the items with a short reason each (several unless the person asked for one); otherwise short paragraphs or bullets. Use headings only when the answer has several distinct parts.
4. State what the answers agree on plainly, as the answer, without saying which AIs said it or that they agree.
5. For every place where the answers clearly differ on something, say that the AIs differ (in Korean with 다르다 in the speech level of the rest of the answer, such as 다릅니다, never 갈리다), name the AIs of each side exactly as written in "ai" and what each side says, and do not decide which side is right; you may add a side's own reason or source. Put the differences where they belong in the answer, or together after the main answer when there are several.
6. Detail that only one AI gives: keep it only when it helps the person, in a short line marked with that AI's name, such as "({{Codex}}만)" in Korean or "({{Codex}} only)" in English; leave the rest out.
7. Never write about the process: not how the answers were compared or written, not "all three answers", nor that you do not judge which side is right. Write to the person about their question only.
8. When the QUESTION is not in English, write terms in its language, with the original term in parentheses the first time when it helps, such as "지연 시간(latency)". Write every name of an AI that answered in double braces, exactly as written in "ai", such as {{Claude}}: write a name without braces when it is a product or company the QUESTION is about, and put braces around nothing else. Add nothing that no answer says. Be as long as the QUESTION needs and no longer: a simple question gets a few lines.
OUTPUT. One JSON object and nothing else: {"answer":"<the combined answer in Markdown>"}
`

// c5SoloEdits are the changes the 11-05 night tries on c5-solo, by name,
// each an exact text and its replacement (the person asked for a natural,
// meaningful combined answer; many differences are of no interest to the
// person, and the AI judges keep calling the one-AI labels clutter).
// sel keeps only the differences the person would care about; nat writes
// a one-AI detail into the answer and names its AI only for a claim the
// person may weigh.
var c5SoloEdits = map[string][][2]string{
	"sel": {
		{"5. For every place where the answers clearly differ on something, say that the AIs differ",
			"5. For every place where the answers clearly differ on something the person would care about (a fact, a number, a pick or verdict, a recommendation, a step, a warning), say that the AIs differ"},
		{"Put the differences where they belong in the answer, or together after the main answer when there are several.",
			"Leave out differences the person would not care about: other examples or wording, emphasis or order, numbers that differ only by rounding, and a point only one AI makes (rule 6). Put the differences where they belong in the answer, or together after the main answer when there are several, each in a line or two."},
	},
	"nat": {
		{`6. Detail that only one AI gives: keep it only when it helps the person, in a short line marked with that AI's name, such as "({{Codex}}만)" in Korean or "({{Codex}} only)" in English; leave the rest out.`,
			`6. Detail that only one AI gives: keep it only when it helps the person, and write it into the answer where it fits. Mark it with that AI's name, such as "({{Codex}}만)" in Korean or "({{Codex}} only)" in English, only when it is a claim the person may want to weigh or check: a number, a fact, a pick or a warning that the other answers do not give. Leave the rest out.`},
	},
	// few and nodup came after the first three were scored (04:22: sel won
	// 31:18 over solo; nat lost 19:28, its labels rose 53 to 120).
	"few": {
		{`6. Detail that only one AI gives: keep it only when it helps the person, in a short line marked with that AI's name, such as "({{Codex}}만)" in Korean or "({{Codex}} only)" in English; leave the rest out.`,
			`6. Detail that only one AI gives: keep only the few that help the person most, at most three, each in a short line marked with that AI's name, such as "({{Codex}}만)" in Korean or "({{Codex}} only)" in English; leave the rest out. A safety warning that only one AI gives always stays, marked the same way, in the part of the answer it belongs to.`},
	},
	"nodup": {
		{"Be as long as the QUESTION needs and no longer: a simple question gets a few lines.",
			"Be as long as the QUESTION needs and no longer: a simple question gets a few lines. Say each thing once: no part repeats what another part already says."},
	},
	// inline comes after sel (05:05: c5-m 27 : sel 21, c5-m read more
	// easily 33:16; the judges keep marking talk about the AIs down): each
	// difference sits in the advice it belongs to, not in a part about the
	// AIs. It applies after sel.
	"inline": {
		{"Put the differences where they belong in the answer, or together after the main answer when there are several, each in a line or two.",
			"Put each difference in a line or two at the point of the answer it belongs to, so that it reads as part of the advice there. Gather differences in a part of their own only when they are about the answer as a whole and belong to no one point."},
	},
	// quiet: the judges call AI-name tags on points nobody disputes clunky,
	// so a one-AI detail is written in plainly and only a warning, or a
	// number or pick the person would act on, keeps the tag.
	"quiet": {
		{`6. Detail that only one AI gives: keep it only when it helps the person, in a short line marked with that AI's name, such as "({{Codex}}만)" in Korean or "({{Codex}} only)" in English; leave the rest out.`,
			`6. Detail that only one AI gives: keep it only when it helps the person, written into the answer where it belongs like the rest, with no AI name. Mark it with that AI's name, such as "({{Codex}}만)" in Korean or "({{Codex}} only)" in English, only when it is a warning, or a number or a pick the person would act on. Leave the rest out, and never gather one-AI details in a part of their own.`},
	},
}

// c5SoloWith is c5-solo with the named c5SoloEdits made, in order, each
// exactly once, under the head marker [Asksemble synthesis <name>-draft].
// It gives "" for every task when an edit does not apply.
func c5SoloWith(name string, edits ...string) func(judge.SynthTask, string) string {
	t := c5SoloTask
	for _, e := range edits {
		for _, r := range c5SoloEdits[e] {
			if t == "" || strings.Count(t, r[0]) != 1 {
				t = ""
				break
			}
			t = strings.Replace(t, r[0], r[1], 1)
		}
	}
	return func(task judge.SynthTask, nonce string) string {
		p := synthC5Solo(task, nonce)
		if t == "" || p == "" {
			return ""
		}
		return strings.Replace(strings.Replace(p, "[Asksemble synthesis c5-solo-draft]", "[Asksemble synthesis "+name+"-draft]", 1), c5SoloTask, t, 1)
	}
}

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

// The language line synth-v5.1 took (#27, 2026-11-06), made firm after
// the S10 once wrote an English question's answer in Korean: the draft
// v5-lang (synth-v5 with this line alone changed) wrote the ant question
// in English 10 of 10 times, as synth-v5 did, and Korean questions as
// before (runs c27-*). TestProductIsC5SQ checks the product is c5-sq with
// this line.
const (
	v5LangLine = "TASK. Write in the language of the QUESTION, in Markdown, for the person who asked it.\n"
	v5LangFirm = "TASK. Write the whole answer in the language of the QUESTION, in Markdown, for the person who asked it: a QUESTION in English gets an answer in English, whatever language the answers are in. The Korean words in the rules below (such as 다릅니다) are only for a QUESTION in Korean.\n"
)
