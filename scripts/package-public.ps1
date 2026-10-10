[CmdletBinding()]
param([string]$OutputPath)
$ErrorActionPreference='Stop'
$repo=Split-Path -Parent $PSScriptRoot
if (!$OutputPath) {$OutputPath=Join-Path (Split-Path -Parent $repo) 'DJ4GHUB-GitHub-Ready-0.6.7.zip'}
$target=[IO.Path]::GetFullPath($OutputPath)
if (Test-Path -LiteralPath $target) {throw 'Archive exists; refusing to overwrite.'}
& (Join-Path $PSScriptRoot 'audit-public.ps1')
Add-Type -AssemblyName System.IO.Compression
Add-Type -AssemblyName System.IO.Compression.FileSystem
$stream=[IO.File]::Open($target,[IO.FileMode]::CreateNew)
$archive=[IO.Compression.ZipArchive]::new($stream,[IO.Compression.ZipArchiveMode]::Create)
$count=0
try {
    Get-ChildItem -LiteralPath $repo -Recurse -Force -File | ForEach-Object {
        $relative=[IO.Path]::GetRelativePath($repo,$_.FullName).Replace('\','/')
        if ($relative -match '(^|/)(\.git|dist|artifacts|bin|obj|TestResults|node_modules)(/|$)') {return}
        [void][IO.Compression.ZipFileExtensions]::CreateEntryFromFile($archive,$_.FullName,'DJ4GHUB/'+$relative,[IO.Compression.CompressionLevel]::Optimal)
        $count++
    }
} finally {$archive.Dispose();$stream.Dispose()}
$verify=[IO.Compression.ZipFile]::OpenRead($target)
try {
    if($verify.Entries.Count -ne $count){throw 'Entry count mismatch.'}
    foreach($entry in $verify.Entries) {
        $input=$entry.Open()
        try {$input.CopyTo([IO.Stream]::Null)} finally {$input.Dispose()}
    }
} finally {$verify.Dispose()}
$hash=(Get-FileHash -LiteralPath $target -Algorithm SHA256).Hash.ToLowerInvariant()
"$hash  $([IO.Path]::GetFileName($target))" | Set-Content -LiteralPath ($target+'.sha256') -Encoding utf8
[pscustomobject]@{Archive=$target;Entries=$count;SHA256=$hash;Bytes=(Get-Item -LiteralPath $target).Length} | ConvertTo-Json -Compress
