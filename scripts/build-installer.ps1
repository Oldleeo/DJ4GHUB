param([string]$CompilerPath)
$ErrorActionPreference = 'Stop'
$repo = Split-Path -Parent $PSScriptRoot
$portable = Join-Path $repo 'dist/windows/portable'
$releases = Join-Path $repo 'dist/releases'
$prerequisites = Join-Path $repo 'dist/windows/prerequisites'
foreach ($required in @('DJ4Hub.Windows.exe', 'backend/dj4ghub-service.exe', 'LICENSE.txt', 'licenses/COMPONENTS.txt')) {
    if (!(Test-Path -LiteralPath (Join-Path $portable $required))) { throw "Missing payload: $required. Run build-windows.ps1 first." }
}
New-Item -ItemType Directory -Path $releases, $prerequisites -Force | Out-Null
if (!$CompilerPath) {
    # Pinned upstream build tool, with the digest from its official GitHub release.
    $toolDir = Join-Path $repo '.tools/inno-7.1.0'
    $CompilerPath = Join-Path $toolDir 'ISCC.exe'
    if (!(Test-Path -LiteralPath $CompilerPath)) {
        $setup = Join-Path $prerequisites 'innosetup-7.1.0-x64.exe'
        Invoke-WebRequest 'https://github.com/jrsoftware/issrc/releases/download/is-7_1_0/innosetup-7.1.0-x64.exe' -OutFile $setup
        if ((Get-FileHash $setup -Algorithm SHA256).Hash -ne '0362a383ed217d4c4239b5933866dd96d3eb2102737da92f80f6057a4b40df2f') {
            throw 'Official Inno Setup digest mismatch.'
        }
        $process = Start-Process -FilePath $setup -ArgumentList @('/VERYSILENT', '/SUPPRESSMSGBOXES', '/NORESTART', '/SP-', '/CURRENTUSER', '/NOICONS', "/DIR=`"$toolDir`"") -WindowStyle Hidden -Wait -PassThru
        if ($process.ExitCode -ne 0 -or !(Test-Path -LiteralPath $CompilerPath)) { throw 'Inno Setup compiler installation failed.' }
    }
}
$bootstrapper = Join-Path $prerequisites 'MicrosoftEdgeWebview2Setup.exe'
Invoke-WebRequest 'https://go.microsoft.com/fwlink/p/?LinkId=2124703' -OutFile $bootstrapper
$signature = Get-AuthenticodeSignature -LiteralPath $bootstrapper
if ($signature.Status -ne 'Valid' -or $signature.SignerCertificate.Subject -notmatch 'O=Microsoft Corporation') {
    throw 'WebView2 bootstrapper must have a valid Microsoft signature.'
}
# Inno Setup source is UTF-8. A BOM makes encoding explicit on every builder.
$source = Join-Path $repo 'apps/hub-windows/installer/DJ4GHUB.iss'
$temporarySource = Join-Path $prerequisites 'DJ4GHUB.iss'
[IO.File]::WriteAllText($temporarySource, [IO.File]::ReadAllText($source), [Text.UTF8Encoding]::new($true))
[IO.File]::WriteAllText((Join-Path $prerequisites 'INSTALL-INFO.txt'), [IO.File]::ReadAllText((Join-Path $repo 'apps/hub-windows/installer/INSTALL-INFO.txt')), [Text.UTF8Encoding]::new($true))
& $CompilerPath "/DRepoRoot=$repo" $temporarySource
if ($LASTEXITCODE) { throw 'Installer compilation failed.' }
$portableZip = Join-Path $releases 'DJ4GHUB-Windows-x64-0.6.7-Portable.zip'
if (Test-Path -LiteralPath $portableZip) { throw 'Portable ZIP already exists; use a clean output directory.' }
Compress-Archive -Path (Join-Path $portable '*') -DestinationPath $portableZip -CompressionLevel Optimal
Get-ChildItem -LiteralPath $releases -File | Where-Object { $_.Extension -in '.exe', '.zip' } | ForEach-Object {
    '{0}  {1}' -f (Get-FileHash $_.FullName -Algorithm SHA256).Hash.ToLowerInvariant(), $_.Name
} | Set-Content -LiteralPath (Join-Path $releases 'SHA256SUMS.txt') -Encoding utf8
Write-Host 'Release packages: dist/releases. WebView2 dependency is Microsoft-signed; our application installer is not code-signed.'
