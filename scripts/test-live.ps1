# Live scenario tests (docs/testing.md, LT-xx): the agent with the real
# claude on this PC. Sessions are only made in the test folder
# .local\rc-test, and what the tests start is removed. Asks (LT-05) also
# use the real codex and opencode, in %LOCALAPPDATA%\Asksemble\ask\live;
# their conversations stay there as history, like any ask. Run after
# changing how the agent drives an AI. Keep this file ASCII.
#
# LT-06a, judging (P2-3 S2, agent/internal/live/judge_live_test.go):
#   each adapter's Judge with the real claude, codex and opencode. About 6
#   judge calls, two per AI (it uses Codex quota; one more when an output
#   is refused and retried), in the judge folder
#   %LOCALAPPDATA%\Asksemble\ask\judge\live. Judge conversations are not
#   kept: Claude and Codex keep none, and OpenCode's are deleted at the
#   end. Codex's judge schema goes to a temporary folder of the test, never
#   to %APPDATA%\Asksemble, the folder of the agent you run (R-30). A
#   connection to the test listener, or the secret marker anywhere, fails
#   it: then stop and ask the user.
#
# LT-06b, a comparison through the API (P2-3 S3,
#   agent/internal/live/compare_live_test.go): POST /v1/comparisons with
#   LT-06a's three answers and no asks. Six judge calls, two per AI (two
#   of them Codex's), in the judge folder
#   %LOCALAPPDATA%\Asksemble\ask\live\judge. Afterwards no Claude
#   transcript, Codex rollout or thread history holds its marker, the
#   real opencode's session list (read only, no model call) has no judge
#   conversation left there, and GET /v1/sessions lists nothing there.
#
# LT-07, a follow-up while judging (P2-3 S3): one ask with compare to all
#   three on the quick tier in %LOCALAPPDATA%\Asksemble\ask\live, then a
#   follow-up on each session while the six judges run. Two asks and two
#   judge calls per AI (Codex: 2 asks, 2 judges). The asks stay there as
#   history, like LT-05's; the judges keep nothing.
#
# LT-08, combined answers (P2-3 S5b, agent/internal/live/synth_live_test.go):
#   each adapter's Synthesize once with the real claude, codex and opencode
#   on the quick tier's ask model (one Codex call; one more only when an
#   output is refused and written again), in the judge folder
#   %LOCALAPPDATA%\Asksemble\ask\judge\live. LT-06a's three answers, one
#   with an injection, and one fixed differ item (Claude vs Codex and
#   OpenCode). The combined answer must be valid and name both sides. No
#   Claude transcript, Codex rollout or thread history may hold its
#   marker, OpenCode's conversation is deleted, and its session list (read
#   only) shows none left. Codex's synth schema goes to a temporary folder
#   of the test (R-30). A connection to the test listener, a tool, or the
#   secret marker anywhere fails it: then stop and ask the user.
#
# One full run judges with Codex about 6 times (LT-06a 2, LT-06b 2,
# LT-07 2), writes one combined answer with Codex (LT-08 1) and asks Codex
# 5 times (LT-05 3, LT-07 2).
#
#   powershell -NoProfile -File scripts\test-live.ps1              # every LT
#   powershell -NoProfile -File scripts\test-live.ps1 -Run LT06a   # only LT-06a
#   powershell -NoProfile -File scripts\test-live.ps1 -Run LT06b   # only LT-06b
#   powershell -NoProfile -File scripts\test-live.ps1 -Run LT07    # only LT-07
#   powershell -NoProfile -File scripts\test-live.ps1 -Run LT08    # only LT-08
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
