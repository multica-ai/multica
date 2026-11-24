#!/usr/bin/env pwsh
# Port-contract tests for scripts/install.ps1 — the PowerShell counterpart of the
# `--with-server` cases in scripts/install.test.sh.
#
# The installer used to derive the backend/frontend host port from .env with its
# own copy of the alias chain. Docker Compose gives the calling process
# environment precedence over .env, so any ambient PORT / BACKEND_PORT /
# API_PORT / SERVER_PORT / FRONTEND_PORT moved the published port while the
# installer kept probing and printing the file value (#6145).
#
# Here the docker stub plays Compose: it answers `port` from the same resolution
# Compose performs — environment first, then .env. The installer must take that
# answer as given for both the health check and the summary, which a .env-only
# derivation cannot do because the two disagree in every ambient case below.
# That real Compose resolves this way is proven against real `docker compose
# config` in scripts/selfhost-config.test.sh.

Set-StrictMode -Version Latest
$ErrorActionPreference = "Stop"

$RepoRoot = Split-Path (Split-Path $PSCommandPath -Parent) -Parent
$InstallerPath = Join-Path $RepoRoot "scripts/install.ps1"
$PowerShellPath = (Get-Process -Id $PID).Path

# install.ps1 resolves its default install directory from USERPROFILE at load
# time. Provide one so these tests also run on a non-Windows agent.
if (-not $env:USERPROFILE) { $env:USERPROFILE = [System.IO.Path]::GetTempPath() }

function Fail-Test {
    param([string]$Message)
    Write-Host "FAIL: $Message" -ForegroundColor Red
    exit 1
}

# install.ps1 dispatches at the bottom on import, so load only the definitions.
function Get-InstallerDefinitions {
    $source = Get-Content -Raw -Path $InstallerPath
    $marker = "# Entry point"
    $index = $source.IndexOf($marker)
    if ($index -lt 0) {
        Fail-Test "could not find the entry-point marker in install.ps1"
    }
    # Trim back to the comment banner that precedes the marker.
    $banner = $source.LastIndexOf("# ---", $index)
    if ($banner -ge 0) { $index = $banner }
    return $source.Substring(0, $index)
}

# ---------------------------------------------------------------------------
# 1. install.ps1 must parse, and must no longer derive ports from .env
# ---------------------------------------------------------------------------
$parseErrors = $null
# Parse the file directly: Windows PowerShell 5.1 reads BOM-less files as ANSI,
# unlike PowerShell 7. Decoding as UTF-8 first would hide regressions like #8660.
[System.Management.Automation.Language.Parser]::ParseFile($InstallerPath, [ref]$null, [ref]$parseErrors) | Out-Null
if ($parseErrors) {
    $parseErrors | ForEach-Object { Write-Host "  $($_.Message) (line $($_.Extent.StartLineNumber))" }
    Fail-Test "install.ps1 has parse errors"
}

$installerSource = Get-Content -Raw -Path $InstallerPath
foreach ($banned in @("Get-SelfHostBackendPort", "Get-SelfHostFrontendPort", "Get-EnvFileValue")) {
    if ($installerSource -match [regex]::Escape($banned)) {
        Fail-Test "install.ps1 must not re-derive host ports from .env (found $banned)"
    }
}
foreach ($required in @(
        'Get-ComposePublishedPort -Service "backend" -ContainerPort 8080',
        'Get-ComposePublishedPort -Service "frontend" -ContainerPort 3000',
        'http://localhost:$($script:SelfHostBackendPort)/health')) {
    if ($installerSource -notmatch [regex]::Escape($required)) {
        Fail-Test "install.ps1 is missing the Compose-published-port wiring: $required"
    }
}

# ---------------------------------------------------------------------------
# 2. Get-ComposePublishedPort parses what `docker compose port` returns
# ---------------------------------------------------------------------------
$definitions = Get-InstallerDefinitions

$portReaderCases = @(
    @{ Label = "loopback binding"; Output = @("127.0.0.1:9500"); Expected = "9500" }
    @{ Label = "wildcard binding"; Output = @("0.0.0.0:9600"); Expected = "9600" }
    @{ Label = "trailing blank line"; Output = @("127.0.0.1:9700", ""); Expected = "9700" }
    @{ Label = "no output"; Output = @(); Expected = $null }
    @{ Label = "non-numeric"; Output = @("127.0.0.1:notaport"); Expected = $null }
)

foreach ($case in $portReaderCases) {
    $script:stubOutput = $case.Output
    $result = & {
        Invoke-Expression $definitions
        function docker {
            $global:LASTEXITCODE = 0
            return $script:stubOutput
        }
        Get-ComposePublishedPort -Service "backend" -ContainerPort 8080
    }
    if ($result -ne $case.Expected) {
        Fail-Test "Get-ComposePublishedPort [$($case.Label)]: expected '$($case.Expected)' got '$result'"
    }
}

# A failing `docker compose port` must produce no port, not a guess.
$failureResult = & {
    Invoke-Expression $definitions
    function docker {
        $global:LASTEXITCODE = 1
        return @()
    }
    Get-ComposePublishedPort -Service "backend" -ContainerPort 8080
}
if ($null -ne $failureResult) {
    Fail-Test "Get-ComposePublishedPort must return null when docker compose port fails, got '$failureResult'"
}

# ---------------------------------------------------------------------------
# 3. End to end: the probed URL and the printed URLs are the published ports
# ---------------------------------------------------------------------------
$cases = @(
    @{ Label = "defaults"; Env = @{}; Mutation = $null; Backend = "8080"; Frontend = "3000" }
    @{ Label = "env-file PORT"; Env = @{}; Mutation = @{ PORT = "9100" }; Backend = "9100"; Frontend = "3000" }
    @{ Label = "env-file BACKEND_PORT"; Env = @{}; Mutation = @{ BACKEND_PORT = "9200" }; Backend = "9200"; Frontend = "3000" }
    @{ Label = "env-file API_PORT"; Env = @{}; Mutation = @{ API_PORT = "9300" }; Backend = "9300"; Frontend = "3000" }
    @{ Label = "env-file SERVER_PORT"; Env = @{}; Mutation = @{ SERVER_PORT = "9400" }; Backend = "9400"; Frontend = "3000" }
    @{ Label = "env-file FRONTEND_PORT"; Env = @{}; Mutation = @{ FRONTEND_PORT = "3100" }; Backend = "8080"; Frontend = "3100" }
    @{ Label = "ambient PORT beats .env"; Env = @{ PORT = "9500" }; Mutation = @{ PORT = "9100" }; Backend = "9500"; Frontend = "3000" }
    @{ Label = "ambient BACKEND_PORT beats .env"; Env = @{ BACKEND_PORT = "9600" }; Mutation = @{ PORT = "9100" }; Backend = "9600"; Frontend = "3000" }
    @{ Label = "ambient API_PORT beats .env"; Env = @{ API_PORT = "9700" }; Mutation = @{ PORT = "9100" }; Backend = "9700"; Frontend = "3000" }
    @{ Label = "ambient SERVER_PORT beats .env"; Env = @{ SERVER_PORT = "9800" }; Mutation = @{ PORT = "9100" }; Backend = "9800"; Frontend = "3000" }
    @{ Label = "ambient FRONTEND_PORT beats .env"; Env = @{ FRONTEND_PORT = "3200" }; Mutation = @{ FRONTEND_PORT = "3100" }; Backend = "8080"; Frontend = "3200" }
    # An explicitly empty higher-priority alias contributes no port, so the chain
    # continues. Expressed through .env because whether an *ambient* empty value
    # survives into a child process is platform-dependent; the Bash suite covers
    # the ambient-empty case on POSIX.
    @{ Label = "env-file empty BACKEND_PORT falls back"; Env = @{}; Mutation = @{ BACKEND_PORT = ""; PORT = "9100" }; Backend = "9100"; Frontend = "3000" }
)

$runnerScript = Join-Path ([System.IO.Path]::GetTempPath()) "multica-install-ps1-case.ps1"

# Each case runs in its own process using the current PowerShell host, so the
# Windows PowerShell 5.1 suite cannot silently delegate its cases to PowerShell 7.
# install.ps1 uses `exit` on failure, and
# a child process is also the only way to control the ambient environment
# cleanly rather than inheriting a CI runner's PORT.
@'
param(
    [string]$InstallerPath,
    [string]$WorkDir,
    [string]$ProbeLog
)

Set-StrictMode -Version Latest
$ErrorActionPreference = "Stop"

if (-not $env:USERPROFILE) { $env:USERPROFILE = [System.IO.Path]::GetTempPath() }
$env:MULTICA_INSTALL_DIR = $WorkDir
$env:MULTICA_SELFHOST_REF = "main"

$source = Get-Content -Raw -Path $InstallerPath
$index = $source.IndexOf("# Entry point")
$banner = $source.LastIndexOf("# ---", $index)
if ($banner -ge 0) { $index = $banner }
Invoke-Expression $source.Substring(0, $index)

function Get-EnvFilePortValue {
    param([string]$Key)
    $line = Get-Content (Join-Path $WorkDir ".env") |
        Where-Object { $_ -match "^$Key=" } |
        Select-Object -Last 1
    if (-not $line) { return $null }
    return $line.Substring($Key.Length + 1)
}

# Compose resolution: the process environment wins over .env.
function Resolve-ComposeValue {
    param([string]$Key)
    $fromEnv = [System.Environment]::GetEnvironmentVariable($Key)
    if ($null -ne $fromEnv) { return $fromEnv }
    return Get-EnvFilePortValue -Key $Key
}

function Get-StubBackendPort {
    foreach ($key in @("BACKEND_PORT", "API_PORT", "SERVER_PORT", "PORT")) {
        $value = Resolve-ComposeValue -Key $key
        if (-not [string]::IsNullOrEmpty($value)) { return $value }
    }
    return "8080"
}

function Get-StubFrontendPort {
    $value = Resolve-ComposeValue -Key "FRONTEND_PORT"
    if (-not [string]::IsNullOrEmpty($value)) { return $value }
    return "3000"
}

# Stand in for the external commands the installer shells out to.
$script:StubStackUp = $false
function docker {
    $global:LASTEXITCODE = 0
    $args_ = @($args)
    if ($args_.Count -ge 1 -and $args_[0] -eq "info") { return }
    if ($args_.Count -ge 1 -and $args_[0] -eq "compose") {
        if ($args_ -contains "up") {
            $script:StubStackUp = $true
            return
        }
        if ($args_ -contains "config") {
            # Opt-in, so the port matrix never probes real host ports.
            if (-not $env:MULTICA_TEST_COMPOSE_CONFIG) {
                $global:LASTEXITCODE = 1
                return
            }
            $config = @{
                services = @{
                    backend  = @{ ports = @(@{ mode = "ingress"; host_ip = "127.0.0.1"; target = 8080; published = "$(Get-StubBackendPort)"; protocol = "tcp" }) }
                    frontend = @{ ports = @(@{ mode = "ingress"; host_ip = "127.0.0.1"; target = 3000; published = "$(Get-StubFrontendPort)"; protocol = "tcp" }) }
                }
            }
            return (($config | ConvertTo-Json -Depth 6) -split "`n")
        }
        if ($args_ -contains "port") {
            # Nothing is published until `up`, unless the case models this
            # installation's own stack already running (an upgrade re-run).
            if (-not $script:StubStackUp -and -not $env:MULTICA_TEST_STACK_RUNNING) {
                $global:LASTEXITCODE = 1
                return
            }
            if ($args_ -contains "backend") { return "127.0.0.1:$(Get-StubBackendPort)" }
            if ($args_ -contains "frontend") { return "127.0.0.1:$(Get-StubFrontendPort)" }
            $global:LASTEXITCODE = 1
            return
        }
        if ($args_ -contains "version") { return "2.30.0" }
    }
    return
}

function git { $global:LASTEXITCODE = 0 }

# Record every probed URL instead of making a request.
function Invoke-WebRequest {
    param(
        [Parameter(Position = 0)][string]$Uri,
        [switch]$UseBasicParsing,
        [int]$TimeoutSec
    )
    Add-Content -Path $ProbeLog -Value $Uri
    return [pscustomobject]@{ StatusCode = 200 }
}

# The CLI install itself is out of scope for the port contract.
function Install-Cli { }
function Test-Docker { }

Start-LocalInstall
'@ | Set-Content -Path $runnerScript -Encoding UTF8

# Runs install.ps1 for one case in a child process and returns its exit code,
# rendered output and probe log. Ambient environment comes from $case.Env only.
function Invoke-InstallerCase {
    param([hashtable]$Case)

    $workDir = Join-Path ([System.IO.Path]::GetTempPath()) ("multica-ps1-" + [guid]::NewGuid().ToString("N"))
    New-Item -ItemType Directory -Path (Join-Path $workDir ".git") -Force | Out-Null

    $envLines = @(
        "PORT=8080"
        "# BACKEND_PORT=8080"
        "# API_PORT=8080"
        "# SERVER_PORT=8080"
        "FRONTEND_PORT=3000"
        "JWT_SECRET=change-me-in-production"
        "POSTGRES_PASSWORD=multica"
    )
    if ($Case.Mutation) {
        foreach ($key in $Case.Mutation.Keys) {
            $value = $Case.Mutation[$key]
            $envLines = $envLines | ForEach-Object {
                if ($_ -match "^#?\s*$key=") { "$key=$value" } else { $_ }
            }
        }
    }
    $envLines | Set-Content -Path (Join-Path $workDir ".env") -Encoding UTF8
    Copy-Item (Join-Path $workDir ".env") (Join-Path $workDir ".env.example")
    New-Item -ItemType File -Path (Join-Path $workDir "docker-compose.selfhost.yml") -Force | Out-Null

    $probeLog = Join-Path $workDir "probes.log"
    New-Item -ItemType File -Path $probeLog -Force | Out-Null

    # Control the ambient environment explicitly so a CI runner's own PORT
    # cannot leak into the matrix. Absent must mean absent: an empty string is a
    # different input, and the chain treats it as "set but no value".
    $caseVars = @("PORT", "BACKEND_PORT", "API_PORT", "SERVER_PORT", "FRONTEND_PORT", "MULTICA_TEST_COMPOSE_CONFIG", "MULTICA_TEST_STACK_RUNNING")
    $previous = @{}
    foreach ($key in $caseVars) {
        $previous[$key] = [System.Environment]::GetEnvironmentVariable($key)
        if ($Case.Env.ContainsKey($key)) {
            Set-Item -Path "Env:$key" -Value $Case.Env[$key]
        } else {
            Remove-Item -Path "Env:$key" -ErrorAction SilentlyContinue
        }
    }

    try {
        $output = & $PowerShellPath -NoProfile -File $runnerScript -InstallerPath $InstallerPath -WorkDir $workDir -ProbeLog $probeLog 2>&1
        $exitCode = $LASTEXITCODE
    } finally {
        foreach ($key in $caseVars) {
            if ($null -eq $previous[$key]) {
                Remove-Item -Path "Env:$key" -ErrorAction SilentlyContinue
            } else {
                Set-Item -Path "Env:$key" -Value $previous[$key]
            }
        }
    }

    $probes = @(Get-Content $probeLog)
    Remove-Item $workDir -Recurse -Force -ErrorAction SilentlyContinue
    return @{ ExitCode = $exitCode; Output = ($output | Out-String); Probes = $probes }
}

function Assert-PublishedPorts {
    param([hashtable]$Case, [hashtable]$Result)

    $rendered = $Result.Output
    if ($Result.ExitCode -ne 0) {
        Write-Host $rendered
        Fail-Test "[$($Case.Label)] install.ps1 exited $($Result.ExitCode)"
    }

    $probed = ($Result.Probes | Select-Object -First 1)
    $probedPort = if ($probed -match "localhost:(\d+)/health") { $Matches[1] } else { $null }
    $printedBackend = if ($rendered -match "Backend:\s+http://localhost:(\d+)") { $Matches[1] } else { $null }
    $printedFrontend = if ($rendered -match "Frontend:\s+http://localhost:(\d+)") { $Matches[1] } else { $null }

    if ($probedPort -ne $Case.Backend -or $printedBackend -ne $Case.Backend -or $printedFrontend -ne $Case.Frontend) {
        Write-Host $rendered
        Write-Host "  compose published:   backend=$($Case.Backend) frontend=$($Case.Frontend)"
        Write-Host "  health check probed: $probedPort"
        Write-Host "  printed:             backend=$printedBackend frontend=$printedFrontend"
        Fail-Test "[$($Case.Label)] installer ports disagree with the port Compose published"
    }
}

foreach ($case in $cases) {
    Assert-PublishedPorts -Case $case -Result (Invoke-InstallerCase -Case $case)
}

# ---------------------------------------------------------------------------
# 4. A host port that is already taken stops the installer before anything is
#    pulled or started, with a fix the user can act on (#883). The port comes
#    from Compose's own resolution, so an ambient override is what gets checked.
# ---------------------------------------------------------------------------
$listener = New-Object System.Net.Sockets.TcpListener ([System.Net.IPAddress]::Loopback, 0)
$listener.Start()
try {
    $held = "$($listener.LocalEndpoint.Port)"

    $occupiedCases = @(
        @{ Label = "env-file PORT"; Env = @{ MULTICA_TEST_COMPOSE_CONFIG = "1" }; Mutation = @{ PORT = $held }; Setting = "PORT=<free port>" }
        @{ Label = "ambient PORT beats .env"; Env = @{ MULTICA_TEST_COMPOSE_CONFIG = "1"; PORT = $held }; Mutation = $null; Setting = "PORT=<free port>" }
        @{ Label = "env-file BACKEND_PORT"; Env = @{ MULTICA_TEST_COMPOSE_CONFIG = "1" }; Mutation = @{ BACKEND_PORT = $held }; Setting = "PORT=<free port>" }
        @{ Label = "env-file FRONTEND_PORT"; Env = @{ MULTICA_TEST_COMPOSE_CONFIG = "1" }; Mutation = @{ FRONTEND_PORT = $held }; Setting = "FRONTEND_PORT=<free port>" }
    )
    foreach ($case in $occupiedCases) {
        $result = Invoke-InstallerCase -Case $case
        if ($result.ExitCode -eq 0) {
            Write-Host $result.Output
            Fail-Test "[$($case.Label)] installer must not continue when port $held is already in use"
        }
        if ($result.Output -notmatch [regex]::Escape("Port $held is already in use") -or
            $result.Output -notmatch [regex]::Escape($case.Setting)) {
            Write-Host $result.Output
            Fail-Test "[$($case.Label)] expected an actionable occupied-port error naming $held and $($case.Setting)"
        }
        if ($result.Probes.Count -gt 0 -or $result.Output -match "Starting Multica services") {
            Write-Host $result.Output
            Fail-Test "[$($case.Label)] installer started the stack despite the occupied port"
        }
    }

    # Re-running over this installation's own running stack: the port is busy
    # only because Compose already publishes it for us, which is not a conflict.
    $ownStack = @{ Label = "own running stack"; Env = @{ MULTICA_TEST_COMPOSE_CONFIG = "1"; MULTICA_TEST_STACK_RUNNING = "1" }; Mutation = @{ PORT = $held }; Backend = $held; Frontend = "3000" }
    Assert-PublishedPorts -Case $ownStack -Result (Invoke-InstallerCase -Case $ownStack)
} finally {
    $listener.Stop()
}

# A free port goes straight through the preflight.
$probe = New-Object System.Net.Sockets.TcpListener ([System.Net.IPAddress]::Loopback, 0)
$probe.Start()
$free = "$($probe.LocalEndpoint.Port)"
$probe.Stop()
$freeCase = @{ Label = "free port"; Env = @{ MULTICA_TEST_COMPOSE_CONFIG = "1" }; Mutation = @{ PORT = $free }; Backend = $free; Frontend = "3000" }
Assert-PublishedPorts -Case $freeCase -Result (Invoke-InstallerCase -Case $freeCase)

Remove-Item $runnerScript -Force -ErrorAction SilentlyContinue
Write-Host "install.ps1 tests passed" -ForegroundColor Green
