[CmdletBinding()]
param(
    [ValidateSet("up", "up-ai", "down", "restart", "logs", "ps", "config", "reset")]
    [string]$Action = "up",
    [switch]$Yes
)

$ErrorActionPreference = "Stop"
$RootDir = Split-Path -Parent $PSScriptRoot
$EnvFile = Join-Path $RootDir ".env.local"
$EnvTemplate = Join-Path $RootDir ".env.local.example"
$BaseCompose = Join-Path $RootDir "docker-compose.local.yml"
$AiCompose = Join-Path $RootDir "docker-compose.local-ai.yml"

function Invoke-Docker {
    param([string[]]$Arguments)

    & docker @Arguments
    if ($LASTEXITCODE -ne 0) {
        throw "docker exited with code $LASTEXITCODE"
    }
}

function New-HexSecret {
    param([int]$ByteCount)

    $bytes = [byte[]]::new($ByteCount)
    $generator = [System.Security.Cryptography.RandomNumberGenerator]::Create()
    try {
        $generator.GetBytes($bytes)
    } finally {
        $generator.Dispose()
    }
    return -join ($bytes | ForEach-Object { $_.ToString("x2") })
}

function New-Base64Secret {
    $bytes = [byte[]]::new(32)
    $generator = [System.Security.Cryptography.RandomNumberGenerator]::Create()
    try {
        $generator.GetBytes($bytes)
    } finally {
        $generator.Dispose()
    }
    return [Convert]::ToBase64String($bytes)
}

function Set-EnvValue {
    param(
        [string]$Key,
        [string]$Value
    )

    $content = [IO.File]::ReadAllText($EnvFile)
    $pattern = "(?m)^$([Regex]::Escape($Key))=.*$"
    if ([Regex]::IsMatch($content, $pattern)) {
        $content = [Regex]::Replace($content, $pattern, "$Key=$Value")
    } else {
        $content = $content.TrimEnd() + [Environment]::NewLine + "$Key=$Value" + [Environment]::NewLine
    }
    [IO.File]::WriteAllText($EnvFile, $content, [Text.UTF8Encoding]::new($false))
}

function Initialize-LocalEnv {
    $created = $false
    if (-not (Test-Path -LiteralPath $EnvFile)) {
        Copy-Item -LiteralPath $EnvTemplate -Destination $EnvFile
        $created = $true
    }

    $content = [IO.File]::ReadAllText($EnvFile)
    if ($content -match "(?m)^POSTGRES_PASSWORD=replace-with-") {
        Set-EnvValue -Key "POSTGRES_PASSWORD" -Value (New-HexSecret -ByteCount 24)
        $created = $true
    }
    if ($content -match "(?m)^JWT_SECRET=replace-with-") {
        Set-EnvValue -Key "JWT_SECRET" -Value (New-HexSecret -ByteCount 32)
        $created = $true
    }
    if ($content -match "(?m)^MULTICA_VCS_SECRET_KEY=replace-with-") {
        Set-EnvValue -Key "MULTICA_VCS_SECRET_KEY" -Value (New-Base64Secret)
        $created = $true
    }
    if ($created) {
        Write-Host "Initialized .env.local with random local secrets."
    }
}

if (-not (Get-Command docker -ErrorAction SilentlyContinue)) {
    throw "docker is required."
}
Invoke-Docker -Arguments @("compose", "version")
Initialize-LocalEnv
Set-Location $RootDir

$baseArgs = @("compose", "--env-file", $EnvFile, "-f", $BaseCompose)
$allArgs = @("compose", "--env-file", $EnvFile, "-f", $BaseCompose, "-f", $AiCompose)

switch ($Action) {
    "up" {
        Invoke-Docker -Arguments ($baseArgs + @("up", "-d", "--build", "--wait"))
        $settings = Get-Content -Raw $EnvFile | ConvertFrom-StringData
        Write-Host "Multica: http://localhost:$($settings.FRONTEND_PORT)"
        Write-Host "Mailpit: http://localhost:$($settings.MAILPIT_UI_PORT)"
    }
    "up-ai" {
        Invoke-Docker -Arguments ($allArgs + @("up", "-d", "--build", "--wait"))
        $settings = Get-Content -Raw $EnvFile | ConvertFrom-StringData
        Write-Host "Multica and Ollama are ready."
        Write-Host "Multica: http://localhost:$($settings.FRONTEND_PORT)"
        Write-Host "Mailpit: http://localhost:$($settings.MAILPIT_UI_PORT)"
    }
    "down" {
        Invoke-Docker -Arguments ($allArgs + @("down", "--remove-orphans"))
    }
    "restart" {
        Invoke-Docker -Arguments ($baseArgs + @("up", "-d", "--build", "--force-recreate", "--wait"))
    }
    "logs" {
        Invoke-Docker -Arguments ($allArgs + @("logs", "-f", "--tail=200"))
    }
    "ps" {
        Invoke-Docker -Arguments ($allArgs + @("ps"))
    }
    "config" {
        Invoke-Docker -Arguments ($allArgs + @("config"))
    }
    "reset" {
        if (-not $Yes) {
            throw "Pass -Yes to delete all local data volumes."
        }
        Invoke-Docker -Arguments ($allArgs + @("down", "--volumes", "--remove-orphans"))
    }
}
