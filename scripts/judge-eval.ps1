# judge-eval: measures comparison judging (P2-3 design part 9, docs/phase2-plan.md)
# with the product's own judge path. This is measurement, not a test: a run
# USES THE PERSON'S AI SUBSCRIPTIONS (Claude Code, Codex, OpenCode quota), so
# start one only when it was agreed. report, perturb, data and a run with
# -engines gold make no model call.
#
# It builds agent/cmd/judge-eval (-tags judgeeval, never part of the agent)
# into %LOCALAPPDATA%\Asksemble\judge-eval\bin and runs it with normal rights.
# Runs and reports go to %LOCALAPPDATA%\Asksemble\judge-eval\<run>, outside the
# repo; judges run in <ask folder>\judge\eval. It never touches the running
# agent, Tailscale serve or the Claude services.
#
#   powershell -NoProfile -File scripts\judge-eval.ps1 data
#   powershell -NoProfile -File scripts\judge-eval.ps1 run -set p06 -split all -engines gold -run gold-replay
#   powershell -NoProfile -File scripts\judge-eval.ps1 run -set p06 -split dev -questions 2,12 -engines claude,codex,opencode -tier quick -run trial-2026-11-01 -expect 36 -max-calls 40
#   powershell -NoProfile -File scripts\judge-eval.ps1 report -run trial-2026-11-01
#
# Exit codes pass through: 0 done, 1 error, 2 bad flags, 3 stopped on a usage
# limit (run again with -resume), 4 interrupted, 5 call budget reached,
# 6 a judge reached for a tool or answered with another model (look first).
# Keep this file ASCII: Windows PowerShell 5.1 reads scripts without a BOM as
# the ANSI code page.
#
# No param block: [Parameter(ValueFromRemainingArguments)] makes this an
# advanced script with the common parameters, and then -out (judge-eval's
# flag) is refused as ambiguous (-OutVariable, -OutBuffer). Every argument is
# passed on as a string; an array (2,12 typed inside PowerShell) is joined
# with commas, as -File passes it.
$Rest = @()
foreach ($a in $args) {
  if ($a -is [array]) { $Rest += ($a -join ',') } else { $Rest += "$a" }
}

$root = Split-Path -Parent $PSScriptRoot
$go = 'C:\Users\me\sdk\go1.27.3\bin'
if (Test-Path $go) { $env:Path = "$go;" + $env:Path }
[Console]::OutputEncoding = [System.Text.Encoding]::UTF8

$principal = New-Object Security.Principal.WindowsPrincipal([Security.Principal.WindowsIdentity]::GetCurrent())
if ($principal.IsInRole([Security.Principal.WindowsBuiltInRole]::Administrator)) {
  Write-Output 'judge-eval runs with normal rights: use a PowerShell that is not run as administrator.'
  exit 1
}

# The agent starts at logon, not inside a Claude Code session: when this runs
# from one, drop that session's variables so the AIs see what they see under
# the real agent. Variables set for the user or the PC stay (as test-live.ps1).
Get-ChildItem env: | Where-Object { $_.Name -eq 'CLAUDECODE' -or $_.Name -eq 'CLAUDE_PID' -or $_.Name -like 'CLAUDE_CODE_*' } |
  Where-Object { $null -eq [Environment]::GetEnvironmentVariable($_.Name, 'User') -and $null -eq [Environment]::GetEnvironmentVariable($_.Name, 'Machine') } |
  ForEach-Object { Remove-Item "env:$($_.Name)" }

$exe = Join-Path $env:LOCALAPPDATA 'Asksemble\judge-eval\bin\judge-eval.exe'
Push-Location (Join-Path $root 'agent')
go build -tags judgeeval -o $exe ./cmd/judge-eval
$code = $LASTEXITCODE
Pop-Location
if ($code -ne 0) {
  Write-Output 'BUILD FAILED'
  exit 1
}

$exeArgs = @()
if ($Rest) { $exeArgs += $Rest }
$exeArgs += @('-data', (Join-Path $root 'docs\judge'))
if ($Rest -and $Rest[0] -eq 'run') {
  $commit = & git -C $root rev-parse --short HEAD
  if ($LASTEXITCODE -eq 0 -and $commit) { $exeArgs += @('-commit', "$commit".Trim()) }
}
& $exe @exeArgs
$code = $LASTEXITCODE
switch ($code) {
  3 { Write-Output 'STOPPED: usage limit, run again with -resume' }
  4 { Write-Output 'INTERRUPTED' }
  5 { Write-Output 'CALL BUDGET REACHED' }
  6 { Write-Output 'STOPPED: a judge reached for a tool or answered with another model; look at events.log before -resume' }
}
exit $code
