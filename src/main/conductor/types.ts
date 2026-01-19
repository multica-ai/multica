/**
 * Type definitions for Conductor modules
 *
 * This file defines interfaces for the modular Conductor architecture,
 * enabling dependency injection and easier testing.
 */
import type {
  ClientSideConnection,
  SessionNotification,
  RequestPermissionRequest,
  RequestPermissionResponse
} from '@agentclientprotocol/sdk'
import type { AgentProcess } from './AgentProcess'
import type {
  AgentConfig,
  MulticaSession,
  SessionData,
  StoredSessionUpdate,
  ListSessionsOptions,
  CreateSessionParams,
  AskUserQuestionResponseData
} from '../../shared/types'
import type { MessageContent } from '../../shared/types/message'

// =============================================================================
// Core Types
// =============================================================================

/**
 * Per-session agent state (internal to conductor modules)
 */
export interface SessionAgent {
  agentProcess: AgentProcess
  connection: ClientSideConnection
  agentConfig: AgentConfig
  agentSessionId: string
  /** Whether the first prompt needs history prepended (for resumed sessions) */
  needsHistoryReplay: boolean
}

/**
 * Pending answer for G-3 mechanism (AskUserQuestion workaround)
 */
export interface PendingAnswer {
  question: string
  answer: string
}

/**
 * Result of starting an agent for a session
 */
export interface AgentStartResult {
  connection: ClientSideConnection
  agentSessionId: string
}

// =============================================================================
// Event Types
// =============================================================================

/**
 * Session update callback signature
 */
export interface SessionUpdateCallback {
  (update: SessionNotification, sequenceNumber?: number): void
}

/**
 * Events emitted by Conductor and its modules
 */
export interface ConductorEvents {
  /** Called when a session update is received from the agent */
  onSessionUpdate?: SessionUpdateCallback
  /** Called when a permission request is received from the agent */
  onPermissionRequest?: (params: RequestPermissionRequest) => Promise<RequestPermissionResponse>
  /** Called when processing state changes (for UI status updates) */
  onStatusChange?: () => void
  /** Called when session metadata changes (e.g., agentSessionId after lazy start) */
  onSessionMetaUpdated?: (session: MulticaSession) => void
}

/**
 * Options for Conductor initialization
 */
export interface ConductorOptions {
  events?: ConductorEvents
  /** Skip session persistence (for CLI mode) */
  skipPersistence?: boolean
  /** Custom storage path for sessions */
  storagePath?: string
}

// =============================================================================
// Module Interfaces
// =============================================================================

/**
 * Interface for session storage operations
 * Abstracts the persistence layer for dependency injection
 */
export interface ISessionStore {
  /** Initialize the storage (create directories, load index) */
  initialize(): Promise<void>

  /** Create a new session */
  create(params: CreateSessionParams): Promise<MulticaSession>

  /** Get complete session data including updates */
  get(sessionId: string): Promise<SessionData | null>

  /** List sessions with optional filtering */
  list(options?: ListSessionsOptions): Promise<MulticaSession[]>

  /** Update session metadata */
  updateMeta(sessionId: string, updates: Partial<MulticaSession>): Promise<MulticaSession>

  /** Delete a session */
  delete(sessionId: string): Promise<void>

  /** Append an update to a session */
  appendUpdate(sessionId: string, update: SessionNotification): Promise<StoredSessionUpdate>

  /** Find session by agent session ID (synchronous, in-memory lookup) */
  getByAgentSessionId(agentSessionId: string): MulticaSession | null
}

/**
 * Interface for agent process management
 */
export interface IAgentProcessManager {
  /**
   * Start an agent process for a session
   * @param sessionId - Multica session ID
   * @param config - Agent configuration
   * @param cwd - Working directory
   * @param isResumed - Whether this is a resumed session (needs history replay)
   */
  start(
    sessionId: string,
    config: AgentConfig,
    cwd: string,
    isResumed?: boolean
  ): Promise<AgentStartResult>

  /** Stop a session's agent process */
  stop(sessionId: string): Promise<void>

  /** Stop all running agent processes */
  stopAll(): Promise<void>

  /** Get session agent state by session ID */
  get(sessionId: string): SessionAgent | undefined

  /** Set session agent state (used by other modules) */
  set(sessionId: string, sessionAgent: SessionAgent): void

  /** Remove session agent state */
  remove(sessionId: string): void

  /** Check if a session has a running agent */
  isRunning(sessionId: string): boolean

  /** Get all running session IDs */
  getRunningSessionIds(): string[]

  /** Get agent config for a session */
  getAgentConfig(sessionId: string): AgentConfig | null
}

/**
 * Interface for prompt handling
 */
export interface IPromptHandler {
  /**
   * Send a prompt to the agent
   * @param sessionId - Multica session ID
   * @param content - Message content (text, images)
   * @param options.internal - If true, message is not displayed in UI (G-3 mechanism)
   * @returns Stop reason from the agent
   */
  send(
    sessionId: string,
    content: MessageContent,
    options?: { internal?: boolean }
  ): Promise<string>

  /** Cancel an ongoing request */
  cancel(sessionId: string): Promise<void>

  /** Check if a session is currently processing */
  isProcessing(sessionId: string): boolean

  /** Get all session IDs currently processing */
  getProcessingSessionIds(): string[]
}

/**
 * Interface for G-3 workaround (AskUserQuestion answer injection)
 *
 * The G-3 mechanism handles a limitation in ACP's AskUserQuestion tool:
 * the tool only returns "User has answered" without the actual selection.
 * This module stores user answers and injects them into the next prompt.
 */
export interface IG3Workaround {
  /** Store a user's answer for later injection */
  addPendingAnswer(sessionId: string, question: string, answer: string): void

  /** Get all pending answers for a session */
  getPendingAnswers(sessionId: string): PendingAnswer[]

  /** Clear pending answers for a session (after injection) */
  clearPendingAnswers(sessionId: string): void

  /** Store AskUserQuestion response for persistence (allows state restore after restart) */
  storeResponse(
    sessionId: string,
    toolCallId: string,
    response: AskUserQuestionResponseData
  ): Promise<void>
}

/**
 * Interface for session lifecycle management
 */
export interface ISessionLifecycle {
  /** Initialize the lifecycle manager */
  initialize(): Promise<void>

  /** Create a new session (agent starts lazily on first prompt) */
  create(cwd: string, agentConfig: AgentConfig): Promise<MulticaSession>

  /** Load a session without starting its agent (lazy loading) */
  load(sessionId: string): Promise<MulticaSession>

  /** Resume an existing session (starts a new agent process) */
  resume(sessionId: string): Promise<MulticaSession>

  /** Delete a session and stop its agent */
  delete(sessionId: string): Promise<void>

  /** Update session metadata */
  updateMeta(sessionId: string, updates: Partial<MulticaSession>): Promise<MulticaSession>

  /** Switch a session's agent (stops current, starts new) */
  switchAgent(sessionId: string, newAgentId: string): Promise<MulticaSession>

  /** List sessions with optional filtering */
  list(options?: ListSessionsOptions): Promise<MulticaSession[]>

  /** Get complete session data */
  getData(sessionId: string): Promise<SessionData | null>

  /** Find Multica session ID by agent session ID */
  getSessionIdByAgentSessionId(agentSessionId: string): string | null
}

// =============================================================================
// Module Options
// =============================================================================

/**
 * Options for AgentProcessManager
 */
export interface AgentProcessManagerOptions {
  sessionStore: ISessionStore | null
  events: ConductorEvents
}

/**
 * Options for PromptHandler
 */
export interface PromptHandlerOptions {
  sessionStore: ISessionStore | null
  agentProcessManager: IAgentProcessManager
  g3Workaround: IG3Workaround
  events: ConductorEvents
}

/**
 * Options for G3Workaround
 */
export interface G3WorkaroundOptions {
  sessionStore: ISessionStore | null
  events: ConductorEvents
}

/**
 * Options for SessionLifecycle
 */
export interface SessionLifecycleOptions {
  sessionStore: ISessionStore | null
  agentProcessManager: IAgentProcessManager
  events: ConductorEvents
  /** In-memory session for CLI mode */
  inMemorySession?: MulticaSession | null
}
