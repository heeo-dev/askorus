# Askorus PC agent installer for Windows. Askorus is not an official Anthropic product.
#
#   irm https://github.com/heeo-dev/askorus/releases/latest/download/install.ps1 | iex
#
# What it does, and nothing more:
#   1. downloads askorus-agent-windows-amd64.exe (saved as askorus-agent.exe) and
#      SHA256SUMS.txt from the GitHub release
#   2. checks the exe's SHA-256 against SHA256SUMS.txt (a damaged download stops here)
#   3. puts it in %LOCALAPPDATA%\Programs\Askorus, without administrator rights
#   4. on a new install runs `askorus-agent setup`, which walks you through the
#      rest (PATH, AI programs, start, start at logon, Tailscale sign-in, pairing)
#   Run it again to update: it stops the agent, swaps the exe (the old one stays
#   as askorus-agent.prev.exe) and starts the agent again. If setup stopped
#   before it finished, it runs setup again.
#
# To read it before you run it:
#   irm https://github.com/heeo-dev/askorus/releases/latest/download/install.ps1 -OutFile install.ps1
#   notepad install.ps1
#   powershell -ExecutionPolicy Bypass -File .\install.ps1
#
# Optional settings, as environment variables:
#   ASKORUS_VERSION        a release such as 0.2.0 instead of the latest
#   ASKORUS_INSTALL_DIR    another folder instead of %LOCALAPPDATA%\Programs\Askorus
#   ASKORUS_NO_SETUP=1     only install or update, do not run setup
#   ASKORUS_DOWNLOAD_BASE  where the release files are (an https address or a folder; for tests)
#
# The file is ASCII only, because `irm | iex` in Windows PowerShell 5.1 can garble
# other letters. The agent itself speaks Korean and English. Through iex a failure
# sets $LASTEXITCODE to 1; with -File it is the exit code.

& {
    Set-StrictMode -Version 2
    $ErrorActionPreference = 'Stop'
    $ProgressPreference = 'SilentlyContinue' # the progress bar makes downloads in 5.1 very slow
    $fromFile = [bool]$PSCommandPath         # run with -File (exit codes work) or through iex
    $asset = 'askorus-agent-windows-amd64.exe'

    function Say([string]$text) { Write-Host "askorus: $text" }

    # Copies one release file to a local path, from https or from a folder.
    function Get-ReleaseFile([string]$base, [string]$name, [string]$to) {
        if ($base -match '^https://') {
            Invoke-WebRequest -Uri "$base/$name" -OutFile $to -UseBasicParsing
        } elseif ($base -match '^[a-z]+://') {
            throw "ASKORUS_DOWNLOAD_BASE must be an https address or a folder, not $base"
        } else {
            Copy-Item -LiteralPath (Join-Path $base $name) -Destination $to
        }
    }

    # Runs setup; a setup that stops says what to fix, and it can run again.
    function Invoke-Setup([string]$exe) {
        & $exe setup
        if ($LASTEXITCODE -ne 0) { Say 'setup stopped before it finished: fix what it says, then run "askorus-agent setup" (or this installer) again' }
    }

    $tmp = $null
    $new = $null
    $tls = $null
    try {
        if ($env:OS -ne 'Windows_NT') { throw 'This installer is for Windows.' }
        $arch = $env:PROCESSOR_ARCHITEW6432
        if (-not $arch) { $arch = $env:PROCESSOR_ARCHITECTURE }
        if ($arch -ne 'AMD64' -and $arch -ne 'ARM64') { throw "Askorus needs 64-bit Windows (this PC is $arch)." }
        $me = New-Object Security.Principal.WindowsPrincipal ([Security.Principal.WindowsIdentity]::GetCurrent())
        if ($me.IsInRole([Security.Principal.WindowsBuiltInRole]::Administrator)) {
            throw 'Open a normal PowerShell window (not "Run as administrator") and run this again: the agent runs with normal rights.'
        }
        if ($PSVersionTable.PSVersion.Major -lt 6) {
            # Windows PowerShell 5.1 may not offer TLS 1.2, which GitHub needs (put back at the end).
            $tls = [Net.ServicePointManager]::SecurityProtocol
            [Net.ServicePointManager]::SecurityProtocol = $tls -bor [Net.SecurityProtocolType]::Tls12
        }

        $base = $env:ASKORUS_DOWNLOAD_BASE
        $from = "$base (ASKORUS_DOWNLOAD_BASE)"
        if (-not $base) {
            $base = 'https://github.com/heeo-dev/askorus/releases/latest/download'
            if ($env:ASKORUS_VERSION) { $base = 'https://github.com/heeo-dev/askorus/releases/download/v' + $env:ASKORUS_VERSION.TrimStart('v') }
            $from = $base
        }
        $dir = $env:ASKORUS_INSTALL_DIR
        if (-not $dir) { $dir = Join-Path $env:LOCALAPPDATA 'Programs\Askorus' }
        # One full spelling, the one the running agent's process path has.
        $dir = [IO.Path]::GetFullPath($ExecutionContext.SessionState.Path.GetUnresolvedProviderPathFromPSPath($dir)).TrimEnd('\')
        $exe = Join-Path $dir 'askorus-agent.exe'

        $tmp = Join-Path ([IO.Path]::GetTempPath()) ('askorus-install-' + [Guid]::NewGuid().ToString('N').Substring(0, 8))
        New-Item -ItemType Directory -Path $tmp | Out-Null
        $download = Join-Path $tmp $asset
        $sums = Join-Path $tmp 'SHA256SUMS.txt'
        Say "downloading $asset from $from"
        Get-ReleaseFile $base $asset $download
        Get-ReleaseFile $base 'SHA256SUMS.txt' $sums

        $want = $null
        foreach ($line in Get-Content -LiteralPath $sums) {
            $f = -split $line # "<sha-256>  <file name>"
            if ($f.Count -eq 2 -and $f[1].TrimStart('*') -eq $asset) { $want = $f[0] }
        }
        if (-not $want) { throw "SHA256SUMS.txt has no line for $asset; nothing was installed." }
        $got = (Get-FileHash -Algorithm SHA256 -LiteralPath $download).Hash
        if ($got -ne $want) { throw "The download is damaged or is not the released file (SHA-256 $got, the release says $want); nothing was installed." }
        Say 'the SHA-256 matches the release'

        # The new exe waits next to the old one, so the swap below is a rename,
        # and it must run here before anything is changed.
        New-Item -ItemType Directory -Path $dir -Force | Out-Null
        $new = Join-Path $dir 'askorus-agent.new.exe'
        Move-Item -LiteralPath $download -Destination $new -Force
        $said = (& $new version) -join ' '
        if ($LASTEXITCODE -ne 0 -or $said -notmatch '^askorus-agent \S+$') { throw "The downloaded agent does not run on this PC ($said); nothing was changed." }

        if (-not (Test-Path -LiteralPath $exe)) {
            Move-Item -LiteralPath $new -Destination $exe
            $new = $null
            Say "installed $said in $dir"
            if ($env:ASKORUS_NO_SETUP -eq '1') {
                Say "next, run: & '$exe' setup"
                return
            }
            Invoke-Setup $exe
            # setup put the folder on this user's PATH for new windows; this one finds it too.
            if (($env:Path -split ';') -notcontains $dir) { $env:Path = $env:Path.TrimEnd(';') + ';' + $dir }
            return
        }

        $old = (& $exe version) -join ' '
        $running = [bool](Get-Process -Name 'askorus-agent' -ErrorAction SilentlyContinue | Where-Object { $_.Path -eq $exe })
        if ($running) {
            Say 'stopping the agent to update it'
            & $exe shutdown
            if ($LASTEXITCODE -ne 0) { throw 'The agent did not stop, so nothing was changed; run "askorus-agent shutdown" and then this again.' }
        }
        $prev = Join-Path $dir 'askorus-agent.prev.exe'
        try {
            if (Test-Path -LiteralPath $prev) { Remove-Item -LiteralPath $prev -Force }
            Move-Item -LiteralPath $exe -Destination $prev
            try {
                Move-Item -LiteralPath $new -Destination $exe
                $new = $null
            } catch {
                Move-Item -LiteralPath $prev -Destination $exe
                throw
            }
        } catch {
            $why = $_.Exception.Message
            $back = ''
            if ($running) {
                & $exe start | Out-Null
                if ($LASTEXITCODE -eq 0) { $back = ' and runs again' } else { $back = ', but did not start again: run "askorus-agent start"' }
            }
            throw "Could not put the new agent in place ($why); the old one stays$back."
        }
        Say "updated $old to $said in $dir (the old one is askorus-agent.prev.exe)"
        if ($running) {
            & $exe start
            if ($LASTEXITCODE -ne 0) { throw 'The new agent did not start (see above); the old one is askorus-agent.prev.exe in the same folder.' }
        }
        # A setup that finished set the logon entry; without it, setup stopped early.
        if ($env:ASKORUS_NO_SETUP -ne '1' -and @(& $exe autostart status)[0] -eq 'off') {
            Say 'setup did not finish before; running it again'
            Invoke-Setup $exe
        }
        # An agent from before 0.3.0 had no Tailscale inside: the PC was never signed in.
        if ("$(& $exe status)" -match 'not signed in to Tailscale') {
            Say 'this version has Tailscale built in: run "askorus-agent login" once, then pair the phone again in the app (tap this PC)'
        }
    } catch {
        Write-Host "askorus: $($_.Exception.Message)" -ForegroundColor Red
        if ($fromFile) { exit 1 }
        $global:LASTEXITCODE = 1
    } finally {
        if ($new -and (Test-Path -LiteralPath $new)) { Remove-Item -LiteralPath $new -Force -ErrorAction SilentlyContinue }
        if ($tmp -and (Test-Path -LiteralPath $tmp)) { Remove-Item -LiteralPath $tmp -Recurse -Force -ErrorAction SilentlyContinue }
        if ($null -ne $tls) { [Net.ServicePointManager]::SecurityProtocol = $tls }
    }
}
