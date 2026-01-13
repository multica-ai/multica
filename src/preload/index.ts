import { contextBridge, ipcRenderer } from 'electron'
import { IPC_CHANNELS } from '../shared/ipc-channels'
import type { ElectronAPI } from '../shared/electron-api'
import type { ListSessionsOptions, MulticaSession } from '../shared/types'

// Electron API exposed to renderer process
const electronAPI: ElectronAPI = {
  // Agent lifecycle
  startAgent: (agentId: string) => ipcRenderer.invoke(IPC_CHANNELS.AGENT_START, agentId),

  stopAgent: () => ipcRenderer.invoke(IPC_CHANNELS.AGENT_STOP),

  switchAgent: (agentId: string) => ipcRenderer.invoke(IPC_CHANNELS.AGENT_SWITCH, agentId),

  getAgentStatus: () => ipcRenderer.invoke(IPC_CHANNELS.AGENT_STATUS),

  // Agent communication
  sendPrompt: (sessionId: string, content: string) =>
    ipcRenderer.invoke(IPC_CHANNELS.AGENT_PROMPT, sessionId, content),

  cancelRequest: (sessionId: string) => ipcRenderer.invoke(IPC_CHANNELS.AGENT_CANCEL, sessionId),

  // Session management
  createSession: (workingDirectory: string) =>
    ipcRenderer.invoke(IPC_CHANNELS.SESSION_CREATE, workingDirectory),

  listSessions: (options?: ListSessionsOptions) =>
    ipcRenderer.invoke(IPC_CHANNELS.SESSION_LIST, options),

  getSession: (sessionId: string) => ipcRenderer.invoke(IPC_CHANNELS.SESSION_GET, sessionId),

  resumeSession: (sessionId: string) => ipcRenderer.invoke(IPC_CHANNELS.SESSION_RESUME, sessionId),

  deleteSession: (sessionId: string) => ipcRenderer.invoke(IPC_CHANNELS.SESSION_DELETE, sessionId),

  updateSession: (sessionId: string, updates: Partial<MulticaSession>) =>
    ipcRenderer.invoke(IPC_CHANNELS.SESSION_UPDATE, sessionId, updates),

  // Configuration
  getConfig: () => ipcRenderer.invoke(IPC_CHANNELS.CONFIG_GET),

  updateConfig: (config) => ipcRenderer.invoke(IPC_CHANNELS.CONFIG_UPDATE, config),

  // Dialog
  selectDirectory: () => ipcRenderer.invoke(IPC_CHANNELS.DIALOG_SELECT_DIRECTORY),

  // System
  checkAgents: () => ipcRenderer.invoke(IPC_CHANNELS.SYSTEM_CHECK_AGENTS),

  // Event listeners
  onAgentMessage: (callback) => {
    const listener = (_event: Electron.IpcRendererEvent, message: unknown) =>
      callback(message as Parameters<typeof callback>[0])
    ipcRenderer.on(IPC_CHANNELS.AGENT_MESSAGE, listener)
    return () => ipcRenderer.removeListener(IPC_CHANNELS.AGENT_MESSAGE, listener)
  },

  onAgentStatus: (callback) => {
    const listener = (_event: Electron.IpcRendererEvent, status: unknown) =>
      callback(status as Parameters<typeof callback>[0])
    ipcRenderer.on(IPC_CHANNELS.AGENT_STATUS, listener)
    return () => ipcRenderer.removeListener(IPC_CHANNELS.AGENT_STATUS, listener)
  },

  onAgentError: (callback) => {
    const listener = (_event: Electron.IpcRendererEvent, error: unknown) => callback(error as Error)
    ipcRenderer.on(IPC_CHANNELS.AGENT_ERROR, listener)
    return () => ipcRenderer.removeListener(IPC_CHANNELS.AGENT_ERROR, listener)
  },
}

// Expose API to renderer via contextBridge
if (process.contextIsolated) {
  try {
    contextBridge.exposeInMainWorld('electronAPI', electronAPI)
  } catch (error) {
    console.error('Failed to expose electronAPI:', error)
  }
} else {
  // Fallback for non-isolated context (not recommended)
  ;(window as { electronAPI?: ElectronAPI }).electronAPI = electronAPI
}
