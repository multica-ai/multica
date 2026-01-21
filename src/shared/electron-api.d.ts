/**
 * Type declarations for the Electron API exposed to the renderer process
 */
import type {
  AppConfig,
  MulticaSession,
  SessionData,
  ListSessionsOptions,
  SessionModeState,
  SessionModelState,
  SessionModeId,
  ModelId,
  AvailableCommand
} from './types'
import type { MessageContent } from './types/message'

export interface AgentMessage {
  sessionId: string // ACP agent session ID
  multicaSessionId: string // Multica session ID (stable, for filtering)
  sequenceNumber?: number // Monotonically increasing for ordering concurrent updates
  update: {
    sessionUpdate: string
    content?: { type: string; text: string }
    toolCallId?: string
    title?: string
    status?: string
    kind?: string
    rawInput?: unknown
    rawOutput?: unknown
    [key: string]: unknown
  }
  done: boolean
}

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

export interface RunningSessionsStatus {
  runningSessions: number
  sessionIds: string[]
  processingSessionIds: string[] // Sessions currently handling a request
}

// Permission request types (from ACP)
export interface PermissionOption {
  optionId: string
  name: string
  kind: string
}

export interface PermissionToolCall {
  toolCallId: string
  title?: string
  kind?: string
  status?: string
  rawInput?: unknown
}

export interface PermissionRequest {
  requestId: string
  sessionId: string
  multicaSessionId: string // Multica session ID for matching
  toolCall: PermissionToolCall
  options: PermissionOption[]
}

// Answer for a single question (used in multi-question AskUserQuestion)
export interface QuestionAnswer {
  question: string // The question text
  answer: string // User's answer
  isCustom?: boolean // Whether this was a custom text input
}

export interface PermissionResponseData {
  selectedOption?: string // User's selected option label (single select)
  selectedOptions?: string[] // User's selected option labels (multi-select)
  customText?: string // User's custom free-form input
  // Multi-question support: array of all question-answer pairs
  answers?: QuestionAnswer[]
}

export interface PermissionResponse {
  requestId: string
  optionId: string
  data?: PermissionResponseData // Additional data for AskUserQuestion
}

// File tree types
export interface FileTreeNode {
  name: string
  path: string
  type: 'file' | 'directory'
  extension?: string
}

export interface DetectedApp {
  id: string
  name: string
  icon?: string // App icon identifier
}

export interface OpenWithOptions {
  path: string
  appId: string
}

// File watcher types
// Note: fs.watch only provides 'rename' (add/delete) and 'change' (modify)
// We normalize 'rename' to 'change' for simplicity
export interface FileChangeEvent {
  directory: string
  eventType: 'change' | 'rename'
  path: string
  sessionIds: string[]
}

// Agent installation
export type InstallStep = 'check-npm' | 'install-cli' | 'install-acp'

export interface InstallProgressEvent {
  agentId: string
  step: InstallStep
  status: 'started' | 'progress' | 'completed' | 'error'
  message?: string
  error?: string
}

export interface InstallResult {
  success: boolean
  error?: string
}

// Auto-update types
export interface UpdateInfo {
  version: string
  releaseDate?: string
  releaseNotes?: string | null
}

export interface UpdateProgress {
  percent: number
  bytesPerSecond: number
  total: number
  transferred: number
}

export interface UpdateStatus {
  status: 'checking' | 'available' | 'not-available' | 'downloading' | 'downloaded' | 'error'
  info?: UpdateInfo
  progress?: UpdateProgress
  error?: string
}

export interface ElectronAPI {
  // Agent status (per-session agents)
  getAgentStatus(): Promise<RunningSessionsStatus>

  // Agent communication
  sendPrompt(sessionId: string, content: MessageContent): Promise<{ stopReason: string }>
  cancelRequest(sessionId: string): Promise<{ success: boolean }>

  // Session management (agent starts when session is created)
  createSession(workingDirectory: string, agentId: string): Promise<MulticaSession>
  listSessions(options?: ListSessionsOptions): Promise<MulticaSession[]>
  getSession(sessionId: string): Promise<SessionData | null>
  loadSession(sessionId: string): Promise<MulticaSession> // Load without starting agent
  startSessionAgent(sessionId: string): Promise<MulticaSession> // Start agent for a session
  resumeSession(sessionId: string): Promise<MulticaSession>
  deleteSession(sessionId: string): Promise<{ success: boolean }>
  updateSession(sessionId: string, updates: Partial<MulticaSession>): Promise<MulticaSession>
  switchSessionAgent(sessionId: string, newAgentId: string): Promise<MulticaSession>

  // Mode/Model management
  getSessionModes(sessionId: string): Promise<SessionModeState | null>
  getSessionModels(sessionId: string): Promise<SessionModelState | null>
  setSessionMode(sessionId: string, modeId: SessionModeId): Promise<void>
  setSessionModel(sessionId: string, modelId: ModelId): Promise<void>

  // Slash commands
  getSessionCommands(sessionId: string): Promise<AvailableCommand[]>

  // Configuration
  getConfig(): Promise<AppConfig>
  updateConfig(config: Partial<AppConfig>): Promise<AppConfig>

  // Dialog
  selectDirectory(): Promise<string | null>

  // System
  checkAgents(): Promise<AgentCheckResult[]>
  checkAgent(agentId: string): Promise<AgentCheckResult | null>

  // Agent installation
  installAgent(agentId: string): Promise<InstallResult>
  onInstallProgress(callback: (event: InstallProgressEvent) => void): () => void

  // File tree
  listDirectory(path: string): Promise<FileTreeNode[]>
  detectApps(): Promise<DetectedApp[]>
  openWith(options: OpenWithOptions): Promise<void>

  // File watcher
  startFileWatch(sessionId: string, directory: string): Promise<void>
  stopFileWatch(sessionId: string): Promise<void>
  onFileChanged(callback: (event: FileChangeEvent) => void): () => void

  // Event listeners (return unsubscribe function)
  onAgentMessage(callback: (message: AgentMessage) => void): () => void
  onAgentStatus(callback: (status: RunningSessionsStatus) => void): () => void
  onAgentError(callback: (error: Error) => void): () => void
  onPermissionRequest(callback: (request: PermissionRequest) => void): () => void
  onSessionMetaUpdated(callback: (session: MulticaSession) => void): () => void

  // Permission response
  respondToPermission(response: PermissionResponse): void

  // Terminal
  runInTerminal(command: string): Promise<void>

  // App lifecycle
  onAppFocus(callback: () => void): () => void

  // Auto-update
  checkForUpdates(): Promise<void>
  downloadUpdate(): Promise<void>
  installUpdate(): Promise<void>
  onUpdateStatus(callback: (status: UpdateStatus) => void): () => void
}

declare global {
  interface Window {
    electronAPI: ElectronAPI
  }
}

export {}
