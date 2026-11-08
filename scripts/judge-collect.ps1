# Collects the AIs' answers to the judgment questions (P0-6,
# docs/judge/questions.json) with the installed agent's `ask`, one question
# at a time, into docs/judge/answers-<date>.json. Run it again to fill in
# what failed (a usage limit, a time limit): answers already there are
# kept, and only the missing AIs are asked. Each ask stays in the ask
# folder's history, like any ask. Keep this file ASCII.
#   powershell -NoProfile -File scripts\judge-collect.ps1 [-Tier standard] [-Out file] [-AIs claude,codex,opencode] [-Ids 1,2]
param(
  [string]$Tier = 'standard',
  [string]$Out = '',
  [string]$AIs = 'claude,codex,opencode',
  [int[]]$Ids = @(),
  [string]$Agent = ''
)
$root = Split-Path -Parent $PSScriptRoot
$utf8 = New-Object Text.UTF8Encoding $false
[Console]::OutputEncoding = $utf8
$questions = ([IO.File]::ReadAllText((Join-Path $root 'docs\judge\questions.json'), $utf8) | ConvertFrom-Json).questions
if (-not $Out) { $Out = Join-Path $root ('docs\judge\answers-' + (Get-Date -Format 'yyyy-MM-dd') + '.json') }
if (-not $Agent) { $Agent = (Get-Command askorus-agent.exe -ErrorAction Stop).Source }
$want = $AIs.Split(',') | ForEach-Object { $_.Trim() } | Where-Object { $_ }

# The agent starts at logon, not inside a Claude Code session: drop that
# session's variables so the AIs answer as they do under the real agent.
Get-ChildItem env: | Where-Object { $_.Name -eq 'CLAUDECODE' -or $_.Name -eq 'CLAUDE_PID' -or $_.Name -like 'CLAUDE_CODE_*' } |
  Where-Object { $null -eq [Environment]::GetEnvironmentVariable($_.Name, 'User') -and $null -eq [Environment]::GetEnvironmentVariable($_.Name, 'Machine') } |
  ForEach-Object { Remove-Item "env:$($_.Name)" }

if (Test-Path $Out) {
  $data = [IO.File]::ReadAllText($Out, $utf8) | ConvertFrom-Json
} else {
  $data = [pscustomobject]@{ set = 'P0-6'; tier = $Tier; agent = "$(& $Agent version)"; started = (Get-Date -Format 's'); answers = @() }
}
$answers = New-Object System.Collections.ArrayList
foreach ($a in @($data.answers)) { [void]$answers.Add($a) }

# "== codex · gpt-6-luna · 6.0 s · codex:<id>" starts each answer (the dot is U+00B7).
$dot = [string][char]0x00B7
$head = '^== (\S+) ' + $dot + ' (.+?) ' + $dot + ' ([\d.]+) s(?: ' + $dot + ' (\S+))?$'

function Save {
  $data.answers = $answers.ToArray()
  [IO.File]::WriteAllText($Out, ($data | ConvertTo-Json -Depth 6), $utf8)
}

foreach ($q in $questions) {
  if ($Ids.Count -and $Ids -notcontains $q.id) { continue }
  $good =@($answers | Where-Object { $_.id -eq $q.id -and -not $_.error } | ForEach-Object { $_.ai })
  $missing = @($want | Where-Object { $good -notcontains $_ })
  if ($missing.Count -eq 0) { continue }
  Write-Output ("Q{0} asking {1}" -f $q.id, ($missing -join ','))
  $t0 = Get-Date
  $lines = & $Agent ask -agents ($missing -join ',') -tier $Tier $q.text 2>&1 | ForEach-Object { "$_" }
  $rows = @()
  $cur = $null
  foreach ($line in $lines) {
    if ($line -match $head) {
      if ($cur) { $rows += $cur }
      $cur = [pscustomobject]@{ id = $q.id; ai = $Matches[1]; model = $Matches[2]; seconds = [double]$Matches[3]; session = $Matches[4]; answer = ''; error = $null; body = @() }
    } elseif ($cur) {
      $cur.body += $line
    }
  }
  if ($cur) { $rows += $cur }
  foreach ($r in $rows) {
    $text = ($r.body -join "`n").Trim()
    if ($text -match '^failed: (.*)') { $r.error = $Matches[1] } else { $r.answer = $text }
    $r.PSObject.Properties.Remove('body')
    foreach ($old in @($answers | Where-Object { $_.id -eq $q.id -and $_.ai -eq $r.ai })) { $answers.Remove($old) }
    [void]$answers.Add($r)
  }
  foreach ($ai in $missing) {
    if (-not ($rows | Where-Object { $_.ai -eq $ai })) {
      foreach ($old in @($answers | Where-Object { $_.id -eq $q.id -and $_.ai -eq $ai })) { $answers.Remove($old) }
      [void]$answers.Add([pscustomobject]@{ id = $q.id; ai = $ai; model = ''; seconds = 0; session = $null; answer = ''; error = (($lines | Select-Object -Last 2) -join ' ') })
    }
  }
  Save
  $failed = @($rows | Where-Object { $_.error }).Count
  Write-Output ("Q{0} done in {1:n0} s, {2} answers, {3} failed" -f $q.id, ((Get-Date) - $t0).TotalSeconds, $rows.Count, $failed)
}
$bad = @($answers | Where-Object { $_.error })
Write-Output ("{0} answers, {1} failed -> {2}" -f $answers.Count, $bad.Count, $Out)
