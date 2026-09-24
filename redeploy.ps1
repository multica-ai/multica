<#
.SYNOPSIS
    Multica 一键重新部署脚本

.DESCRIPTION
    从源码构建镜像并替换运行中的 Multica 容器栈（PostgreSQL + 后端 + 前端）。
    支持两种模式：从源码构建（默认）或拉取官方镜像。

.PARAMETER BackendPort
    后端 API 宿主机映射端口，默认 8080

.PARAMETER FrontendPort
    前端 Next.js 宿主机映射端口，默认 3000

.PARAMETER PostgresPort
    PostgreSQL 宿主机映射端口，默认 5432

.PARAMETER ImageTag
    官方镜像 tag，默认 latest（仅 -Pull 模式使用）

.PARAMETER Pull
    拉取官方镜像启动，不从源码构建

.PARAMETER NoBuildCache
    构建镜像时使用 --no-cache，调试编译问题时打开

.PARAMETER SkipBuild
    跳过镜像构建/拉取，直接用现有镜像重启容器

.PARAMETER EnvFile
    环境变量文件路径，默认 .env

.EXAMPLE
    .\redeploy.ps1
    默认全流程：从源码构建镜像 + 替换容器

.EXAMPLE
    .\redeploy.ps1 -Pull
    拉取官方 GHCR 镜像启动（需网络通畅）

.EXAMPLE
    .\redeploy.ps1 -SkipBuild
    跳过构建/拉取，仅用现有镜像重新部署容器

.EXAMPLE
    .\redeploy.ps1 -NoBuildCache
    构建不使用缓存，输出完整编译日志，用于排错

.EXAMPLE
    .\redeploy.ps1 -BackendPort 9090 -FrontendPort 3030
    使用自定义端口
#>

[CmdletBinding()]
param(
    [int]   $BackendPort  = 8080,
    [int]   $FrontendPort = 3030,
    [int]   $PostgresPort = 5432,
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

$scriptRoot = Split-Path -Parent $MyInvocation.MyCommand.Path
Set-Location $scriptRoot

$ComposeFile     = "docker-compose.selfhost.yml"
$ComposeBuildArg = @("-f", $ComposeFile, "-f", "docker-compose.selfhost.build.yml")

# ---------- 1. 前置检查 ----------
Write-Step "前置检查"

# 检查 docker
if (-not (Get-Command docker -ErrorAction SilentlyContinue)) {
    Write-Err "未找到 docker 命令，请确认 Docker Desktop 已启动"
    exit 1
}
Write-Ok "docker 已就绪"

# 检查 docker compose（CLI plugin）
$composeVersion = $null
try {
    $composeVersion = & docker compose version --short 2>&1
    if ($LASTEXITCODE -ne 0) { throw }
} catch {
    Write-Err "未找到 docker compose (CLI plugin)，请安装 Docker Compose"
    Write-Host "  参考: https://docs.docker.com/compose/install/" -ForegroundColor Gray
    exit 1
}
if ($composeVersion -match "^v?1\.") {
    Write-Err "检测到 Docker Compose v1 ($composeVersion)，需要 v2+ (docker compose 插件)"
    exit 1
}
Write-Ok "docker compose $composeVersion 已就绪"

# 检查必要文件
if (-not (Test-Path $ComposeFile)) {
    Write-Err "未在当前目录找到 $ComposeFile"
    exit 1
}
Write-Ok "Compose 文件已定位"

if (-not (Test-Path "Dockerfile")) {
    Write-Err "未在当前目录找到 Dockerfile"
    exit 1
}
Write-Ok "Dockerfile 已定位"

# ---------- 2. 环境变量 ----------
Write-Step "检查环境变量: $EnvFile"

if (-not (Test-Path $EnvFile)) {
    if (Test-Path ".env.example") {
        Write-Warn2 "$EnvFile 不存在，将从 .env.example 创建"
        Copy-Item ".env.example" $EnvFile

        # 生成随机密钥
        $jwt     = -join ((1..32) | ForEach-Object { '{0:x2}' -f (Get-Random -Max 256) })
        $pgPass  = -join ((1..24) | ForEach-Object { '{0:x2}' -f (Get-Random -Max 256) })
        $vcsKey  = [Convert]::ToBase64String((1..32 | ForEach-Object { Get-Random -Max 256 }))

        $content = Get-Content $EnvFile -Raw
        $content = $content -replace "^JWT_SECRET=.*", "JWT_SECRET=$jwt"
        $content = $content -replace "^POSTGRES_PASSWORD=.*", "POSTGRES_PASSWORD=$pgPass"
        $content = $content -replace "^(DATABASE_URL=postgres://[^:]+:)[^@]*(@.*)", "`$1$pgPass`$2"
        $content = $content -replace "^MULTICA_VCS_SECRET_KEY=.*", "MULTICA_VCS_SECRET_KEY=$vcsKey"
        Set-Content -Path $EnvFile -Value $content

        Write-Ok "已生成随机 JWT_SECRET, POSTGRES_PASSWORD, MULTICA_VCS_SECRET_KEY"
    } else {
        Write-Err "$EnvFile 不存在，且未找到 .env.example 模板"
        exit 1
    }
}
Write-Ok "环境变量文件: $EnvFile"

# ---------- 3. 构建/拉取镜像 ----------
if ($SkipBuild) {
    Write-Step "跳过镜像构建/拉取 (-SkipBuild)"
} elseif ($Pull) {
    # ---------- 3a. 拉取官方镜像 ----------
    Write-Step "拉取官方镜像 (tag: $ImageTag)"

    $pullArgs = @("-f", $ComposeFile, "pull")
    & docker @pullArgs
    if ($LASTEXITCODE -ne 0) {
        Write-Err "镜像拉取失败 (exit $LASTEXITCODE)"
        Write-Host "  如果 tag '$ImageTag' 尚未发布，请尝试从源码构建: .\redeploy.ps1" -ForegroundColor Gray
        exit 1
    }
    Write-Ok "镜像拉取成功"
} else {
    # ---------- 3b. 从源码构建 ----------
    Write-Step "从源码构建镜像"

    $version = git describe --tags --match 'v[0-9]*' --always --dirty 2>$null
    if (-not $version) { $version = "dev" }
    $commit = git rev-parse --short HEAD 2>$null
    if (-not $commit) { $commit = "unknown" }
    $date = (Get-Date).ToUniversalTime().ToString("yyyy-MM-ddTHH:mm:ssZ")

    $env:VERSION = $version
    $env:COMMIT  = $commit
    $env:DATE    = $date

    Write-Host "  VERSION=$version  COMMIT=$commit" -ForegroundColor Gray
    Write-Host "  构建 Go 后端 + Next.js 前端，可能需要几分钟..." -ForegroundColor Gray

    $buildArgs = $ComposeBuildArg + @("build")
    if ($NoBuildCache) {
        $buildArgs += "--no-cache"
    }

    & docker compose @buildArgs
    if ($LASTEXITCODE -ne 0) {
        Write-Err "镜像构建失败 (exit $LASTEXITCODE)"
        exit 1
    }
    Write-Ok "镜像构建成功"

    # 清理临时环境变量
    Remove-Item Env:VERSION -ErrorAction SilentlyContinue
    Remove-Item Env:COMMIT  -ErrorAction SilentlyContinue
    Remove-Item Env:DATE    -ErrorAction SilentlyContinue
}

# ---------- 4. 停止并移除旧容器 ----------
Write-Step "处理旧容器"

$downArgs = @()
if (-not $SkipBuild -and -not $Pull) {
    $downArgs = $ComposeBuildArg
} else {
    $downArgs = @("-f", $ComposeFile)
}
$downArgs += "down"

& docker compose @downArgs
if ($LASTEXITCODE -ne 0) {
    Write-Warn2 "docker compose down 返回非 0，继续启动..."
}
Write-Ok "旧容器已停止"

# ---------- 5. 启动新容器 ----------
Write-Step "启动新容器"

# 覆盖端口环境变量供 Compose 使用
$env:BACKEND_PORT  = [string]$BackendPort
$env:FRONTEND_PORT = [string]$FrontendPort
$env:POSTGRES_PORT = [string]$PostgresPort

$upArgs = @("-f", $ComposeFile, "up", "-d")
& docker compose @upArgs
if ($LASTEXITCODE -ne 0) {
    Write-Err "容器启动失败 (exit $LASTEXITCODE)"
    exit 1
}
Write-Ok "容器已启动"

# 清理临时环境变量
Remove-Item Env:BACKEND_PORT  -ErrorAction SilentlyContinue
Remove-Item Env:FRONTEND_PORT -ErrorAction SilentlyContinue
Remove-Item Env:POSTGRES_PORT -ErrorAction SilentlyContinue

# ---------- 6. 健康检查 ----------
Write-Step "健康检查"

Start-Sleep -Seconds 5

# 检查所有容器状态
$services = @("multica-postgres-1", "multica-backend-1", "multica-frontend-1")
$allHealthy = $true

foreach ($svc in $services) {
    $status = & docker inspect $svc --format "{{.State.Status}}" 2>$null
    if ($LASTEXITCODE -ne 0 -or $status -ne "running") {
        Write-Err "容器状态异常: $svc -> $status"
        $allHealthy = $false
    } else {
        Write-Ok "容器运行中: $svc"
    }
}

# 等待后端 API 就绪（最多 60 秒）
Write-Host ""
Write-Host "  等待后端 API 就绪..." -ForegroundColor Gray
$probeOk = $false
for ($i = 1; $i -le 12; $i++) {
    try {
        $resp = Invoke-WebRequest -Uri "http://localhost:$BackendPort/healthz" -UseBasicParsing -TimeoutSec 3 -ErrorAction Stop
        if ($resp.StatusCode -eq 200) {
            $probeOk = $true
            break
        }
    } catch {
        Write-Host "  等待中... ($i/12)" -ForegroundColor Gray
        Start-Sleep -Seconds 5
    }
}

if ($probeOk) {
    Write-Ok "后端 API 探针成功: http://localhost:$BackendPort/healthz"
} else {
    Write-Warn2 "后端 API 探针未通过（服务可能仍在启动中）"
    Write-Host "  可稍后重试: curl http://localhost:$BackendPort/healthz" -ForegroundColor Gray
    Write-Host "  查看日志:   docker compose -f $ComposeFile logs backend --tail 50" -ForegroundColor Gray
}

# 等待前端就绪（最多 30 秒）
Write-Host ""
Write-Host "  等待前端就绪..." -ForegroundColor Gray
$feOk = $false
for ($i = 1; $i -le 6; $i++) {
    try {
        $resp = Invoke-WebRequest -Uri "http://localhost:$FrontendPort" -UseBasicParsing -TimeoutSec 3 -ErrorAction Stop
        if ($resp.StatusCode -eq 200 -or $resp.StatusCode -eq 301 -or $resp.StatusCode -eq 302) {
            $feOk = $true
            break
        }
    } catch {
        Write-Host "  等待中... ($i/6)" -ForegroundColor Gray
        Start-Sleep -Seconds 5
    }
}

if ($feOk) {
    Write-Ok "前端探针成功: http://localhost:$FrontendPort"
} else {
    Write-Warn2 "前端探针未通过（服务可能仍在启动中）"
}

# ---------- 7. 总结 ----------
Write-Step "部署完成"

$containerInfo = & docker compose -f $ComposeFile ps --format "table {{.Name}}\t{{.Image}}\t{{.Status}}\t{{.Ports}}"
Write-Host $containerInfo

Write-Host ""
Write-Host "  模式:        $(if ($Pull) { '官方镜像 (tag: ' + $ImageTag + ')' } elseif ($SkipBuild) { '跳过构建 (现有镜像)' } else { '源码构建' })" -ForegroundColor White
Write-Host "  后端 API:    http://localhost:$BackendPort" -ForegroundColor White
Write-Host "  前端 Web:    http://localhost:$FrontendPort" -ForegroundColor White
Write-Host "  PostgreSQL:  localhost:$PostgresPort" -ForegroundColor White
Write-Host ""
Write-Host "  查看日志:    docker compose -f $ComposeFile logs -f" -ForegroundColor White
Write-Host "  停止服务:    docker compose -f $ComposeFile down" -ForegroundColor White
Write-Host ""
