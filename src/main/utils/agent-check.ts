/**
 * Utility to check agent installations
 */
import { exec } from 'node:child_process'
import { promisify } from 'node:util'
import { platform } from 'node:os'
import { DEFAULT_AGENTS } from '../config/defaults'
import { getEnhancedPath } from './path'

const execAsync = promisify(exec)

export interface CommandInfo {
  command: string
  path?: string
}

export interface AgentCheckResult {
  id: string
  name: string
  command: string
  installed: boolean
  path?: string
  installHint?: string
  commands?: CommandInfo[]
}

// Install hints for each agent
const INSTALL_HINTS: Record<string, string> = {
  'claude-code': 'npm install -g @zed-industries/claude-code-acp',
  opencode: 'go install github.com/anomalyco/opencode@latest',
  codex: 'npm install -g @openai/codex',
  gemini: 'npm install -g @google/gemini-cli',
}

// Commands to check for each agent
const AGENT_COMMANDS: Record<string, string[]> = {
  'claude-code': ['claude', 'claude-code-acp'],
  opencode: ['opencode'],
  codex: ['codex', 'codex-acp'],
  gemini: ['gemini'],
}

/**
 * Check if a command exists in the system PATH (async for true concurrency)
 */
export async function commandExists(cmd: string): Promise<{ exists: boolean; path?: string }> {
  const whichCmd = platform() === 'win32' ? 'where' : 'which'
  const enhancedEnv = { ...process.env, PATH: getEnhancedPath() }

  try {
    const { stdout } = await execAsync(`${whichCmd} ${cmd}`, { env: enhancedEnv })
    return { exists: true, path: stdout.trim().split('\n')[0] }
  } catch {
    return { exists: false }
  }
}

/**
 * Check a single agent's installation status (async for true concurrency)
 */
export async function checkAgent(agentId: string): Promise<AgentCheckResult | null> {
  const config = DEFAULT_AGENTS[agentId]
  if (!config) {
    return null
  }

  // Check all related commands for this agent concurrently
  const commandsToCheck = AGENT_COMMANDS[agentId] || [config.command]
  const commandChecks = await Promise.all(
    commandsToCheck.map(async (cmd) => {
      const cmdCheck = await commandExists(cmd)
      return {
        command: cmd,
        path: cmdCheck.path,
        exists: cmdCheck.exists,
      }
    })
  )

  // Find primary command result from already-checked commands (avoid duplicate check)
  const primaryResult = commandChecks.find((c) => c.command === config.command)

  return {
    id: agentId,
    name: config.name,
    command: config.command,
    installed: primaryResult?.exists ?? false,
    path: primaryResult?.path,
    installHint: INSTALL_HINTS[agentId],
    commands: commandChecks.map(({ command, path }) => ({ command, path })),
  }
}

/**
 * Check all configured agents and return their installation status (async for true concurrency)
 */
export async function checkAgents(): Promise<AgentCheckResult[]> {
  const agentIds = Object.keys(DEFAULT_AGENTS)

  // Check all agents concurrently
  const results = await Promise.all(
    agentIds.map((id) => checkAgent(id))
  )

  // Filter out null results
  return results.filter((r): r is AgentCheckResult => r !== null)
}
