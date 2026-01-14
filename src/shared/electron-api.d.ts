/**
 * Type declarations for the Electron API exposed to the renderer process
 */
import type {
  AgentStatus,
  AppConfig,
  MulticaSession,
  SessionData,
  ListSessionsOptions,
} from './types'

export interface AgentMessage {
  sessionId: string
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

export interface AgentCheckResult {
  id: string
  name: string
  command: string
  installed: boolean
  path?: string
  version?: string
  installHint?: string
}

export interface RunningSessionsStatus {
  runningSessions: number
  sessionIds: string[]
  processingSessionIds: string[] // Sessions currently handling a request
}

export interface ElectronAPI {
  // Agent status (per-session agents)
  getAgentStatus(): Promise<RunningSessionsStatus>

  // Agent communication
  sendPrompt(sessionId: string, content: string): Promise<{ stopReason: string }>
  cancelRequest(sessionId: string): Promise<{ success: boolean }>

  // Session management (agent starts when session is created)
  createSession(workingDirectory: string, agentId: string): Promise<MulticaSession>
  listSessions(options?: ListSessionsOptions): Promise<MulticaSession[]>
  getSession(sessionId: string): Promise<SessionData | null>
  loadSession(sessionId: string): Promise<MulticaSession> // Load without starting agent
  resumeSession(sessionId: string): Promise<MulticaSession>
  deleteSession(sessionId: string): Promise<{ success: boolean }>
  updateSession(sessionId: string, updates: Partial<MulticaSession>): Promise<MulticaSession>

  // Configuration
  getConfig(): Promise<AppConfig>
  updateConfig(config: Partial<AppConfig>): Promise<AppConfig>

  // Dialog
  selectDirectory(): Promise<string | null>

  // System
  checkAgents(): Promise<AgentCheckResult[]>

  // Event listeners (return unsubscribe function)
  onAgentMessage(callback: (message: AgentMessage) => void): () => void
  onAgentStatus(callback: (status: RunningSessionsStatus) => void): () => void
  onAgentError(callback: (error: Error) => void): () => void
}

declare global {
  interface Window {
    electronAPI: ElectronAPI
  }
}

export {}
