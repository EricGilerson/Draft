param(
    [Parameter(Mandatory = $true)]
    [string]$Path
)

$required = @(
    'AZURE_TENANT_ID',
    'AZURE_CLIENT_ID',
    'AZURE_CLIENT_SECRET',
    'AZURE_CODE_SIGNING_ENDPOINT',
    'AZURE_CODE_SIGNING_ACCOUNT',
    'AZURE_CERTIFICATE_PROFILE'
)

foreach ($name in $required) {
    if ([string]::IsNullOrWhiteSpace([Environment]::GetEnvironmentVariable($name))) {
        throw "Missing required signing environment variable: $name"
    }
}

if (-not (Test-Path -LiteralPath $Path -PathType Leaf)) {
    throw "NSIS uninstaller was not found: $Path"
}

Import-Module TrustedSigning -RequiredVersion 0.5.0 -ErrorAction Stop

$signingArgs = @{
    Endpoint                = $env:AZURE_CODE_SIGNING_ENDPOINT
    CodeSigningAccountName  = $env:AZURE_CODE_SIGNING_ACCOUNT
    CertificateProfileName  = $env:AZURE_CERTIFICATE_PROFILE
    Files                   = $Path
    FileDigest              = 'SHA256'
    TimestampRfc3161        = 'http://timestamp.acs.microsoft.com'
    TimestampDigest         = 'SHA256'
}
Invoke-TrustedSigning @signingArgs
