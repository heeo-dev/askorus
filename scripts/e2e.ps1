# Runs the app E2E flows (app\e2e, Maestro) on the Android emulator against
# the real agent. Starts the emulator and installs Expo Go when needed. The
# dev server must already run (exp://<PC>:8081). Keep this file ASCII.
#   powershell -NoProfile -File scripts\e2e.ps1 [-Flow 01] [-Headless] [-Prepare]
param([string]$Flow = '', [switch]$Headless, [switch]$Prepare)
# -Prepare only boots the emulator and installs Expo Go.

$android = 'D:\Android'
$env:JAVA_HOME = "$android\jdk17"
$env:ANDROID_HOME = "$android\Sdk"
$env:ANDROID_AVD_HOME = "$android\avd" # the AVD lives on D:, C: is short on space
$env:Path = "$android\jdk17\bin;$android\Sdk\platform-tools;$android\Sdk\emulator;$android\maestro\bin;" + $env:Path
$env:MAESTRO_CLI_NO_ANALYTICS = '1'
$env:MAESTRO_CLI_ANALYSIS_NOTIFICATION_DISABLED = 'true'
$root = Split-Path -Parent $PSScriptRoot
$adb = "$android\Sdk\platform-tools\adb.exe"

function Booted { (& $adb shell getprop sys.boot_completed 2>$null) -eq '1' }

# The emulator remembers its window position, which can be off this PC's
# small screen; bring it back to the top of the screen.
function Show-EmulatorWindow {
  Add-Type -Namespace E2E -Name Win -MemberDefinition @'
[DllImport("user32.dll")] public static extern bool ShowWindow(System.IntPtr h, int c);
[DllImport("user32.dll")] public static extern bool SetWindowPos(System.IntPtr h, System.IntPtr a, int x, int y, int cx, int cy, uint f);
'@
  $p = Get-Process -Name 'qemu-system-x86_64' -ErrorAction SilentlyContinue | Select-Object -First 1
  if ($p -and $p.MainWindowHandle -ne [System.IntPtr]::Zero) {
    [E2E.Win]::ShowWindow($p.MainWindowHandle, 9) | Out-Null
    [E2E.Win]::SetWindowPos($p.MainWindowHandle, [System.IntPtr]::Zero, 400, 0, 0, 0, 5) | Out-Null
  }
}

if (-not (Booted)) {
  Write-Output 'starting emulator'
  # asksemble-play: Google Play image with the Tailscale app signed in, so it
  # reaches the agent by its MagicDNS name like a real phone. Host GPU.
  $emuArgs = @('-avd', 'asksemble-play', '-no-audio', '-no-boot-anim', '-gpu', 'host')
  if ($Headless) { $emuArgs += '-no-window' }
  Start-Process -FilePath "$android\Sdk\emulator\emulator.exe" -ArgumentList $emuArgs -WindowStyle Hidden
  for ($i = 0; $i -lt 180 -and -not (Booted); $i++) { Start-Sleep 2 }
  if (-not (Booted)) { Write-Output 'emulator did not boot'; exit 1 }
  if (-not $Headless) { Show-EmulatorWindow }
}
Write-Output 'emulator ready'

# Tailscale must be up inside the emulator, or every flow fails to reach
# the PC (open the Tailscale app there and connect).
$ping = & $adb shell 'ping -c1 -W3 MY-PC.example.ts.net' 2>$null
if (-not ($ping -match '1 received')) { Write-Output 'the emulator cannot reach the PC: is Tailscale connected in the emulator?'; exit 1 }

$installed = & $adb shell pm list packages host.exp.exponent 2>$null
if (-not $installed) {
  $apk = "$android\dl\Expo-Go-58.0.5.apk"
  if (-not (Test-Path $apk)) {
    Invoke-WebRequest -Uri 'https://github.com/expo/expo-go-releases/releases/download/Expo-Go-58.0.5/Expo-Go-58.0.5.apk' -OutFile $apk -UseBasicParsing
  }
  & $adb install -r $apk | Select-Object -Last 1
}

if ($Prepare) { Write-Output 'prepared'; exit 0 }

# Start Expo Go fresh so it loads the code the dev server serves now, not
# a bundle cached from an earlier server or branch.
& $adb shell am force-stop host.exp.exponent

$flows = Join-Path $root 'app\e2e'
if ($Flow) {
  $target = Get-ChildItem $flows -Filter "$Flow*.yaml" | Select-Object -First 1
  if (-not $target) { Write-Output "no flow $Flow"; exit 1 }
  & maestro test $target.FullName
} else {
  $all = Get-ChildItem $flows -Filter '*.yaml' | Where-Object { $_.Name -notlike '_*' } | Sort-Object Name
  & maestro test @($all.FullName)
}
exit $LASTEXITCODE
