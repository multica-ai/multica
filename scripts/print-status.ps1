# Harness Tier 1 — project status
$ErrorActionPreference = "Stop"
$root = Split-Path $PSScriptRoot -Parent
$sessionPath = Join-Path $root "SESSION.md"

Write-Host "=== multica status ===" -ForegroundColor Cyan
Write-Host ""

if (Test-Path $sessionPath) {
    Write-Host "[SESSION]" -ForegroundColor Green
    Get-Content $sessionPath -Encoding UTF8 | Select-Object -First 40
    Write-Host ""
} else {
    Write-Host "[SESSION] missing: $sessionPath" -ForegroundColor Yellow
    Write-Host ""
}

Write-Host "[Git]" -ForegroundColor Green
Push-Location $root
try {
    $branch = git rev-parse --abbrev-ref HEAD 2>$null
    $head = git rev-parse --short HEAD 2>$null
    Write-Host "  branch: $branch @ $head"
} finally {
    Pop-Location
}

Write-Host ""
Write-Host "Detail: SESSION.md · Vault projects/multica.md"
