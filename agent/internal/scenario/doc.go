// Package scenario holds the scenario tests of comparisons and combined
// answers (IT-15, IT-16 and IT-18 in docs/testing.md): the whole agent,
// with its API, runs, comparisons and the three real adapters, over HTTP the way the phone
// talks to it, and internal/fakecli playing claude, codex and opencode.
// Only its tests have code; the agent never imports it.
package scenario
