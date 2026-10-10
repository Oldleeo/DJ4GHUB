# This script installs/uninstalls ONLY on disposable GitHub-hosted Windows runners.
if ($env:GITHUB_ACTIONS -ne 'true' -or $env:RUNNER_ENVIRONMENT -ne 'github-hosted') {
    throw 'Installer smoke test is restricted to disposable GitHub-hosted runners.'
}
$ErrorActionPreference = 'Stop'
$repo = Split-Path -Parent $PSScriptRoot
$testRoot = Join-Path $env:RUNNER_TEMP ("dj4ghub-install-qa-" + [guid]::NewGuid().ToString('N'))
$app = Join-Path $testRoot 'app'
New-Item -ItemType Directory -Path $testRoot -Force | Out-Null
$installer = Join-Path $repo 'dist/releases/DJ4GHUB-Windows-x64-0.6.7-Setup.exe'
function Run-Setup([string]$File, [string[]]$Arguments) {
    $process = Start-Process -FilePath $File -ArgumentList $Arguments -WindowStyle Hidden -PassThru
    if (!$process.WaitForExit(240000)) {
        $process.Kill()
        throw 'Installer/uninstaller timeout.'
    }
    if ($process.ExitCode -ne 0) { throw "Setup failed with exit code $($process.ExitCode)." }
}
$backend = $null
try {
    Run-Setup $installer @('/VERYSILENT', '/SUPPRESSMSGBOXES', '/NORESTART', '/SP-', "/DIR=`"$app`"", "/LOG=`"$testRoot/install.log`"")
    foreach ($file in @('DJ4Hub.Windows.exe', 'backend/dj4ghub-service.exe', 'LICENSE.txt', 'licenses/COMPONENTS.txt', 'unins000.exe')) {
        if (!(Test-Path -LiteralPath (Join-Path $app $file))) { throw "Installed file missing: $file" }
    }
    $uninstallKey = 'HKCU:\Software\Microsoft\Windows\CurrentVersion\Uninstall\{8E8A13B6-437C-4F92-AD79-AF4B12978465}_is1'
    if (!(Test-Path -LiteralPath $uninstallKey)) { throw 'Per-user uninstall entry missing.' }
    $entry = Get-ItemProperty -LiteralPath $uninstallKey
    if ($entry.InstallLocation.TrimEnd('\') -ne $app) { throw 'Unexpected installation directory.' }
    if (!(Test-Path -LiteralPath (Join-Path ([Environment]::GetFolderPath('Desktop')) 'DJ4GHUB.lnk'))) { throw 'Desktop shortcut missing.' }
    # Retain user records during an in-place upgrade and uninstall.
    $userData = Join-Path $env:LOCALAPPDATA 'DJ4Hub'
    New-Item -ItemType Directory -Path $userData -Force | Out-Null
    $sentinel = Join-Path $userData 'installer-smoke-sentinel.txt'
    Set-Content -LiteralPath $sentinel -Value 'retain-user-records' -Encoding utf8
    Run-Setup $installer @('/VERYSILENT', '/SUPPRESSMSGBOXES', '/NORESTART', '/SP-', "/DIR=`"$app`"")
    if (!(Test-Path -LiteralPath $sentinel)) { throw 'Upgrade erased user records.' }
    $listener = [Net.Sockets.TcpListener]::new([Net.IPAddress]::Loopback, 0)
    $listener.Start()
    $port = $listener.LocalEndpoint.Port
    $listener.Stop()
    $backend = Start-Process -FilePath (Join-Path $app 'backend/dj4ghub-service.exe') -ArgumentList @('-demo', '-read-only', '-listen', "127.0.0.1:$port", '-data-dir', "`"$testRoot/demo-data`"") -WindowStyle Hidden -PassThru
    $url = "http://127.0.0.1:$port"
    $ready = $false
    for ($i = 0; $i -lt 100; $i++) {
        try { $null = Invoke-RestMethod "$url/api/health" -TimeoutSec 2; $ready = $true; break } catch { Start-Sleep -Milliseconds 200 }
    }
    if (!$ready) { throw 'Installed demo backend did not become healthy.' }
    $remote = Invoke-RestMethod "$url/api/remote"
    if ($remote.relay_url -or $remote.host_enabled -or $remote.owner_configured -or $remote.host_registered) {
        throw 'Public installation unexpectedly has a configured cloud account.'
    }
    $page = Invoke-WebRequest "$url/" -TimeoutSec 10
    if ($page.StatusCode -ne 200) { throw 'Installed UI assets unavailable.' }
    $backend.Kill()
    $backend.WaitForExit()
    $backend = $null
    Run-Setup (Join-Path $app 'unins000.exe') @('/VERYSILENT', '/SUPPRESSMSGBOXES', '/NORESTART', "/LOG=`"$testRoot/uninstall.log`"")
    if (Test-Path -LiteralPath (Join-Path $app 'DJ4Hub.Windows.exe')) { throw 'Uninstall left application executable.' }
    if (Test-Path -LiteralPath $uninstallKey) { throw 'Uninstall entry was not removed.' }
    if ((Get-Content -LiteralPath $sentinel -Raw).Trim() -ne 'retain-user-records') { throw 'Uninstall did not preserve user records.' }
    Write-Host 'Installer smoke passed: install, shortcut, upgrade, demo backend, cloud defaults, uninstall, retained user data.'
} finally {
    if ($backend -and !$backend.HasExited) { $backend.Kill(); $backend.WaitForExit() }
}
