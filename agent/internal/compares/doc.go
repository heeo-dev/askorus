// Package compares runs comparisons (P2-3) in memory: the AIs' answers to
// one question, taken once its run has ended (exactly once) or given
// directly, and the combined answer (종합 답변) one AI, the moderator,
// writes from them. Until C6 (#19, 2026-11-05) the AIs also judged each
// other's answers unit by unit; C6d (#24) removed that, and the snapshot's
// requests, judgments and items stay as empty lists. internal/judge holds
// the prompt and its checks; this package drives them: who may write
// (Gate), how many one-off processes run at once, cancel, cleanup and
// expiry. Nothing here knows a particular AI (principle 2): a writer is a
// core.Synthesizer that names its models' companies (core.Judger), and an
// OpenCode-like engine that leaves conversations behind is a
// core.JudgeCleaner.
//
// A comparison belongs to the device that made it, lives KeepFor (a day)
// after it ended and does not survive an agent restart. Logs carry codes and
// counts only: never a question, an answer, an AI's message or a nonce.
// Comparison errors are judge.Error values, whose Error() is the code
// only.
//
// The design is "P2-3 비교 판정 설계" in docs/phase2-plan.md, parts 3
// (processes at once), 6 (API, lifetime, logs), 8 (engines, cleanup) and
// 20 (the combined answer).
package compares
