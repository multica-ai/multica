/**
 * IPC handlers for main process
 * Registers all IPC handlers for communication with renderer process
 */
import { ipcMain, dialog } from 'electron'
import { IPC_CHANNELS } from '../../shared/ipc-channels'
import { DEFAULT_AGENTS } from '../config/defaults'
import { checkAgents } from '../utils/agent-check'
import type { Conductor } from '../conductor/Conductor'
import type { ListSessionsOptions, MulticaSession } from '../../shared/types'

export function registerIPCHandlers(conductor: Conductor): void {
  // --- Agent handlers (per-session) ---

  ipcMain.handle(IPC_CHANNELS.AGENT_PROMPT, async (_event, sessionId: string, content: string) => {
    const stopReason = await conductor.sendPrompt(sessionId, content)
    return { stopReason }
  })

  ipcMain.handle(IPC_CHANNELS.AGENT_CANCEL, async (_event, sessionId: string) => {
    await conductor.cancelRequest(sessionId)
    return { success: true }
  })

  ipcMain.handle(IPC_CHANNELS.AGENT_STATUS, async () => {
    // Return status of all running sessions
    const runningSessionIds = conductor.getRunningSessionIds()
    const processingSessionIds = conductor.getProcessingSessionIds()
    return {
      runningSessions: runningSessionIds.length,
      sessionIds: runningSessionIds,
      processingSessionIds,
    }
  })

  // --- Session handlers ---

  ipcMain.handle(
    IPC_CHANNELS.SESSION_CREATE,
    async (_event, workingDirectory: string, agentId: string) => {
      const config = DEFAULT_AGENTS[agentId]
      if (!config) {
        throw new Error(`Unknown agent: ${agentId}`)
      }
      return conductor.createSession(workingDirectory, config)
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

  // --- Configuration handlers ---

  ipcMain.handle(IPC_CHANNELS.CONFIG_GET, async () => {
    return {
      version: '0.1.0',
      defaultAgentId: 'opencode', // Default agent for new sessions
      agents: DEFAULT_AGENTS,
      ui: {
        theme: 'system',
        fontSize: 14,
      },
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
      title: 'Select Working Directory',
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

  console.log('[IPC] All handlers registered')
}
