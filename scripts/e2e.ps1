# Runs the app E2E flows (app\e2e, Maestro) against the real agent, on the
# test phone over USB or wireless debugging (default) or on the Android emulator
# (-Emulator). Installs Expo Go when needed. The dev server must already run
# (scripts\dev-server.ps1). Keep this file ASCII.
#   powershell -NoProfile -File scripts\e2e.ps1 [-Flow 01] [-Prepare]
#   powershell -NoProfile -File scripts\e2e.ps1 -Emulator [-Window] [-Keep]
param([string]$Flow = '', [switch]$Emulator, [switch]$Window, [switch]$Keep, [switch]$Prepare)
# Phone: plugged into the PC with USB debugging on (or wireless debugging,
# paired once with a QR in Developer options, same Wi-Fi as the PC),
# Tailscale connected, screen lock None or Swipe.
# Emulator: runs without a window unless -Window (to watch it). An emulator
# this script started is shut down afterwards unless -Keep; one that was
# already running is left as it was. -Prepare only gets the device ready.
$Headless = -not $Window
$startedHere = $false

. (Join-Path $PSScriptRoot 'phone-lib.ps1')
$env:JAVA_HOME = "$android\jdk17"
$env:ANDROID_AVD_HOME = "$android\avd" # the AVD lives on D:, C: is short on space
$env:Path = "$android\jdk17\bin;$android\Sdk\emulator;$android\maestro\bin;" + $env:Path
$env:MAESTRO_CLI_NO_ANALYTICS = '1'
$env:MAESTRO_CLI_ANALYSIS_NOTIFICATION_DISABLED = 'true'
$root = Split-Path -Parent $PSScriptRoot

function Booted { (Adb shell getprop sys.boot_completed 2>$null) -eq '1' }

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

if ($Emulator) {
  $serial = @(Ready-Devices $true) | Select-Object -First 1
  if (-not $serial) {
    Write-Output 'starting emulator'
    # asksemble-play: Google Play image with the Tailscale app signed in, so it
    # reaches the agent by its MagicDNS name like a real phone. Host GPU.
    $emuArgs = @('-avd', 'asksemble-play', '-no-audio', '-no-boot-anim', '-gpu', 'host')
    if ($Headless) { $emuArgs += '-no-window' }
    Start-Process -FilePath "$android\Sdk\emulator\emulator.exe" -ArgumentList $emuArgs -WindowStyle Hidden
    for ($i = 0; $i -lt 30 -and -not $serial; $i++) { Start-Sleep 2; $serial = @(Ready-Devices $true) | Select-Object -First 1 }
    if (-not $serial) { $serial = 'emulator-5554' }
    for ($i = 0; $i -lt 180 -and -not (Booted); $i++) { Start-Sleep 2 }
    if (-not (Booted)) { Write-Output 'emulator did not boot'; exit 1 }
    $startedHere = $true
    if (-not $Headless) { Show-EmulatorWindow }
  }
  # The emulator reaches the PC's dev server at its host alias.
  $env:MAESTRO_HOST = '10.0.2.2'
  $env:MAESTRO_DEVICE = 'emulator'
  Write-Output "emulator ready ($serial)"
} else {
  $serial = Connect-Phone
  if (-not $serial) {
    Write-Output $noPhone
    exit 1
  }
  # The phone reaches the dev server over Tailscale (dev-server.ps1 serves on
  # the PC's Tailscale address).
  $env:MAESTRO_HOST = 'PC-TAILSCALE-IP'
  $env:MAESTRO_DEVICE = 'phone'
  # Flows tap English text; the app follows the phone's first language.
  $locale = Adb shell getprop persist.sys.locale
  if ($locale -notlike 'en*') { Write-Output "the phone's language is $locale; set English first (flows use the English UI)"; exit 1 }
  # Wake it and keep the screen on while the flows run; restored at the end.
  if (-not (Wake-Phone)) { Write-Output 'the phone is locked: set its screen lock to None or Swipe'; exit 1 }
  # A live session (phone.ps1, agent-device) may hold the screen reader.
  Release-Screen
  $screenTimeout = Adb shell settings get system screen_off_timeout
  Adb shell settings put system screen_off_timeout 1800000
  # Without animations each tap settles sooner (Maestro waits for the
  # screen to stop changing). Restored at the end; 'null' means unset.
  $animKeys = 'window_animation_scale', 'transition_animation_scale', 'animator_duration_scale'
  $anim = @{}
  foreach ($k in $animKeys) { $anim[$k] = Adb shell settings get global $k; Adb shell settings put global $k 0 }
  $battery = (Adb shell dumpsys battery) -join "`n"
  if (($battery -match 'level: (\d+)') -and [int]$Matches[1] -lt 20 -and -not ($battery -match 'powered: true')) {
    Write-Output 'warning: the phone battery is low and not charging'
  }
  Write-Output "phone ready ($serial)"
}

# Tailscale must be up on the device, or every flow fails to reach the PC
# (open the Tailscale app there and connect). After a fresh emulator boot
# Tailscale reconnects by itself within a minute or so; on the phone it is
# either up or not, so fail fast.
$tries = if ($Emulator) { 30 } else { 3 }
for ($i = 0; $i -lt $tries; $i++) {
  $ping = Adb shell 'ping -c1 -W2 MY-PC.example.ts.net' 2>$null
  if ($ping -match '1 received') { break }
  Start-Sleep 3
}
if (-not ($ping -match '1 received')) { Write-Output 'the device cannot reach the PC: is Tailscale connected on it?'; exit 1 }

$installed = Adb shell pm list packages host.exp.exponent 2>$null
if (-not $installed) {
  $apk = "$android\dl\Expo-Go-58.0.5.apk"
  if (-not (Test-Path $apk)) {
    Invoke-WebRequest -Uri 'https://github.com/expo/expo-go-releases/releases/download/Expo-Go-58.0.5/Expo-Go-58.0.5.apk' -OutFile $apk -UseBasicParsing
  }
  Adb install -r $apk | Select-Object -Last 1
}

$code = 0
if ($Prepare) {
  Write-Output 'prepared'
} else {
  # Start Expo Go fresh so it loads the code the dev server serves now, not
  # a bundle cached from an earlier server or branch.
  Adb shell am force-stop host.exp.exponent

  $flows = Join-Path $root 'app\e2e'
  if ($Flow) {
    $target = Get-ChildItem $flows -Filter "$Flow*.yaml" | Select-Object -First 1
    if (-not $target) { Write-Output "no flow $Flow"; exit 1 }
    & maestro --device $serial test --no-reinstall-driver $target.FullName
  } else {
    $all = Get-ChildItem $flows -Filter '*.yaml' | Where-Object { $_.Name -notlike '_*' } | Sort-Object Name
    & maestro --device $serial test --no-reinstall-driver @($all.FullName)
  }
  $code = $LASTEXITCODE
  # On a failure, keep the screen the flow left: what agent-device reads
  # there (exact texts to fix a selector) and a picture, next to Maestro's
  # own report. phone.ps1 can then look further.
  if ($code -ne 0) {
    $report = Get-ChildItem "$env:USERPROFILE\.maestro\tests" -Directory | Sort-Object Name | Select-Object -Last 1
    Release-Screen
    Invoke-AgentDevice snapshot -i | Out-File -Encoding utf8 "$($report.FullName)\after-failure.txt"
    Invoke-AgentDevice close | Out-Null
    Adb shell screencap -p /sdcard/after-failure.png
    Adb pull /sdcard/after-failure.png "$($report.FullName)\after-failure.png" | Out-Null
    Adb shell rm /sdcard/after-failure.png
    Write-Output "failed: Maestro report and the screen after the failure (after-failure.txt, .png) in $($report.FullName)"
  }
}
if (-not $Emulator) {
  # Hand the screen reader back for live use (phone.ps1).
  Release-Screen
  if ($screenTimeout -match '^\d+$') { Adb shell settings put system screen_off_timeout $screenTimeout }
  foreach ($k in $anim.Keys) {
    if ($anim[$k] -match '^[\d.]+$') { Adb shell settings put global $k $anim[$k] } else { Adb shell settings delete global $k | Out-Null }
  }
}
if ($startedHere -and -not $Keep) {
  Write-Output 'shutting the emulator down'
  Adb emu kill | Out-Null
}
exit $code
