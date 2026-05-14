# Multica installer for Windows — one command to get started.
#
# Install CLI (default): connects to the internal cloud
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
$DefaultInstallDir = Join-Path $env:USERPROFILE ".multica\server"
$InstallDir    = if ($env:MULTICA_INSTALL_DIR) { $env:MULTICA_INSTALL_DIR } else { $DefaultInstallDir }
$CliBinDir     = if ($env:MULTICA_CLI_BIN_DIR) { $env:MULTICA_CLI_BIN_DIR } else { (Join-Path $env:USERPROFILE ".multica\bin") }
$CliBinPath    = Join-Path $CliBinDir "multica.exe"
$AppUrl        = if ($env:MULTICA_APP_URL) { $env:MULTICA_APP_URL } else { "https://multica.wujieai.com" }
$ServerUrl     = if ($env:MULTICA_SERVER_URL) { $env:MULTICA_SERVER_URL } else { "https://multica.wujieai.com" }
$UpdateManifestUrl = if ($env:MULTICA_UPDATE_MANIFEST_URL) { $env:MULTICA_UPDATE_MANIFEST_URL } else { "https://multica.obs.cn-east-3.myhuaweicloud.com/cli/manifest.json" }

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

function Get-UpdateManifest {
    try {
        return Invoke-RestMethod -Uri $UpdateManifestUrl -ErrorAction Stop
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
    $nativeArchSignalFound = $false

    # Prefer the native processor architecture over the current PowerShell
    # process architecture. This keeps Windows on ARM from being misdetected
    # when PowerShell is running through x64/x86 emulation.
    try {
        if (Get-Command Get-CimInstance -ErrorAction SilentlyContinue) {
            $processorArch = Get-CimInstance -ClassName Win32_Processor -ErrorAction Stop |
                Select-Object -First 1 -ExpandProperty Architecture
            $signals += [pscustomobject]@{ Source = "Win32_Processor.Architecture"; Value = $processorArch }
            $nativeArchSignalFound = $true
        }
    } catch {}

    try {
        if (-not $nativeArchSignalFound -and (Get-Command Get-WmiObject -ErrorAction SilentlyContinue)) {
            $processorArch = Get-WmiObject -Class Win32_Processor -ErrorAction Stop |
                Select-Object -First 1 -ExpandProperty Architecture
            $signals += [pscustomobject]@{ Source = "Win32_Processor.Architecture"; Value = $processorArch }
            $nativeArchSignalFound = $true
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

function Get-InstalledCliVersion {
    param([string]$Command = "multica")

    try {
        $firstLine = & $Command version 2>$null | Select-Object -First 1
        if ("$firstLine" -match '\b(v?\d+\.\d+\.\d+(?:-\d+(?:-[0-9A-Za-z.-]+)?)?)\b') {
            $version = $Matches[1]
            if ($version -notlike 'v*') {
                $version = "v$version"
            }
            return $version
        }
    } catch {}

    return $null
}

function ConvertFrom-CliVersion {
    param([string]$Version)

    if (-not $Version) {
        return $null
    }

    $trimmed = $Version.Trim()
    if ($trimmed -notmatch '^v?(\d+)\.(\d+)\.(\d+)(?:-(\d+)(?:-[0-9A-Za-z.-]+)?)?$') {
        return $null
    }

    [pscustomobject]@{
        Major   = [int]$Matches[1]
        Minor   = [int]$Matches[2]
        Patch   = [int]$Matches[3]
        Commits = if ($Matches[4]) { [int]$Matches[4] } else { 0 }
    }
}

function Test-CliVersionAtLeast {
    param(
        [string]$Current,
        [string]$Latest
    )

    $currentVersion = ConvertFrom-CliVersion $Current
    $latestVersion = ConvertFrom-CliVersion $Latest
    if (-not $currentVersion -or -not $latestVersion) {
        return $false
    }

    foreach ($part in @("Major", "Minor", "Patch", "Commits")) {
        if ($currentVersion.$part -gt $latestVersion.$part) {
            return $true
        }
        if ($currentVersion.$part -lt $latestVersion.$part) {
            return $false
        }
    }

    return $true
}

# ---------------------------------------------------------------------------
# CLI Installation
# ---------------------------------------------------------------------------
function Get-ManagedInstallMarkerPath {
    Join-Path $CliBinDir ".install-source.json"
}

function Test-ManagedInstall {
    if (-not (Test-Path $CliBinPath)) {
        return $false
    }

    $markerPath = Get-ManagedInstallMarkerPath
    if (-not (Test-Path $markerPath)) {
        return $false
    }

    try {
        $marker = Get-Content $markerPath -Raw | ConvertFrom-Json
        return $marker.install_channel -eq "managed-manifest"
    } catch {
        return $false
    }
}

function Write-ManagedInstallMarker {
    $markerPath = Get-ManagedInstallMarkerPath
    $payload = @{
        install_channel  = "managed-manifest"
        installed_at     = [DateTime]::UtcNow.ToString("yyyy-MM-ddTHH:mm:ssZ")
        manifest_url     = $UpdateManifestUrl
        installer_version = "managed-manifest-v1"
    }
    $payload | ConvertTo-Json | Set-Content -Path $markerPath -Encoding UTF8
}

function Test-LegacyInstall {
    (Test-Path $CliBinPath) -and -not (Test-ManagedInstall)
}

function Uninstall-LegacyInstall {
    Write-Info "Detected legacy CLI install from the main-branch installer. It will be replaced in place by the managed manifest install."
}

function Migrate-LegacyInstallIfNeeded {
    if (-not (Test-LegacyInstall)) {
        return $false
    }
    Uninstall-LegacyInstall
    return $true
}

function Install-CliBinary {
    Write-Info "Installing Multica CLI from update manifest..."

    if (-not [Environment]::Is64BitOperatingSystem) {
        Write-Fail "Multica requires a 64-bit Windows installation."
    }

    $arch = Get-WindowsCliArch

    $manifest = Get-UpdateManifest
    if (-not $manifest) {
        Write-Fail "Could not download update manifest from $UpdateManifestUrl"
    }

    $asset = $manifest.assets | Where-Object { $_.os -eq "windows" -and $_.arch -eq $arch } | Select-Object -First 1
    if (-not $asset) {
        Write-Fail "No matching asset in manifest for windows/$arch"
    }

    $tmpDir = Join-Path ([System.IO.Path]::GetTempPath()) "multica-install"

    if (Test-Path $tmpDir) { Remove-Item $tmpDir -Recurse -Force }
    New-Item -ItemType Directory -Path $tmpDir | Out-Null

    $url = $asset.download_url
    $checksum = "$($asset.checksum)".ToLower()
    Write-Info "Downloading $url ..."
    try {
        Invoke-WebRequest -Uri $url -OutFile (Join-Path $tmpDir "multica.zip") -UseBasicParsing
    } catch {
        Remove-Item $tmpDir -Recurse -Force
        Write-Fail "Failed to download CLI binary: $_"
    }

    $zipFile = Join-Path $tmpDir "multica.zip"
    $actualHash = (Get-FileHash -Path $zipFile -Algorithm SHA256).Hash.ToLower()
    if ($actualHash -ne $checksum) {
        Remove-Item $tmpDir -Recurse -Force
        Write-Fail "Checksum verification failed. Expected: $checksum, Got: $actualHash"
    }
    Write-Ok "Checksum verified"

    Expand-Archive -Path (Join-Path $tmpDir "multica.zip") -DestinationPath $tmpDir -Force

    if (-not (Test-Path $CliBinDir)) {
        New-Item -ItemType Directory -Path $CliBinDir -Force | Out-Null
    }

    $exeSrc = Join-Path $tmpDir "multica.exe"
    if (-not (Test-Path $exeSrc)) {
        $exeSrc = Get-ChildItem -Path $tmpDir -Filter "multica.exe" -Recurse | Select-Object -First 1 -ExpandProperty FullName
    }
    if (-not $exeSrc -or -not (Test-Path $exeSrc)) {
        Remove-Item $tmpDir -Recurse -Force
        Write-Fail "multica.exe not found in downloaded archive."
    }

    $backupPath = "$CliBinPath.bak"
    if (Test-Path $backupPath) {
        Remove-Item $backupPath -Force
    }

    $hadExistingCli = Test-Path $CliBinPath
    if ($hadExistingCli) {
        Stop-ManagedDaemonForReplace
        try {
            Move-Item $CliBinPath $backupPath -Force
        } catch {
            Remove-Item $tmpDir -Recurse -Force
            Write-Fail "Failed to move existing CLI out of the way before installing the new version: $_"
        }
    }

    $runtimeNodeModules = Join-Path $CliBinDir "node_modules"
    $runtimePackageJson = Join-Path $CliBinDir "package.json"
    $nodeModulesSrc = Join-Path $tmpDir "node_modules"
    $packageJsonSrc = Join-Path $tmpDir "package.json"

    try {
        if (Test-Path $runtimeNodeModules) {
            Remove-Item $runtimeNodeModules -Recurse -Force
        }
        if (Test-Path $runtimePackageJson) {
            Remove-Item $runtimePackageJson -Force
        }
        if (Test-Path $nodeModulesSrc) {
            Move-Item $nodeModulesSrc $runtimeNodeModules -Force
        }
        if (Test-Path $packageJsonSrc) {
            Move-Item $packageJsonSrc $runtimePackageJson -Force
        }
        Move-Item $exeSrc $CliBinPath -Force
        if (Test-Path $backupPath) {
            Remove-Item $backupPath -Force
        }
    } catch {
        if ($hadExistingCli -and (Test-Path $backupPath)) {
            try {
                Move-Item $backupPath $CliBinPath -Force
            } catch {
                Write-Warn "Failed to restore the previous CLI from backup at $backupPath."
            }
        }
        Remove-Item $tmpDir -Recurse -Force
        Write-Fail "Failed to install the new CLI binary: $_"
    }

    try {
        Write-ManagedInstallMarker
    } catch {
        Remove-Item $tmpDir -Recurse -Force
        Write-Fail "Installed the CLI but failed to write the managed install marker: $_"
    }

    Remove-Item $tmpDir -Recurse -Force

    Add-ToUserPath $CliBinDir
    Write-CurrentSessionPathHint
    Write-Ok "Multica CLI installed to $CliBinPath"
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

function Write-CurrentSessionPathHint {
    Write-Warn "The installer updated your user PATH, but the PowerShell session that invoked this script may not pick it up automatically."
    Write-Host "For this terminal, run: " -NoNewline
    Write-Host "`$env:Path = `"$CliBinDir;`$env:Path`"" -ForegroundColor Yellow
}

function Install-Cli {
    $null = Migrate-LegacyInstallIfNeeded

    if ((Test-CommandExists "multica") -and ((Get-Command multica).Source -ne $CliBinPath)) {
        Write-Warn "Detected another multica on PATH at $((Get-Command multica).Source). The managed install will use $CliBinPath."
    }

    if (Test-ManagedInstall) {
        $currentVer = Get-InstalledCliVersion -Command $CliBinPath
        $manifest = Get-UpdateManifest
        $latestVer = if ($manifest) { $manifest.version } else { $null }

        $isUpToDate = $latestVer -and (Test-CliVersionAtLeast $currentVer $latestVer)
        if (-not $latestVer) { $isUpToDate = $true }

        if ($isUpToDate) {
            Write-Ok "Multica CLI is up to date ($currentVer)"
            return
        }

        Write-Info "Multica CLI $currentVer installed, latest is $latestVer - upgrading..."
        Install-CliBinary

        $newVer = Get-InstalledCliVersion -Command $CliBinPath
        Write-Ok "Multica CLI upgraded ($currentVer -> $newVer)"
        return
    }

    Install-CliBinary

    if (-not (Test-Path $CliBinPath)) {
        Write-Fail "CLI installed but 'multica' not found on PATH. Restart your terminal and try again."
    }
}

function Configure-InternalCloud {
    Write-Info "Configuring Multica CLI for $AppUrl ..."
    & $CliBinPath config set server_url $ServerUrl | Out-Null
    & $CliBinPath config set app_url $AppUrl | Out-Null
    & $CliBinPath config set update_manifest_url $UpdateManifestUrl | Out-Null
    Write-Ok "CLI config updated for the internal cloud"
}

function Test-DaemonRunning {
    try {
        $status = (& $CliBinPath daemon status 2>$null | Out-String)
        return $status -match 'running'
    } catch {
        return $false
    }
}

function Stop-ManagedDaemonForReplace {
    if (-not (Test-Path $CliBinPath)) {
        return
    }
    if (-not (Test-DaemonRunning)) {
        return
    }

    Write-Info "Stopping running Multica daemon before replacing CLI..."
    & $CliBinPath daemon stop | Out-Null
    if ($LASTEXITCODE -ne 0) {
        Write-Fail "Failed to stop the running Multica daemon. Please stop it manually and retry."
    }
}

function Start-LoginAndDaemon {
    Write-Host ""
    Write-Info "Opening browser login for $AppUrl ..."
    Write-Info "Complete authorization in the browser, then return here."
    & $CliBinPath login
    if ($LASTEXITCODE -ne 0) {
        Write-Fail "Login did not complete successfully."
    }

    if (Test-DaemonRunning) {
        Write-Ok "Multica daemon is already running"
        return
    }

    Write-Info "Starting Multica daemon..."
    & $CliBinPath daemon start
    if ($LASTEXITCODE -ne 0) {
        Write-Fail "Failed to start the Multica daemon."
    }
    Write-Ok "Multica daemon started"
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
    Write-Host "  Configuring the internal cloud at $AppUrl"
    Write-Host ""

    Install-Cli
    Configure-InternalCloud
    Start-LoginAndDaemon

    Write-Host ""
    Write-Host "  ============================================" -ForegroundColor Green
    Write-Host "  [OK] Multica CLI is ready!" -ForegroundColor Green
    Write-Host "  ============================================" -ForegroundColor Green
    Write-Host ""
    Write-Host "  Configured server: $ServerUrl"
    Write-Host "  Configured app:    $AppUrl"
    Write-Host ""
    Write-Host "     multica config list          " -NoNewline; Write-Host "# Verify config values" -ForegroundColor DarkGray
    Write-Host "     multica daemon status        " -NoNewline; Write-Host "# Verify daemon status" -ForegroundColor DarkGray
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
    Write-Host "  or read the generated code from backend logs when Resend is unset."
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
