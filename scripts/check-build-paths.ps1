# Looks through every file of an APK or AAB for the folder it was built in
# (#63): this checkout, the user's own folder, the Go module cache. Prints
# which files have one, never the path itself, and fails when any does.
#
#   scripts\check-build-paths.ps1 -File <app.apk|app.aab> [-Also <folder>...]
param(
    [Parameter(Mandatory = $true)][string]$File,
    [string[]]$Also = @()
)
$ErrorActionPreference = 'Stop'
Add-Type -AssemblyName System.IO.Compression.FileSystem

$root = (Resolve-Path (Join-Path $PSScriptRoot '..')).Path
# A worktree of the checkout lies inside it: the checkout's own folder counts too.
$i = $root.IndexOf('\.claude\worktrees', [StringComparison]::OrdinalIgnoreCase)
$main = if ($i -gt 0) { $root.Substring(0, $i) } else { $root }
$folders = @($root, $main, $env:USERPROFILE) + $Also | Where-Object { $_ } | Sort-Object -Unique
# Each folder as the compiler and Java write it: back and forward slashes.
$needles = foreach ($f in $folders) {
    $f.TrimEnd('\', '/')
    $f.TrimEnd('\', '/').Replace('\', '/')
}
$needles = $needles | Sort-Object -Unique

$zip = [System.IO.Compression.ZipFile]::OpenRead((Resolve-Path $File).Path)
$count = $zip.Entries.Count
$found = @()
try {
    foreach ($e in $zip.Entries) {
        if ($e.Length -eq 0) { continue }
        $s = $e.Open()
        try {
            $ms = New-Object System.IO.MemoryStream
            $s.CopyTo($ms)
            # Latin-1 keeps every byte as one character, so a path in UTF-8 or ASCII is found as written.
            $text = [System.Text.Encoding]::GetEncoding(28591).GetString($ms.ToArray())
        } finally {
            $s.Dispose()
        }
        foreach ($n in $needles) {
            if ($text.IndexOf($n, [StringComparison]::OrdinalIgnoreCase) -ge 0) {
                $found += $e.FullName
                break
            }
        }
    }
} finally {
    $zip.Dispose()
}
if ($found.Count) {
    "build paths in $($found.Count) file(s) of $(Split-Path -Leaf $File):"
    $found | Sort-Object -Unique | ForEach-Object { "  $_" }
    exit 1
}
"no build paths in $(Split-Path -Leaf $File) ($count files)"
