# Fork install entry point — skips Homebrew and delegates to install.ps1.
#
# Repo identity resolution (first match wins):
#   1. MULTICA_GITHUB_REPO already set in the environment
#   2. Positional arg: ./install-fork.ps1 owner/repo
#   3. Derived from `git remote get-url origin` in a local checkout
#   4. Thin fork overlay: $ForkDefaultGithubRepo below (empty = require 1–3)
#
# Usage (any fork — preferred):
#   $env:MULTICA_GITHUB_REPO='owner/repo'; irm https://raw.githubusercontent.com/owner/repo/main/scripts/install-fork.ps1 | iex
#
# Local clone:
#   ./scripts/install-fork.ps1

$ErrorActionPreference = "Stop"

# Thin fork overlay for bare `irm | iex` without MULTICA_GITHUB_REPO.
# Leave empty when contributing upstreamable patches; set to this fork's
# owner/repo so existing one-liners keep working.
$ForkDefaultGithubRepo = "Git-on-my-level/multica"

$env:MULTICA_SKIP_BREW = "1"
if (-not $env:MULTICA_CLI_REF) {
    $env:MULTICA_CLI_REF = "main"
}
if (-not $env:MULTICA_GITHUB_BRANCH) {
    $env:MULTICA_GITHUB_BRANCH = "main"
}

function Get-RepoFromGitRemote {
    param([string]$Dir)
    if (-not (Get-Command git -ErrorAction SilentlyContinue)) {
        return $null
    }
    try {
        $url = & git -C $Dir remote get-url origin 2>$null
    } catch {
        return $null
    }
    if (-not $url) {
        return $null
    }
    if ($url -match 'github\.com[:/]([^/]+)/([^/.]+)') {
        return "$($Matches[1])/$($Matches[2])"
    }
    return $null
}

$forwardArgs = @()
if (-not $env:MULTICA_GITHUB_REPO -and $args.Count -gt 0 -and $args[0] -match '^[^/]+/[^/]+$' -and $args[0] -notlike '-*') {
    $env:MULTICA_GITHUB_REPO = $args[0]
    if ($args.Count -gt 1) {
        $forwardArgs = $args[1..($args.Count - 1)]
    }
} else {
    $forwardArgs = $args
}

if (-not $env:MULTICA_GITHUB_REPO) {
    if ($PSScriptRoot) {
        $repoRoot = Split-Path -Parent $PSScriptRoot
        $derived = Get-RepoFromGitRemote -Dir $repoRoot
        if ($derived) {
            $env:MULTICA_GITHUB_REPO = $derived
        }
    }
}

if (-not $env:MULTICA_GITHUB_REPO -and $ForkDefaultGithubRepo) {
    $env:MULTICA_GITHUB_REPO = $ForkDefaultGithubRepo
    Write-Host "note: using fork default MULTICA_GITHUB_REPO=$($env:MULTICA_GITHUB_REPO) (set the env var to override)"
}

if (-not $env:MULTICA_GITHUB_REPO) {
    Write-Error @"
could not determine the GitHub repo for this fork.
Set MULTICA_GITHUB_REPO=owner/repo and re-run, for example:
  `$env:MULTICA_GITHUB_REPO='owner/repo'; irm https://raw.githubusercontent.com/owner/repo/main/scripts/install-fork.ps1 | iex
"@
    exit 1
}

$repo = $env:MULTICA_GITHUB_REPO
$branch = $env:MULTICA_GITHUB_BRANCH

if ($PSScriptRoot -and (Test-Path (Join-Path $PSScriptRoot "install.ps1"))) {
    & (Join-Path $PSScriptRoot "install.ps1") @forwardArgs
    exit $LASTEXITCODE
}

# irm .../install-fork.ps1 | iex — download install.ps1 from the fork repo.
$tmp = Join-Path ([System.IO.Path]::GetTempPath()) "multica-install.ps1"
try {
    Invoke-WebRequest -Uri "https://raw.githubusercontent.com/$repo/$branch/scripts/install.ps1" -OutFile $tmp -UseBasicParsing
    & $tmp @forwardArgs
    exit $LASTEXITCODE
} finally {
    if (Test-Path $tmp) {
        Remove-Item $tmp -Force -ErrorAction SilentlyContinue
    }
}
