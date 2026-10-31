// Package judge is the comparison judgment (비교 판정, P2-3) apart from any
// AI. When two or more AIs answer one question, it splits each answer into
// units (sentences, list items, table rows), plans which AI judges which
// answer against which (never its own, nor one of its company's), writes
// the judge prompt, checks what a judge returns and turns the judgments
// into the cards 다른 말, 확인 필요, 같은 말 and 한 AI만. The phone shows
// only the AIs' own sentences; a judge returns ids, fixed words and
// booleans.
//
// Everything here is pure: no processes, no network, no files. The
// adapters run the judges (core.Judger) and the comparisons drive them.
// The parts are split.go and math.go (units), numbers.go (quantities),
// plan.go, prompt.go and validate.go (requests, the prompt and its checks)
// and aggregate.go (cards).
//
// The design is "P2-3 비교 판정 설계" in docs/phase2-plan.md (decided
// 2026-10-31): part 2 splitting, 3 who judges what, 4 the prompt and its
// checks, 5 the cards, 6 the API shapes these types follow and 11 the
// tests. The tests use the P0-6 judgment set in docs/judge.
package judge
