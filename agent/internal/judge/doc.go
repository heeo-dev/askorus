// Package judge holds the prompts of the one-off calls of a comparison,
// apart from any AI: the combined answer (synth.go, synth-v5) and the
// refining of a question (refine.go, refine-v1, off since 2026-11-04). It
// renders each task as a prompt with a nonce-fenced data block and checks
// what the AI returns. Everything here is pure: no processes, no network,
// no files. internal/compares drives it.
//
// The package is named after the comparison judging (비교 판정, P2-3) it
// held until C6 (#19, 2026-11-05: the moderator alone writes the combined
// answer); C6d (#24) took that code out. docs/phase2-plan.md has the
// history.
package judge
