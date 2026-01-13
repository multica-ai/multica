/**
 * IPC handlers for main process
 * Registers all IPC handlers for communication with renderer process
 */
import { ipcMain } from 'electron'
import { IPC_CHANNELS } from '../../shared/ipc-channels'

export function registerIPCHandlers(): void {
  // Agent communication handlers
  ipcMain.handle(IPC_CHANNELS.AGENT_PROMPT, async (_event, sessionId: string, content: string) => {
    // TODO: Implement agent prompt handling
    console.log(`[IPC] agent:prompt - sessionId: ${sessionId}, content: ${content}`)
  })

  ipcMain.handle(IPC_CHANNELS.AGENT_CANCEL, async (_event, sessionId: string) => {
    // TODO: Implement request cancellation
    console.log(`[IPC] agent:cancel - sessionId: ${sessionId}`)
  })

  ipcMain.handle(IPC_CHANNELS.AGENT_SWITCH, async (_event, agentId: string) => {
    // TODO: Implement agent switching
    console.log(`[IPC] agent:switch - agentId: ${agentId}`)
  })

  // Session management handlers
  ipcMain.handle(IPC_CHANNELS.SESSION_CREATE, async (_event, workingDirectory: string) => {
    // TODO: Implement session creation
    console.log(`[IPC] session:create - workingDirectory: ${workingDirectory}`)
    return {
      id: `session_${Date.now()}`,
      workingDirectory,
      agentId: 'default',
      createdAt: new Date().toISOString(),
      isActive: true,
    }
  })

  ipcMain.handle(IPC_CHANNELS.SESSION_CLOSE, async (_event, sessionId: string) => {
    // TODO: Implement session closing
    console.log(`[IPC] session:close - sessionId: ${sessionId}`)
  })

  ipcMain.handle(IPC_CHANNELS.SESSION_LIST, async () => {
    // TODO: Implement session listing
    console.log(`[IPC] session:list`)
    return []
  })

  // Configuration handlers
  ipcMain.handle(IPC_CHANNELS.CONFIG_GET, async () => {
    // TODO: Implement config retrieval
    console.log(`[IPC] config:get`)
    return {
      version: '0.1.0',
      activeAgentId: 'opencode',
      agents: {},
      ui: {
        theme: 'system',
        fontSize: 14,
      },
    }
  })

  ipcMain.handle(IPC_CHANNELS.CONFIG_UPDATE, async (_event, config: unknown) => {
    // TODO: Implement config update
    console.log(`[IPC] config:update`, config)
    return config
  })

  console.log('[IPC] All handlers registered')
}
