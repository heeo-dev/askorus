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

if (-not (Booted)) {
  Write-Output 'starting emulator'
  # The agent is reached by its MagicDNS name, which only Tailscale's own
  # resolver knows and the emulator ignores. The name is in the emulator's
  # /system/etc/hosts (docs/testing.md 4), which needs a writable system.
  $emuArgs = @('-avd', 'asksemble', '-no-audio', '-no-boot-anim', '-no-snapshot-save', '-writable-system')
  if ($Headless) { $emuArgs += '-no-window' }
  Start-Process -FilePath "$android\Sdk\emulator\emulator.exe" -ArgumentList $emuArgs -WindowStyle Hidden
  for ($i = 0; $i -lt 180 -and -not (Booted); $i++) { Start-Sleep 2 }
  if (-not (Booted)) { Write-Output 'emulator did not boot'; exit 1 }
}
Write-Output 'emulator ready'

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
