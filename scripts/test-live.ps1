# Live scenario tests (docs/testing.md, LT-xx): the agent with the real
# claude on this PC. Sessions are only made in the test folder
# .local\rc-test, and what the tests start is removed. Asks (LT-05) also
# use the real codex and opencode, in %LOCALAPPDATA%\Asksemble\ask\live;
# their conversations stay there as history, like any ask. Run after
# changing how the agent drives an AI. Keep this file ASCII.
#   powershell -NoProfile -File scripts\test-live.ps1
$go = 'C:\Users\me\sdk\go1.27.3\bin'
if (Test-Path $go) { $env:Path = "$go;" + $env:Path }
# The agent starts at logon, not inside a Claude Code session: when this
# runs from one, drop that session's variables so the AIs see what they
# see under the real agent. Variables set for the user or the PC stay.
Get-ChildItem env: | Where-Object { $_.Name -eq 'CLAUDECODE' -or $_.Name -eq 'CLAUDE_PID' -or $_.Name -like 'CLAUDE_CODE_*' } |
  Where-Object { $null -eq [Environment]::GetEnvironmentVariable($_.Name, 'User') -and $null -eq [Environment]::GetEnvironmentVariable($_.Name, 'Machine') } |
  ForEach-Object { Remove-Item "env:$($_.Name)" }
Push-Location (Join-Path (Split-Path -Parent $PSScriptRoot) 'agent')
go test -tags live -count=1 -v ./internal/live 2>&1 | ForEach-Object { "$_" } | Select-String -Pattern '^(--- |    |ok|FAIL|PASS)'
$code = $LASTEXITCODE
Pop-Location
if ($code -eq 0) { Write-Output 'LIVE PASSED' } else { Write-Output 'LIVE FAILED' }
exit $code
