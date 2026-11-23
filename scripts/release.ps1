# Builds the files of a PC agent release (#37, #32, #52) into one folder:
#   askorus-agent-windows-amd64.exe  the agent for Windows
#   askorus-agent-darwin-arm64       the agent for macOS on Apple silicon
#   askorus-agent-darwin-amd64       the agent for macOS on Intel
#   askorus-agent-linux-amd64        the agent for Linux on Intel/AMD
#   askorus-agent-linux-arm64        the agent for Linux on ARM
#   install.ps1                      scripts\install.ps1 (it must stay ASCII)
#   install.sh                       scripts\install.sh (ASCII, LF line ends)
#   SHA256SUMS.txt                   "<sha-256>  <file name>" per file, as sha256sum
#   release.json(.minisig)           what an agent updates to, signed by the
#   release-key.json(.minisig)       release key the root key vouches for (#35)
# Every agent is built with -trimpath and the version. It only builds and
# checks; publishing the GitHub release is a step of its own.
#   powershell -NoProfile -File scripts\release.ps1 -Version 0.5.0 -SignDir DIR [-Out DIR]
# A release version (three numbers) is signed with the release key in
# -SignDir (askorus-sign, Phase 1 plan part 27), so agents can update to it
# from the phone; -NoSign leaves it unsigned on purpose. A test version
# (0.0.1-test) is never signed. The default folder is .local\release\v<version>
# of this checkout. Keep this file ASCII: Windows PowerShell 5.1 reads
# scripts without a BOM as the ANSI code page.
# A release version is built only from a commit that has no changes beside
# it, names that version in the agent (the release commit), is on GitHub,
# and whose latest run of the agent CI (agent-unix.yml, macOS and Linux)
# passed (#48: 0.5.0 to 0.6.2 went out while the Mac job failed). That needs
# gh, signed in. There is no switch to skip it; a test version is not
# checked.
param(
    [Parameter(Mandatory = $true)][string]$Version,
    [string]$Out,
    [string]$SignDir,
    [switch]$NoSign
)
$ErrorActionPreference = 'Stop'
$root = Split-Path -Parent $PSScriptRoot
# ASCII digits only, and one line (\A, \z): \d and $ would let other digits and a line end in.
if ($Version -cnotmatch '\A[0-9]+\.[0-9]+\.[0-9]+(-[0-9A-Za-z.]+)?\z') { throw "the version must look like 0.2.0 or 0.2.0-rc.1, not $Version" }
$isRelease = $Version -cmatch '\A[0-9]+\.[0-9]+\.[0-9]+\z'
if ($isRelease -and -not $SignDir -and -not $NoSign) { throw "a release version is signed: give -SignDir <the release key folder>, or -NoSign to leave it unsigned (phones then cannot update agents to it)" }
if (-not $isRelease -and $SignDir) { throw "$Version is a test version; only release versions (three numbers) are signed" }
if ($SignDir -and $NoSign) { throw 'give -SignDir or -NoSign, not both' }
if (-not $Out) { $Out = Join-Path $root ".local\release\v$Version" }
# Now, before the build works in other folders (2ho's re-check).
if ($SignDir) { $SignDir = (Resolve-Path -LiteralPath $SignDir).Path }

# The checks of a release version (#48), before anything is built.
$repo = 'heeo-dev/askorus'
$commit = (& git -C $root rev-parse HEAD) -join ''
if ($LASTEXITCODE -ne 0 -or $commit -notmatch '^[0-9a-f]{40}$') { throw "git rev-parse HEAD failed in $root" }
# gh's answer and exit code. In Windows PowerShell 5.1 with Stop, a native
# command that writes to stderr ends the script even with 2>$null, which
# would hide the reason below behind gh's own words.
function Invoke-Gh {
    $e = $ErrorActionPreference
    $ErrorActionPreference = 'Continue'
    try {
        $o = @(& gh @args 2>$null)
        return @{ code = $LASTEXITCODE; out = $o }
    } finally { $ErrorActionPreference = $e }
}
function Assert-ReleaseReady {
    # Every file, whatever this checkout's settings hide (status.showUntrackedFiles).
    $changes = @(& git -C $root status --porcelain --untracked-files=all --ignore-submodules=none)
    if ($LASTEXITCODE -ne 0) { throw 'git status failed' }
    if ($changes.Count) { throw "the checkout has changes that are not committed ($($changes.Count), first: $($changes[0].Trim())); a release is built only from a commit" }
    $main = [IO.File]::ReadAllText((Join-Path $root 'agent\cmd\askorus-agent\main.go'))
    if ($main -notmatch ('(?m)^var version = "' + [regex]::Escape($Version) + '"\r?$')) {
        throw "agent\cmd\askorus-agent\main.go does not say version $Version; make the release commit first"
    }
    if (-not (Get-Command gh -ErrorAction SilentlyContinue)) { throw 'gh is needed to see that the commit is on GitHub and that its CI passed' }
    $known = Invoke-Gh api "repos/$repo/commits/$commit" --jq '.sha'
    if ($known.code -ne 0 -or ($known.out -join '') -ne $commit) { throw "commit $commit is not on GitHub; push it first" }
    # Each run: id, event, status, conclusion, html_url. Only runs of a push
    # or of workflow_dispatch tested this very commit: a pull request's run
    # tests a merge of it (2ho's review). The latest is the one with the
    # highest id; a run started again keeps its id and takes the new result.
    $listed = Invoke-Gh api -X GET "repos/$repo/actions/workflows/agent-unix.yml/runs" -f "head_sha=$commit" -f 'per_page=100' --jq '.workflow_runs[] | [.id, .event, .status, .conclusion, .html_url] | @tsv'
    if ($listed.code -ne 0) { throw 'gh could not list the CI runs of the commit (is gh signed in?)' }
    $runs = @($listed.out | Where-Object { $_ } | ForEach-Object { , ($_ -split "`t") } |
        Where-Object { $_.Count -eq 5 -and $_[0] -match '\A[0-9]+\z' -and ($_[1] -eq 'push' -or $_[1] -eq 'workflow_dispatch') } |
        Sort-Object { [long]$_[0] } -Descending)
    if (-not $runs.Count) { throw "the agent CI has not run on commit $commit itself (a push or workflow_dispatch); run it (gh workflow run agent-unix.yml --ref <branch>), wait for it to pass, then build" }
    $last = $runs[0]
    if ($last[2] -ne 'completed') { throw "the agent CI for commit $commit is still $($last[2]); wait for it: $($last[4])" }
    if ($last[3] -ne 'success') { throw "the latest agent CI run for commit $commit ended '$($last[3])'; a release needs it to pass: $($last[4])" }
    Write-Output "release checks passed for commit $commit (no changes, version $Version, on GitHub, agent CI passed: $($last[4]))"
    $where = (Invoke-Gh api "repos/$repo/compare/main...$commit" --jq '.status').out -join ''
    if ($where -ne 'identical' -and $where -ne 'behind') { Write-Warning "commit $commit is not on main yet ($where): put it on main before publishing" }
}
if ($isRelease) { Assert-ReleaseReady }

# What the files are built from. A release version comes from a clean copy
# of the commit, not from this checkout: a file git ignores would not show
# in the checks above, yet the agent carries the whole
# agent\internal\update\roots folder (go:embed roots) (#48, 2ho's review).
# The copy is a git clone that shares this checkout's objects, so Go still
# writes the commit into the agent (vcs.revision, vcs.modified=false): the
# 0.6.3 agent built so came out byte for byte the published one.
# It is made inside the try below, which removes it however the build ends.
$src = Join-Path $root 'agent'
$scripts = $PSScriptRoot
$copy = $null
$go = Join-Path $env:USERPROFILE 'sdk\go1.27.3\bin'
if (Test-Path $go) { $env:Path = "$go;" + $env:Path }

New-Item -ItemType Directory -Force -Path $Out | Out-Null
$Out = (Resolve-Path $Out).Path
$exeName = 'askorus-agent-windows-amd64.exe'
$exe = Join-Path $Out $exeName
# The agents for the other systems: file name, GOOS, GOARCH.
$others = @(
    @('askorus-agent-darwin-arm64', 'darwin', 'arm64'),
    @('askorus-agent-darwin-amd64', 'darwin', 'amd64'),
    @('askorus-agent-linux-amd64', 'linux', 'amd64'),
    @('askorus-agent-linux-arm64', 'linux', 'arm64')
)

# An agent: no local paths in it (-trimpath), no debug symbols, the version
# in main.version (the app reads it from /v1/info). The tags leave out the
# parts of the Tailscale library the agent must not carry (tsnode.BuildTags:
# a port on the PC's own addresses, diagnostics, the log uploader).
$tsTags = 'ts_omit_peerapiserver,ts_omit_c2n,ts_omit_logtail'
function Build([string]$file, [string]$os, [string]$arch) {
    $env:GOOS = $os; $env:GOARCH = $arch; $env:CGO_ENABLED = '0'
    Push-Location $src
    try {
        & go build -trimpath -tags $tsTags -ldflags "-s -w -X main.version=$Version" -o $file ./cmd/askorus-agent
        if ($LASTEXITCODE -ne 0) { throw "go build failed for $os/$arch" }
    } finally {
        Pop-Location
        Remove-Item Env:GOOS, Env:GOARCH, Env:CGO_ENABLED -ErrorAction SilentlyContinue
    }
}

# Nothing of this PC may be inside an agent: this checkout's path, the home
# folder, and the personal patterns test-all checks the repo with
# (.local\private-patterns.txt of the main checkout, when it is there).
# The file is read one char per byte, so each check is turned into its UTF-8
# bytes the same way: a Korean user or folder name is found too.
$latin = [Text.Encoding]::GetEncoding(28591)
function AsBytes([string]$s) { $latin.GetString([Text.Encoding]::UTF8.GetBytes($s)) }
$checks = @()
foreach ($p in $root, $env:USERPROFILE) {
    $checks += [regex]::Escape((AsBytes $p)), [regex]::Escape((AsBytes $p.Replace('\', '/')))
}
$common = & git -C $root rev-parse --path-format=absolute --git-common-dir
$patterns = Join-Path (Split-Path -Parent $common) '.local\private-patterns.txt'
if (Test-Path $patterns) { $checks += @(Get-Content -Encoding UTF8 $patterns | Where-Object { $_ -ne '' } | ForEach-Object { AsBytes $_ }) }
function NothingOfThisPC([string]$file) {
    $text = $latin.GetString([IO.File]::ReadAllBytes($file))
    $hits = @()
    for ($i = 0; $i -lt $checks.Count; $i++) {
        if ([regex]::IsMatch($text, $checks[$i], 'IgnoreCase')) { $hits += $i }
    }
    if ($hits.Count) { Remove-Item $file; throw "$(Split-Path -Leaf $file) holds a local path or a personal value (checks $($hits -join ', ')); it was deleted" }
    # The version is in the file as text, whichever system it is for.
    if (-not $text.Contains($Version)) { throw "$(Split-Path -Leaf $file) does not hold the version $Version" }
}

try {
if ($isRelease) {
    $copy = Join-Path ([IO.Path]::GetTempPath()) ('askorus-release-' + [Guid]::NewGuid().ToString('N').Substring(0, 8))
    # No hook or template of this user's git runs in the copy (a hook could
    # put files into it), and afterwards nothing may be in it beside the
    # commit, ignored files included (2ho's re-check).
    $noHooks = $copy + '-no-hooks' # a folder that is not there: no hooks
    & git -c "core.hooksPath=$noHooks" clone --quiet --no-checkout --shared --template= $root $copy
    if ($LASTEXITCODE -ne 0) { throw 'git clone of this checkout failed' }
    & git -C $copy -c "core.hooksPath=$noHooks" -c advice.detachedHead=false checkout --quiet --detach $commit
    if ($LASTEXITCODE -ne 0) { throw "git could not check out commit $commit" }
    $extra = @(& git -C $copy -c "core.hooksPath=$noHooks" status --porcelain --untracked-files=all --ignored --ignore-submodules=none)
    if ($LASTEXITCODE -ne 0 -or $extra.Count) { throw "the copy of commit $commit holds more than the commit ($($extra.Count), first: $(@($extra)[0])); nothing was built" }
    $checks += [regex]::Escape((AsBytes $copy)), [regex]::Escape((AsBytes $copy.Replace('\', '/')))
    $src = Join-Path $copy 'agent'
    $scripts = Join-Path $copy 'scripts'
    Write-Output "building from a clean copy of commit $commit"
}
Build $exe 'windows' 'amd64'
$said = (& $exe version) -join ' '
if ($said -ne "askorus-agent $Version") { throw "the exe says '$said', not 'askorus-agent $Version'" }
NothingOfThisPC $exe
foreach ($o in $others) {
    $file = Join-Path $Out $o[0]
    Build $file $o[1] $o[2]
    NothingOfThisPC $file
}

# install.ps1 goes as it is; irm | iex in Windows PowerShell 5.1 can garble
# letters past ASCII.
$script = Join-Path $scripts 'install.ps1'
$bytes = [IO.File]::ReadAllBytes($script)
if (@($bytes | Where-Object { $_ -gt 127 }).Count) { throw 'scripts\install.ps1 must be ASCII only' }
Copy-Item $script (Join-Path $Out 'install.ps1') -Force

# install.sh is run by sh on macOS and Linux: a carriage return in it would
# be read as part of each command, so it is written out with LF line ends
# whatever this checkout has.
$sh = [IO.File]::ReadAllBytes((Join-Path $scripts 'install.sh'))
if (@($sh | Where-Object { $_ -gt 127 }).Count) { throw 'scripts\install.sh must be ASCII only' }
[IO.File]::WriteAllBytes((Join-Path $Out 'install.sh'), [byte[]]@($sh | Where-Object { $_ -ne 13 }))

$names = @($exeName) + @($others | ForEach-Object { $_[0] }) + @('install.ps1', 'install.sh')
$lines = foreach ($name in $names) {
    (Get-FileHash -Algorithm SHA256 (Join-Path $Out $name)).Hash.ToLower() + '  ' + $name
}
[IO.File]::WriteAllText((Join-Path $Out 'SHA256SUMS.txt'), (($lines -join "`n") + "`n"), [Text.Encoding]::ASCII)

# The signed release information (#35): written by askorus-sign with the
# release key, then checked the way the agents just built will check it,
# with the root keys they carry.
$signed = @()
if ($SignDir) {
    Push-Location $src
    try {
        & go run ./cmd/askorus-sign release $SignDir $Out $Version
        if ($LASTEXITCODE -ne 0) { throw 'askorus-sign release failed' }
        & go run ./cmd/askorus-sign verify $Out
        if ($LASTEXITCODE -ne 0) { throw 'the signed release does not check out with the root keys the agents carry (agent\internal\update\roots)' }
    } finally {
        Pop-Location
    }
    $signed = @('release.json', 'release.json.minisig', 'release-key.json', 'release-key.json.minisig')
} elseif ($isRelease) {
    Write-Warning "not signed (-NoSign): phones cannot update agents to $Version"
}
} finally {
    if ($copy) { Remove-Item -LiteralPath $copy -Recurse -Force -ErrorAction SilentlyContinue }
}

Write-Output "release files for $Version in ${Out}:"
Get-ChildItem $Out | ForEach-Object { '  {0,-34} {1,10:N0} bytes' -f $_.Name, $_.Length }
Write-Output ''
Write-Output "Built from commit $commit; name it in the release notes."
Write-Output 'To publish:'
$files = ($names + 'SHA256SUMS.txt' + $signed | ForEach-Object { "`"$Out\$_`"" }) -join ' '
Write-Output "  gh release create v$Version $files --target $commit --title `"Askorus $Version`" --notes-file <notes file>"
Write-Output ''
Write-Output "Also attach the app's APK, THIRD-PARTY-NOTICES.md and THIRD-PARTY-GO.md, and give the SHA-256"
Write-Output "of the APK in the notes (README, Install the Android app): askorus-android-$Version.apk when the"
Write-Output "app changed, or the previous release's APK under its own name when only the agent did"
Write-Output '(docs/android-release.md).'
