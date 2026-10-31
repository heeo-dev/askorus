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
#   end. Codex's judge schema goes to %APPDATA%\Asksemble\judge-schema-v1.json,
#   as with the real agent. A connection to the test listener, or the
#   secret marker anywhere, fails it: then stop and ask the user.
#
#   powershell -NoProfile -File scripts\test-live.ps1              # every LT
#   powershell -NoProfile -File scripts\test-live.ps1 -Run LT06a   # only LT-06a
param([string]$Run)
$go = 'C:\Users\me\sdk\go1.27.3\bin'
if (Test-Path $go) { $env:Path = "$go;" + $env:Path }
# The agent starts at logon, not inside a Claude Code session: when this
# runs from one, drop that session's variables so the AIs see what they
# see under the real agent. Variables set for the user or the PC stay.
Get-ChildItem env: | Where-Object { $_.Name -eq 'CLAUDECODE' -or $_.Name -eq 'CLAUDE_PID' -or $_.Name -like 'CLAUDE_CODE_*' } |
  Where-Object { $null -eq [Environment]::GetEnvironmentVariable($_.Name, 'User') -and $null -eq [Environment]::GetEnvironmentVariable($_.Name, 'Machine') } |
  ForEach-Object { Remove-Item "env:$($_.Name)" }
$goArgs = @('test', '-tags', 'live', '-count=1', '-timeout', '20m', '-v')
if ($Run) { $goArgs += @('-run', $Run) }
$goArgs += './internal/live'
Push-Location (Join-Path (Split-Path -Parent $PSScriptRoot) 'agent')
go @goArgs 2>&1 | ForEach-Object { "$_" } | Select-String -Pattern '^(--- |    |ok|FAIL|PASS)'
$code = $LASTEXITCODE
Pop-Location
if ($code -eq 0) { Write-Output 'LIVE PASSED' } else { Write-Output 'LIVE FAILED' }
exit $code
