# Live use of the test phone for one-off checks (MT items, P0-4 and the
# like), with agent-device (Callstack) over USB (wireless debugging as a
# fallback). Repeated checks belong in Maestro flows (e2e.ps1). Keep this
# file ASCII.
#   powershell -NoProfile -File scripts\phone.ps1 ready              # connect, wake, free the screen reader
#   powershell -NoProfile -File scripts\phone.ps1 snapshot -i        # agent-device command (see below)
#   powershell -NoProfile -File scripts\phone.ps1 shot <file.png>    # adb screenshot, no helper app
#   powershell -NoProfile -File scripts\phone.ps1 ui                 # system windows too (share sheet)
#   powershell -NoProfile -File scripts\phone.ps1 release            # close the session (e2e.ps1 also does)
# Common agent-device commands: snapshot -i | click @e3 | fill @e2 "text" |
# press 540 1200 | back | open <package|url> | screenshot <file> |
# record start <file.mp4> / record stop | logs start / logs stop | apps
# Full list: phone.ps1 --help. Only one tool reads the screen at a time, so
# this and Maestro take turns (Release-Screen in phone-lib.ps1).
. (Join-Path $PSScriptRoot 'phone-lib.ps1')

$serial = Connect-Phone
if (-not $serial) {
  Write-Output $noPhone
  exit 1
}
$cmd = if ($args.Count) { $args[0] } else { 'ready' }
switch ($cmd) {
  'ready' {
    if (-not (Wake-Phone)) { Write-Output 'the phone is locked: set its screen lock to None or Swipe'; exit 1 }
    Release-Screen
    Write-Output "phone ready ($serial)"
  }
  'shot' {
    if ($args.Count -lt 2) { Write-Output 'usage: phone.ps1 shot <file.png>'; exit 1 }
    Adb shell screencap -p /sdcard/phone-shot.png
    Adb pull /sdcard/phone-shot.png $args[1] | Out-Null
    Adb shell rm /sdcard/phone-shot.png
    Write-Output $args[1]
  }
  'ui' {
    # Android's own dump: reads system windows (share sheet, permission
    # dialogs) that agent-device's snapshot refuses. Text, bounds and ids;
    # tap a center with "phone.ps1 press X Y".
    Release-Screen
    Adb shell uiautomator dump /sdcard/phone-ui.xml | Out-Null
    $xml = [xml](Adb exec-out cat /sdcard/phone-ui.xml)
    foreach ($n in $xml.SelectNodes('//node')) {
      $label = if ($n.text) { $n.text } else { $n.'content-desc' }
      if (-not $label) { continue }
      if ($n.bounds -match '\[(\d+),(\d+)\]\[(\d+),(\d+)\]') {
        $x = ([int]$Matches[1] + [int]$Matches[3]) / 2; $y = ([int]$Matches[2] + [int]$Matches[4]) / 2
        Write-Output ("{0,5} {1,5}  {2}  {3}" -f $x, $y, $label, $n.'resource-id')
      }
    }
  }
  'release' {
    Invoke-AgentDevice close | Out-Null
    Release-Screen
    Write-Output 'released'
  }
  default {
    Invoke-AgentDevice @args
    exit $agentDeviceCode
  }
}
