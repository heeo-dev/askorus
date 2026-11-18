# Runs one command as a heavy job (heavy-job.ps1): it waits while another
# heavy job runs on this PC, in this or another checkout. Use it for the
# heavy jobs that have no lock of their own: the app builds, codex exec,
# synth-eval. test-all.ps1 and test-live.ps1 take the lock themselves.
# The command is one string, run by a new Windows PowerShell in the current
# folder (Windows PowerShell 5.1 does not take "--" after -File):
#   powershell -NoProfile -File scripts\one-at-a-time.ps1 -Name "apk build" -Command "powershell -NoProfile -File build.ps1 -VersionCode 10"
# The exit code is the command's. Keep this file ASCII.
param(
    [Parameter(Mandatory = $true)][string]$Name,
    [Parameter(Mandatory = $true)][string]$Command
)
. (Join-Path $PSScriptRoot 'heavy-job.ps1')
$heavy = Enter-HeavyJob $Name
$code = 1
try {
    & powershell -NoProfile -ExecutionPolicy Bypass -Command $Command
    $code = $LASTEXITCODE
} finally {
    Exit-HeavyJob $heavy
}
exit $code
