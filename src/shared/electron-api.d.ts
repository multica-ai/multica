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
  content: Array<{ type: 'text'; text: string } | { type: 'code'; language: string; code: string }>
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

export interface ElectronAPI {
  // Agent lifecycle
  startAgent(agentId: string): Promise<{ success: boolean; agentId: string }>
  stopAgent(): Promise<{ success: boolean }>
  switchAgent(agentId: string): Promise<{ success: boolean; agentId: string }>
  getAgentStatus(): Promise<AgentStatus>

  // Agent communication
  sendPrompt(sessionId: string, content: string): Promise<{ stopReason: string }>
  cancelRequest(sessionId: string): Promise<{ success: boolean }>

  // Session management
  createSession(workingDirectory: string): Promise<MulticaSession>
  listSessions(options?: ListSessionsOptions): Promise<MulticaSession[]>
  getSession(sessionId: string): Promise<SessionData | null>
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
  onAgentStatus(callback: (status: AgentStatus) => void): () => void
  onAgentError(callback: (error: Error) => void): () => void
}

declare global {
  interface Window {
    electronAPI: ElectronAPI
  }
}

export {}
