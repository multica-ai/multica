import { contextBridge, ipcRenderer } from 'electron'
import { IPC_CHANNELS } from '../shared/ipc-channels'
import type { ElectronAPI } from '../shared/electron-api'

// Electron API exposed to renderer process
const electronAPI: ElectronAPI = {
  // Agent communication
  sendPrompt: (sessionId: string, content: string) =>
    ipcRenderer.invoke(IPC_CHANNELS.AGENT_PROMPT, sessionId, content),

  cancelRequest: (sessionId: string) => ipcRenderer.invoke(IPC_CHANNELS.AGENT_CANCEL, sessionId),

  switchAgent: (agentId: string) => ipcRenderer.invoke(IPC_CHANNELS.AGENT_SWITCH, agentId),

  // Session management
  createSession: (workingDirectory: string) =>
    ipcRenderer.invoke(IPC_CHANNELS.SESSION_CREATE, workingDirectory),

  closeSession: (sessionId: string) => ipcRenderer.invoke(IPC_CHANNELS.SESSION_CLOSE, sessionId),

  listSessions: () => ipcRenderer.invoke(IPC_CHANNELS.SESSION_LIST),

  // Configuration
  getConfig: () => ipcRenderer.invoke(IPC_CHANNELS.CONFIG_GET),

  updateConfig: (config) => ipcRenderer.invoke(IPC_CHANNELS.CONFIG_UPDATE, config),

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
    const listener = (_event: Electron.IpcRendererEvent, error: unknown) =>
      callback(error as Error)
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
