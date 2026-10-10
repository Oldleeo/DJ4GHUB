$ErrorActionPreference = 'Stop'
$repo = Split-Path -Parent $PSScriptRoot
$output = Join-Path $repo 'dist/windows/portable/licenses'
New-Item -ItemType Directory -Path $output -Force | Out-Null
$manifest = [System.Collections.Generic.List[string]]::new()
function Copy-Notices([string]$Directory, [string]$Component, [switch]$Required) {
    $files = @(Get-ChildItem -LiteralPath $Directory -File | Where-Object {
        $_.Name -match '^(LICENSE|LICENCE|COPYING|NOTICE|ThirdPartyNotices|THIRD.PARTY.NOTICES)($|[.\-_])'
    })
    if ($Required -and !$files.Count) { throw "No license found for $Component" }
    if ($files.Count) {
        $target = Join-Path $output ($Component -replace '[^a-zA-Z0-9_.-]', '_')
        New-Item -ItemType Directory -Path $target -Force | Out-Null
        $files | Copy-Item -Destination $target
    }
}
Push-Location $repo
try {
    # Enumerate modules actually linked into the shipped backend, not unused test dependencies.
    $modules = & go list -deps -f '{{if .Module}}{{.Module.Path}}|{{.Module.Version}}|{{if .Module.Replace}}{{.Module.Replace.Dir}}{{else}}{{.Module.Dir}}{{end}}{{end}}' ./cmd/dj4ghub-macos
    if ($LASTEXITCODE) { throw 'Cannot enumerate Go licenses.' }
    foreach ($line in ($modules | Where-Object { $_ } | Sort-Object -Unique)) {
        $fields = $line.Split('|', 3)
        if (!$fields[1]) { continue } # The main project license is already included.
        Copy-Notices $fields[2] $fields[0] -Required
        $manifest.Add("Go module: $($fields[0]) $($fields[1])")
    }
    Copy-Notices (& go env GOROOT) 'Go-runtime' -Required
    foreach ($dir in Get-ChildItem -LiteralPath (Join-Path $repo 'third_party') -Directory) {
        Copy-Notices $dir.FullName "asset-$($dir.Name)"
    }
    $assets = Get-Content 'apps/hub-windows/src/DJ4Hub.Windows/obj/project.assets.json' -Raw | ConvertFrom-Json
    foreach ($library in $assets.libraries.PSObject.Properties) {
        if ($library.Value.type -ne 'package') { continue }
        foreach ($folder in $assets.packageFolders.PSObject.Properties.Name) {
            $directory = Join-Path $folder $library.Value.path
            if (Test-Path -LiteralPath $directory) {
                Copy-Notices $directory "NuGet-$($library.Name)"
                $manifest.Add("NuGet package: $($library.Name)")
                break
            }
        }
    }
    $dotnetRoot = Split-Path -Parent (Get-Command dotnet).Source
    Copy-Notices $dotnetRoot '.NET-runtime' -Required
    $manifest.Add('License sources: original module/package distributions and .NET installation.')
    $manifest | Set-Content -LiteralPath (Join-Path $output 'COMPONENTS.txt') -Encoding utf8
    Write-Host "Packaged dependency notices for $($manifest.Count - 1) components."
} finally { Pop-Location }
