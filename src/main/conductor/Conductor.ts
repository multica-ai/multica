/**
 * Conductor - Central orchestrator for ACP agent communication (Facade)
 *
 * This class acts as a facade that coordinates the following modules:
 * - SessionLifecycle: Session CRUD operations
 * - AgentProcessManager: Agent process lifecycle
 * - PromptHandler: Prompt sending with history replay
 * - G3Workaround: AskUserQuestion answer injection
 *
 * The public API remains unchanged for backward compatibility with callers.
 */
import { SessionStore } from '../session/SessionStore'
import type {
  AgentConfig,
  MulticaSession,
  SessionData,
  ListSessionsOptions,
  AskUserQuestionResponseData
} from '../../shared/types'
import type { MessageContent } from '../../shared/types/message'

// Import modules
import { AgentProcessManager } from './AgentProcessManager'
import { PromptHandler } from './PromptHandler'
import { G3Workaround } from './G3Workaround'
import { SessionLifecycle } from './SessionLifecycle'

// Import types
import type {
  ConductorEvents,
  ConductorOptions,
  SessionUpdateCallback,
  ISessionStore
} from './types'

// Re-export types for backward compatibility
export type { SessionUpdateCallback, ConductorEvents, ConductorOptions }

export class Conductor {
  private events: ConductorEvents
  private sessionStore: ISessionStore | null = null
  private skipPersistence: boolean

  // Modules
  private agentProcessManager: AgentProcessManager
  private g3Workaround: G3Workaround
  private promptHandler: PromptHandler
  private sessionLifecycle: SessionLifecycle

  constructor(options: ConductorOptions = {}) {
    this.events = options.events ?? {}
    this.skipPersistence = options.skipPersistence ?? false

    // Initialize session store
    if (!this.skipPersistence) {
      this.sessionStore = new SessionStore(options.storagePath) as unknown as ISessionStore
    }

    // Initialize modules with dependencies
    this.agentProcessManager = new AgentProcessManager({
      sessionStore: this.sessionStore,
      events: this.events
    })

    this.g3Workaround = new G3Workaround({
      sessionStore: this.sessionStore,
      events: this.events
    })

    this.sessionLifecycle = new SessionLifecycle({
      sessionStore: this.sessionStore,
      agentProcessManager: this.agentProcessManager,
      events: this.events
    })

    this.promptHandler = new PromptHandler({
      sessionStore: this.sessionStore,
      agentProcessManager: this.agentProcessManager,
      g3Workaround: this.g3Workaround,
      events: this.events,
      ensureAgent: (sessionId: string) => this.sessionLifecycle.ensureAgentForSession(sessionId)
    })
  }

  // ==========================================================================
  // Initialization
  // ==========================================================================

  /**
   * Initialize the conductor (must be called before use in GUI mode)
   */
  async initialize(): Promise<void> {
    await this.sessionLifecycle.initialize()
  }

  // ==========================================================================
  // Session Lifecycle (delegated to SessionLifecycle)
  // ==========================================================================

  /**
   * Create a new session (agent starts lazily on first prompt)
   */
  async createSession(cwd: string, agentConfig: AgentConfig): Promise<MulticaSession> {
    return this.sessionLifecycle.create(cwd, agentConfig)
  }

  /**
   * Resume an existing session (starts a new agent process for it)
   */
  async resumeSession(sessionId: string): Promise<MulticaSession> {
    return this.sessionLifecycle.resume(sessionId)
  }

  /**
   * Load a session without starting its agent (lazy loading)
   */
  async loadSession(sessionId: string): Promise<MulticaSession> {
    return this.sessionLifecycle.load(sessionId)
  }

  /**
   * Delete a session
   */
  async deleteSession(sessionId: string): Promise<void> {
    return this.sessionLifecycle.delete(sessionId)
  }

  /**
   * Update session metadata
   */
  async updateSessionMeta(
    sessionId: string,
    updates: Partial<MulticaSession>
  ): Promise<MulticaSession> {
    return this.sessionLifecycle.updateMeta(sessionId, updates)
  }

  /**
   * Switch a session's agent (stops current, updates, starts new)
   */
  async switchSessionAgent(sessionId: string, newAgentId: string): Promise<MulticaSession> {
    return this.sessionLifecycle.switchAgent(sessionId, newAgentId)
  }

  /**
   * Get session list
   */
  async listSessions(options?: ListSessionsOptions): Promise<MulticaSession[]> {
    return this.sessionLifecycle.list(options)
  }

  /**
   * Get session complete data (including message history)
   */
  async getSessionData(sessionId: string): Promise<SessionData | null> {
    return this.sessionLifecycle.getData(sessionId)
  }

  // ==========================================================================
  // Agent Process Management (delegated to AgentProcessManager)
  // ==========================================================================

  /**
   * Stop a session's agent process
   */
  async stopSession(sessionId: string): Promise<void> {
    return this.agentProcessManager.stop(sessionId)
  }

  /**
   * Stop all session agents
   */
  async stopAllSessions(): Promise<void> {
    return this.agentProcessManager.stopAll()
  }

  /**
   * Get agent config for a session
   */
  getSessionAgent(sessionId: string): AgentConfig | null {
    return this.agentProcessManager.getAgentConfig(sessionId)
  }

  /**
   * Check if a session has a running agent
   */
  isSessionRunning(sessionId: string): boolean {
    return this.agentProcessManager.isRunning(sessionId)
  }

  /**
   * Get all running session IDs (sessions with agent process running)
   */
  getRunningSessionIds(): string[] {
    return this.agentProcessManager.getRunningSessionIds()
  }

  // ==========================================================================
  // Prompt Handling (delegated to PromptHandler)
  // ==========================================================================

  /**
   * Send a prompt to the agent (supports text and images)
   */
  async sendPrompt(
    sessionId: string,
    content: MessageContent,
    options?: { internal?: boolean }
  ): Promise<string> {
    return this.promptHandler.send(sessionId, content, options)
  }

  /**
   * Cancel an ongoing request
   */
  async cancelRequest(sessionId: string): Promise<void> {
    return this.promptHandler.cancel(sessionId)
  }

  /**
   * Get all processing session IDs (sessions currently handling a request)
   */
  getProcessingSessionIds(): string[] {
    return this.promptHandler.getProcessingSessionIds()
  }

  /**
   * Check if a session is currently processing a request
   */
  isSessionProcessing(sessionId: string): boolean {
    return this.promptHandler.isProcessing(sessionId)
  }

  // ==========================================================================
  // G-3 Workaround (delegated to G3Workaround)
  // ==========================================================================

  /**
   * Store a user's answer from AskUserQuestion for later injection (G-3 workaround)
   */
  addPendingAnswer(sessionId: string, question: string, answer: string): void {
    this.g3Workaround.addPendingAnswer(sessionId, question, answer)
  }

  /**
   * Store AskUserQuestion response for persistence
   */
  async storeAskUserQuestionResponse(
    sessionId: string,
    toolCallId: string,
    response: AskUserQuestionResponseData
  ): Promise<void> {
    return this.g3Workaround.storeResponse(sessionId, toolCallId, response)
  }

  // ==========================================================================
  // Session ID Lookup
  // ==========================================================================

  /**
   * Find Multica session ID by ACP agent session ID
   */
  getSessionIdByAgentSessionId(agentSessionId: string): string | null {
    return this.sessionLifecycle.getSessionIdByAgentSessionId(agentSessionId)
  }

  /**
   * Get Multica session ID from ACP session ID (alias for getSessionIdByAgentSessionId)
   */
  getMulticaSessionIdByAcp(acpSessionId: string): string | null {
    return this.getSessionIdByAgentSessionId(acpSessionId)
  }
}
