param(
    [Parameter(Mandatory = $true)]
    [string]$Path
)

if (-not (Test-Path -LiteralPath $Path -PathType Leaf)) {
    throw "Signed file was not found: $Path"
}

$signature = Get-AuthenticodeSignature -FilePath $Path
if ($signature.Status -ne 'Valid') {
    throw "Authenticode is not valid for ${Path}: $($signature.Status) $($signature.StatusMessage)"
}

$signtool = Get-Command signtool.exe -ErrorAction SilentlyContinue
if (-not $signtool) {
    $searchRoot = Join-Path $env:LOCALAPPDATA 'TrustedSigning\Microsoft.Windows.SDK.BuildTools'
    $signtool = Get-ChildItem -Path $searchRoot -Filter signtool.exe -File -Recurse -ErrorAction SilentlyContinue |
        Select-Object -First 1
}
if (-not $signtool) {
    throw 'signtool.exe is unavailable; cannot verify the timestamped signature.'
}

$signtoolPath = if ($signtool -is [System.IO.FileInfo]) { $signtool.FullName } else { $signtool.Path }
& $signtoolPath verify /pa /tw $Path
if ($LASTEXITCODE -ne 0) {
    throw "signtool verification failed for ${Path} with exit code $LASTEXITCODE"
}
