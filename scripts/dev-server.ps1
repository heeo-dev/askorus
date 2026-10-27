# Opens the app's dev server (Metro, port 8081) in its own window, reachable
# from the phone over Tailscale and from the emulator at 10.0.2.2. It runs
# the app folder next to this script, so a worktree serves its own code.
# Closing the window stops it. Keep this file ASCII.
#   powershell -NoProfile -File scripts\dev-server.ps1
$app = Join-Path (Split-Path -Parent $PSScriptRoot) 'app'
$log = Join-Path $env:TEMP 'asksemble-metro.log'
$cmd = "[Console]::OutputEncoding = [Text.Encoding]::UTF8; " +
  "`$host.UI.RawUI.WindowTitle = 'Asksemble dev server (close to stop)'; " +
  "`$env:REACT_NATIVE_PACKAGER_HOSTNAME = 'PC-TAILSCALE-IP'; " +
  "Set-Location '$app'; npx expo start --go --port 8081 2>&1 | Tee-Object -FilePath '$log'"
Start-Process -FilePath powershell.exe -ArgumentList '-NoExit', '-NoProfile', '-Command', $cmd -WorkingDirectory $app
for ($i = 0; $i -lt 90; $i++) {
  $status = & curl.exe -s --max-time 2 'http://127.0.0.1:8081/status'
  if ($status -match 'running') { Write-Output "dev server running for $app"; exit 0 }
  Start-Sleep 2
}
Write-Output 'dev server did not start'
exit 1
