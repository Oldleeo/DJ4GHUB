param([switch]$SkipTests)
$ErrorActionPreference = 'Stop'
$repo = Split-Path -Parent $PSScriptRoot
Push-Location $repo
try {
    $env:CGO_ENABLED = '0'
    if (!$SkipTests) {
        & go test ./...
        if ($LASTEXITCODE) { throw 'Go tests failed.' }
    }
    New-Item -ItemType Directory -Path dist/windows -Force | Out-Null
    & go build -trimpath -ldflags '-s -w' -o dist/windows/dj4ghub-service.exe ./cmd/dj4ghub-macos
    if ($LASTEXITCODE) { throw 'Backend build failed.' }
    & dotnet publish apps/hub-windows/src/DJ4Hub.Windows/DJ4Hub.Windows.csproj -c Release -r win-x64 --self-contained true -p:Platform=x64 -p:DebugType=None -p:DebugSymbols=false -o dist/windows/portable
    if ($LASTEXITCODE) { throw 'Windows publish failed.' }
    if (!(Test-Path dist/windows/portable/backend/dj4ghub-service.exe)) { throw 'Backend missing from portable package.' }
    & "$PSScriptRoot/package-licenses.ps1"
    if (!$SkipTests) {
        & dotnet run --project apps/hub-windows/tests/SupervisionSmoke -c Release -- dist/windows/dj4ghub-service.exe
        if ($LASTEXITCODE) { throw 'Host supervision smoke failed.' }
        # Requires an interactive Windows desktop and WebView2 Runtime; run manually:
        # dotnet run --project apps/hub-windows/tests/DeviceSwitchSmoke -c Release -- dist/windows/dj4ghub-service.exe
    }
    Get-ChildItem dist/windows/portable -Recurse -File | ForEach-Object {
        $relative = $_.FullName.Substring((Resolve-Path dist/windows/portable).Path.Length + 1)
        '{0}  {1}' -f (Get-FileHash -LiteralPath $_.FullName -Algorithm SHA256).Hash.ToLowerInvariant(), $relative
    } | Set-Content -LiteralPath dist/windows/SHA256SUMS.txt -Encoding utf8
    Write-Host 'Portable package: dist/windows/portable. Start DJ4Hub.Windows.exe with all sibling files intact.'
} finally { Pop-Location }
