# Tests scripts\install.ps1 with release files that scripts\release.ps1 builds
# into a temp folder (#37): a new install through iex (as `irm | iex` runs
# it), a damaged download, a refused address, an update while the agent runs
# and one while it does not. Nothing of the real install is touched: the
# install folder, the agent folder (ASKORUS_AGENT_DIR, with a port number of
# its own to tell it from the real agent) and the ask folder are temp
# folders, and setup is not run (ASKORUS_NO_SETUP). The test agent runs
# without its Tailscale node (ASKORUS_TEST_NO_TAILNET): it signs in nowhere,
# talks to no server and no phone can reach it. Exit code 0 only if all pass.
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

# Whether the test agent runs, as its own status says (it opens no port on
# the PC that could be tried, #57).
function Running() {
    if (-not $script:exe -or -not (Test-Path $script:exe)) { return $false }
    return (@(& $script:exe status)[0] -eq 'running')
}

# Runs install.ps1 in a new PowerShell ($Shell) with the ASKORUS_ variables
# given, through -File (with the arguments in $More) or, as `irm | iex`
# does, through Invoke-Expression.
function Install([hashtable]$vars, [switch]$Iex, [string[]]$More = @()) {
    $saved = @{}
    foreach ($k in $vars.Keys) { $saved[$k] = [Environment]::GetEnvironmentVariable($k); [Environment]::SetEnvironmentVariable($k, $vars[$k]) }
    try {
        if ($Iex) {
            # As `irm | iex` in a window; $LASTEXITCODE afterwards is what the person or an AI sees.
            $out = & $Shell -NoProfile -ExecutionPolicy Bypass -Command "Get-Content -Raw -LiteralPath '$install' | Invoke-Expression; exit `$LASTEXITCODE" 2>&1 | ForEach-Object { "$_" }
        } else {
            $out = & $Shell -NoProfile -ExecutionPolicy Bypass -File $install @More 2>&1 | ForEach-Object { "$_" }
        }
        return @{ code = $LASTEXITCODE; out = @($out); text = ($out -join "`n") }
    } finally {
        foreach ($k in $vars.Keys) { [Environment]::SetEnvironmentVariable($k, $saved[$k]) }
    }
}

# A port number of its own for the test agent, so the real one is never
# asked to stop (the number names the running agent for start and shutdown).
$port = 18797 + (Get-Random -Maximum 1000)
$agentDir = Join-Path $T 'agent'
New-Item -ItemType Directory -Path $agentDir, (Join-Path $T 'ask') | Out-Null
$cfg = @{ port = $port; folders = @(); ask = @{ folder = (Join-Path $T 'ask') } } | ConvertTo-Json
[IO.File]::WriteAllText((Join-Path $agentDir 'agent.json'), $cfg, (New-Object Text.UTF8Encoding $false))
$env:ASKORUS_AGENT_DIR = $agentDir
$env:ASKORUS_TEST_NO_TAILNET = '1'
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
    Check 'it says the files do not come from the GitHub release (#48)' ($r.text -match 'ASKORUS_DOWNLOAD_BASE is set: the files come from') $r.out
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

    Write-Output '== a version that is not one (#48)'
    # The last two: a line end, and Arabic-Indic digits (2ho's review).
    $arabic = (([char]0x0660), '.', ([char]0x0666), '.', ([char]0x0663)) -join ''
    foreach ($v in '0.2.0/../../x', '0.6.3 x', 'latest', '0.6.3-', "0.6.3`n", $arabic) {
        $r = Install @{ ASKORUS_VERSION = $v; ASKORUS_INSTALL_DIR = $badDir; ASKORUS_NO_SETUP = '1' }
        Check "refused before anything is fetched: '$([string]::Join(' ', [int[]][char[]]$v))'" ($r.code -eq 1 -and $r.text -match 'must be a version such as' -and $r.text -notmatch 'downloading' -and -not (Test-Path (Join-Path $badDir 'askorus-agent.exe'))) $r.out
    }

    Write-Output '== -NoSetup, with no ASKORUS_NO_SETUP (#48)'
    $nsDir = Join-Path $T 'nosetup'
    $r = Install @{ ASKORUS_DOWNLOAD_BASE = $rel1; ASKORUS_INSTALL_DIR = $nsDir; ASKORUS_NO_SETUP = '' } -More '-NoSetup'
    Check 'installed, and setup is left for later' ($r.code -eq 0 -and (Test-Path (Join-Path $nsDir 'askorus-agent.exe')) -and $r.text -match 'next, run: .+ setup') $r.out

    Write-Output "== update while the agent runs, but its file is locked (test agent, port number $port)"
    & $exe start | Out-Null
    Check 'the test agent runs' (Running) $null
    $lock = [IO.File]::Open($exe, 'Open', 'Read', 'Read') # no delete sharing: the file cannot be renamed
    try { $r = Install @{ ASKORUS_DOWNLOAD_BASE = $rel2; ASKORUS_INSTALL_DIR = $dir; ASKORUS_NO_SETUP = '1' } } finally { $lock.Close() }
    Check 'exit code 1, says the old one stays and runs again' ($r.code -eq 1 -and $r.text -match 'Could not put the new agent in place' -and $r.text -match 'the old one stays and runs again') $r.out
    Check 'the old exe is still in place' (((& $exe version) -join '') -eq 'askorus-agent 0.0.1-test') $null
    Check 'the agent runs again' (Running) $null
    Check 'no askorus-agent.new.exe is left' (-not (Test-Path (Join-Path $dir 'askorus-agent.new.exe'))) $null

    Write-Output "== update while the agent runs"
    $r = Install @{ ASKORUS_DOWNLOAD_BASE = $rel2; ASKORUS_INSTALL_DIR = $dir; ASKORUS_NO_SETUP = '1' }
    Check 'exit code 0' ($r.code -eq 0) $r.out
    Check 'stopped it, updated, kept the old one' ($r.text -match 'stopping the agent' -and $r.text -match 'updated askorus-agent 0\.0\.1-test to askorus-agent 0\.0\.2-test') $r.out
    Check 'the new exe is in place' (((& $exe version) -join '') -eq 'askorus-agent 0.0.2-test') $null
    Check 'the old exe is askorus-agent.prev.exe' (((& (Join-Path $dir 'askorus-agent.prev.exe') version) -join '') -eq 'askorus-agent 0.0.1-test') $null
    Check 'the agent runs again, from the new exe' ((Running) -and [bool](Get-Process -Name 'askorus-agent' -ErrorAction SilentlyContinue | Where-Object { $_.Path -eq $exe })) $r.out
    & $exe shutdown | Out-Null

    Write-Output '== update while the agent does not run (and -NoSetup in place of ASKORUS_NO_SETUP)'
    $r = Install @{ ASKORUS_DOWNLOAD_BASE = $rel2; ASKORUS_INSTALL_DIR = $dir; ASKORUS_NO_SETUP = '' } -More '-NoSetup'
    Check 'exit code 0, no stop' ($r.code -eq 0 -and $r.text -notmatch 'stopping') $r.out
    Check 'does not start it' (-not (Running)) $null
    Check 'does not run setup again' ($r.text -notmatch 'running it again') $r.out

    Write-Output '== release checks (#48): a release version only from a clean, pushed release commit whose CI passed'
    # A repository of its own with release.ps1 and the line it reads, and a
    # gh in front of the real one on PATH that answers from files.
    $repo = Join-Path $T 'relrepo'
    New-Item -ItemType Directory -Path (Join-Path $repo 'scripts'), (Join-Path $repo 'agent\cmd\askorus-agent') | Out-Null
    foreach ($f in 'release.ps1', 'install.ps1', 'install.sh') { Copy-Item (Join-Path $PSScriptRoot $f) (Join-Path $repo "scripts\$f") }
    [IO.File]::WriteAllText((Join-Path $repo 'agent\cmd\askorus-agent\main.go'), "package main`n`nvar version = `"9.9.9`"`n")
    # git and release.ps1 write to stderr; in 5.1 with Stop that would end the test.
    function RepoGit {
        $e = $ErrorActionPreference
        $ErrorActionPreference = 'Continue'
        try { & git -C $repo -c core.autocrlf=false @args 2>&1 | ForEach-Object { "$_" } } finally { $ErrorActionPreference = $e }
    }
    RepoGit init -q | Out-Null
    RepoGit add -A | Out-Null
    RepoGit -c user.name=test -c user.email=test@users.noreply.github.com -c commit.gpgsign=false commit -q -m 'release 9.9.9' | Out-Null
    $sha = (RepoGit rev-parse HEAD) -join ''
    if ($sha -notmatch '^[0-9a-f]{40}$') { throw "the test repository has no commit: $sha" }
    $fake = Join-Path $T 'fakegh'
    New-Item -ItemType Directory -Path $fake | Out-Null
    Set-Content (Join-Path $fake 'gh.cmd') '@powershell -NoProfile -ExecutionPolicy Bypass -File "%~dp0fakegh.ps1" %*', '@exit /b %ERRORLEVEL%' -Encoding Ascii
    Set-Content (Join-Path $fake 'fakegh.ps1') -Encoding Ascii -Value @'
$a = $args -join ' '
Add-Content -LiteralPath (Join-Path $PSScriptRoot 'calls.txt') -Value $a
if ($a -match 'commits/') { $f = Join-Path $PSScriptRoot 'commit.txt'; if (Test-Path $f) { Get-Content $f; exit 0 }; [Console]::Error.WriteLine('HTTP 422: No commit found'); exit 1 }
if ($a -match '/runs') { $f = Join-Path $PSScriptRoot 'runs.txt'; if (Test-Path $f) { Get-Content $f }; exit 0 }
if ($a -match 'compare/') { 'identical'; exit 0 }
exit 2
'@
    function Runs([string[]]$lines) {
        $f = Join-Path $fake 'runs.txt'
        if ($lines) { Set-Content $f $lines -Encoding Ascii } elseif (Test-Path $f) { Remove-Item $f }
    }
    function Release([string]$version) {
        $o = Join-Path $T ('relout-' + [Guid]::NewGuid().ToString('N').Substring(0, 6))
        $saved = $env:Path
        $e = $ErrorActionPreference
        $env:Path = "$fake;$env:Path"
        $ErrorActionPreference = 'Continue'
        try { $got = & $Shell -NoProfile -ExecutionPolicy Bypass -File (Join-Path $repo 'scripts\release.ps1') -Version $version -NoSign -Out $o 2>&1 | ForEach-Object { "$_" } } finally { $env:Path = $saved; $ErrorActionPreference = $e }
        return @{ code = $LASTEXITCODE; out = @($got); text = ($got -join "`n"); built = (Test-Path $o) }
    }
    Set-Content (Join-Path $fake 'commit.txt') $sha -Encoding Ascii
    # A run: id, event, status, conclusion, address. The order given is not the order of the ids.
    function Run([long]$id, [string]$event, [string]$status, [string]$conclusion) { "$id`t$event`t$status`t$conclusion`thttps://ci.invalid/$id" }
    Runs (Run 9 push completed success), (Run 7 workflow_dispatch completed failure)
    $r = Release '9.9.9'
    Check 'the checks pass when the run with the highest id passed' ($r.text -match "release checks passed for commit $sha" -and $r.text -match 'https://ci.invalid/9') $r.out
    Check 'it builds from a clean copy of the commit' ($r.text -match "building from a clean copy of commit $sha") $r.out
    Check 'it asked about this very commit' ((Get-Content (Join-Path $fake 'calls.txt') -Raw) -match "head_sha=$sha") $null
    Runs (Run 9 push completed failure), (Run 7 push completed success)
    # A hook in this user's git that puts a file into every checkout: it runs
    # in a plain clone, and not in the copy a release is built from.
    $hooks = Join-Path $T 'hooks'
    New-Item -ItemType Directory -Path $hooks | Out-Null
    $ran = Join-Path $T 'hook-ran.txt'
    [IO.File]::WriteAllText((Join-Path $hooks 'post-checkout'), "#!/bin/sh`necho ran > '$($ran.Replace('\', '/'))'`nmkdir -p agent && echo x > agent/planted.log`n")
    $global = Join-Path $T 'gitconfig'
    [IO.File]::WriteAllText($global, "[core]`n`thooksPath = $($hooks.Replace('\', '/'))`n")
    $savedGlobal = $env:GIT_CONFIG_GLOBAL
    $env:GIT_CONFIG_GLOBAL = $global
    try {
        $e = $ErrorActionPreference; $ErrorActionPreference = 'Continue'
        & git clone --quiet $repo (Join-Path $T 'plainclone') 2>&1 | Out-Null
        $ErrorActionPreference = $e
        Check 'the planted hook runs in a plain clone' (Test-Path $ran) $null
        if (Test-Path $ran) { Remove-Item $ran }
        Runs (Run 9 push completed success)
        $r = Release '9.9.9'
        Check 'no hook of the user runs in the release copy' ($r.text -match "building from a clean copy of commit $sha" -and -not (Test-Path $ran)) $r.out
    } finally { $env:GIT_CONFIG_GLOBAL = $savedGlobal } # setting it empty removes it, as it was when unset
    Runs (Run 9 push completed failure), (Run 7 push completed success)
    $r = Release '9.9.9'
    Check 'refused when the latest run failed, nothing built' ($r.code -ne 0 -and $r.text -match "ended 'failure'" -and -not $r.built) $r.out
    Runs (Run 9 push in_progress ''), (Run 7 push completed success)
    $r = Release '9.9.9'
    Check 'refused while the latest run still runs' ($r.code -ne 0 -and $r.text -match 'still in_progress' -and -not $r.built) $r.out
    Runs (Run 11 pull_request completed success), (Run 9 push completed failure)
    $r = Release '9.9.9'
    Check "a pull request's run does not count (it tests a merge)" ($r.code -ne 0 -and $r.text -match "ended 'failure'" -and -not $r.built) $r.out
    Runs (Run 11 pull_request completed success)
    $r = Release '9.9.9'
    Check 'refused when only a pull request ran' ($r.code -ne 0 -and $r.text -match 'has not run on commit' -and -not $r.built) $r.out
    Runs $null
    $r = Release '9.9.9'
    Check 'refused when CI has not run for the commit' ($r.code -ne 0 -and $r.text -match 'has not run on commit' -and -not $r.built) $r.out
    Runs (Run 9 push completed success)
    Remove-Item (Join-Path $fake 'commit.txt')
    $r = Release '9.9.9'
    Check 'refused when the commit is not on GitHub' ($r.code -ne 0 -and $r.text -match 'is not on GitHub' -and -not $r.built) $r.out
    Set-Content (Join-Path $fake 'commit.txt') $sha -Encoding Ascii
    $r = Release '9.9.8'
    Check 'refused when the agent does not name the version' ($r.code -ne 0 -and $r.text -match 'does not say version 9\.9\.8' -and -not $r.built) $r.out
    Set-Content (Join-Path $repo 'stray.txt') 'x' -Encoding Ascii
    $r = Release '9.9.9'
    Check 'refused with changes beside the commit' ($r.code -ne 0 -and $r.text -match 'not committed' -and -not $r.built) $r.out
    RepoGit config status.showUntrackedFiles no | Out-Null
    $r = Release '9.9.9'
    Check 'refused although this checkout hides new files' ($r.code -ne 0 -and $r.text -match 'not committed' -and -not $r.built) $r.out
    $r = Release '0.0.9-test'
    Check 'a test version is not checked' ($r.text -notmatch 'release checks|not committed|not on GitHub') $r.out
} catch {
    Write-Output "   FAIL $($_.Exception.Message)"
    $failed += 'error'
} finally {
    if (Running) { & $exe shutdown | Out-Null }
    Remove-Item Env:ASKORUS_AGENT_DIR -ErrorAction SilentlyContinue
    Remove-Item Env:ASKORUS_TEST_NO_TAILNET -ErrorAction SilentlyContinue
    if ($Keep) { Write-Output "kept $T" } else { Remove-Item $T -Recurse -Force -ErrorAction SilentlyContinue }
}

if ($failed.Count) {
    Write-Output ('FAILED: ' + ($failed -join ', '))
    exit 1
}
Write-Output 'ALL PASSED'
exit 0
