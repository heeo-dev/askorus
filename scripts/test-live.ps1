# Live scenario tests (docs/testing.md, LT-xx): the agent with the real
# claude on this PC. Only the test folder .local\rc-test is used, and what
# the tests start is removed. Run after changing how the agent drives
# claude. Keep this file ASCII.
#   powershell -NoProfile -File scripts\test-live.ps1
$go = 'C:\Users\me\sdk\go1.27.3\bin'
if (Test-Path $go) { $env:Path = "$go;" + $env:Path }
Push-Location (Join-Path (Split-Path -Parent $PSScriptRoot) 'agent')
go test -tags live -count=1 -v ./internal/live 2>&1 | ForEach-Object { "$_" } | Select-String -Pattern '^(--- |    |ok|FAIL|PASS)'
$code = $LASTEXITCODE
Pop-Location
if ($code -eq 0) { Write-Output 'LIVE PASSED' } else { Write-Output 'LIVE FAILED' }
exit $code
