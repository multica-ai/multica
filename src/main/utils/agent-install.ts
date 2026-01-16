/**
 * Agent installation utilities
 * Handles one-click installation of AI agents
 */
import { spawn } from 'child_process'
import { platform } from 'os'
import type { BrowserWindow } from 'electron'
import { IPC_CHANNELS } from '../../shared/ipc-channels'
import type { InstallStep, InstallResult } from '../../shared/electron-api'
import { commandExists } from './agent-check'

interface InstallOptions {
  window: BrowserWindow
  agentId: string
}

type ProgressCallback = (
  step: InstallStep,
  status: string,
  message?: string,
  error?: string
) => void

/**
 * Execute a command and stream output
 */
function spawnWithProgress(
  command: string,
  args: string[],
  onProgress: (data: string) => void,
  useShell: boolean = false
): Promise<{ success: boolean; error?: string }> {
  return new Promise((resolve) => {
    // Ensure common paths are in PATH for npm, node, etc.
    const enhancedPath = `/usr/local/bin:/opt/homebrew/bin:${process.env.PATH || ''}`

    console.log(`[agent-install] Spawning: ${command} ${args.join(' ')} (shell: ${useShell})`)

    const proc = spawn(command, args, {
      shell: useShell,
      env: {
        ...process.env,
        PATH: enhancedPath,
        HOME: process.env.HOME || ''
      },
      stdio: ['pipe', 'pipe', 'pipe']
    })

    let errorOutput = ''
    let stdoutOutput = ''

    proc.stdout?.on('data', (data) => {
      const text = data.toString()
      stdoutOutput += text
      console.log(`[agent-install] stdout: ${text}`)
      onProgress(text)
    })

    proc.stderr?.on('data', (data) => {
      const text = data.toString()
      errorOutput += text
      console.log(`[agent-install] stderr: ${text}`)
      onProgress(text)
    })

    proc.on('close', (code) => {
      console.log(`[agent-install] Process closed with code: ${code}`)
      console.log(`[agent-install] stdout total: ${stdoutOutput}`)
      console.log(`[agent-install] stderr total: ${errorOutput}`)
      if (code === 0) {
        resolve({ success: true })
      } else {
        resolve({ success: false, error: errorOutput || `Exit code: ${code}` })
      }
    })

    proc.on('error', (err) => {
      console.log(`[agent-install] Process error: ${err.message}`)
      resolve({ success: false, error: err.message })
    })
  })
}

/**
 * Install Claude Code CLI using official install script
 */
async function installClaudeCLI(
  onProgress: (message: string) => void
): Promise<{ success: boolean; error?: string }> {
  const isWindows = platform() === 'win32'

  if (isWindows) {
    // Windows: use PowerShell
    return spawnWithProgress(
      'powershell',
      ['-Command', 'irm https://claude.ai/install.ps1 | iex'],
      onProgress,
      true
    )
  } else {
    // macOS/Linux: use curl + bash with full path
    return spawnWithProgress(
      '/bin/bash',
      ['-c', 'curl -fsSL https://claude.ai/install.sh | /bin/bash'],
      onProgress,
      false
    )
  }
}

/**
 * Install claude-code-acp via npm
 */
async function installClaudeCodeACP(
  onProgress: (message: string) => void
): Promise<{ success: boolean; error?: string }> {
  return spawnWithProgress(
    'npm',
    ['install', '-g', '@zed-industries/claude-code-acp'],
    onProgress,
    true
  )
}

/**
 * Install OpenCode using official install script
 */
async function installOpenCodeCLI(
  onProgress: (message: string) => void
): Promise<{ success: boolean; error?: string }> {
  const isWindows = platform() === 'win32'

  if (isWindows) {
    // Windows: use PowerShell
    return spawnWithProgress(
      'powershell',
      ['-Command', 'irm https://opencode.ai/install.ps1 | iex'],
      onProgress,
      true
    )
  } else {
    // macOS/Linux: use curl + bash with full path
    return spawnWithProgress(
      '/bin/bash',
      ['-c', 'curl -fsSL https://opencode.ai/install | /bin/bash'],
      onProgress,
      false
    )
  }
}

/**
 * Main installation function for Claude Code
 */
async function installClaudeCode(options: InstallOptions): Promise<InstallResult> {
  const { window, agentId } = options

  const sendProgress: ProgressCallback = (step, status, message, error) => {
    window.webContents.send(IPC_CHANNELS.AGENT_INSTALL_PROGRESS, {
      agentId,
      step,
      status,
      message,
      error
    })
  }

  try {
    // Step 1: Check npm
    sendProgress('check-npm', 'started')
    const npmCheck = await commandExists('npm')
    if (!npmCheck.exists) {
      const errorMsg = 'Node.js is required. Please install from https://nodejs.org'
      sendProgress('check-npm', 'error', undefined, errorMsg)
      return { success: false, error: errorMsg }
    }
    sendProgress('check-npm', 'completed')

    // Step 2: Install Claude Code CLI (if not already installed)
    const claudeCheck = await commandExists('claude')
    if (!claudeCheck.exists) {
      sendProgress('install-cli', 'started')
      const cliResult = await installClaudeCLI((msg) =>
        sendProgress('install-cli', 'progress', msg)
      )

      if (!cliResult.success) {
        // CLI installation failure is not fatal - ACP might still work
        console.warn('[agent-install] Claude CLI installation failed:', cliResult.error)
        sendProgress('install-cli', 'error', undefined, cliResult.error)
      } else {
        sendProgress('install-cli', 'completed')
      }
    } else {
      // Already installed, skip
      sendProgress('install-cli', 'completed', 'Already installed')
    }

    // Step 3: Install claude-code-acp
    sendProgress('install-acp', 'started')
    const acpResult = await installClaudeCodeACP((msg) =>
      sendProgress('install-acp', 'progress', msg)
    )

    if (!acpResult.success) {
      const errorMsg = formatInstallError(acpResult.error || 'Unknown error')
      sendProgress('install-acp', 'error', undefined, errorMsg)
      return { success: false, error: errorMsg }
    }

    sendProgress('install-acp', 'completed')
    return { success: true }
  } catch (error) {
    const errorMsg = error instanceof Error ? error.message : 'Unknown error'
    return { success: false, error: errorMsg }
  }
}

/**
 * Install Codex CLI via npm
 */
async function installCodexCLI(
  onProgress: (message: string) => void
): Promise<{ success: boolean; error?: string }> {
  return spawnWithProgress('npm', ['install', '-g', '@openai/codex'], onProgress, true)
}

/**
 * Install codex-acp via npm
 */
async function installCodexACP(
  onProgress: (message: string) => void
): Promise<{ success: boolean; error?: string }> {
  return spawnWithProgress('npm', ['install', '-g', '@zed-industries/codex-acp'], onProgress, true)
}

/**
 * Main installation function for Codex
 */
async function installCodex(options: InstallOptions): Promise<InstallResult> {
  const { window, agentId } = options

  const sendProgress: ProgressCallback = (step, status, message, error) => {
    window.webContents.send(IPC_CHANNELS.AGENT_INSTALL_PROGRESS, {
      agentId,
      step,
      status,
      message,
      error
    })
  }

  try {
    // Step 1: Check npm
    sendProgress('check-npm', 'started')
    const npmCheck = await commandExists('npm')
    if (!npmCheck.exists) {
      const errorMsg = 'Node.js is required. Please install from https://nodejs.org'
      sendProgress('check-npm', 'error', undefined, errorMsg)
      return { success: false, error: errorMsg }
    }
    sendProgress('check-npm', 'completed')

    // Step 2: Install Codex CLI (if not already installed)
    const codexCheck = await commandExists('codex')
    if (!codexCheck.exists) {
      sendProgress('install-cli', 'started')
      const cliResult = await installCodexCLI((msg) => sendProgress('install-cli', 'progress', msg))

      if (!cliResult.success) {
        // CLI installation failure is not fatal - ACP might still work
        console.warn('[agent-install] Codex CLI installation failed:', cliResult.error)
        sendProgress('install-cli', 'error', undefined, cliResult.error)
      } else {
        sendProgress('install-cli', 'completed')
      }
    } else {
      // Already installed, skip
      sendProgress('install-cli', 'completed', 'Already installed')
    }

    // Step 3: Install codex-acp
    sendProgress('install-acp', 'started')
    const acpResult = await installCodexACP((msg) => sendProgress('install-acp', 'progress', msg))

    if (!acpResult.success) {
      const errorMsg = formatInstallError(acpResult.error || 'Unknown error')
      sendProgress('install-acp', 'error', undefined, errorMsg)
      return { success: false, error: errorMsg }
    }

    sendProgress('install-acp', 'completed')
    return { success: true }
  } catch (error) {
    const errorMsg = error instanceof Error ? error.message : 'Unknown error'
    return { success: false, error: errorMsg }
  }
}

/**
 * Main installation function for OpenCode
 */
async function installOpenCode(options: InstallOptions): Promise<InstallResult> {
  const { window, agentId } = options

  console.log(`[agent-install] Starting OpenCode installation`)

  const sendProgress = (status: string, message?: string, error?: string): void => {
    console.log(`[agent-install] OpenCode progress: ${status} - ${message || ''} - ${error || ''}`)
    window.webContents.send(IPC_CHANNELS.AGENT_INSTALL_PROGRESS, {
      agentId,
      step: 'install-cli',
      status,
      message,
      error
    })
  }

  try {
    sendProgress('started')

    console.log(`[agent-install] Calling installOpenCodeCLI...`)
    const result = await installOpenCodeCLI((msg) => sendProgress('progress', msg))
    console.log(`[agent-install] installOpenCodeCLI result:`, result)

    if (!result.success) {
      const errorMsg = formatInstallError(result.error || 'Unknown error')
      sendProgress('error', undefined, errorMsg)
      return { success: false, error: errorMsg }
    }

    sendProgress('completed')
    return { success: true }
  } catch (error) {
    console.error(`[agent-install] OpenCode installation error:`, error)
    const errorMsg = error instanceof Error ? error.message : 'Unknown error'
    sendProgress('error', undefined, errorMsg)
    return { success: false, error: errorMsg }
  }
}

/**
 * Main entry point for agent installation
 */
export async function installAgent(options: InstallOptions): Promise<InstallResult> {
  const { agentId } = options

  switch (agentId) {
    case 'claude-code':
      return installClaudeCode(options)
    case 'opencode':
      return installOpenCode(options)
    case 'codex':
      return installCodex(options)
    default:
      return { success: false, error: `Installation not supported for: ${agentId}` }
  }
}

/**
 * Format installation error messages for user display
 */
function formatInstallError(error: string): string {
  if (error.includes('EACCES') || error.includes('permission denied')) {
    return 'Permission denied. Please check your permissions and try again.'
  }
  if (error.includes('Could not resolve host') || error.includes('ENOTFOUND')) {
    return 'Network error. Please check your internet connection and try again.'
  }
  if (error.includes('ENOENT')) {
    return 'Command not found. Please ensure required tools are installed.'
  }
  return error
}
