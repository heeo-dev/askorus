// Package compares runs comparisons (비교 판정, P2-3) in memory: the AIs'
// answers to one question split into units, judged by other AIs, request
// by request, and turned into the cards of the 정리 tab. internal/judge
// holds the logic; this package drives it: who may judge (Gate), when a
// comparison starts (when its run has ended, exactly once), how many judge
// processes run at once, retries, cancel, cleanup and expiry. Nothing here
// knows a particular AI (principle 2): judges are core.Judger, and an
// OpenCode-like engine that leaves conversations behind is a
// core.JudgeCleaner.
//
// A comparison belongs to the device that made it, lives 30 minutes after
// it ended and does not survive an agent restart. Logs carry codes and
// counts only: never a question, an answer, a unit, an AI's message, a
// judge's reason or a nonce. Comparison errors are judge.Error values,
// whose Error() is the code only.
//
// The design is "P2-3 비교 판정 설계" in docs/phase2-plan.md, parts 3
// (who judges, processes at once), 4 (retries), 6 (API, lifetime, logs)
// and 8 (engines, cleanup).
package compares
