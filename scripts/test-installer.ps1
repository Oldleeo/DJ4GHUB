# This script installs/uninstalls ONLY on disposable GitHub-hosted Windows runners.
if ($env:GITHUB_ACTIONS -ne 'true' -or $env:RUNNER_ENVIRONMENT -ne 'github-hosted') {
    throw 'Installer smoke test is restricted to disposable GitHub-hosted runners.'
}
$ErrorActionPreference = 'Stop'
$repo = Split-Path -Parent $PSScriptRoot
$testRoot = Join-Path $env:RUNNER_TEMP ("dj4ghub-install-qa-" + [guid]::NewGuid().ToString('N'))
$logRoot = Join-Path $repo 'artifacts/installer-smoke'
New-Item -ItemType Directory -Path $logRoot -Force | Out-Null
$app = Join-Path $testRoot 'app'
New-Item -ItemType Directory -Path $testRoot -Force | Out-Null
$installer = Join-Path $repo 'dist/releases/DJ4GHUB-Windows-x64-0.6.7-Setup.exe'
function Run-Setup([string]$File, [string[]]$Arguments) {
    Write-Host "Running setup: $([IO.Path]::GetFileName($File))"
    # Inno launchers can spawn a temporary second-stage executable. Wait for the tree.
    $log = Join-Path $logRoot ([guid]::NewGuid().ToString('N') + '.log')
    $process = Start-Process -FilePath $File -ArgumentList ($Arguments + "/LOG=`"$log`"") -WindowStyle Hidden -Wait -PassThru
    if ($process.ExitCode -ne 0) { throw "Setup failed with exit code $($process.ExitCode)." }
}
function Assert-RunningInstanceBlocked([string]$File) {
    $mutex = [Threading.Mutex]::new($true, 'Local\DJ4Hub.Windows')
    try {
        Write-Host "Checking running-instance guard: $([IO.Path]::GetFileName($File))"
        $log = Join-Path $logRoot ('blocked-' + [guid]::NewGuid().ToString('N') + '.log')
        $process = Start-Process -FilePath $File -ArgumentList @('/VERYSILENT', '/SUPPRESSMSGBOXES', '/NORESTART', '/SP-', "/DIR=`"$app`"", "/LOG=`"$log`"") -WindowStyle Hidden -Wait -PassThru
        if ($process.ExitCode -eq 0) { throw 'Installer/uninstaller did not block a running instance.' }
    } finally { $mutex.ReleaseMutex(); $mutex.Dispose() }
}
$backend = $null
try {
    Assert-RunningInstanceBlocked $installer
    if (Test-Path -LiteralPath (Join-Path $app 'DJ4Hub.Windows.exe')) { throw 'Blocked installation wrote application files.' }
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
    Assert-RunningInstanceBlocked (Join-Path $app 'unins000.exe')
    if (!(Test-Path -LiteralPath (Join-Path $app 'DJ4Hub.Windows.exe'))) { throw 'Blocked uninstall removed application files.' }
    $startupKey = 'HKCU:\Software\Microsoft\Windows\CurrentVersion\Run'
    New-Item -Path $startupKey -Force | Out-Null
    $foreignStartup = '"C:\OtherInstallation\DJ4Hub.Windows.exe" --minimized'
    New-ItemProperty -LiteralPath $startupKey -Name 'DJ4GHUB' -Value $foreignStartup -PropertyType String -Force | Out-Null
    Run-Setup (Join-Path $app 'unins000.exe') @('/VERYSILENT', '/SUPPRESSMSGBOXES', '/NORESTART', "/LOG=`"$testRoot/uninstall.log`"")
    if (Test-Path -LiteralPath (Join-Path $app 'DJ4Hub.Windows.exe')) { throw 'Uninstall left application executable.' }
    if (Test-Path -LiteralPath $uninstallKey) { throw 'Uninstall entry was not removed.' }
    if ((Get-Content -LiteralPath $sentinel -Raw).Trim() -ne 'retain-user-records') { throw 'Uninstall did not preserve user records.' }
    if ((Get-ItemPropertyValue -LiteralPath $startupKey -Name 'DJ4GHUB') -ne $foreignStartup) { throw 'Uninstaller changed another installation auto-start.' }
    Run-Setup $installer @('/VERYSILENT', '/SUPPRESSMSGBOXES', '/NORESTART', '/SP-', "/DIR=`"$app`"")
    New-ItemProperty -LiteralPath $startupKey -Name 'DJ4GHUB' -Value "`"$app\DJ4Hub.Windows.exe`" --minimized" -PropertyType String -Force | Out-Null
    Run-Setup (Join-Path $app 'unins000.exe') @('/VERYSILENT', '/SUPPRESSMSGBOXES', '/NORESTART')
    if ((Get-ItemProperty -LiteralPath $startupKey).PSObject.Properties.Name -contains 'DJ4GHUB') { throw 'Own auto-start was not removed.' }
    Write-Host 'Installer smoke passed: running-instance guards, install, shortcut, upgrade, demo backend, cloud defaults, uninstall, retained user data, scoped auto-start cleanup.'
} finally {
    if ($backend -and !$backend.HasExited) { $backend.Kill(); $backend.WaitForExit() }
}
