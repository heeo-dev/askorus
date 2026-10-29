# Shared by e2e.ps1 and phone.ps1: finds the test phone over wireless
# debugging and gets it ready. Dot-source it. Keep this file ASCII.

$android = 'D:\Android'
$env:ANDROID_HOME = "$android\Sdk"
$env:Path = "$android\Sdk\platform-tools;" + $env:Path
$adb = "$android\Sdk\platform-tools\adb.exe"
$agentDevice = "$android\agent-device\node_modules\.bin\agent-device.cmd" # 0.21.34, pinned
# The last address the phone answered on. Wireless debugging keeps its port
# until it is turned off or the phone restarts.
$phoneAddrFile = "$android\phone-adb.txt"

$serial = ''
function Adb { & $adb -s $serial @args }

# Serials adb sees as ready, emulators or not.
function Ready-Devices([bool]$emu) {
  foreach ($line in (& $adb devices | Select-Object -Skip 1)) {
    if ($line -match '^(\S+)\s+device$' -and (($Matches[1] -like 'emulator-*') -eq $emu)) { $Matches[1] }
  }
}

# Finds the phone: already connected, else by mDNS (adb also connects to a
# paired phone it sees there by itself), else the last address that worked.
# The phone's VPN (Tailscale) can hide it from mDNS, hence the address file.
function Connect-Phone {
  $d = @(Ready-Devices $false)
  if ($d.Count -eq 0) {
    $svc = & $adb mdns services | Select-String '_adb-tls-connect' | Select-Object -First 1
    if ($svc -and "$svc" -match '(\d+\.\d+\.\d+\.\d+:\d+)') { & $adb connect $Matches[1] | Out-Null }
    elseif (Test-Path $phoneAddrFile) { & $adb connect (Get-Content $phoneAddrFile -Raw).Trim() | Out-Null }
    Start-Sleep 2
    $d = @(Ready-Devices $false)
  }
  if ($d.Count -eq 0) { return '' }
  if ($d[0] -match '^\d+\.\d+\.\d+\.\d+:\d+$') { Set-Content -Path $phoneAddrFile -Value $d[0] -Encoding ascii }
  elseif ($d[0] -match '_adb-tls-connect') {
    $svc = & $adb mdns services | Select-String ([regex]::Escape(($d[0] -split '\.')[0])) | Select-Object -First 1
    if ("$svc" -match '(\d+\.\d+\.\d+\.\d+:\d+)') { Set-Content -Path $phoneAddrFile -Value $Matches[1] -Encoding ascii }
  }
  return $d[0]
}

# Runs agent-device on the phone and returns its output; sets
# $script:agentDeviceCode. Its first call starts a background daemon, which
# would inherit every handle of a child started the usual way, our output
# pipe too, and the caller would never finish. A shell-started cmd passes
# no handles on, and writes the output to files of its own.
function Invoke-AgentDevice {
  $all = @($args) + @('--platform', 'android', '--serial', $serial)
  $line = ($all | ForEach-Object { '"' + ("$_" -replace '"', '\"') + '"' }) -join ' '
  $out = Join-Path $env:TEMP ("agent-device-" + [guid]::NewGuid().ToString('N'))
  $cmd = "/d /c `"`"$agentDevice`" $line > `"$out.out`" 2> `"$out.err`"`""
  # Not -Wait: in Windows PowerShell it also waits for the daemon.
  $p = Start-Process -FilePath cmd.exe -ArgumentList $cmd -WindowStyle Hidden -PassThru
  $null = $p.Handle # keeps the exit code readable after exit
  $p.WaitForExit()
  $script:agentDeviceCode = $p.ExitCode
  Get-Content "$out.out", "$out.err" -Encoding utf8 -ErrorAction SilentlyContinue
  Remove-Item "$out.out", "$out.err" -ErrorAction SilentlyContinue
}

# Wakes the phone and takes the swipe lock off. False if it stays locked
# (a PIN or pattern lock: set the screen lock to None or Swipe).
function Wake-Phone {
  Adb shell input keyevent KEYCODE_WAKEUP
  Adb shell wm dismiss-keyguard
  return -not (((Adb shell dumpsys window) -join "`n") -match 'isKeyguardShowing=true')
}

# Only one tool at a time can read the screen (Android's UiAutomation).
# Maestro's driver keeps it after a run, agent-device's helper while its
# session is open, and mobilecli (mobile-mcp) leaves a server running for
# 30 minutes; stop them all before handing the phone to another tool.
function Release-Screen {
  Adb shell am force-stop dev.mobile.maestro.test
  Adb shell am force-stop dev.mobile.maestro
  Adb shell am force-stop com.callstack.agentdevice.snapshothelper
  Adb shell pkill -f com.mobilenext.mobilecli 2>$null
}
