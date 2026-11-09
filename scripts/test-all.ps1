# Runs every automated test that needs no phone or emulator: the regression
# gate before merging to main (docs/testing.md). Exit code 0 only if all pass.
#   powershell -NoProfile -File scripts\test-all.ps1 [-Quick]
# -Quick skips expo-doctor (it downloads a checker). Keep this file ASCII:
# Windows PowerShell 5.1 reads scripts without a BOM as the ANSI code page.
param([switch]$Quick)

$root = Split-Path -Parent $PSScriptRoot
$go = Join-Path $env:USERPROFILE 'sdk\go1.27.3\bin'
if (Test-Path $go) { $env:Path = "$go;" + $env:Path }
$failed = @()

# Step runs a native command, shows the lines worth reading, and records a
# failure from its exit code.
function Step($name, $dir, $exe, [string[]]$cmdArgs, $show) {
  Write-Output "== $name"
  $start = Get-Date
  Push-Location $dir
  $out = & $exe @cmdArgs 2>&1 | ForEach-Object { "$_" }
  $code = $LASTEXITCODE
  Pop-Location
  $sec = [math]::Round(((Get-Date) - $start).TotalSeconds, 1)
  if ($code -eq 0) {
    if ($show) { $out | Select-String -Pattern $show | ForEach-Object { "   $_" } }
    Write-Output "   ok ($sec s)"
  } else {
    $out | Select-Object -Last 40 | ForEach-Object { "   $_" }
    Write-Output "   FAILED ($sec s)"
    $script:failed += $name
  }
}

$agent = Join-Path $root 'agent'
$app = Join-Path $root 'app'

$unformatted = & gofmt -l $agent
if ($unformatted) { Write-Output '== agent: gofmt'; $unformatted; $failed += 'gofmt' }
# Personal values (addresses, machine names, user paths, private links)
# never go into the repo. Their patterns live outside git, one extended
# regex per line in .local\private-patterns.txt of the main checkout; no
# blank lines (a blank pattern matches everything). Without the file this
# check is skipped.
$common = & git -C $root rev-parse --path-format=absolute --git-common-dir
$patterns = Join-Path (Split-Path -Parent $common) '.local\private-patterns.txt'
if (Test-Path $patterns) {
  $hits = & git -C $root grep --untracked -I -n -i -E -f $patterns
  if ($hits) { Write-Output '== private values in the repo'; $hits | ForEach-Object { "   $_" }; $failed += 'private values' }
}
# The E2E script changes no phone setting but the screen-off timeout (it
# puts that back): no other settings, properties, services, permissions or
# display size, in it or in phone-lib.ps1 it loads (R-83: an animation scale
# it restored stayed 0 on the S10 and froze the spinners).
$phoneScripts = 'e2e.ps1', 'phone-lib.ps1' | ForEach-Object { Join-Path $PSScriptRoot $_ }
$phoneWrites = Select-String -Path $phoneScripts -Pattern 'shell\s+(settings\s+(put|delete|reset)|setprop|svc|cmd|pm\s+(grant|revoke|enable|disable|clear)|wm\s+(size|density))\b' |
  Where-Object { $_.Line -notmatch 'shell\s+settings\s+put\s+system\s+screen_off_timeout\b' }
if ($phoneWrites) {
  Write-Output '== e2e: phone settings besides the screen-off timeout'
  $phoneWrites | ForEach-Object { "   $($_.Filename):$($_.LineNumber): $($_.Line.Trim())" }
  $failed += 'e2e phone settings'
}
Step 'agent: go vet (windows)' $agent 'go' @('vet', './...')
$env:GOOS = 'linux'
Step 'agent: go vet (linux)' $agent 'go' @('vet', './...')
Remove-Item Env:GOOS
Step 'agent: unit + scenario tests' $agent 'go' @('test', '-count=1', './...') '^(ok|FAIL)'
# synth-eval (agent/cmd/synth-eval, milestone C) is built only with -tags
# syntheval: vetted on both systems and tested with the fake AI programs.
Step 'agent: go vet syntheval (windows)' $agent 'go' @('vet', '-tags', 'syntheval', './cmd/synth-eval/...')
$env:GOOS = 'linux'
Step 'agent: go vet syntheval (linux)' $agent 'go' @('vet', '-tags', 'syntheval', './cmd/synth-eval/...')
Remove-Item Env:GOOS
Step 'agent: synth-eval tests' $agent 'go' @('test', '-tags', 'syntheval', '-count=1', './cmd/synth-eval/...') '^(ok|FAIL)'

# The installer (#37): release files built into a temp folder, installed and
# updated there with a test agent on a port of its own.
Step 'install script' $root 'powershell' @('-NoProfile', '-ExecutionPolicy', 'Bypass', '-File', (Join-Path $PSScriptRoot 'test-install.ps1')) '^(ALL PASSED|   FAIL)'

if (-not (Test-Path (Join-Path $app 'node_modules'))) { Step 'app: npm ci' $app 'npm' @('ci', '--no-audit', '--no-fund') }
Step 'app: typecheck' $app 'npx' @('tsc', '--noEmit')
Step 'app: unit + screen tests' $app 'npx' @('jest', '--silent') '^Tests:'
if (-not $Quick) { Step 'app: expo-doctor' $app 'npx' @('expo-doctor') 'checks passed' }

if ($failed.Count) {
  Write-Output ('FAILED: ' + ($failed -join ', '))
  exit 1
}
Write-Output 'ALL PASSED'
exit 0
