param(
    [Parameter(Mandatory = $true)]
    [string]$Path
)

Set-StrictMode -Version Latest
$ErrorActionPreference = 'Stop'

function Get-HostSignToolArch {
    $arch = [System.Runtime.InteropServices.RuntimeInformation]::OSArchitecture
    switch ($arch) {
        'X64' { return 'x64' }
        'Arm64' { return 'arm64' }
        'X86' { return 'x86' }
        default {
            switch -Regex ($env:PROCESSOR_ARCHITECTURE) {
                '^(AMD64|X64)$' { return 'x64' }
                '^ARM64$' { return 'arm64' }
                '^(x86|X86)$' { return 'x86' }
                default { return 'x64' }
            }
        }
    }
}

function Test-SignToolArchMatch {
    param(
        [Parameter(Mandatory = $true)]
        [string]$SignToolPath,
        [Parameter(Mandatory = $true)]
        [string[]]$AcceptedArch
    )

    foreach ($arch in $AcceptedArch) {
        if ($SignToolPath -match "[\\/]$([regex]::Escape($arch))[\\/]signtool\.exe$") {
            return $true
        }
    }
    # PATH-installed copies often have no arch folder; accept only if native start looks possible later.
    return $SignToolPath -match '[\\/]signtool\.exe$' -and ($SignToolPath -notmatch '[\\/](arm64|x64|x86)[\\/]signtool\.exe$')
}

function Find-SignTool {
    $hostArch = Get-HostSignToolArch
    # Prefer the host arch; x86 is a common fallback on x64 Windows.
    $archPreference = [System.Collections.Generic.List[string]]::new()
    [void]$archPreference.Add($hostArch)
    if ($hostArch -eq 'x64') {
        [void]$archPreference.Add('x86')
    } elseif ($hostArch -eq 'arm64') {
        [void]$archPreference.Add('x64')
        [void]$archPreference.Add('x86')
    }

    $searchRoots = @(
        (Join-Path $env:LOCALAPPDATA 'TrustedSigning\Microsoft.Windows.SDK.BuildTools'),
        (Join-Path ${env:ProgramFiles(x86)} 'Windows Kits\10\bin'),
        (Join-Path $env:ProgramFiles 'Windows Kits\10\bin')
    ) | Where-Object { $_ -and (Test-Path -LiteralPath $_) }

    $candidates = @(foreach ($root in $searchRoots) {
            Get-ChildItem -Path $root -Filter signtool.exe -File -Recurse -ErrorAction SilentlyContinue
        })

    if ($candidates.Count -eq 0) {
        return $null
    }

    foreach ($arch in $archPreference) {
        $match = $candidates |
            Where-Object { $_.FullName -match "[\\/]$([regex]::Escape($arch))[\\/]signtool\.exe$" } |
            Sort-Object FullName -Descending |
            Select-Object -First 1
        if ($match) {
            return $match.FullName
        }
    }

    return $null
}

if (-not (Test-Path -LiteralPath $Path -PathType Leaf)) {
    throw "Signed file was not found: $Path"
}

$signature = Get-AuthenticodeSignature -FilePath $Path
if ($signature.Status -ne 'Valid') {
    throw "Authenticode is not valid for ${Path}: $($signature.Status) $($signature.StatusMessage)"
}

$hostArch = Get-HostSignToolArch
$acceptedArch = @($hostArch)
if ($hostArch -eq 'x64') { $acceptedArch += 'x86' }
elseif ($hostArch -eq 'arm64') { $acceptedArch += @('x64', 'x86') }

$signtoolPath = $null
$fromPath = Get-Command signtool.exe -ErrorAction SilentlyContinue
if ($fromPath -and (Test-SignToolArchMatch -SignToolPath $fromPath.Path -AcceptedArch $acceptedArch)) {
    $signtoolPath = $fromPath.Path
}
if (-not $signtoolPath) {
    $signtoolPath = Find-SignTool
}

if (-not $signtoolPath) {
    throw 'signtool.exe is unavailable for this OS architecture; cannot verify the timestamped signature.'
}

Write-Host "Using signtool: $signtoolPath"
& $signtoolPath verify /pa /tw $Path
if ($LASTEXITCODE -ne 0) {
    throw "signtool verification failed for ${Path} with exit code $LASTEXITCODE"
}
