# One heavy job at a time on this PC (2026-11-18): test-all, test-live, the
# app builds, codex exec, synth-eval. Two at once ran the PC out of memory
# (11-16, 11-18), and Claude Code sessions in two worktrees could start them
# together. The lock is a named Windows mutex: a job that ends or dies lets
# go of it and leaves nothing behind (a lock file would stay after a crash).
# A job started from inside one that holds it (a release build that runs
# test-all) does not take it again: ASKORUS_HEAVY_JOB_PID names the holder.
# A small text file says who holds it, only to tell the one that waits.
#
#   . (Join-Path $PSScriptRoot 'heavy-job.ps1')
#   $heavy = Enter-HeavyJob 'test-all'
#   try { ... } finally { Exit-HeavyJob $heavy }
#
# Commands of their own go through scripts\one-at-a-time.ps1. Keep this file
# ASCII: Windows PowerShell 5.1 reads scripts without a BOM as the ANSI code
# page.

$script:HeavyMutexName = 'Local\Askorus-heavy-job'
$script:HeavyInfo = Join-Path $env:LOCALAPPDATA 'askorus-dev\heavy-job.txt'

# Enter-HeavyJob waits until no other heavy job runs, then holds the lock.
# It returns the lock for Exit-HeavyJob, or $null when a job above this one
# holds it already. The waiting lines go to the host, not the output.
function Enter-HeavyJob([string]$Name) {
    $holder = $env:ASKORUS_HEAVY_JOB_PID
    if ($holder -and (Get-Process -Id $holder -ErrorAction SilentlyContinue)) { return $null }
    $mutex = New-Object System.Threading.Mutex($false, $script:HeavyMutexName)
    $wait = [TimeSpan]::Zero
    while ($true) {
        try { $got = $mutex.WaitOne($wait) }
        catch [System.Threading.AbandonedMutexException] { $got = $true } # its job died: the lock is ours
        if ($got) { break }
        $who = Get-Content -Raw -Path $script:HeavyInfo -ErrorAction SilentlyContinue
        if (-not $who) { $who = 'a job that did not say' }
        Write-Host ("{0} waits for another heavy job to end: {1}" -f $Name, $who.Trim())
        $wait = [TimeSpan]::FromSeconds(60)
    }
    New-Item -ItemType Directory -Force -Path (Split-Path -Parent $script:HeavyInfo) | Out-Null
    Set-Content -Path $script:HeavyInfo -Value ("{0}, from {1}, pid {2}, since {3}" -f $Name, (Split-Path -Leaf (Get-Location).Path), $PID, (Get-Date -Format 'HH:mm'))
    $env:ASKORUS_HEAVY_JOB_PID = "$PID"
    return $mutex
}

# Exit-HeavyJob lets go of the lock Enter-HeavyJob returned ($null: nothing to do).
function Exit-HeavyJob($Mutex) {
    if ($null -eq $Mutex) { return }
    Remove-Item -Path $script:HeavyInfo -ErrorAction SilentlyContinue
    Remove-Item -Path Env:ASKORUS_HEAVY_JOB_PID -ErrorAction SilentlyContinue
    try { $Mutex.ReleaseMutex() } catch { }
    $Mutex.Dispose()
}
