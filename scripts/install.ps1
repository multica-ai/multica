# Multica installer for Windows — one command to get started.
#
# Install CLI (default): connects to multica.ai
#   irm https://raw.githubusercontent.com/multica-ai/multica/main/scripts/install.ps1 | iex
#
# Self-host: starts a local Multica server + installs CLI + configures
#   $env:MULTICA_MODE="local"; irm https://raw.githubusercontent.com/multica-ai/multica/main/scripts/install.ps1 | iex
#

$ErrorActionPreference = "Stop"

# ---------------------------------------------------------------------------
# Configuration
# ---------------------------------------------------------------------------
$RepoUrl       = "https://github.com/multica-ai/multica.git"
$RepoWebUrl    = "https://github.com/multica-ai/multica"
$DefaultInstallDir = Join-Path $env:USERPROFILE ".multica\server"
$InstallDir    = if ($env:MULTICA_INSTALL_DIR) { $env:MULTICA_INSTALL_DIR } else { $DefaultInstallDir }

# ---------------------------------------------------------------------------
# Helpers
# ---------------------------------------------------------------------------
function Write-Info  { param([string]$Msg) Write-Host "==> $Msg" -ForegroundColor Cyan }
function Write-Ok    { param([string]$Msg) Write-Host "[OK] $Msg" -ForegroundColor Green }
function Write-Warn  { param([string]$Msg) Write-Warning $Msg }
function Write-Fail  { param([string]$Msg) Write-Host "[ERROR] $Msg" -ForegroundColor Red; exit 1 }

function Test-CommandExists {
    param([string]$Name)
    $null -ne (Get-Command $Name -ErrorAction SilentlyContinue)
}

function Get-LatestVersion {
    try {
        $release = Invoke-RestMethod -Uri "https://api.github.com/repos/multica-ai/multica/releases/latest" -ErrorAction Stop
        return $release.tag_name
    } catch {
        return $null
    }
}

function Get-SelfHostRef {
    if ($env:MULTICA_SELFHOST_REF) {
        return $env:MULTICA_SELFHOST_REF
    }

    $latest = Get-LatestVersion
    if ($latest) {
        return $latest
    }

    return "main"
}

function Checkout-ServerRef {
    param([string]$Ref)

    if ($Ref -eq "main") {
        git fetch origin main --depth 1 2>$null
        git checkout --force main 2>$null
        git reset --hard origin/main 2>$null
        return
    }

    git fetch origin --tags --force 2>$null
    $tagRef = "refs/tags/$Ref"
    git show-ref --verify --quiet $tagRef 2>$null
    if ($LASTEXITCODE -eq 0) {
        git checkout --force $Ref 2>$null
        return
    }

    git fetch origin $Ref --depth 1 2>$null
    git checkout --force $Ref 2>$null
}

function Pull-OfficialSelfHostImages {
    docker compose -f docker-compose.selfhost.yml pull
    if ($LASTEXITCODE -eq 0) {
        return
    }

    Write-Host ""
    Write-Warn "Official images for the selected self-host channel are not published yet."
    Write-Host "This can happen before the first GHCR release is available."
    Write-Host "From $InstallDir, build from source instead:"
    Write-Host "  docker compose -f docker-compose.selfhost.yml -f docker-compose.selfhost.build.yml up -d --build"
    exit 1
}

function Convert-ToCliArch {
    param([object]$Value)

    if ($null -eq $Value) {
        return $null
    }

    $normalized = "$Value".Trim().ToUpperInvariant()
    switch ($normalized) {
        "9"      { return "amd64" }
        "AMD64"  { return "amd64" }
        "X64"    { return "amd64" }
        "X86_64" { return "amd64" }
        "12"     { return "arm64" }
        "ARM64"  { return "arm64" }
        "AARCH64" { return "arm64" }
        default  { return $null }
    }
}

function Get-WindowsCliArch {
    $signals = @()

    # Prefer the native processor architecture over the current PowerShell
    # process architecture. This keeps Windows on ARM from being misdetected
    # when PowerShell is running through x64/x86 emulation.
    try {
        if (Get-Command Get-CimInstance -ErrorAction SilentlyContinue) {
            $processorArch = Get-CimInstance -ClassName Win32_Processor -ErrorAction Stop |
                Select-Object -First 1 -ExpandProperty Architecture
            $signals += [pscustomobject]@{ Source = "Win32_Processor.Architecture"; Value = $processorArch }
        }
    } catch {}

    try {
        if (Get-Command Get-WmiObject -ErrorAction SilentlyContinue) {
            $processorArch = Get-WmiObject -Class Win32_Processor -ErrorAction Stop |
                Select-Object -First 1 -ExpandProperty Architecture
            $signals += [pscustomobject]@{ Source = "Win32_Processor.Architecture"; Value = $processorArch }
        }
    } catch {}

    try {
        $signals += [pscustomobject]@{
            Source = "RuntimeInformation.OSArchitecture"
            Value = [System.Runtime.InteropServices.RuntimeInformation]::OSArchitecture
        }
    } catch {}

    $signals += [pscustomobject]@{ Source = "PROCESSOR_ARCHITEW6432"; Value = $env:PROCESSOR_ARCHITEW6432 }
    $signals += [pscustomobject]@{ Source = "PROCESSOR_ARCHITECTURE"; Value = $env:PROCESSOR_ARCHITECTURE }

    foreach ($signal in $signals) {
        $arch = Convert-ToCliArch $signal.Value
        if ($arch) {
            return $arch
        }
    }

    $details = ($signals |
        Where-Object { $null -ne $_.Value -and "$($_.Value)".Trim() -ne "" } |
        ForEach-Object { "$($_.Source)=$($_.Value)" }) -join ", "
    if (-not $details) {
        $details = "no architecture signals available"
    }

    Write-Fail "Unsupported Windows architecture ($details). Only x64 and ARM64 are supported."
}

# ---------------------------------------------------------------------------
# CLI Installation
# ---------------------------------------------------------------------------
function Install-CliBinary {
    Write-Info "Installing Multica CLI from GitHub Releases..."

    if (-not [Environment]::Is64BitOperatingSystem) {
        Write-Fail "Multica requires a 64-bit Windows installation."
    }

    $arch = Get-WindowsCliArch

    $latest = Get-LatestVersion
    if (-not $latest) {
        Write-Fail "Could not determine latest release. Check your network connection."
    }

    $version = $latest.TrimStart('v')
    $url = "https://github.com/multica-ai/multica/releases/download/$latest/multica-cli-$version-windows-$arch.zip"
    $tmpDir = Join-Path ([System.IO.Path]::GetTempPath()) "multica-install"

    if (Test-Path $tmpDir) { Remove-Item $tmpDir -Recurse -Force }
    New-Item -ItemType Directory -Path $tmpDir | Out-Null

    Write-Info "Downloading $url ..."
    try {
        Invoke-WebRequest -Uri $url -OutFile (Join-Path $tmpDir "multica.zip") -UseBasicParsing
    } catch {
        Remove-Item $tmpDir -Recurse -Force
        Write-Fail "Failed to download CLI binary: $_"
    }

    # Verify SHA256 checksum
    $checksumUrl = "https://github.com/multica-ai/multica/releases/download/$latest/checksums.txt"
    try {
        $checksums = Invoke-WebRequest -Uri $checksumUrl -UseBasicParsing -ErrorAction Stop
        $zipFile = Join-Path $tmpDir "multica.zip"
        $actualHash = (Get-FileHash -Path $zipFile -Algorithm SHA256).Hash.ToLower()
        $expectedLine = ($checksums.Content -split "`n") | Where-Object { $_ -match "multica-cli-$version-windows-$arch\.zip" } | Select-Object -First 1
        if ($expectedLine) {
            $expectedHash = ($expectedLine -split "\s+")[0].ToLower()
            if ($actualHash -ne $expectedHash) {
                Remove-Item $tmpDir -Recurse -Force
                Write-Fail "Checksum verification failed. Expected: $expectedHash, Got: $actualHash"
            }
            Write-Ok "Checksum verified"
        } else {
            Write-Warn "Could not find checksum entry for windows_$arch — skipping verification."
        }
    } catch {
        Write-Warn "Could not download checksums.txt — skipping verification."
    }

    Expand-Archive -Path (Join-Path $tmpDir "multica.zip") -DestinationPath $tmpDir -Force

    $binDir = Join-Path $env:USERPROFILE ".multica\bin"
    if (-not (Test-Path $binDir)) {
        New-Item -ItemType Directory -Path $binDir -Force | Out-Null
    }

    $exeSrc = Join-Path $tmpDir "multica.exe"
    if (-not (Test-Path $exeSrc)) {
        $exeSrc = Get-ChildItem -Path $tmpDir -Filter "multica.exe" -Recurse | Select-Object -First 1 -ExpandProperty FullName
    }
    if (-not $exeSrc -or -not (Test-Path $exeSrc)) {
        Remove-Item $tmpDir -Recurse -Force
        Write-Fail "multica.exe not found in downloaded archive."
    }

    Copy-Item $exeSrc (Join-Path $binDir "multica.exe") -Force
    Remove-Item $tmpDir -Recurse -Force

    Add-ToUserPath $binDir
    Write-Ok "Multica CLI installed to $binDir\multica.exe"
}

function Add-ToUserPath {
    param([string]$Dir)
    $currentPath = [Environment]::GetEnvironmentVariable("Path", "User")
    if ($currentPath -and $currentPath.Split(";") -contains $Dir) {
        return
    }
    $newPath = if ($currentPath) { "$currentPath;$Dir" } else { $Dir }
    [Environment]::SetEnvironmentVariable("Path", $newPath, "User")
    # Also update current session
    if ($env:Path -notlike "*$Dir*") {
        $env:Path = "$Dir;$env:Path"
    }
    Write-Info "Added $Dir to user PATH (restart your terminal for other sessions to pick it up)."
}

function Install-Cli {
    if (Test-CommandExists "multica") {
        $currentVer = (multica version 2>$null) -replace '.*?(v[\d.]+).*','$1'
        $latestVer = Get-LatestVersion

        $currentCmp = $currentVer -replace '^v',''
        $latestCmp = if ($latestVer) { $latestVer -replace '^v','' } else { $null }

        $isUpToDate = -not $latestCmp
        if (-not $isUpToDate) {
            try {
                $isUpToDate = [System.Version]$currentCmp -ge [System.Version]$latestCmp
            } catch {
                $isUpToDate = $currentCmp -eq $latestCmp
            }
        }

        if ($isUpToDate) {
            Write-Ok "Multica CLI is up to date ($currentVer)"
            return
        }

        Write-Info "Multica CLI $currentVer installed, latest is $latestVer - upgrading..."
        Install-CliBinary

        $newVer = (multica version 2>$null) -replace '.*?(v[\d.]+).*','$1'
        Write-Ok "Multica CLI upgraded ($currentVer -> $newVer)"
        return
    }

    Install-CliBinary

    if (-not (Test-CommandExists "multica")) {
        Write-Fail "CLI installed but 'multica' not found on PATH. Restart your terminal and try again."
    }
}

# ---------------------------------------------------------------------------
# Docker check
# ---------------------------------------------------------------------------
function Test-Docker {
    if (-not (Test-CommandExists "docker")) {
        Write-Fail @"
Docker is not installed. Multica self-hosting requires Docker and Docker Compose.

Install Docker Desktop for Windows:
  https://docs.docker.com/desktop/install/windows-install/

After installing Docker, re-run this script with `$env:MULTICA_MODE="local"`.
"@
    }

    try {
        docker info 2>$null | Out-Null
    } catch {
        Write-Fail "Docker is installed but not running. Please start Docker Desktop and re-run this script."
    }

    Write-Ok "Docker is available"
}

# ---------------------------------------------------------------------------
# Server setup (self-host / local)
# ---------------------------------------------------------------------------
function Install-Server {
    Write-Info "Setting up Multica server..."
    $serverRef = Get-SelfHostRef
    Write-Info "Using self-host assets from $serverRef..."

    if (Test-Path (Join-Path $InstallDir ".git")) {
        Write-Info "Updating existing installation at $InstallDir..."
        Write-Warn "Any local changes in $InstallDir will be overwritten."
    } else {
        Write-Info "Cloning Multica repository..."
        if (-not (Test-CommandExists "git")) {
            Write-Fail "Git is not installed. Please install git and re-run."
        }
        if (Test-Path $InstallDir) {
            Write-Warn "Removing incomplete installation at $InstallDir..."
            Remove-Item $InstallDir -Recurse -Force
        }
        $parentDir = Split-Path $InstallDir -Parent
        if (-not (Test-Path $parentDir)) {
            New-Item -ItemType Directory -Path $parentDir -Force | Out-Null
        }
        git clone --depth 1 $RepoUrl $InstallDir
    }

    Push-Location $InstallDir
    Checkout-ServerRef $serverRef
    Write-Ok "Repository ready at $InstallDir ($serverRef)"

    if (-not (Test-Path ".env")) {
        Write-Info "Creating .env with random JWT_SECRET..."
        Copy-Item ".env.example" ".env"
        $jwt = -join ((1..32) | ForEach-Object { "{0:x2}" -f (Get-Random -Maximum 256) })
        (Get-Content ".env") -replace '^JWT_SECRET=.*', "JWT_SECRET=$jwt" | Set-Content ".env"
        Write-Ok "Generated .env with random JWT_SECRET"
    } else {
        Write-Ok "Using existing .env"
    }

    Write-Info "Pulling official Multica images..."
    Pull-OfficialSelfHostImages
    Write-Info "Starting Multica services (this may take a few minutes on first run)..."
    docker compose -f docker-compose.selfhost.yml up -d

    Write-Info "Waiting for backend to be ready..."
    $ready = $false
    for ($i = 1; $i -le 45; $i++) {
        try {
            $null = Invoke-WebRequest -Uri "http://localhost:8080/health" -UseBasicParsing -TimeoutSec 2
            $ready = $true
            break
        } catch {
            Start-Sleep -Seconds 2
        }
    }

    if ($ready) {
        Write-Ok "Multica server is running"
    } else {
        Write-Warn "Server is still starting. Check logs with:"
        Write-Host "  cd $InstallDir; docker compose -f docker-compose.selfhost.yml logs"
    }

    Pop-Location
}


# ---------------------------------------------------------------------------
# Main: Default mode (cloud)
# ---------------------------------------------------------------------------
function Start-DefaultInstall {
    Write-Host ""
    Write-Host "  Multica - Installer" -ForegroundColor White
    Write-Host ""

    Install-Cli

    Write-Host ""
    Write-Host "  ============================================" -ForegroundColor Green
    Write-Host "  [OK] Multica CLI is ready!" -ForegroundColor Green
    Write-Host "  ============================================" -ForegroundColor Green
    Write-Host ""
    Write-Host "  Next: configure your environment"
    Write-Host ""
    Write-Host "     multica setup               " -NoNewline; Write-Host "# Connect to Multica Cloud (multica.ai)" -ForegroundColor DarkGray
    Write-Host "     multica setup self-host      " -NoNewline; Write-Host "# Connect to a self-hosted server" -ForegroundColor DarkGray
    Write-Host ""
    Write-Host "  Self-hosting? Install the server first:"
    Write-Host '     $env:MULTICA_MODE="with-server"; irm https://raw.githubusercontent.com/multica-ai/multica/main/scripts/install.ps1 | iex'
    Write-Host ""
}

# ---------------------------------------------------------------------------
# Main: Local mode (self-host)
# ---------------------------------------------------------------------------
function Start-LocalInstall {
    Write-Host ""
    Write-Host "  Multica - Self-Host Installer" -ForegroundColor White
    Write-Host "  Provisioning server infrastructure + installing CLI"
    Write-Host ""

    Test-Docker
    Install-Server
    Install-Cli

    Write-Host ""
    Write-Host "  ============================================" -ForegroundColor Green
    Write-Host "  [OK] Multica server is running and CLI is ready!" -ForegroundColor Green
    Write-Host "  ============================================" -ForegroundColor Green
    Write-Host ""
    Write-Host "  Frontend:  http://localhost:3000"
    Write-Host "  Backend:   http://localhost:8080"
    Write-Host "  Server at: $InstallDir"
    Write-Host ""
    Write-Host "  Next: configure your CLI to connect"
    Write-Host ""
    Write-Host "     multica setup self-host  " -NoNewline; Write-Host "# Configure + authenticate + start daemon" -ForegroundColor DarkGray
    Write-Host ""
    Write-Host "  Login: configure RESEND_API_KEY in .env for email codes,"
    Write-Host "  or set APP_ENV=development in .env to enable the dev master code 888888."
    Write-Host ""
    Write-Host "  To stop all services:"
    Write-Host '     $env:MULTICA_MODE="stop"; irm https://raw.githubusercontent.com/multica-ai/multica/main/scripts/install.ps1 | iex'
    Write-Host ""
}

# ---------------------------------------------------------------------------
# Stop: shut down a self-hosted installation
# ---------------------------------------------------------------------------
function Start-Stop {
    Write-Host ""
    Write-Info "Stopping Multica services..."

    if (Test-Path $InstallDir) {
        Push-Location $InstallDir
        if (Test-Path "docker-compose.selfhost.yml") {
            docker compose -f docker-compose.selfhost.yml down
            Write-Ok "Docker services stopped"
        } else {
            Write-Warn "No docker-compose.selfhost.yml found at $InstallDir"
        }
        Pop-Location
    } else {
        Write-Warn "No Multica installation found at $InstallDir"
    }

    if (Test-CommandExists "multica") {
        try {
            multica daemon stop 2>$null
            Write-Ok "Daemon stopped"
        } catch {}
    }

    Write-Host ""
}

# ---------------------------------------------------------------------------
# Entry point
# ---------------------------------------------------------------------------
$mode = if ($env:MULTICA_MODE) { $env:MULTICA_MODE.ToLower() } else { "default" }

switch ($mode) {
    "with-server" { Start-LocalInstall }
    "local"       { Start-LocalInstall }  # backwards compat alias
    "stop"        { Start-Stop }
    default       { Start-DefaultInstall }
}
