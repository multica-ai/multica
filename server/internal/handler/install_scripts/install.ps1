# Multica CLI installer for Windows PowerShell
# Usage:
#   irm https://multica.wujieai.com/install.ps1 | iex
#   $env:MULTICA_VERSION = "0.3.1-514-gc59dc875"; irm https://multica.wujieai.com/install.ps1 | iex
#
# Environment variables:
#   MULTICA_VERSION   — install a specific version instead of latest
#   MULTICA_DIR       — installation directory (default: ~/.multica/bin)
#   MULTICA_SERVER    — server URL (default: https://multica.wujieai.com)
#   MULTICA_CHANNEL   — release channel: prod (default) or test

$ErrorActionPreference = "Stop"

# --- Configuration ---
$DefaultServer = "https://multica.wujieai.com"
$OBSHost = "https://multica.obs.cn-east-3.myhuaweicloud.com"
$Channel = if ($env:MULTICA_CHANNEL) { $env:MULTICA_CHANNEL } else { "" }
$InstallDir = if ($env:MULTICA_DIR) { $env:MULTICA_DIR } else { Join-Path $HOME ".multica\bin" }
$ServerURL = if ($env:MULTICA_SERVER) { $env:MULTICA_SERVER } else { $DefaultServer }
$Version = $env:MULTICA_VERSION

# Resolve OBS paths based on channel
switch ($Channel) {
    "" {
        $Channel = "prod"
        $OBSPrefix = "cli"
    }
    "prod" {
        $OBSPrefix = "cli"
    }
    "test" {
        $OBSPrefix = "cli-test"
    }
    default {
        Exit-Fatal "Unsupported channel: $Channel (supported: prod, test)"
    }
}
$OBSBase = "$OBSHost/$OBSPrefix/releases"
$ManifestURL = "$OBSHost/$OBSPrefix/manifest.json"

# --- Helpers ---
function Write-Info  { param($msg) Write-Host "[info]  $msg" -ForegroundColor Blue }
function Write-Ok    { param($msg) Write-Host "[ok]    $msg" -ForegroundColor Green }
function Write-Warn  { param($msg) Write-Host "[warn]  $msg" -ForegroundColor Yellow }
function Write-Err   { param($msg) Write-Host "[error] $msg" -ForegroundColor Red }
function Exit-Fatal  { param($msg) Write-Err $msg; exit 1 }

# --- Detect architecture ---
function Get-Arch {
    $arch = [System.Runtime.InteropServices.RuntimeInformation]::OSArchitecture
    switch ($arch) {
        "X64"   { return "amd64" }
        "Arm64" { return "arm64" }
        default { Exit-Fatal "Unsupported architecture: $arch" }
    }
}

# --- Fetch latest version ---
function Get-LatestVersion {
    Write-Info "Fetching latest CLI version..."

    # Try server endpoint first
    try {
        $versionEndpoint = "$ServerURL/install/latest-cli-version"
        if ($Channel -eq "test") {
            $versionEndpoint = "$versionEndpoint?channel=test"
        }
        $version = (Invoke-RestMethod -Uri $versionEndpoint -TimeoutSec 10).Trim()
        if ($version) {
            return $version.TrimStart("v")
        }
    } catch {
        Write-Warn "Server endpoint unavailable, falling back to manifest..."
    }

    # Fallback: parse manifest
    try {
        $manifest = Invoke-RestMethod -Uri $ManifestURL -TimeoutSec 10
        $version = $manifest.version.TrimStart("v")
        if ($version) {
            return $version
        }
    } catch {
        Exit-Fatal "Failed to determine latest CLI version. Try setting `$env:MULTICA_VERSION manually."
    }
}

# --- Verify checksum ---
function Test-Checksum {
    param($FilePath, $Filename)

    try {
        $manifest = Invoke-RestMethod -Uri $ManifestURL -TimeoutSec 10
        $asset = $manifest.assets | Where-Object { $_.filename -eq $Filename }
        if ($asset -and $asset.checksum) {
            $expected = $asset.checksum
            $actual = (Get-FileHash -Path $FilePath -Algorithm SHA256).Hash.ToLower()
            if ($actual -ne $expected) {
                Exit-Fatal "Checksum verification failed!`n  Expected: $expected`n  Actual:   $actual"
            }
            Write-Ok "Checksum verified"
        } else {
            Write-Warn "No checksum available, skipping verification"
        }
    } catch {
        Write-Warn "Could not verify checksum: $_"
    }
}

# --- Main ---
function Install-MulticaCLI {
    Write-Host ""
    Write-Info "Multica CLI Installer (Windows)"
    Write-Host ""

    $arch = Get-Arch
    $os = "windows"

    # Determine version
    if (-not $Version) {
        $Version = Get-LatestVersion
    } else {
        $Version = $Version.TrimStart("v")
        Write-Info "Installing specified version: $Version"
    }

    $filename = "multica-cli-$Version-$os-$arch.zip"
    $url = "$OBSBase/$filename"

    Write-Info "Downloading Multica CLI v$Version for $os/$arch..."
    Write-Info "URL: $url"

    # Create temp directory
    $tmpDir = Join-Path ([System.IO.Path]::GetTempPath()) "multica-install-$(Get-Random)"
    New-Item -ItemType Directory -Path $tmpDir -Force | Out-Null

    try {
        # Download
        $downloadPath = Join-Path $tmpDir $filename
        try {
            Invoke-WebRequest -Uri $url -OutFile $downloadPath -UseBasicParsing
        } catch {
            Exit-Fatal "Download failed. Version '$Version' may not exist for $os/$arch.`nURL: $url`nError: $_"
        }

        # Check file exists and not empty
        if (-not (Test-Path $downloadPath) -or (Get-Item $downloadPath).Length -eq 0) {
            Exit-Fatal "Downloaded file is empty or missing"
        }

        # Verify checksum
        Test-Checksum -FilePath $downloadPath -Filename $filename

        # Extract
        Write-Info "Installing to $InstallDir..."
        if (-not (Test-Path $InstallDir)) {
            New-Item -ItemType Directory -Path $InstallDir -Force | Out-Null
        }

        Expand-Archive -Path $downloadPath -DestinationPath $tmpDir -Force

        # Find and copy binary
        $binary = Get-ChildItem -Path $tmpDir -Recurse -Filter "multica.exe" | Select-Object -First 1
        if (-not $binary) {
            Exit-Fatal "Binary 'multica.exe' not found in archive"
        }

        Copy-Item -Path $binary.FullName -Destination (Join-Path $InstallDir "multica.exe") -Force
        Write-Ok "Installed $(Join-Path $InstallDir 'multica.exe')"

    } finally {
        # Cleanup
        Remove-Item -Path $tmpDir -Recurse -Force -ErrorAction SilentlyContinue
    }

    # Configure PATH
    $currentPath = [Environment]::GetEnvironmentVariable("Path", "User")
    if ($currentPath -notlike "*$InstallDir*") {
        [Environment]::SetEnvironmentVariable("Path", "$InstallDir;$currentPath", "User")
        $env:Path = "$InstallDir;$env:Path"
        Write-Info "Added $InstallDir to user PATH"
    }

    # Configure server URL
    $multica = Join-Path $InstallDir "multica.exe"
    Write-Info "Configuring server URL: $ServerURL"
    & $multica config set server_url $ServerURL 2>$null
    & $multica config set app_url $ServerURL 2>$null
    Write-Ok "Server configured: $ServerURL"

    # Configure test channel update manifest
    if ($Channel -eq "test") {
        Write-Info "Configuring update manifest for test channel..."
        & $multica config set update_manifest_url $ManifestURL 2>$null
        Write-Ok "Update manifest set to test channel: $ManifestURL"
    }

    # Restart daemon
    Write-Info "Restarting daemon..."
    & $multica daemon stop 2>$null
    Start-Sleep -Seconds 1
    try {
        & $multica daemon start 2>$null
        Write-Ok "Daemon started"
    } catch {
        Write-Warn "Failed to start daemon. Run manually: multica daemon start"
    }

    # Summary
    Write-Host ""
    Write-Host "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━" -ForegroundColor Cyan
    Write-Ok "Multica CLI installed successfully!"
    Write-Host ""
    try {
        $ver = & $multica version 2>$null
        Write-Host "  Version:  $ver"
    } catch {
        Write-Host "  Version:  v$Version"
    }
    Write-Host "  Binary:   $(Join-Path $InstallDir 'multica.exe')"
    Write-Host "  Server:   $ServerURL"
    Write-Host ""
    Write-Host "  Next step: Log in to your Multica account:"
    Write-Host ""
    Write-Host "    multica login"
    Write-Host ""
    Write-Host "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━" -ForegroundColor Cyan
}

Install-MulticaCLI
