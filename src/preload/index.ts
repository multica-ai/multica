import { contextBridge, ipcRenderer } from 'electron'
import { IPC_CHANNELS } from '../shared/ipc-channels'
import type { ElectronAPI, OpenWithOptions } from '../shared/electron-api'
import type { ListSessionsOptions, MulticaSession } from '../shared/types'
import type { MessageContent } from '../shared/types/message'

// Electron API exposed to renderer process
const electronAPI: ElectronAPI = {
  // Agent status (per-session agents)
  getAgentStatus: () => ipcRenderer.invoke(IPC_CHANNELS.AGENT_STATUS),

  // Agent communication
  sendPrompt: (sessionId: string, content: MessageContent) =>
    ipcRenderer.invoke(IPC_CHANNELS.AGENT_PROMPT, sessionId, content),

  cancelRequest: (sessionId: string) => ipcRenderer.invoke(IPC_CHANNELS.AGENT_CANCEL, sessionId),

  // Session management (agent starts when session is created)
  createSession: (workingDirectory: string, agentId: string) =>
    ipcRenderer.invoke(IPC_CHANNELS.SESSION_CREATE, workingDirectory, agentId),

  listSessions: (options?: ListSessionsOptions) =>
    ipcRenderer.invoke(IPC_CHANNELS.SESSION_LIST, options),

  getSession: (sessionId: string) => ipcRenderer.invoke(IPC_CHANNELS.SESSION_GET, sessionId),

  loadSession: (sessionId: string) => ipcRenderer.invoke(IPC_CHANNELS.SESSION_LOAD, sessionId),

  resumeSession: (sessionId: string) => ipcRenderer.invoke(IPC_CHANNELS.SESSION_RESUME, sessionId),

  deleteSession: (sessionId: string) => ipcRenderer.invoke(IPC_CHANNELS.SESSION_DELETE, sessionId),

  updateSession: (sessionId: string, updates: Partial<MulticaSession>) =>
    ipcRenderer.invoke(IPC_CHANNELS.SESSION_UPDATE, sessionId, updates),

  switchSessionAgent: (sessionId: string, newAgentId: string) =>
    ipcRenderer.invoke(IPC_CHANNELS.SESSION_SWITCH_AGENT, sessionId, newAgentId),

  // Configuration
  getConfig: () => ipcRenderer.invoke(IPC_CHANNELS.CONFIG_GET),

  updateConfig: (config) => ipcRenderer.invoke(IPC_CHANNELS.CONFIG_UPDATE, config),

  // Dialog
  selectDirectory: () => ipcRenderer.invoke(IPC_CHANNELS.DIALOG_SELECT_DIRECTORY),

  // System
  checkAgents: () => ipcRenderer.invoke(IPC_CHANNELS.SYSTEM_CHECK_AGENTS),

  // File tree
  listDirectory: (path: string) => ipcRenderer.invoke(IPC_CHANNELS.FS_LIST_DIRECTORY, path),
  detectApps: () => ipcRenderer.invoke(IPC_CHANNELS.FS_DETECT_APPS),
  openWith: (options: OpenWithOptions) => ipcRenderer.invoke(IPC_CHANNELS.FS_OPEN_WITH, options),

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

  onPermissionRequest: (callback) => {
    const listener = (_event: Electron.IpcRendererEvent, request: unknown) =>
      callback(request as Parameters<typeof callback>[0])
    ipcRenderer.on(IPC_CHANNELS.PERMISSION_REQUEST, listener)
    return () => ipcRenderer.removeListener(IPC_CHANNELS.PERMISSION_REQUEST, listener)
  },

  respondToPermission: (response) => {
    ipcRenderer.send(IPC_CHANNELS.PERMISSION_RESPONSE, response)
  },

  onSessionMetaUpdated: (callback) => {
    const listener = (_event: Electron.IpcRendererEvent, session: unknown) =>
      callback(session as Parameters<typeof callback>[0])
    ipcRenderer.on(IPC_CHANNELS.SESSION_META_UPDATED, listener)
    return () => ipcRenderer.removeListener(IPC_CHANNELS.SESSION_META_UPDATED, listener)
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
