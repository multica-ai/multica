/**
 * Utility to check agent installations
 */
import { execSync } from 'node:child_process'
import { platform } from 'node:os'
import { DEFAULT_AGENTS } from '../config/defaults'

export interface AgentCheckResult {
  id: string
  name: string
  command: string
  installed: boolean
  path?: string
  version?: string
  installHint?: string
}

// Install hints for each agent
const INSTALL_HINTS: Record<string, string> = {
  'claude-code': 'npm install -g @zed-industries/claude-code-acp',
  opencode: 'go install github.com/anomalyco/opencode@latest',
  codex: 'npm install -g @openai/codex',
  gemini: 'npm install -g @google/gemini-cli',
}

/**
 * Check if a command exists in the system PATH
 */
export function commandExists(cmd: string): { exists: boolean; path?: string; version?: string } {
  const isWindows = platform() === 'win32'
  const whichCmd = isWindows ? 'where' : 'which'

  try {
    const path = execSync(`${whichCmd} ${cmd}`, {
      encoding: 'utf-8',
      stdio: ['pipe', 'pipe', 'pipe'],
    })
      .trim()
      .split('\n')[0]

    // Try to get version
    let version: string | undefined
    try {
      const versionOutput = execSync(`${cmd} --version`, {
        encoding: 'utf-8',
        stdio: ['pipe', 'pipe', 'pipe'],
        timeout: 5000,
      }).trim()
      // Extract first line or first meaningful part
      version = versionOutput.split('\n')[0].slice(0, 50)
    } catch {
      // Some commands don't support --version
    }

    return { exists: true, path, version }
  } catch {
    return { exists: false }
  }
}

/**
 * Check all configured agents and return their installation status
 */
export function checkAgents(): AgentCheckResult[] {
  const results: AgentCheckResult[] = []

  for (const [id, config] of Object.entries(DEFAULT_AGENTS)) {
    const check = commandExists(config.command)

    results.push({
      id,
      name: config.name,
      command: config.command,
      installed: check.exists,
      path: check.path,
      version: check.version,
      installHint: INSTALL_HINTS[id],
    })
  }

  return results
}
