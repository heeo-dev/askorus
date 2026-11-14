# synth-eval: measures the combined answer (milestone C, design C-9, #12,
# docs/phase2-plan.md) with the product's own ask and combined-answer path
# (the moderator alone since C6). This is measurement, not a test: a run USES THE PERSON'S AI
# SUBSCRIPTIONS (Claude Code, Codex, OpenCode quota; up to 10 calls a
# question), so start one only when it was agreed. questions and report make
# no model call.
#
# It builds agent/cmd/synth-eval (-tags syntheval, never part of the agent)
# into %LOCALAPPDATA%\Askorus\synth-eval\bin and runs it with normal rights.
# Runs go to %LOCALAPPDATA%\Askorus\synth-eval\<run>, outside the repo. The
# AIs answer in the agent's ask folder (as for the phone); the writers and
# the AIs that score run in its judge folder. It never touches the running
# agent, its Tailscale sign-in or the
# Claude services.
#
#   powershell -NoProfile -File scripts\synth-eval.ps1 questions -q agent\cmd\synth-eval\testdata\general.txt,.local\synth-eval\mine.txt
#   powershell -NoProfile -File scripts\synth-eval.ps1 run -q agent\cmd\synth-eval\testdata\general.txt,.local\synth-eval\mine.txt -run base-2026-11-03 -tier quick -expect 16
#   powershell -NoProfile -File scripts\synth-eval.ps1 report -run base-2026-11-03 -texts
#
# The quality experiments (C4, E0..E3 in docs/phase2-plan.md) add commands.
# resynth, refine and score USE AI SUBSCRIPTIONS too (ask first); pairs,
# picks and scores make no model call:
#   ... resynth -from RUN -run NAME -tier standard -expect N   (combined answers again, same answers)
#   ... refine -q FILES -run NAME -tier quick -form live -expect N   (the moderator only)
#   ... score -a RUN -b RUN -name NAME -expect N   (judges claude,codex at deep, both orders)
#   ... pairs -a RUN -b RUN -name NAME   (data for the blind A/B page)
#   ... picks -name NAME -in FILE_OR_FOLDER [-score NAME]
#
# Question paths are taken from the repo's root. Exit codes pass through:
# 0 done, 1 error, 2 bad flags, 3 stopped on a usage limit (run again with
# -resume), 4 interrupted.
# Keep this file ASCII: Windows PowerShell 5.1 reads scripts without a BOM as
# the ANSI code page.
#
# No param block: every argument is passed on as
# a string; an array typed inside PowerShell is joined with commas.
$Rest = @()
foreach ($a in $args) {
  if ($a -is [array]) { $Rest += ($a -join ',') } else { $Rest += "$a" }
}

$root = Split-Path -Parent $PSScriptRoot
$go = Join-Path $env:USERPROFILE 'sdk\go1.27.3\bin'
if (Test-Path $go) { $env:Path = "$go;" + $env:Path }
[Console]::OutputEncoding = [System.Text.Encoding]::UTF8

$principal = New-Object Security.Principal.WindowsPrincipal([Security.Principal.WindowsIdentity]::GetCurrent())
if ($principal.IsInRole([Security.Principal.WindowsBuiltInRole]::Administrator)) {
  Write-Output 'synth-eval runs with normal rights: use a PowerShell that is not run as administrator.'
  exit 1
}

# The agent starts at logon, not inside a Claude Code session: when this runs
# from one, drop that session's variables so the AIs see what they see under
# the real agent (as test-live.ps1).
Get-ChildItem env: | Where-Object { $_.Name -eq 'CLAUDECODE' -or $_.Name -eq 'CLAUDE_PID' -or $_.Name -like 'CLAUDE_CODE_*' } |
  Where-Object { $null -eq [Environment]::GetEnvironmentVariable($_.Name, 'User') -and $null -eq [Environment]::GetEnvironmentVariable($_.Name, 'Machine') } |
  ForEach-Object { Remove-Item "env:$($_.Name)" }

$base = Join-Path $env:LOCALAPPDATA 'Askorus'
$exe = Join-Path $base 'synth-eval\bin\synth-eval.exe'
Push-Location (Join-Path $root 'agent')
go build -tags syntheval -o $exe ./cmd/synth-eval
$code = $LASTEXITCODE
Pop-Location
if ($code -ne 0) {
  Write-Output 'BUILD FAILED'
  exit 1
}

# Question files are given from the repo's root.
Push-Location $root
& $exe @Rest
$code = $LASTEXITCODE
Pop-Location
switch ($code) {
  3 { Write-Output 'STOPPED: usage limit, run again with -resume' }
  4 { Write-Output 'INTERRUPTED' }
}
exit $code
