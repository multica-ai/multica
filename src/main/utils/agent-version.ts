/**
 * Agent version checking utilities
 * Handles fetching installed versions and checking for updates
 */
import { exec } from 'node:child_process'
import { promisify } from 'node:util'
import { getEnhancedPath } from './path'

const execAsync = promisify(exec)

export interface CommandVersionInfo {
  command: string
  installedVersion?: string
  latestVersion?: string
  hasUpdate: boolean
}

export interface AgentVersionInfo {
  agentId: string
  commands: CommandVersionInfo[]
}

// Commands and how to get their versions
// For CLI tools: use --version flag
// For npm packages (ACP): use npm list -g
interface VersionSource {
  type: 'cli' | 'npm'
  command?: string // For CLI: the command to run with --version
  package?: string // For npm: the package name
}

const COMMAND_VERSION_SOURCES: Record<string, VersionSource> = {
  claude: { type: 'cli', command: 'claude' },
  'claude-code-acp': { type: 'npm', package: '@zed-industries/claude-code-acp' },
  opencode: { type: 'cli', command: 'opencode' },
  codex: { type: 'cli', command: 'codex' },
  'codex-acp': { type: 'npm', package: '@zed-industries/codex-acp' }
}

// Sources for checking latest versions
interface LatestVersionSource {
  type: 'npm' | 'github'
  package?: string // For npm
  repo?: string // For github (owner/repo)
}

const LATEST_VERSION_SOURCES: Record<string, LatestVersionSource> = {
  claude: { type: 'npm', package: '@anthropic-ai/claude-code' },
  'claude-code-acp': { type: 'npm', package: '@zed-industries/claude-code-acp' },
  opencode: { type: 'github', repo: 'anomalyco/opencode' },
  codex: { type: 'npm', package: '@openai/codex' },
  'codex-acp': { type: 'npm', package: '@zed-industries/codex-acp' }
}

// Cache for latest versions (avoid frequent network requests)
interface VersionCache {
  versions: Record<string, string>
  timestamp: number
}

let latestVersionCache: VersionCache | null = null
const CACHE_TTL_MS = 60 * 60 * 1000 // 1 hour

/**
 * Parse version string from CLI output
 * Handles formats like:
 * - "2.1.15 (Claude Code)" -> "2.1.15"
 * - "1.1.31" -> "1.1.31"
 * - "codex-cli 0.88.0" -> "0.88.0"
 */
function parseVersionFromOutput(output: string): string | undefined {
  const trimmed = output.trim()

  // Match version pattern (x.y.z or x.y.z-something)
  const versionMatch = trimmed.match(/(\d+\.\d+\.\d+(?:-[\w.]+)?)/)
  return versionMatch ? versionMatch[1] : undefined
}

/**
 * Get installed version of a CLI command
 */
async function getCliVersion(command: string): Promise<string | undefined> {
  const enhancedEnv = { ...process.env, PATH: getEnhancedPath() }

  try {
    const { stdout } = await execAsync(`${command} --version`, {
      env: enhancedEnv,
      timeout: 5000
    })
    return parseVersionFromOutput(stdout)
  } catch {
    return undefined
  }
}

/**
 * Get installed version of a global npm package
 */
async function getNpmPackageVersion(packageName: string): Promise<string | undefined> {
  const enhancedEnv = { ...process.env, PATH: getEnhancedPath() }

  try {
    const { stdout } = await execAsync(`npm list -g ${packageName} --depth=0 2>/dev/null`, {
      env: enhancedEnv,
      timeout: 10000
    })
    // Output format: "├── @zed-industries/claude-code-acp@0.13.1"
    const versionMatch = stdout.match(
      new RegExp(`${escapeRegExp(packageName)}@(\\d+\\.\\d+\\.\\d+)`)
    )
    return versionMatch ? versionMatch[1] : undefined
  } catch {
    return undefined
  }
}

/**
 * Escape special regex characters
 */
function escapeRegExp(string: string): string {
  return string.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')
}

/**
 * Get installed version for a command
 */
export async function getInstalledVersion(command: string): Promise<string | undefined> {
  const source = COMMAND_VERSION_SOURCES[command]
  if (!source) {
    return undefined
  }

  if (source.type === 'cli' && source.command) {
    return getCliVersion(source.command)
  } else if (source.type === 'npm' && source.package) {
    return getNpmPackageVersion(source.package)
  }

  return undefined
}

/**
 * Fetch JSON with timeout
 */
async function fetchJson<T>(
  url: string,
  options?: { timeoutMs?: number; headers?: Record<string, string> }
): Promise<T | undefined> {
  const timeoutMs = options?.timeoutMs ?? 10000
  const controller = new AbortController()
  const timeout = setTimeout(() => controller.abort(), timeoutMs)

  try {
    const response = await fetch(url, {
      headers: options?.headers,
      signal: controller.signal
    })
    if (!response.ok) {
      return undefined
    }
    return (await response.json()) as T
  } catch {
    return undefined
  } finally {
    clearTimeout(timeout)
  }
}

/**
 * Fetch latest version from npm registry
 */
async function fetchLatestNpmVersion(packageName: string): Promise<string | undefined> {
  const data = await fetchJson<{ 'dist-tags'?: { latest?: string } }>(
    `https://registry.npmjs.org/${packageName}`
  )
  return data?.['dist-tags']?.latest
}

/**
 * Fetch latest version from GitHub releases
 */
async function fetchLatestGithubVersion(repo: string): Promise<string | undefined> {
  const data = await fetchJson<{ tag_name?: string }>(
    `https://api.github.com/repos/${repo}/releases/latest`,
    {
      headers: {
        Accept: 'application/vnd.github+json',
        'User-Agent': 'multica'
      }
    }
  )
  if (!data?.tag_name) {
    return undefined
  }

  return data.tag_name.startsWith('v') ? data.tag_name.slice(1) : data.tag_name
}

/**
 * Fetch latest version for a command
 */
async function fetchLatestVersion(command: string): Promise<string | undefined> {
  const source = LATEST_VERSION_SOURCES[command]
  if (!source) {
    return undefined
  }

  if (source.type === 'npm' && source.package) {
    return fetchLatestNpmVersion(source.package)
  } else if (source.type === 'github' && source.repo) {
    return fetchLatestGithubVersion(source.repo)
  }

  return undefined
}

/**
 * Compare two semver versions
 * Returns true if latest > installed
 */
export function isNewerVersion(installed: string, latest: string): boolean {
  const installedParts = installed.split('.').map((p) => parseInt(p, 10) || 0)
  const latestParts = latest.split('.').map((p) => parseInt(p, 10) || 0)

  for (let i = 0; i < Math.max(installedParts.length, latestParts.length); i++) {
    const installedPart = installedParts[i] || 0
    const latestPart = latestParts[i] || 0
    if (latestPart > installedPart) return true
    if (latestPart < installedPart) return false
  }

  return false
}

/**
 * Get all latest versions (with caching)
 */
async function getAllLatestVersions(): Promise<Record<string, string>> {
  // Check cache
  if (latestVersionCache && Date.now() - latestVersionCache.timestamp < CACHE_TTL_MS) {
    return latestVersionCache.versions
  }

  // Fetch all latest versions in parallel
  const commands = Object.keys(LATEST_VERSION_SOURCES)
  const results = await Promise.all(
    commands.map(async (cmd) => {
      const version = await fetchLatestVersion(cmd)
      return { cmd, version }
    })
  )

  const versions: Record<string, string> = {}
  for (const { cmd, version } of results) {
    if (version) {
      versions[cmd] = version
    }
  }

  // Update cache
  latestVersionCache = {
    versions,
    timestamp: Date.now()
  }

  return versions
}

/**
 * Check versions for all commands of an agent
 */
export async function checkAgentVersions(
  _agentId: string,
  commands: string[]
): Promise<CommandVersionInfo[]> {
  // Get all latest versions (cached)
  const latestVersions = await getAllLatestVersions()

  // Check each command
  const results = await Promise.all(
    commands.map(async (command): Promise<CommandVersionInfo> => {
      const installedVersion = await getInstalledVersion(command)
      const latestVersion = latestVersions[command]

      return {
        command,
        installedVersion,
        latestVersion,
        hasUpdate:
          installedVersion && latestVersion
            ? isNewerVersion(installedVersion, latestVersion)
            : false
      }
    })
  )

  return results
}

/**
 * Clear the version cache (useful for forcing refresh)
 */
export function clearVersionCache(): void {
  latestVersionCache = null
}
