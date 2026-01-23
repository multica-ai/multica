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
  projectId: string // Project this session belongs to
  agentId: string // Agent used (opencode/codex/gemini)

  // Timestamps
  createdAt: string // ISO 8601
  updatedAt: string // Last activity time

  // State
  status: 'active' | 'completed' | 'error'
  isArchived: boolean // Whether this session has been archived

  // Metadata
  title?: string // User-defined or auto-generated title
  messageCount: number // Message count for list display

  // Runtime state (not persisted, computed from project)
  workingDirectory?: string // Computed from parent project
  directoryExists?: boolean // true = exists, false = deleted/moved, undefined = not checked
  gitBranch?: string // Current git branch name (runtime only, refreshed on load)
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
  sequenceNumber?: number // Monotonically increasing sequence number for ordering (added for concurrent update handling)
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
  projectId: string // Project to create session under
  agentSessionId: string
  agentId: string
}

/**
 * Options for listing sessions
 */
export interface ListSessionsOptions {
  projectId?: string // Filter by project
  agentId?: string // Filter by agent
  status?: MulticaSession['status']
  includeArchived?: boolean // Include archived sessions (default: false)
  limit?: number
  offset?: number
}
