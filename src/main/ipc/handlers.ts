/**
 * IPC handlers for main process
 * Registers all IPC handlers for communication with renderer process
 */
import { ipcMain, dialog, clipboard, shell, BrowserWindow } from 'electron'
import { IPC_CHANNELS } from '../../shared/ipc-channels'
import { DEFAULT_AGENTS } from '../config/defaults'
import { checkAgents, checkAgent } from '../utils/agent-check'
import { installAgent } from '../utils/agent-install'
import type { Conductor } from '../conductor/Conductor'
import type { ListSessionsOptions, MulticaSession } from '../../shared/types'
import type { FileTreeNode, DetectedApp } from '../../shared/electron-api'
import type { MessageContent } from '../../shared/types/message'
import * as fs from 'fs'
import * as path from 'path'
import { spawn } from 'child_process'

/**
 * Promisified spawn that waits for the process to complete
 */
function spawnAsync(command: string, args: string[]): Promise<void> {
  return new Promise((resolve, reject) => {
    const proc = spawn(command, args, { stdio: 'ignore' })
    proc.on('close', (code) => {
      if (code === 0) {
        resolve()
      } else {
        reject(new Error(`Command failed with code ${code}`))
      }
    })
    proc.on('error', reject)
  })
}

/**
 * Validates a file path for safety:
 * - Must be absolute
 * - Must be normalized (no .. traversal tricks)
 */
function isValidPath(inputPath: string): boolean {
  // Must be absolute
  if (!path.isAbsolute(inputPath)) {
    return false
  }
  // Resolved path must equal input (catches .. in the middle)
  const resolved = path.resolve(inputPath)
  return resolved === inputPath
}

/**
 * Extract error message from various error types
 * Handles ACP SDK errors which are plain objects with message property
 */
function extractErrorMessage(err: unknown): string {
  if (err instanceof Error) {
    return err.message
  }
  if (err && typeof err === 'object') {
    if ('message' in err && typeof (err as Record<string, unknown>).message === 'string') {
      return (err as Record<string, unknown>).message as string
    }
    try {
      return JSON.stringify(err)
    } catch {
      return 'Unknown error'
    }
  }
  return String(err)
}

export function registerIPCHandlers(conductor: Conductor): void {
  // --- Agent handlers (per-session) ---

  ipcMain.handle(
    IPC_CHANNELS.AGENT_PROMPT,
    async (_event, sessionId: string, content: MessageContent) => {
      try {
        const stopReason = await conductor.sendPrompt(sessionId, content)
        return { stopReason }
      } catch (err) {
        throw new Error(extractErrorMessage(err))
      }
    }
  )

  ipcMain.handle(IPC_CHANNELS.AGENT_CANCEL, async (_event, sessionId: string) => {
    try {
      await conductor.cancelRequest(sessionId)
      return { success: true }
    } catch (err) {
      throw new Error(extractErrorMessage(err))
    }
  })

  ipcMain.handle(IPC_CHANNELS.AGENT_STATUS, async () => {
    // Return status of all running sessions
    const runningSessionIds = conductor.getRunningSessionIds()
    const processingSessionIds = conductor.getProcessingSessionIds()
    return {
      runningSessions: runningSessionIds.length,
      sessionIds: runningSessionIds,
      processingSessionIds
    }
  })

  // --- Session handlers ---

  ipcMain.handle(
    IPC_CHANNELS.SESSION_CREATE,
    async (_event, workingDirectory: string, agentId: string) => {
      try {
        const config = DEFAULT_AGENTS[agentId]
        if (!config) {
          throw new Error(`Unknown agent: ${agentId}`)
        }
        return await conductor.createSession(workingDirectory, config)
      } catch (err) {
        throw new Error(extractErrorMessage(err))
      }
    }
  )

  ipcMain.handle(IPC_CHANNELS.SESSION_LIST, async (_event, options?: ListSessionsOptions) => {
    return conductor.listSessions(options)
  })

  ipcMain.handle(IPC_CHANNELS.SESSION_GET, async (_event, sessionId: string) => {
    return conductor.getSessionData(sessionId)
  })

  ipcMain.handle(IPC_CHANNELS.SESSION_LOAD, async (_event, sessionId: string) => {
    return conductor.loadSession(sessionId)
  })

  ipcMain.handle(IPC_CHANNELS.SESSION_RESUME, async (_event, sessionId: string) => {
    return conductor.resumeSession(sessionId)
  })

  ipcMain.handle(IPC_CHANNELS.SESSION_DELETE, async (_event, sessionId: string) => {
    await conductor.deleteSession(sessionId)
    return { success: true }
  })

  ipcMain.handle(
    IPC_CHANNELS.SESSION_UPDATE,
    async (_event, sessionId: string, updates: Partial<MulticaSession>) => {
      return conductor.updateSessionMeta(sessionId, updates)
    }
  )

  ipcMain.handle(
    IPC_CHANNELS.SESSION_SWITCH_AGENT,
    async (_event, sessionId: string, newAgentId: string) => {
      try {
        return await conductor.switchSessionAgent(sessionId, newAgentId)
      } catch (err) {
        throw new Error(extractErrorMessage(err))
      }
    }
  )

  // --- Configuration handlers ---

  ipcMain.handle(IPC_CHANNELS.CONFIG_GET, async () => {
    return {
      version: '0.1.0',
      defaultAgentId: 'opencode', // Default agent for new sessions
      agents: DEFAULT_AGENTS,
      ui: {
        theme: 'system',
        fontSize: 14
      }
    }
  })

  ipcMain.handle(IPC_CHANNELS.CONFIG_UPDATE, async (_event, config: unknown) => {
    // TODO: Implement config persistence
    console.log(`[IPC] config:update`, config)
    return config
  })

  // --- Dialog handlers ---

  ipcMain.handle(IPC_CHANNELS.DIALOG_SELECT_DIRECTORY, async () => {
    const result = await dialog.showOpenDialog({
      properties: ['openDirectory', 'createDirectory'],
      title: 'Select Working Directory'
    })

    if (result.canceled || result.filePaths.length === 0) {
      return null
    }

    return result.filePaths[0]
  })

  // --- System handlers ---

  ipcMain.handle(IPC_CHANNELS.SYSTEM_CHECK_AGENTS, async () => {
    return checkAgents()
  })

  ipcMain.handle(IPC_CHANNELS.SYSTEM_CHECK_AGENT, async (_event, agentId: string) => {
    return checkAgent(agentId)
  })

  // --- Agent installation handler ---

  ipcMain.handle(IPC_CHANNELS.AGENT_INSTALL, async (_event, agentId: string) => {
    try {
      const window = BrowserWindow.getFocusedWindow()
      if (!window) {
        throw new Error('No focused window')
      }
      return await installAgent({ window, agentId })
    } catch (err) {
      throw new Error(extractErrorMessage(err))
    }
  })

  // --- File tree handlers ---

  ipcMain.handle(IPC_CHANNELS.FS_LIST_DIRECTORY, async (_event, dirPath: string) => {
    // Validate path to prevent traversal attacks
    if (!isValidPath(dirPath)) {
      console.warn(`[IPC] Invalid path rejected: ${dirPath}`)
      return []
    }

    try {
      const entries = await fs.promises.readdir(dirPath, { withFileTypes: true })
      const nodes: FileTreeNode[] = entries.map((entry) => {
        const fullPath = path.join(dirPath, entry.name)
        const isDirectory = entry.isDirectory()
        const ext = isDirectory ? undefined : path.extname(entry.name).toLowerCase().slice(1)
        return {
          name: entry.name,
          path: fullPath,
          type: isDirectory ? 'directory' : 'file',
          extension: ext || undefined
        }
      })

      // Sort: directories first, then files, both alphabetical
      nodes.sort((a, b) => {
        if (a.type !== b.type) {
          return a.type === 'directory' ? -1 : 1
        }
        return a.name.localeCompare(b.name)
      })

      return nodes
    } catch (error) {
      console.error(`[IPC] Failed to list directory: ${dirPath}`, error)
      return []
    }
  })

  ipcMain.handle(IPC_CHANNELS.FS_DETECT_APPS, async () => {
    const apps: DetectedApp[] = [{ id: 'finder', name: 'Finder' }]

    // App definitions to check
    const appChecks = [
      { id: 'cursor', name: 'Cursor', appName: 'Cursor.app' },
      { id: 'vscode', name: 'VS Code', appName: 'Visual Studio Code.app' },
      { id: 'xcode', name: 'Xcode', appName: 'Xcode.app' },
      { id: 'ghostty', name: 'Ghostty', appName: 'Ghostty.app' },
      { id: 'iterm', name: 'iTerm', appName: 'iTerm.app' }
    ]

    const homeDir = process.env.HOME || ''

    for (const app of appChecks) {
      const systemPath = `/Applications/${app.appName}`
      const userPath = `${homeDir}/Applications/${app.appName}`

      if (fs.existsSync(systemPath) || fs.existsSync(userPath)) {
        apps.push({ id: app.id, name: app.name })
      }
    }

    // Terminal is always available
    apps.push({ id: 'terminal', name: 'Terminal' })
    apps.push({ id: 'copy-path', name: 'Copy path' })

    return apps
  })

  ipcMain.handle(
    IPC_CHANNELS.FS_OPEN_WITH,
    async (_event, options: { path: string; appId: string }) => {
      const { path: filePath, appId } = options

      // Validate path to prevent traversal attacks
      if (!isValidPath(filePath)) {
        console.warn(`[IPC] Invalid path rejected: ${filePath}`)
        throw new Error('Invalid path')
      }

      try {
        // Helper to get directory for terminal apps
        const getDir = (p: string) => (fs.statSync(p).isDirectory() ? p : path.dirname(p))

        switch (appId) {
          case 'finder':
            // Reveal in Finder (uses Electron's safe API)
            shell.showItemInFolder(filePath)
            break
          case 'cursor':
            await spawnAsync('open', ['-a', 'Cursor', filePath])
            break
          case 'vscode':
            await spawnAsync('open', ['-a', 'Visual Studio Code', filePath])
            break
          case 'xcode':
            await spawnAsync('open', ['-a', 'Xcode', filePath])
            break
          case 'ghostty':
            // For terminals, open the directory (not file)
            await spawnAsync('open', ['-a', 'Ghostty', getDir(filePath)])
            break
          case 'iterm':
            await spawnAsync('open', ['-a', 'iTerm', getDir(filePath)])
            break
          case 'terminal':
            await spawnAsync('open', ['-a', 'Terminal', getDir(filePath)])
            break
          case 'copy-path':
            clipboard.writeText(filePath)
            break
          default:
            console.warn(`[IPC] Unknown app ID: ${appId}`)
        }
      } catch (error) {
        console.error(`[IPC] Failed to open with ${appId}:`, error)
        throw error
      }
    }
  )

  console.log('[IPC] All handlers registered')
}
