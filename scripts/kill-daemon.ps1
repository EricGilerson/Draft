<#
.SYNOPSIS
  Stop a running Draft daemon using its statefile.

.DESCRIPTION
  The Draft daemon records its address, auth token, and PID in a JSON statefile
  (see internal/daemon/paths.go, internal/daemon/server.go):

    { "addr": "127.0.0.1:xxxxx", "token": "...", "pid": 12345 }

  This script reads that statefile, sends a graceful stop to the recorded PID,
  waits briefly, escalates to a force kill if needed, then verifies the process
  is actually gone. Exits non-zero if the kill could not be confirmed.

.PARAMETER Force
  Skip the live /health check and force-kill without confirming the PID is a
  Draft daemon.

.PARAMETER Keep
  Do not remove the stale statefile after the daemon stops.

.EXAMPLE
  .\scripts\kill-daemon.ps1

.EXAMPLE
  .\scripts\kill-daemon.ps1 -Force

.EXAMPLE
  .\scripts\kill-daemon.ps1 -Keep
#>

[CmdletBinding()]
param(
    [Alias('f')]
    [switch] $Force,

    [switch] $Keep
)

Set-StrictMode -Version Latest
$ErrorActionPreference = 'Stop'

function Write-Info([string] $Message) {
    Write-Host $Message
}

function Write-Warn([string] $Message) {
    Write-Host $Message -ForegroundColor Yellow
}

function Write-Err([string] $Message) {
    Write-Host $Message -ForegroundColor Red
}

function Get-DraftStateFilePath {
    $configRoot = [Environment]::GetFolderPath('ApplicationData')
    if ([string]::IsNullOrWhiteSpace($configRoot)) {
        throw 'could not resolve ApplicationData config directory'
    }
    return Join-Path (Join-Path $configRoot 'Draft') 'daemon.json'
}

function Test-ProcessAlive([int] $ProcessId) {
    return $null -ne (Get-Process -Id $ProcessId -ErrorAction SilentlyContinue)
}

function Wait-UntilProcessDead {
    param(
        [int] $ProcessId,
        [int] $TimeoutSeconds
    )

    $deadline = (Get-Date).AddSeconds($TimeoutSeconds)
    while ((Get-Date) -lt $deadline) {
        if (-not (Test-ProcessAlive $ProcessId)) {
            return $true
        }
        Start-Sleep -Milliseconds 250
    }
    return -not (Test-ProcessAlive $ProcessId)
}

$stateFile = Get-DraftStateFilePath

if (-not (Test-Path -LiteralPath $stateFile)) {
    Write-Info "no daemon statefile at: $stateFile"
    Write-Info 'nothing to kill.'
    exit 0
}

try {
    $state = Get-Content -LiteralPath $stateFile -Raw | ConvertFrom-Json
} catch {
    Write-Err "failed to parse statefile: $stateFile"
    Write-Err $_.Exception.Message
    exit 1
}

$pidValue = $state.pid
if ($null -eq $pidValue -or [string]::IsNullOrWhiteSpace("$pidValue")) {
    Write-Info 'statefile exists but contains no pid:'
    Write-Info "  $stateFile"
    if (-not $Keep) {
        Remove-Item -LiteralPath $stateFile -Force
        Write-Info 'removed stale statefile.'
    }
    exit 0
}

try {
    $daemonPid = [int] $pidValue
} catch {
    Write-Err "statefile pid is not a positive integer: '$pidValue'"
    exit 1
}

if ($daemonPid -le 0) {
    Write-Err "statefile pid is not a positive integer: '$pidValue'"
    exit 1
}

Write-Info "draft daemon statefile: $stateFile"
Write-Info "recorded pid:           $daemonPid"

if (-not (Test-ProcessAlive $daemonPid)) {
    Write-Info "pid $daemonPid is not running; daemon already stopped."
    if (-not $Keep) {
        Remove-Item -LiteralPath $stateFile -Force
        Write-Info 'removed stale statefile.'
    }
    exit 0
}

if (-not $Force) {
    $addr = [string] $state.addr
    if (-not [string]::IsNullOrWhiteSpace($addr)) {
        try {
            $null = Invoke-WebRequest -Uri "http://$addr/health" -Method Get -TimeoutSec 2 -UseBasicParsing
            Write-Info "health check ok at $addr - confirmed Draft daemon."
        } catch {
            Write-Warn "warning: pid $daemonPid is alive but did not answer /health at $addr."
            Write-Warn '         rerun with -Force to force-kill without the health check.'
            exit 1
        }
    }
}

Write-Info "sending graceful stop to pid $daemonPid ..."
try {
    Stop-Process -Id $daemonPid -ErrorAction Stop
} catch {
    # Process may already be exiting; keep going to verification.
}

if (-not (Wait-UntilProcessDead -ProcessId $daemonPid -TimeoutSeconds 8)) {
    Write-Info "graceful stop did not finish within 8s; escalating to force kill."
    try {
        Stop-Process -Id $daemonPid -Force -ErrorAction Stop
    } catch {
        if (Test-ProcessAlive $daemonPid) {
            Write-Err $_.Exception.Message
        }
    }
    $null = Wait-UntilProcessDead -ProcessId $daemonPid -TimeoutSeconds 4
}

if (Test-ProcessAlive $daemonPid) {
    Write-Err "FAILED: pid $daemonPid is still running after graceful stop + force kill."
    exit 1
}

Write-Info "confirmed: pid $daemonPid is no longer running."

if (-not $Keep) {
    Remove-Item -LiteralPath $stateFile -Force
    Write-Info "removed stale statefile: $stateFile"
} else {
    Write-Info "kept statefile (-Keep): $stateFile"
}

Write-Info 'done.'
