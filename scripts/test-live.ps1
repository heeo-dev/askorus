# Live scenario tests (docs/testing.md, LT-xx): the agent with the real
# claude on this PC. Sessions are only made in the test folder
# .local\rc-test, and what the tests start is removed. Asks (LT-05) also
# use the real codex and opencode, in %LOCALAPPDATA%\Asksemble\ask\live;
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
#   %LOCALAPPDATA%\Asksemble\ask\judge\live. Three short answers, one
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
# One full run writes one combined answer with Codex (LT-08 1), refines
# once with Codex (LT-09 1) and asks Codex 3 times (LT-05).
#
#   powershell -NoProfile -File scripts\test-live.ps1              # every LT
#   powershell -NoProfile -File scripts\test-live.ps1 -Run LT08    # only LT-08
#   powershell -NoProfile -File scripts\test-live.ps1 -Run LT09    # only LT-09
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
