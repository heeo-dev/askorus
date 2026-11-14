# Opens the app's dev server (Metro, port 8081) in its own window, reachable
# from the phone over Tailscale and from the emulator at 10.0.2.2. It runs
# the app folder next to this script, so a worktree serves its own code.
# Closing the window stops it. Keep this file ASCII.
# A developer tool: unlike the app itself (built-in Tailscale since 0.3.0), this
# still uses Tailscale installed on the PC and the Tailscale app on the phone.
# Over USB, `adb reverse tcp:8081 tcp:8081` does without both.
#   powershell -NoProfile -File scripts\dev-server.ps1
$app = Join-Path (Split-Path -Parent $PSScriptRoot) 'app'
$log = Join-Path $env:TEMP 'askorus-metro.log'
# The phone opens the PC's Tailscale address; ask Tailscale for it so no
# address is written here.
$ip = & tailscale ip -4 | Select-Object -First 1
if (-not $ip) { Write-Output 'no Tailscale address: is Tailscale running on this PC?'; exit 1 }
$cmd = "[Console]::OutputEncoding = [Text.Encoding]::UTF8; " +
  "`$host.UI.RawUI.WindowTitle = 'Askorus dev server (close to stop)'; " +
  "`$env:REACT_NATIVE_PACKAGER_HOSTNAME = '$ip'; " +
  "Set-Location '$app'; npx expo start --go --port 8081 2>&1 | Tee-Object -FilePath '$log'"
Start-Process -FilePath powershell.exe -ArgumentList '-NoExit', '-NoProfile', '-Command', $cmd -WorkingDirectory $app
for ($i = 0; $i -lt 90; $i++) {
  $status = & curl.exe -s --max-time 2 'http://127.0.0.1:8081/status'
  if ($status -match 'running') { Write-Output "dev server running for $app"; exit 0 }
  Start-Sleep 2
}
Write-Output 'dev server did not start'
exit 1
