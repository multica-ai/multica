/**
 * Type declarations for the Electron API exposed to the renderer process
 */
import type { AgentStatus, AppConfig, SessionInfo } from './types'

export interface AgentMessage {
  sessionId: string
  content: Array<{ type: 'text'; text: string } | { type: 'code'; language: string; code: string }>
  done: boolean
}

export interface ElectronAPI {
  // Agent communication
  sendPrompt(sessionId: string, content: string): Promise<void>
  cancelRequest(sessionId: string): Promise<void>
  switchAgent(agentId: string): Promise<void>

  // Session management
  createSession(workingDirectory: string): Promise<SessionInfo>
  closeSession(sessionId: string): Promise<void>
  listSessions(): Promise<SessionInfo[]>

  // Configuration
  getConfig(): Promise<AppConfig>
  updateConfig(config: Partial<AppConfig>): Promise<AppConfig>

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
