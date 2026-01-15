/**
 * Session types for Multica
 */
import type { SessionNotification } from '@agentclientprotocol/sdk'

/**
 * Multica Session - client-side session representation
 */
export interface MulticaSession {
  // Identity
  id: string // Multica-generated UUID
  agentSessionId: string // Agent-returned session ID

  // Association
  agentId: string // Agent used (opencode/codex/gemini)
  workingDirectory: string

  // Timestamps
  createdAt: string // ISO 8601
  updatedAt: string // Last activity time

  // State
  status: 'active' | 'completed' | 'error'

  // Metadata
  title?: string // User-defined or auto-generated title
  messageCount: number // Message count for list display
}

/**
 * AskUserQuestion response data (for persistence)
 * Compatible with PermissionResponseData from electron-api
 */
export interface AskUserQuestionResponseData {
  optionId?: string // Optional: permission optionId (not always needed for display)
  selectedOption?: string
  selectedOptions?: string[]
  customText?: string
  answers?: Array<{ question: string; answer: string }>
}

/**
 * Custom session update for AskUserQuestion response persistence
 * This is stored when user answers an AskUserQuestion, allowing
 * the completed state to be restored after app restart.
 */
export interface AskUserQuestionResponseUpdate {
  sessionUpdate: 'askuserquestion_response'
  toolCallId: string
  response: AskUserQuestionResponseData
}

/**
 * Session Update - stores raw ACP session/update data or custom updates
 */
export interface StoredSessionUpdate {
  timestamp: string // Receive time
  update: SessionNotification | { update: AskUserQuestionResponseUpdate } // Raw ACP data or custom update
}

/**
 * Complete session data (for persistence)
 */
export interface SessionData {
  session: MulticaSession
  updates: StoredSessionUpdate[]
}

/**
 * Parameters for creating a new session
 */
export interface CreateSessionParams {
  agentSessionId: string
  agentId: string
  workingDirectory: string
}

/**
 * Options for listing sessions
 */
export interface ListSessionsOptions {
  agentId?: string // Filter by agent
  status?: MulticaSession['status']
  limit?: number
  offset?: number
}
