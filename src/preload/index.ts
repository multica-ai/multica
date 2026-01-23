import { contextBridge, ipcRenderer } from 'electron'
import { IPC_CHANNELS } from '../shared/ipc-channels'
import type { ElectronAPI, OpenWithOptions } from '../shared/electron-api'
import type { ListSessionsOptions, MulticaSession, SessionModeId, ModelId } from '../shared/types'
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

  startSessionAgent: (sessionId: string) =>
    ipcRenderer.invoke(IPC_CHANNELS.SESSION_START_AGENT, sessionId),

  resumeSession: (sessionId: string) => ipcRenderer.invoke(IPC_CHANNELS.SESSION_RESUME, sessionId),

  deleteSession: (sessionId: string) => ipcRenderer.invoke(IPC_CHANNELS.SESSION_DELETE, sessionId),

  updateSession: (sessionId: string, updates: Partial<MulticaSession>) =>
    ipcRenderer.invoke(IPC_CHANNELS.SESSION_UPDATE, sessionId, updates),

  switchSessionAgent: (sessionId: string, newAgentId: string) =>
    ipcRenderer.invoke(IPC_CHANNELS.SESSION_SWITCH_AGENT, sessionId, newAgentId),

  // Mode/Model management
  getSessionModes: (sessionId: string) =>
    ipcRenderer.invoke(IPC_CHANNELS.SESSION_GET_MODES, sessionId),

  getSessionModels: (sessionId: string) =>
    ipcRenderer.invoke(IPC_CHANNELS.SESSION_GET_MODELS, sessionId),

  // Slash commands
  getSessionCommands: (sessionId: string) =>
    ipcRenderer.invoke(IPC_CHANNELS.SESSION_GET_COMMANDS, sessionId),

  setSessionMode: (sessionId: string, modeId: SessionModeId) =>
    ipcRenderer.invoke(IPC_CHANNELS.SESSION_SET_MODE, sessionId, modeId),

  setSessionModel: (sessionId: string, modelId: ModelId) =>
    ipcRenderer.invoke(IPC_CHANNELS.SESSION_SET_MODEL, sessionId, modelId),

  // Configuration
  getConfig: () => ipcRenderer.invoke(IPC_CHANNELS.CONFIG_GET),

  updateConfig: (config) => ipcRenderer.invoke(IPC_CHANNELS.CONFIG_UPDATE, config),

  // Dialog
  selectDirectory: () => ipcRenderer.invoke(IPC_CHANNELS.DIALOG_SELECT_DIRECTORY),

  // System
  checkAgents: () => ipcRenderer.invoke(IPC_CHANNELS.SYSTEM_CHECK_AGENTS),
  checkAgent: (agentId: string) => ipcRenderer.invoke(IPC_CHANNELS.SYSTEM_CHECK_AGENT, agentId),

  // Agent installation
  installAgent: (agentId: string) => ipcRenderer.invoke(IPC_CHANNELS.AGENT_INSTALL, agentId),

  // Agent version checking
  checkAgentLatestVersions: (agentId: string, commands: string[]) =>
    ipcRenderer.invoke(IPC_CHANNELS.AGENT_CHECK_LATEST_VERSIONS, agentId, commands),

  // Agent/command update
  updateCommand: (commandName: string) =>
    ipcRenderer.invoke(IPC_CHANNELS.AGENT_UPDATE_COMMAND, commandName),

  onInstallProgress: (callback) => {
    const listener = (_event: Electron.IpcRendererEvent, progress: unknown): void => {
      callback(progress as Parameters<typeof callback>[0])
    }
    ipcRenderer.on(IPC_CHANNELS.AGENT_INSTALL_PROGRESS, listener)
    return (): void => {
      ipcRenderer.removeListener(IPC_CHANNELS.AGENT_INSTALL_PROGRESS, listener)
    }
  },

  // File tree
  listDirectory: (path: string) => ipcRenderer.invoke(IPC_CHANNELS.FS_LIST_DIRECTORY, path),
  detectApps: () => ipcRenderer.invoke(IPC_CHANNELS.FS_DETECT_APPS),
  openWith: (options: OpenWithOptions) => ipcRenderer.invoke(IPC_CHANNELS.FS_OPEN_WITH, options),

  // File watcher
  startFileWatch: (sessionId: string, directory: string) =>
    ipcRenderer.invoke(IPC_CHANNELS.FS_WATCH_START, sessionId, directory),
  stopFileWatch: (sessionId: string) => ipcRenderer.invoke(IPC_CHANNELS.FS_WATCH_STOP, sessionId),
  onFileChanged: (callback) => {
    const listener = (_event: Electron.IpcRendererEvent, changeEvent: unknown): void => {
      callback(changeEvent as Parameters<typeof callback>[0])
    }
    ipcRenderer.on(IPC_CHANNELS.FS_FILE_CHANGED, listener)
    return (): void => {
      ipcRenderer.removeListener(IPC_CHANNELS.FS_FILE_CHANGED, listener)
    }
  },

  // Event listeners
  onAgentMessage: (callback) => {
    const listener = (_event: Electron.IpcRendererEvent, message: unknown): void => {
      callback(message as Parameters<typeof callback>[0])
    }
    ipcRenderer.on(IPC_CHANNELS.AGENT_MESSAGE, listener)
    return (): void => {
      ipcRenderer.removeListener(IPC_CHANNELS.AGENT_MESSAGE, listener)
    }
  },

  onAgentStatus: (callback) => {
    const listener = (_event: Electron.IpcRendererEvent, status: unknown): void => {
      callback(status as Parameters<typeof callback>[0])
    }
    ipcRenderer.on(IPC_CHANNELS.AGENT_STATUS, listener)
    return (): void => {
      ipcRenderer.removeListener(IPC_CHANNELS.AGENT_STATUS, listener)
    }
  },

  onAgentError: (callback) => {
    const listener = (_event: Electron.IpcRendererEvent, error: unknown): void => {
      callback(error as Error)
    }
    ipcRenderer.on(IPC_CHANNELS.AGENT_ERROR, listener)
    return (): void => {
      ipcRenderer.removeListener(IPC_CHANNELS.AGENT_ERROR, listener)
    }
  },

  onPermissionRequest: (callback) => {
    const listener = (_event: Electron.IpcRendererEvent, request: unknown): void => {
      callback(request as Parameters<typeof callback>[0])
    }
    ipcRenderer.on(IPC_CHANNELS.PERMISSION_REQUEST, listener)
    return (): void => {
      ipcRenderer.removeListener(IPC_CHANNELS.PERMISSION_REQUEST, listener)
    }
  },

  respondToPermission: (response) => {
    ipcRenderer.send(IPC_CHANNELS.PERMISSION_RESPONSE, response)
  },

  onSessionMetaUpdated: (callback) => {
    const listener = (_event: Electron.IpcRendererEvent, session: unknown): void => {
      callback(session as Parameters<typeof callback>[0])
    }
    ipcRenderer.on(IPC_CHANNELS.SESSION_META_UPDATED, listener)
    return (): void => {
      ipcRenderer.removeListener(IPC_CHANNELS.SESSION_META_UPDATED, listener)
    }
  },

  // Terminal
  runInTerminal: (command: string) => ipcRenderer.invoke(IPC_CHANNELS.TERMINAL_RUN, command),

  // App lifecycle
  onAppFocus: (callback: () => void) => {
    const listener = (): void => {
      callback()
    }
    ipcRenderer.on(IPC_CHANNELS.APP_FOCUS, listener)
    return (): void => {
      ipcRenderer.removeListener(IPC_CHANNELS.APP_FOCUS, listener)
    }
  },

  // App info
  getAppVersion: () => ipcRenderer.invoke(IPC_CHANNELS.APP_GET_VERSION),

  // Auto-update
  checkForUpdates: () => ipcRenderer.invoke(IPC_CHANNELS.UPDATE_CHECK),
  downloadUpdate: () => ipcRenderer.invoke(IPC_CHANNELS.UPDATE_DOWNLOAD),
  installUpdate: () => ipcRenderer.invoke(IPC_CHANNELS.UPDATE_INSTALL),

  onUpdateStatus: (callback) => {
    const listener = (_event: Electron.IpcRendererEvent, status: unknown): void => {
      callback(status as Parameters<typeof callback>[0])
    }
    ipcRenderer.on(IPC_CHANNELS.UPDATE_STATUS, listener)
    return (): void => {
      ipcRenderer.removeListener(IPC_CHANNELS.UPDATE_STATUS, listener)
    }
  }
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
