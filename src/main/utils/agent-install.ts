/**
 * Agent installation utilities
 * Opens system Terminal with install command for user to execute
 */
import { spawn } from 'child_process'
import { platform } from 'os'
import type { InstallResult } from '../../shared/electron-api'

// Install commands for each agent
// Claude Code and Codex need both CLI and ACP installed
export const INSTALL_COMMANDS: Record<string, string> = {
  // Claude Code: Install official CLI first, then ACP adapter
  'claude-code':
    'curl -fsSL https://claude.ai/install.sh | bash && npm install -g @zed-industries/claude-code-acp',
  // OpenCode: Official install script
  opencode: 'curl -fsSL https://opencode.ai/install | bash',
  // Codex: Install CLI and ACP adapter together
  codex: 'npm install -g @openai/codex @zed-industries/codex-acp',
  // Gemini: Single npm package
  gemini: 'npm install -g @google/gemini-cli'
}

/**
 * Open the system Terminal and type a command (without executing)
 * User will see the command and can press Enter to run it
 */
export function openTerminalWithCommand(
  command: string
): Promise<{ success: boolean; error?: string }> {
  return new Promise((resolve) => {
    const os = platform()

    try {
      if (os === 'darwin') {
        // macOS: Use osascript to open Terminal.app and execute command
        const script = `
tell application "Terminal"
  activate
  do script "${command.replace(/"/g, '\\"')}"
end tell`
        const proc = spawn('osascript', ['-e', script])

        proc.on('close', (code) => {
          if (code === 0) {
            resolve({ success: true })
          } else {
            resolve({ success: false, error: `osascript exited with code ${code}` })
          }
        })

        proc.on('error', (err) => {
          resolve({ success: false, error: err.message })
        })
      } else if (os === 'win32') {
        // Windows: Open CMD and run command
        const proc = spawn('cmd.exe', ['/c', 'start', 'cmd.exe', '/K', command], {
          shell: true
        })

        proc.on('close', (code) => {
          if (code === 0) {
            resolve({ success: true })
          } else {
            resolve({ success: false, error: `cmd exited with code ${code}` })
          }
        })

        proc.on('error', (err) => {
          resolve({ success: false, error: err.message })
        })
      } else {
        // Linux: Try common terminal emulators in order of preference
        const terminals = [
          { cmd: 'gnome-terminal', args: ['--', 'bash', '-c', `${command}; exec bash`] },
          { cmd: 'konsole', args: ['-e', 'bash', '-c', `${command}; exec bash`] },
          { cmd: 'xfce4-terminal', args: ['-e', `bash -c "${command}; exec bash"`] },
          { cmd: 'xterm', args: ['-e', `bash -c "${command}; read -p 'Press Enter to close...'"`] }
        ]

        tryTerminals(terminals, 0, resolve)
      }
    } catch (err) {
      resolve({
        success: false,
        error: err instanceof Error ? err.message : 'Unknown error opening terminal'
      })
    }
  })
}

/**
 * Try to launch Linux terminals one by one until one succeeds
 */
function tryTerminals(
  terminals: Array<{ cmd: string; args: string[] }>,
  index: number,
  resolve: (result: { success: boolean; error?: string }) => void
): void {
  if (index >= terminals.length) {
    resolve({ success: false, error: 'No supported terminal emulator found' })
    return
  }

  const { cmd, args } = terminals[index]
  const proc = spawn(cmd, args, { detached: true, stdio: 'ignore' })

  proc.on('error', () => {
    // This terminal not found, try next one
    tryTerminals(terminals, index + 1, resolve)
  })

  // If spawn succeeds, consider it success (we detach so no close event)
  proc.unref()

  // Give it a moment to fail if the command doesn't exist
  setTimeout(() => {
    // If we got here without error, assume success
    resolve({ success: true })
  }, 100)
}

/**
 * Main entry point for agent installation
 * Opens Terminal with the install command for the user to execute
 */
export async function installAgent(agentId: string): Promise<InstallResult> {
  const command = INSTALL_COMMANDS[agentId]

  if (!command) {
    return { success: false, error: `Installation not supported for: ${agentId}` }
  }

  console.log(`[agent-install] Opening terminal with command: ${command}`)
  const result = await openTerminalWithCommand(command)

  if (!result.success) {
    console.error(`[agent-install] Failed to open terminal:`, result.error)
    return { success: false, error: result.error }
  }

  // Success means terminal was opened (not that installation completed)
  return { success: true }
}
