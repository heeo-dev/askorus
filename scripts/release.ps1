# Builds the files of a PC agent release (#37, #32) into one folder:
#   askorus-agent-windows-amd64.exe  built with -trimpath and the version
#   install.ps1                      scripts\install.ps1 (it must stay ASCII)
#   SHA256SUMS.txt                   "<sha-256>  <file name>" per file, as sha256sum
# It only builds and checks; publishing the GitHub release is a person's step.
#   powershell -NoProfile -File scripts\release.ps1 -Version 0.2.0 [-Out DIR]
# The default folder is .local\release\v<version> of this checkout. Keep this
# file ASCII: Windows PowerShell 5.1 reads scripts without a BOM as the ANSI
# code page.
param(
    [Parameter(Mandatory = $true)][string]$Version,
    [string]$Out
)
$ErrorActionPreference = 'Stop'
$root = Split-Path -Parent $PSScriptRoot
if ($Version -notmatch '^\d+\.\d+\.\d+(-[0-9A-Za-z.]+)?$') { throw "the version must look like 0.2.0 or 0.2.0-rc.1, not $Version" }
if (-not $Out) { $Out = Join-Path $root ".local\release\v$Version" }
$go = Join-Path $env:USERPROFILE 'sdk\go1.27.3\bin'
if (Test-Path $go) { $env:Path = "$go;" + $env:Path }

New-Item -ItemType Directory -Force -Path $Out | Out-Null
$Out = (Resolve-Path $Out).Path
$exeName = 'askorus-agent-windows-amd64.exe'
$exe = Join-Path $Out $exeName

# The exe: no local paths in it (-trimpath), no debug symbols, the version
# in main.version (the app reads it from /v1/info).
$env:GOOS = 'windows'; $env:GOARCH = 'amd64'; $env:CGO_ENABLED = '0'
Push-Location (Join-Path $root 'agent')
try {
    & go build -trimpath -ldflags "-s -w -X main.version=$Version" -o $exe ./cmd/askorus-agent
    if ($LASTEXITCODE -ne 0) { throw 'go build failed' }
} finally {
    Pop-Location
    Remove-Item Env:GOOS, Env:GOARCH, Env:CGO_ENABLED -ErrorAction SilentlyContinue
}
$said = (& $exe version) -join ' '
if ($said -ne "askorus-agent $Version") { throw "the exe says '$said', not 'askorus-agent $Version'" }

# Nothing of this PC may be inside the exe: this checkout's path, the home
# folder, and the personal patterns test-all checks the repo with
# (.local\private-patterns.txt of the main checkout, when it is there).
# The exe is read one char per byte, so each check is turned into its UTF-8
# bytes the same way: a Korean user or folder name is found too.
$latin = [Text.Encoding]::GetEncoding(28591)
function AsBytes([string]$s) { $latin.GetString([Text.Encoding]::UTF8.GetBytes($s)) }
$text = $latin.GetString([IO.File]::ReadAllBytes($exe))
$checks = @()
foreach ($p in $root, $env:USERPROFILE) {
    $checks += [regex]::Escape((AsBytes $p)), [regex]::Escape((AsBytes $p.Replace('\', '/')))
}
$common = & git -C $root rev-parse --path-format=absolute --git-common-dir
$patterns = Join-Path (Split-Path -Parent $common) '.local\private-patterns.txt'
if (Test-Path $patterns) { $checks += @(Get-Content -Encoding UTF8 $patterns | Where-Object { $_ -ne '' } | ForEach-Object { AsBytes $_ }) }
$hits = @()
for ($i = 0; $i -lt $checks.Count; $i++) {
    if ([regex]::IsMatch($text, $checks[$i], 'IgnoreCase')) { $hits += $i }
}
if ($hits.Count) { Remove-Item $exe; throw "the exe holds a local path or a personal value (checks $($hits -join ', ')); it was deleted" }

# install.ps1 goes as it is; irm | iex in Windows PowerShell 5.1 can garble
# letters past ASCII.
$script = Join-Path $PSScriptRoot 'install.ps1'
$bytes = [IO.File]::ReadAllBytes($script)
if (@($bytes | Where-Object { $_ -gt 127 }).Count) { throw 'scripts\install.ps1 must be ASCII only' }
Copy-Item $script (Join-Path $Out 'install.ps1') -Force

$lines = foreach ($name in $exeName, 'install.ps1') {
    (Get-FileHash -Algorithm SHA256 (Join-Path $Out $name)).Hash.ToLower() + '  ' + $name
}
[IO.File]::WriteAllText((Join-Path $Out 'SHA256SUMS.txt'), (($lines -join "`n") + "`n"), [Text.Encoding]::ASCII)

Write-Output "release files for $Version in ${Out}:"
Get-ChildItem $Out | ForEach-Object { '  {0,-34} {1,10:N0} bytes' -f $_.Name, $_.Length }
Write-Output ''
Write-Output 'To publish (a person does this, after #15):'
Write-Output "  gh release create v$Version `"$Out\$exeName`" `"$Out\install.ps1`" `"$Out\SHA256SUMS.txt`" --title `"Askorus PC agent $Version`" --notes-file NOTES.md"
