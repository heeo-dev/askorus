package judge

import "strings"

// Answer templates (답 틀; milestone C, design C-3): one short form per
// kind, the same for every AI, in Korean for a Korean question and in
// English otherwise. The agent adds the template of the moderator's kind
// after the question, under a heading that says Askorus added it, so no
// AI takes it for the person's words. KindOpen adds nothing. The depth
// adds one line on length. The phone shows the question as written and
// the template apart (C-4).

// templateHead is the heading of the block added to the question.
var templateHead = map[string]string{
	"ko": "답 형식 (Askorus이 붙임. 질문한 사람이 쓴 글이 아닙니다)",
	"en": "Answer format (added by Askorus, not written by the person asking)",
}

// templates are the forms, kind by kind, in each language.
var templates = map[string]map[string][]string{
	"ko": {
		"fact":         {"첫 문장에 질문에 바로 답하세요.", "근거나 출처를 짧게 덧붙이세요.", "확실하지 않은 부분은 그렇다고 적으세요."},
		"number":       {"첫 문장에 숫자와 단위로 답하세요.", "계산이면 식을 한 줄로 보여 주세요.", "기준(날짜, 조건)을 적으세요."},
		"compare":      {"첫 문장에 결론을 한 줄로 쓰세요.", "비교는 Markdown 표로 쓰세요(행은 비교 기준, 열은 비교하는 것).", "끝에 고를 때 볼 점을 짧게 쓰세요."},
		"recommend":    {"첫 문장에 추천 하나를 이름으로 쓰세요.", "이유를 2~3개 쓰세요.", "다른 선택이 나은 경우를 한 줄로 쓰세요."},
		"howto":        {"첫 문장에 방법을 한 줄로 요약하세요.", "단계는 번호를 매겨 쓰세요.", "주의할 점이 있으면 마지막에 쓰세요."},
		"explain":      {"첫 문장에 한 줄 정의나 답을 쓰세요.", "핵심을 2~4개, 짧은 문단이나 글머리로 쓰세요.", "쉬운 예를 하나 드세요."},
		"troubleshoot": {"첫 문장에 가장 가능성 높은 원인을 쓰세요.", "의심할 곳을 가능성 순서로 목록이나 표로 쓰세요.", "직접 확인하는 방법과 안전 주의를 쓰세요."},
	},
	"en": {
		"fact":         {"Answer the question in the first sentence.", "Add the reason or source briefly.", "Say so where you are not sure."},
		"number":       {"Give the number with its unit in the first sentence.", "For a calculation, show it in one line.", "State what it depends on (date, conditions)."},
		"compare":      {"Give the conclusion in one line first.", "Compare in a Markdown table (a row per criterion, a column per thing compared).", "End with what to look at when choosing."},
		"recommend":    {"Name one recommendation in the first sentence.", "Give 2–3 reasons.", "Say in one line when another choice is better."},
		"howto":        {"Sum up the way in one line first.", "Number the steps.", "End with what to watch out for, if anything."},
		"explain":      {"Give a one-line definition or answer first.", "Give 2–4 key points as short paragraphs or bullets.", "Give one simple example."},
		"troubleshoot": {"Name the most likely cause in the first sentence.", "List what to suspect, most likely first, as a list or table.", "Say how to check it yourself, and any safety warning."},
	},
}

// depthLines are the length line, depth by depth.
var depthLines = map[string]map[string]string{
	"ko": {"short": "짧게 답하세요(3~5문장 안쪽).", "normal": "보통 길이로 답하세요.", "deep": "필요한 만큼 자세히 답하되, 핵심부터 쓰세요."},
	"en": {"short": "Keep it short (within 3–5 sentences).", "normal": "Keep it to a usual length.", "deep": "Go into as much detail as needed, the key points first."},
}

// templateLanguage is the templates' language for a spec's: Korean or
// English.
func templateLanguage(lang string) string {
	if lang == "ko" || strings.HasPrefix(lang, "ko-") {
		return "ko"
	}
	return "en"
}

// AnswerTemplate is the block the agent adds to the question for spec, or
// "" for KindOpen or a kind it does not know.
func AnswerTemplate(spec QuestionSpec) string {
	lang := templateLanguage(spec.Language)
	lines, ok := templates[lang][spec.Kind]
	if !ok {
		return ""
	}
	var b strings.Builder
	b.WriteString(templateHead[lang])
	b.WriteByte('\n')
	for _, l := range lines {
		b.WriteString("- " + l + "\n")
	}
	if d, ok := depthLines[lang][spec.Depth]; ok {
		b.WriteString("- " + d + "\n")
	}
	return strings.TrimSuffix(b.String(), "\n")
}

// RefinedQuestion is the question as the AIs get it: the person's words
// unchanged, then a blank line and the template; the question alone when
// there is no template.
func RefinedQuestion(question string, spec QuestionSpec) string {
	t := AnswerTemplate(spec)
	if t == "" {
		return question
	}
	return strings.TrimRight(question, " \t\r\n") + "\n\n" + t
}
