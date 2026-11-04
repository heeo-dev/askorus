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
