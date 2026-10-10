[CmdletBinding()]
param()
$ErrorActionPreference = 'Stop'
$repo = Split-Path -Parent $PSScriptRoot
$findings = [Collections.Generic.List[string]]::new()
$files = Get-ChildItem -LiteralPath $repo -Recurse -File -Force | Where-Object {
    [IO.Path]::GetRelativePath($repo,$_.FullName) -notmatch '(^|[\\/])(\.git|dist|artifacts|bin|obj|node_modules|TestResults)([\\/]|$)'
}
$patterns = @{
    'private-service' = 'https?://(?:[a-z0-9-]+\.)?oldlee\.art|evoxt[-_]'
    'private-user-path' = '[A-Za-z]:[\\/]Users[\\/](Administrator|Desktop)[\\/]'
    'private-key' = '-----BEGIN (RSA |EC |OPENSSH )?PRIVATE KEY-----'
    'literal-bark-key' = 'https://api\.day\.app/[A-Za-z0-9_-]{16,}'
    'literal-telegram-token' = '(?<![0-9])[0-9]{7,12}:[A-Za-z0-9_-]{30,}'
}
$unsafeNames = @('remote-relay.json','notification-config.json','multi-device.json','owner.token','panel.password','panel.totp','master.key','state.enc')
foreach ($file in $files) {
    $relative = [IO.Path]::GetRelativePath($repo,$file.FullName).Replace('\','/')
    if ($file.Name -in $unsafeNames -or $file.Extension -in @('.db','.log','.sqlite','.pfx','.p12','.exe','.dll','.zip')) {
        $findings.Add("${relative}: forbidden runtime/private file")
    }
    if ($file.Extension -eq '.pem' -and $relative -notin @('third_party/euicc-go/http/rootci/bundle.pem','third_party/euicc-go/http/rootci/bundle-tests.pem')) {
        $findings.Add("${relative}: unapproved certificate file")
    }
    if ($relative -match '(^|/)(\.cache|\.tools|secrets|backups)(/|$)' -or $file.Name -eq '.env') {
        $findings.Add("${relative}: excluded private directory/config")
    }
    if ($file.Extension -in @('.png','.jpg','.jpeg','.gif','.ico','.woff','.woff2','.ttf')) { continue }
    $content = [IO.File]::ReadAllText($file.FullName)
    foreach ($category in $patterns.Keys) {
        # Audit implementation contains detection expressions, not credentials.
        if ($relative -eq 'scripts/audit-public.ps1') { continue }
        if ([regex]::IsMatch($content,$patterns[$category])) { $findings.Add("${relative}: ${category}") }
    }
}
# Embedded public trust certificates are required upstream assets, not private keys.
foreach ($bundle in @('bundle.pem','bundle-tests.pem')) {
    if (!(Test-Path -LiteralPath (Join-Path $repo "third_party/euicc-go/http/rootci/$bundle"))) {
        $findings.Add("missing embedded public trust bundle: $bundle")
    }
}
if ($findings.Count) {
    $findings | ForEach-Object { Write-Host $_ }
    throw "Public source audit failed: $($findings.Count) findings. Matched values are deliberately not printed."
}
Write-Host "Public source audit passed: $($files.Count) files. Manual review and future history scanning still required."
