[CmdletBinding()]
param(
    [string]$Version = "0.4.23"
)

$ErrorActionPreference = "Stop"
$repoRoot = Split-Path (Split-Path $PSCommandPath -Parent) -Parent
$dockerCommand = (Get-Command docker -ErrorAction SilentlyContinue).Source
if (-not $dockerCommand) {
    $dockerPath = "C:\Program Files\Docker\Docker\resources\bin\docker.exe"
    if (Test-Path $dockerPath) {
        $dockerCommand = $dockerPath
    } else {
        throw "Docker is required to build all connector targets."
    }
}

& $dockerCommand run --rm `
    -v "${repoRoot}:/src" `
    -w /src `
    golang:1.26.1-bookworm `
    bash -c "apt-get update -qq && apt-get install -y -qq zip >/dev/null && ./scripts/build-connectors.sh '$Version'"

if ($LASTEXITCODE -ne 0) {
    throw "Connector build failed with exit code $LASTEXITCODE."
}

Write-Host "Connector release assets: $repoRoot\dist\connectors\v$Version"
