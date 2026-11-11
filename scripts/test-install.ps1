# Tests scripts\install.ps1 with release files that scripts\release.ps1 builds
# into a temp folder (#37): a new install through iex (as `irm | iex` runs
# it), a damaged download, a refused address, an update while the agent runs
# and one while it does not. Nothing of the real install is touched: the
# install folder, the agent folder (ASKORUS_AGENT_DIR, with a port of its
# own) and the ask folder are temp folders, and setup is not run
# (ASKORUS_NO_SETUP). Exit code 0 only if all pass.
#   powershell -NoProfile -File scripts\test-install.ps1 [-Keep] [-Shell pwsh]
# -Shell runs the installer and release.ps1 in another PowerShell, such as
# PowerShell 7 (pwsh, or the full path of pwsh.exe). The default is Windows
# PowerShell 5.1, which is what test-all runs.
# Keep this file ASCII: Windows PowerShell 5.1 reads scripts without a BOM
# as the ANSI code page.
param([switch]$Keep, [string]$Shell = 'powershell')
$ErrorActionPreference = 'Stop'
$root = Split-Path -Parent $PSScriptRoot
$install = Join-Path $PSScriptRoot 'install.ps1'
$T = Join-Path ([IO.Path]::GetTempPath()) ('askorus-install-test-' + [Guid]::NewGuid().ToString('N').Substring(0, 8))
New-Item -ItemType Directory -Path $T | Out-Null
$failed = @()

function Check([string]$name, [bool]$ok, $detail) {
    if ($ok) { Write-Output "   ok   $name" } else { Write-Output "   FAIL $name"; if ($detail) { $detail | ForEach-Object { "        $_" } }; $script:failed += $name }
}

function Listening([int]$port) {
    $c = New-Object Net.Sockets.TcpClient
    try { $c.Connect('127.0.0.1', $port); return $true } catch { return $false } finally { $c.Close() }
}

# Runs install.ps1 in a new PowerShell ($Shell) with the ASKORUS_ variables
# given, through -File or, as `irm | iex` does, through Invoke-Expression.
function Install([hashtable]$vars, [switch]$Iex) {
    $saved = @{}
    foreach ($k in $vars.Keys) { $saved[$k] = [Environment]::GetEnvironmentVariable($k); [Environment]::SetEnvironmentVariable($k, $vars[$k]) }
    try {
        if ($Iex) {
            # As `irm | iex` in a window; $LASTEXITCODE afterwards is what the person or an AI sees.
            $out = & $Shell -NoProfile -ExecutionPolicy Bypass -Command "Get-Content -Raw -LiteralPath '$install' | Invoke-Expression; exit `$LASTEXITCODE" 2>&1 | ForEach-Object { "$_" }
        } else {
            $out = & $Shell -NoProfile -ExecutionPolicy Bypass -File $install 2>&1 | ForEach-Object { "$_" }
        }
        return @{ code = $LASTEXITCODE; out = @($out); text = ($out -join "`n") }
    } finally {
        foreach ($k in $vars.Keys) { [Environment]::SetEnvironmentVariable($k, $saved[$k]) }
    }
}

# A port of its own for the test agent, so the real one is never asked to stop.
$port = 18797
while ((Listening $port) -and $port -lt 18897) { $port++ }
$agentDir = Join-Path $T 'agent'
New-Item -ItemType Directory -Path $agentDir, (Join-Path $T 'ask') | Out-Null
$cfg = @{ port = $port; folders = @(); ask = @{ folder = (Join-Path $T 'ask') } } | ConvertTo-Json
[IO.File]::WriteAllText((Join-Path $agentDir 'agent.json'), $cfg, (New-Object Text.UTF8Encoding $false))
$env:ASKORUS_AGENT_DIR = $agentDir
$exe = $null

try {
    Write-Output '== build two releases'
    $rel1 = Join-Path $T 'rel1'
    $rel2 = Join-Path $T 'rel2'
    & $Shell -NoProfile -ExecutionPolicy Bypass -File (Join-Path $PSScriptRoot 'release.ps1') -Version 0.0.1-test -Out $rel1 | Out-Null
    if ($LASTEXITCODE -ne 0) { throw 'release.ps1 0.0.1-test failed' }
    & $Shell -NoProfile -ExecutionPolicy Bypass -File (Join-Path $PSScriptRoot 'release.ps1') -Version 0.0.2-test -Out $rel2 | Out-Null
    if ($LASTEXITCODE -ne 0) { throw 'release.ps1 0.0.2-test failed' }
    $sums = Get-Content (Join-Path $rel1 'SHA256SUMS.txt')
    $want = 'askorus-agent-windows-amd64.exe', 'askorus-agent-darwin-arm64', 'askorus-agent-darwin-amd64', 'askorus-agent-linux-amd64', 'askorus-agent-linux-arm64', 'install.ps1', 'install.sh'
    $listed = @($sums | ForEach-Object { if ($_ -match '^[0-9a-f]{64}  (\S+)$') { $Matches[1] } })
    Check 'SHA256SUMS lists the agents for Windows, macOS and Linux and both install scripts' (($listed -join ' ') -eq ($want -join ' ')) $sums
    $shBytes = [IO.File]::ReadAllBytes((Join-Path $rel1 'install.sh'))
    Check 'install.sh has LF line ends and starts with #!/bin/sh' (-not ($shBytes -contains 13) -and [Text.Encoding]::ASCII.GetString($shBytes, 0, 9) -eq '#!/bin/sh') $null

    Write-Output '== new install through iex'
    $dir = Join-Path $T 'Programs\Askorus'
    $exe = Join-Path $dir 'askorus-agent.exe'
    $r = Install @{ ASKORUS_DOWNLOAD_BASE = $rel1; ASKORUS_INSTALL_DIR = $dir; ASKORUS_NO_SETUP = '1' } -Iex
    Check 'exit code 0' ($r.code -eq 0) $r.out
    Check 'the exe is in the install folder' (Test-Path $exe) $r.out
    Check 'no askorus-agent.new.exe is left' (-not (Test-Path (Join-Path $dir 'askorus-agent.new.exe'))) $null
    Check 'it is the released version' ((Test-Path $exe) -and ((& $exe version) -join '') -eq 'askorus-agent 0.0.1-test') $r.out
    Check 'it says what it did and what is next' ($r.text -match 'the SHA-256 matches' -and $r.text -match 'installed askorus-agent 0\.0\.1-test' -and $r.text -match 'setup') $r.out
    Check 'no temp folder is left' (-not (Get-ChildItem ([IO.Path]::GetTempPath()) -Filter 'askorus-install-????????' -Directory -ErrorAction SilentlyContinue)) $null

    Write-Output '== damaged download'
    $bad = Join-Path $T 'bad'
    Copy-Item $rel1 $bad -Recurse
    $s = Get-Content (Join-Path $bad 'SHA256SUMS.txt')
    $s[0] = ('0' * 64) + '  askorus-agent-windows-amd64.exe'
    Set-Content (Join-Path $bad 'SHA256SUMS.txt') $s -Encoding Ascii
    $badDir = Join-Path $T 'bad-install'
    $r = Install @{ ASKORUS_DOWNLOAD_BASE = $bad; ASKORUS_INSTALL_DIR = $badDir; ASKORUS_NO_SETUP = '1' }
    Check 'exit code 1' ($r.code -eq 1) $r.out
    Check 'says the download is damaged and nothing was installed' ($r.text -match 'damaged' -and $r.text -match 'nothing was installed') $r.out
    Check 'nothing is installed' (-not (Test-Path (Join-Path $badDir 'askorus-agent.exe'))) $null
    $r = Install @{ ASKORUS_DOWNLOAD_BASE = $bad; ASKORUS_INSTALL_DIR = $badDir; ASKORUS_NO_SETUP = '1' } -Iex
    Check 'through iex too: $LASTEXITCODE 1 and the same words' ($r.code -eq 1 -and $r.text -match 'damaged') $r.out

    Write-Output '== a relative install folder'
    Push-Location $T
    try { $r = Install @{ ASKORUS_DOWNLOAD_BASE = $rel1; ASKORUS_INSTALL_DIR = 'rel\Askorus'; ASKORUS_NO_SETUP = '1' } } finally { Pop-Location }
    $relDir = Join-Path $T 'rel\Askorus'
    Check 'lands in the full path and says it' ($r.code -eq 0 -and (Test-Path (Join-Path $relDir 'askorus-agent.exe')) -and $r.text.Contains("in $relDir")) $r.out

    Write-Output '== an address that is not https'
    $r = Install @{ ASKORUS_DOWNLOAD_BASE = 'http://example.com/x'; ASKORUS_INSTALL_DIR = $badDir; ASKORUS_NO_SETUP = '1' }
    Check 'refused' ($r.code -eq 1 -and $r.text -match 'https address or a folder') $r.out

    Write-Output "== update while the agent runs, but its file is locked (test agent on 127.0.0.1:$port)"
    & $exe start | Out-Null
    Check 'the test agent runs' (Listening $port) $null
    $lock = [IO.File]::Open($exe, 'Open', 'Read', 'Read') # no delete sharing: the file cannot be renamed
    try { $r = Install @{ ASKORUS_DOWNLOAD_BASE = $rel2; ASKORUS_INSTALL_DIR = $dir; ASKORUS_NO_SETUP = '1' } } finally { $lock.Close() }
    Check 'exit code 1, says the old one stays and runs again' ($r.code -eq 1 -and $r.text -match 'Could not put the new agent in place' -and $r.text -match 'the old one stays and runs again') $r.out
    Check 'the old exe is still in place' (((& $exe version) -join '') -eq 'askorus-agent 0.0.1-test') $null
    Check 'the agent runs again' (Listening $port) $null
    Check 'no askorus-agent.new.exe is left' (-not (Test-Path (Join-Path $dir 'askorus-agent.new.exe'))) $null

    Write-Output "== update while the agent runs"
    $r = Install @{ ASKORUS_DOWNLOAD_BASE = $rel2; ASKORUS_INSTALL_DIR = $dir; ASKORUS_NO_SETUP = '1' }
    Check 'exit code 0' ($r.code -eq 0) $r.out
    Check 'stopped it, updated, kept the old one' ($r.text -match 'stopping the agent' -and $r.text -match 'updated askorus-agent 0\.0\.1-test to askorus-agent 0\.0\.2-test') $r.out
    Check 'the new exe is in place' (((& $exe version) -join '') -eq 'askorus-agent 0.0.2-test') $null
    Check 'the old exe is askorus-agent.prev.exe' (((& (Join-Path $dir 'askorus-agent.prev.exe') version) -join '') -eq 'askorus-agent 0.0.1-test') $null
    Check 'the agent runs again, from the new exe' ((Listening $port) -and [bool](Get-Process -Name 'askorus-agent' -ErrorAction SilentlyContinue | Where-Object { $_.Path -eq $exe })) $r.out
    & $exe shutdown | Out-Null

    Write-Output '== update while the agent does not run'
    $r = Install @{ ASKORUS_DOWNLOAD_BASE = $rel2; ASKORUS_INSTALL_DIR = $dir; ASKORUS_NO_SETUP = '1' }
    Check 'exit code 0, no stop' ($r.code -eq 0 -and $r.text -notmatch 'stopping') $r.out
    Check 'does not start it' (-not (Listening $port)) $null
} catch {
    Write-Output "   FAIL $($_.Exception.Message)"
    $failed += 'error'
} finally {
    if ($exe -and (Test-Path $exe) -and (Listening $port)) { & $exe shutdown | Out-Null }
    Remove-Item Env:ASKORUS_AGENT_DIR -ErrorAction SilentlyContinue
    if ($Keep) { Write-Output "kept $T" } else { Remove-Item $T -Recurse -Force -ErrorAction SilentlyContinue }
}

if ($failed.Count) {
    Write-Output ('FAILED: ' + ($failed -join ', '))
    exit 1
}
Write-Output 'ALL PASSED'
exit 0
