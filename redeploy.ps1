<#
.SYNOPSIS
    Multica 一键部署/重新部署脚本（Windows PowerShell，等价于 make selfhost / make selfhost-build）

.DESCRIPTION
    以 Docker Compose 部署 Multica 自托管栈（PostgreSQL + 后端 + 前端），
    行为与官方 Makefile 目标保持一致：

    - 默认（源码构建）：等价于 `make selfhost-build`
        docker compose -f docker-compose.selfhost.yml -f docker-compose.selfhost.build.yml up -d --build
        使用本地 checkout 构建的 multica-backend:dev / multica-web:dev 镜像。
    - -Pull（拉取官方镜像）：等价于 `make selfhost`
        docker compose -f docker-compose.selfhost.yml pull，然后 up -d
        使用 GHCR 官方镜像（multica-backend:latest / multica-web:latest）。
    - -SkipBuild：跳过构建/拉取，直接用现有镜像启动（等价于 docker compose up -d）。

    端口、密钥等配置全部来自 .env（与 make 一致），脚本不注入任何默认端口覆盖。

.PARAMETER BackendPort
    可选：覆盖后端宿主机发布端口（等价于 make ... PORT=<port>）。
    不传时使用 .env 中的 PORT/BACKEND_PORT/API_PORT/SERVER_PORT（按顺序取）。

.PARAMETER FrontendPort
    可选：覆盖前端宿主机发布端口（等价于 make ... FRONTEND_PORT=<port>）。
    不传时使用 .env 中的 FRONTEND_PORT。

.PARAMETER PostgresPort
    仅用于本地 dev compose（docker-compose.yml），selfhost 栈不下发 postgres 端口，可忽略。

.PARAMETER ImageTag
    官方镜像 tag，默认 latest（仅 -Pull 模式有效，对应 MULTICA_IMAGE_TAG）。

.PARAMETER Pull
    拉取官方 GHCR 镜像启动，等价于 `make selfhost`。默认关闭 = 从源码构建，等价于 `make selfhost-build`。

.PARAMETER NoBuildCache
    构建时使用 --no-cache，调试源码编译问题打开。

.PARAMETER SkipBuild
    跳过镜像构建/拉取，直接用现有镜像重启容器。

.PARAMETER EnvFile
    环境变量文件，默认 .env。

.EXAMPLE
    .\redeploy.ps1                     # 等价 make selfhost-build：源码构建 + 部署
    .\redeploy.ps1 -Pull               # 等价 make selfhost：拉取官方镜像 + 部署
    .\redeploy.ps1 -SkipBuild          # 仅用现有镜像重启容器
    .\redeploy.ps1 -NoBuildCache       # 无缓存构建（排错用）
    .\redeploy.ps1 -BackendPort 9090   # 覆盖后端端口（= make ... PORT=9090）
#>

[CmdletBinding()]
param(
    [int]   $BackendPort  = 0,
    [int]   $FrontendPort = 0,
    [int]   $PostgresPort = 0,
    [string]$ImageTag     = "latest",
    [switch]$Pull,
    [switch]$NoBuildCache,
    [switch]$SkipBuild,
    [string]$EnvFile      = ".env"
)

$ErrorActionPreference = "Stop"
[Net.ServicePointManager]::SecurityProtocol = [Net.SecurityProtocolType]::Tls12

# ---------- 辅助函数 ----------
function Write-Step  { param($msg) Write-Host "`n[STEP] $msg" -ForegroundColor Cyan }
function Write-Ok    { param($msg) Write-Host "  [OK] $msg" -ForegroundColor Green }
function Write-Warn2 { param($msg) Write-Host "  [!!] $msg" -ForegroundColor Yellow }
function Write-Err   { param($msg) Write-Host "  [X]  $msg" -ForegroundColor Red }

# 加密安全随机 hex（等价 openssl rand -hex N）
function New-RandomHex {
    param([int]$ByteCount)
    $bytes = New-Object byte[] $ByteCount
    $rng = [System.Security.Cryptography.RandomNumberGenerator]::Create()
    try { $rng.GetBytes($bytes) } finally { $rng.Dispose() }
    return -join ($bytes | ForEach-Object { $_.ToString("x2") })
}

# 加密安全随机 base64（等价 openssl rand -base64 N）
function New-RandomBase64 {
    param([int]$ByteCount)
    $bytes = New-Object byte[] $ByteCount
    $rng = [System.Security.Cryptography.RandomNumberGenerator]::Create()
    try { $rng.GetBytes($bytes) } finally { $rng.Dispose() }
    return [Convert]::ToBase64String($bytes)
}

# 从 .env 读取键值（跳过注释行），返回 $null 表示未设置
function Read-DotEnvValue {
    param([string]$Path, [string]$Key)
    if (-not (Test-Path $Path)) { return $null }
    $line = Get-Content $Path | Where-Object { $_ -match "^$Key=" } | Select-Object -First 1
    if (-not $line) { return $null }
    $val = ($line -split "=", 2)[1].Trim()
    $val = $val.Trim('"', "'")
    if ($val -eq "") { return $null }
    return $val
}

# Compose 实际发布的宿主机端口 —— 唯一权威来源（与 scripts/selfhost-wait.sh 一致）
function Get-ComposePublishedPort {
    param([string]$Service, [int]$ContainerPort)
    $out = & docker compose $script:fileArgs port $Service $ContainerPort 2>$null
    if (-not $out) { return $null }
    $line = @($out | Where-Object { $_ }) | Select-Object -Last 1
    $published = ($line -split ":")[-1].Trim()
    if ($published -match "^\d+$") { return [int]$published }
    return $null
}

$scriptRoot = Split-Path -Parent $MyInvocation.MyCommand.Path
Set-Location $scriptRoot

# ---------- Compose 文件集（与 Makefile 完全一致） ----------
$selfhostYml = "docker-compose.selfhost.yml"
$buildYml    = "docker-compose.selfhost.build.yml"

# 构建模式：双文件（镜像引用 = :dev） / 拉取模式：单文件（镜像引用 = GHCR 官方）
$script:composeFiles = @($selfhostYml)
$script:fileArgs = @("-f", $selfhostYml)
$buildMode = $false

if ($SkipBuild) {
    Write-Step "跳过镜像构建/拉取 (-SkipBuild)，使用现有镜像"
} elseif ($Pull) {
    Write-Step "拉取官方镜像模式 (-Pull)，等价 make selfhost"
} else {
    $buildMode = $true
    Write-Step "源码构建模式，等价 make selfhost-build"
    $script:composeFiles = @($selfhostYml, $buildYml)
    $script:fileArgs = @("-f", $selfhostYml, "-f", $buildYml)
}

# ---------- 1. 前置检查 ----------
Write-Step "前置检查"

if (-not (Get-Command docker -ErrorAction SilentlyContinue)) {
    Write-Err "未找到 docker 命令，请确认 Docker Desktop 已启动"
    exit 1
}
Write-Ok "docker 已就绪"

$composeVersion = $null
try {
    $composeVersion = & docker compose version --short 2>&1
    if ($LASTEXITCODE -ne 0) { throw }
} catch {
    Write-Err "未找到 docker compose (CLI plugin)，请安装 Docker Compose"
    exit 1
}
if ($composeVersion -match "^v?1\.") {
    Write-Err "检测到 Docker Compose v1 ($composeVersion)，需要 v2+ (docker compose 插件)"
    exit 1
}
Write-Ok "docker compose $composeVersion 已就绪"

foreach ($f in $script:composeFiles) {
    if (-not (Test-Path $f)) {
        Write-Err "未找到 Compose 文件: $f"
        exit 1
    }
}
Write-Ok "Compose 文件已定位: $($script:composeFiles -join ', ')"

# ---------- 2. 环境变量（对齐 make：无 .env 则从模板生成随机密钥） ----------
Write-Step "检查环境变量: $EnvFile"

if (-not (Test-Path $EnvFile)) {
    if (Test-Path ".env.example") {
        Write-Warn2 "$EnvFile 不存在，将从 .env.example 创建（等价 make 的行为）"
        Copy-Item ".env.example" $EnvFile

        $jwt    = New-RandomHex 32
        $pgPass = New-RandomHex 24
        $vcsKey = New-RandomBase64 32

        $content = Get-Content $EnvFile -Raw
        $content = $content -replace "^JWT_SECRET=.*", "JWT_SECRET=$jwt"
        $content = $content -replace "^POSTGRES_PASSWORD=.*", "POSTGRES_PASSWORD=$pgPass"
        $content = $content -replace "^(DATABASE_URL=postgres://[^:]+:)[^@]*(@.*)", "`$1$pgPass`$2"
        $content = $content -replace "^MULTICA_VCS_SECRET_KEY=.*", "MULTICA_VCS_SECRET_KEY=$vcsKey"
        Set-Content -Path $EnvFile -Value $content -NoNewline

        Write-Ok "已生成随机 JWT_SECRET, POSTGRES_PASSWORD, MULTICA_VCS_SECRET_KEY"
    } else {
        Write-Err "$EnvFile 不存在，且未找到 .env.example 模板"
        exit 1
    }
}

# 可选端口覆盖：仅当显式传入时注入（等价 make ... PORT=<port>）。
# 未传时完全交由 .env 决定，脚本不注入任何默认值（修复旧版覆盖 .env 的问题）。
$envOverrides = @()
if ($BackendPort -gt 0)  { $env:BACKEND_PORT  = [string]$BackendPort;  $envOverrides += "BACKEND_PORT=$BackendPort" }
if ($FrontendPort -gt 0) { $env:FRONTEND_PORT = [string]$FrontendPort; $envOverrides += "FRONTEND_PORT=$FrontendPort" }
if ($envOverrides.Count -gt 0) {
    Write-Ok "端口覆盖注入: $($envOverrides -join ', ')"
} else {
    Write-Ok "端口由 .env 决定（BACKEND_PORT/FRONTEND_PORT 链）"
}

# ---------- 3. 构建 / 拉取 / 直接启动 ----------
if ($buildMode) {
    Write-Step "从源码构建镜像并启动 (docker compose up -d --build)"

    # 对齐 Makefile：git 版本注入为 build args（VERSION/COMMIT/DATE）
    $version = git describe --tags --match 'v[0-9]*' --always --dirty 2>$null
    if (-not $version) { $version = "dev" }
    $commit = git rev-parse --short HEAD 2>$null
    if (-not $commit) { $commit = "unknown" }
    $date = (Get-Date).ToUniversalTime().ToString("yyyy-MM-ddTHH:mm:ssZ")

    $env:VERSION = $version
    $env:COMMIT  = $commit
    $env:DATE    = $date
    Write-Host "  VERSION=$version  COMMIT=$commit  DATE=$date" -ForegroundColor Gray
    Write-Host "  构建 Go 后端 + Next.js 前端（multica-backend:dev / multica-web:dev），可能需要几分钟..." -ForegroundColor Gray

    $upArgs = @("--env-file", $EnvFile) + $script:fileArgs + @("up", "-d", "--build")
    if ($NoBuildCache) { $upArgs += "--no-cache" }

    & docker compose @upArgs
    if ($LASTEXITCODE -ne 0) {
        Write-Err "镜像构建/启动失败 (exit $LASTEXITCODE)"
        exit 1
    }

    Remove-Item Env:VERSION -ErrorAction SilentlyContinue
    Remove-Item Env:COMMIT  -ErrorAction SilentlyContinue
    Remove-Item Env:DATE    -ErrorAction SilentlyContinue
} elseif ($Pull) {
    Write-Step "拉取官方镜像并启动 (等价 make selfhost)"

    $pullArgs = @("--env-file", $EnvFile, "-f", $selfhostYml, "pull")
    & docker compose @pullArgs
    if ($LASTEXITCODE -ne 0) {
        Write-Err "镜像拉取失败 (exit $LASTEXITCODE)"
        Write-Host "  如果 tag '$ImageTag' 尚未发布到 GHCR，请改用源码构建: .\redeploy.ps1" -ForegroundColor Gray
        exit 1
    }
    Write-Ok "镜像拉取成功"

    & docker compose @("--env-file", $EnvFile, "-f", $selfhostYml, "up", "-d")
    if ($LASTEXITCODE -ne 0) {
        Write-Err "容器启动失败 (exit $LASTEXITCODE)"
        exit 1
    }
} else {
    Write-Step "使用现有镜像重启容器"

    $upArgs = @("--env-file", $EnvFile) + $script:fileArgs + @("up", "-d")
    & docker compose @upArgs
    if ($LASTEXITCODE -ne 0) {
        Write-Err "容器启动失败 (exit $LASTEXITCODE)"
        exit 1
    }
}
Write-Ok "容器已启动"

# ---------- 4. 健康检查（对齐 scripts/selfhost-wait.sh：compose port + /health） ----------
Write-Step "健康检查"

# 从 docker compose port 读取真实发布端口（唯一权威）
$backendPort = Get-ComposePublishedPort -Service "backend" -ContainerPort 8080
$frontendPort = Get-ComposePublishedPort -Service "frontend" -ContainerPort 3000

if (-not $backendPort) {
    $backendPort = if ($env:BACKEND_PORT) { [int]$env:BACKEND_PORT } elseif (Read-DotEnvValue $EnvFile "BACKEND_PORT") { [int](Read-DotEnvValue $EnvFile "BACKEND_PORT") } elseif (Read-DotEnvValue $EnvFile "PORT") { [int](Read-DotEnvValue $EnvFile "PORT") } else { 8080 }
}
if (-not $frontendPort) {
    $frontendPort = if ($env:FRONTEND_PORT) { [int]$env:FRONTEND_PORT } elseif (Read-DotEnvValue $EnvFile "FRONTEND_PORT") { [int](Read-DotEnvValue $EnvFile "FRONTEND_PORT") } else { 3000 }
}

Write-Host "  后端发布端口: $backendPort  前端发布端口: $frontendPort" -ForegroundColor Gray

# 容器状态（compose ps -a，动态获取，不硬编码容器名）
$psJson = & docker compose $script:fileArgs ps -a --format json 2>$null | ConvertFrom-Json
$serviceStates = @{}
if ($psJson) {
    foreach ($c in @($psJson)) { $serviceStates[$c.Service] = $c.State }
}
foreach ($svc in @("postgres", "backend", "frontend")) {
    $state = $serviceStates[$svc]
    if ($state -eq "running") {
        Write-Ok "容器运行中: $svc"
    } else {
        Write-Err "容器状态异常: $svc -> $state"
    }
}

# 等待后端 /health 就绪（最多 60 秒，对齐官方 30x2s）
$healthOk = $false
Write-Host ""
Write-Host "  等待后端 /health 就绪..." -ForegroundColor Gray
for ($i = 1; $i -le 30; $i++) {
    try {
        $resp = Invoke-WebRequest -Uri "http://localhost:$backendPort/health" -UseBasicParsing -TimeoutSec 3 -ErrorAction Stop
        if ($resp.StatusCode -eq 200) { $healthOk = $true; break }
    } catch {
        Start-Sleep -Seconds 2
    }
}

if ($healthOk) {
    Write-Ok "后端健康检查通过: http://localhost:$backendPort/health"
} else {
    Write-Warn2 "后端 /health 未通过（服务可能仍在启动中）"
    Write-Host "  可稍后重试: curl http://localhost:$backendPort/health" -ForegroundColor Gray
    Write-Host "  查看日志:   docker compose -f $selfhostYml logs backend --tail 50" -ForegroundColor Gray
}

# 前端探针（可选，官方仅打印 URL；这里做一次非阻塞探测）
$feOk = $false
try {
    $resp = Invoke-WebRequest -Uri "http://localhost:$frontendPort" -UseBasicParsing -TimeoutSec 5 -ErrorAction Stop
    if ($resp.StatusCode -ge 200 -and $resp.StatusCode -lt 500) { $feOk = $true }
} catch { }

if ($feOk) {
    Write-Ok "前端可访问: http://localhost:$frontendPort"
} else {
    Write-Warn2 "前端探针未通过（服务可能仍在启动中）"
}

# ---------- 5. 总结 ----------
Write-Step "部署完成"

$psTable = & docker compose $script:fileArgs ps --format "table {{.Name}}`t{{.Image}}`t{{.Status}}`t{{.Ports}}"
Write-Host $psTable

Write-Host ""
if ($buildMode) {
    Write-Host "  模式:      源码构建 (multica-backend:dev / multica-web:dev) — 等价 make selfhost-build" -ForegroundColor White
} elseif ($Pull) {
    Write-Host "  模式:      官方镜像 (tag: $ImageTag) — 等价 make selfhost" -ForegroundColor White
} else {
    Write-Host "  模式:      现有镜像重启 (-SkipBuild)" -ForegroundColor White
}
Write-Host "  后端 API:  http://localhost:$backendPort" -ForegroundColor White
Write-Host "  前端 Web:  http://localhost:$frontendPort" -ForegroundColor White
Write-Host ""
Write-Host "  查看日志:  docker compose -f $selfhostYml logs -f" -ForegroundColor White
Write-Host "  停止服务:  docker compose -f $selfhostYml down" -ForegroundColor White
Write-Host ""
Write-Host "  下一步（连接 daemon）: 在宿主机安装 multica CLI 后运行 multica setup self-host" -ForegroundColor Gray
Write-Host ""