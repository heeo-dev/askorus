# Live scenario tests (docs/testing.md, LT-xx): the agent with the real
# claude on this PC. Sessions are only made in the test folder
# .local\rc-test, and what the tests start is removed. Asks (LT-05) also
# use the real codex and opencode, in %LOCALAPPDATA%\Askorus\ask\live;
# their conversations stay there as history, like any ask. Run after
# changing how the agent drives an AI. Keep this file ASCII.
#
# The judging tests LT-06a, LT-06b and LT-07 (P2-3) and the topics test
# LT-10 (C5) went with the judging (C6d, #24).
#
# LT-08, combined answers (P2-3 S5b, agent/internal/live/synth_live_test.go):
#   each adapter's Synthesize once with the real claude, codex and opencode
#   on the quick tier's ask model (one Codex call; one more only when an
#   output is refused and written again), in the judge folder
#   %LOCALAPPDATA%\Askorus\ask\judge\live. Three short answers, one
#   with an injection, and synth-v5 (no differ items since C6). The
#   combined answer must be valid and name at least one AI. No
#   Claude transcript, Codex rollout or thread history may hold its
#   marker, OpenCode's conversation is deleted, and its session list (read
#   only) shows none left. Codex's synth schema goes to a temporary folder
#   of the test (R-30). A connection to the test listener, a tool, or the
#   secret marker anywhere fails it: then stop and ask the user.
#
# LT-09, question refining (milestone C, agent/internal/live/refine_live_test.go):
#   each adapter refines one Korean question once with the real claude,
#   codex and opencode on the quick tier's model (one more only when the
#   reply is not a spec), in the same judge folder. The question carries a
#   content marker and an injection line asking for another spec. Each
#   spec must be valid with language ko; OpenCode's conversation is
#   deleted and none left; no Claude transcript or Codex rollout may hold
#   the marker; nothing of the question may be logged.
#
# LT-11, a debate (P2-5, #26, agent/internal/live/debate_live_test.go):
#   the agent with the real claude and codex on the quick tier, in the live
#   ask folder; two answers that differ on a fact (the boiling point),
#   claude moderates. It must end done with a debated point, a turn with a
#   reply and an answer after the debate that keeps 100; the turns run in
#   the judge folder, which the session list leaves out. About 6 to 8 AI
#   calls, 1 or 2 of them Codex asks.
#
# LT-12, a follow-up with the conversation record (#29,
#   agent/internal/live/conversation_live_test.go): the agent with the real
#   claude and codex on the quick tier, in the live ask folder. Claude
#   answers the boiling point at sea level, then the follow-up (3,000 m)
#   goes to claude in its session and to codex anew with a record (a
#   settled point, the last answer), and claude writes the combined answer
#   (synth-v6). Both answers and the combined answer must give a
#   temperature below 100, and the combined answer must not mention a
#   debate. 4 AI calls, 1 of them a Codex ask.
#
# LT-13, asking the moderator (#25, agent/internal/live/moderator_live_test.go):
#   the real claude on the quick tier answers alone, as a one-off in the
#   judge folder, what two given answers (100 vs 90) differed on; the
#   answer must name both, the run part keeps no session and the judge
#   folder lists none. 1 AI call, no Codex.
#
# One full run writes one combined answer with Codex (LT-08 1), refines
# once with Codex (LT-09 1), asks Codex 4 times (LT-05 3, LT-12 1) and lets
# Codex speak once or twice in a debate (LT-11).
#
#   powershell -NoProfile -File scripts\test-live.ps1              # every LT
#   powershell -NoProfile -File scripts\test-live.ps1 -Run LT08    # only LT-08
#   powershell -NoProfile -File scripts\test-live.ps1 -Run LT09    # only LT-09
#   powershell -NoProfile -File scripts\test-live.ps1 -Run LT11    # only LT-11
#   powershell -NoProfile -File scripts\test-live.ps1 -Run LT12    # only LT-12
#   powershell -NoProfile -File scripts\test-live.ps1 -Run LT13    # only LT-13
param([string]$Run)
$go = Join-Path $env:USERPROFILE 'sdk\go1.27.3\bin'
if (Test-Path $go) { $env:Path = "$go;" + $env:Path }
# The agent starts at logon, not inside a Claude Code session: when this
# runs from one, drop that session's variables so the AIs see what they
# see under the real agent. Variables set for the user or the PC stay.
Get-ChildItem env: | Where-Object { $_.Name -eq 'CLAUDECODE' -or $_.Name -eq 'CLAUDE_PID' -or $_.Name -like 'CLAUDE_CODE_*' } |
  Where-Object { $null -eq [Environment]::GetEnvironmentVariable($_.Name, 'User') -and $null -eq [Environment]::GetEnvironmentVariable($_.Name, 'Machine') } |
  ForEach-Object { Remove-Item "env:$($_.Name)" }
$goArgs = @('test', '-tags', 'live', '-count=1', '-timeout', '30m', '-v')
if ($Run) { $goArgs += @('-run', $Run) }
$goArgs += './internal/live'
Push-Location (Join-Path (Split-Path -Parent $PSScriptRoot) 'agent')
go @goArgs 2>&1 | ForEach-Object { "$_" } | Select-String -Pattern '^(--- |    |ok|FAIL|PASS)'
$code = $LASTEXITCODE
Pop-Location
if ($code -eq 0) { Write-Output 'LIVE PASSED' } else { Write-Output 'LIVE FAILED' }
exit $code
