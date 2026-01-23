/**
 * SessionLifecycle - Manages session lifecycle operations
 *
 * Responsibilities:
 * - Creating new sessions
 * - Loading and resuming sessions
 * - Deleting sessions
 * - Switching session agents
 * - Starting agents for sessions (immediate or on-demand)
 */
import { DEFAULT_AGENTS } from '../config/defaults'
import type {
  AgentConfig,
  MulticaSession,
  SessionData,
  ListSessionsOptions
} from '../../shared/types'
import type {
  ISessionLifecycle,
  SessionLifecycleOptions,
  SessionAgent,
  ISessionStore,
  IAgentProcessManager,
  ConductorEvents
} from './types'

export class SessionLifecycle implements ISessionLifecycle {
  private sessionStore: ISessionStore | null
  private agentProcessManager: IAgentProcessManager
  private events: ConductorEvents

  /**
   * In-memory session for CLI mode (when persistence is skipped)
   */
  private inMemorySession: MulticaSession | null = null

  constructor(options: SessionLifecycleOptions) {
    this.sessionStore = options.sessionStore
    this.agentProcessManager = options.agentProcessManager
    this.events = options.events
    this.inMemorySession = options.inMemorySession ?? null
  }

  /**
   * Initialize the lifecycle manager
   */
  async initialize(): Promise<void> {
    if (this.sessionStore) {
      await this.sessionStore.initialize()
    }
  }

  /**
   * Create a new session (agent starts immediately)
   */
  async create(cwd: string, agentConfig: AgentConfig): Promise<MulticaSession> {
    let session: MulticaSession

    if (this.sessionStore) {
      // Get or create project for this working directory
      const project = await this.sessionStore.getOrCreateProject(cwd)

      // Create session record with project association
      session = await this.sessionStore.create({
        projectId: project.id,
        agentSessionId: '', // Will be filled after agent start
        agentId: agentConfig.id
      })
    } else {
      // CLI mode: in-memory session
      const { randomUUID } = await import('crypto')
      const now = new Date().toISOString()
      session = {
        id: randomUUID(),
        agentSessionId: '',
        projectId: '', // No project in CLI mode
        agentId: agentConfig.id,
        workingDirectory: cwd,
        createdAt: now,
        updatedAt: now,
        status: 'active',
        messageCount: 0
      }
      this.inMemorySession = session
    }

    // Start agent immediately (isResumed = false for new session)
    const { agentSessionId } = await this.agentProcessManager.start(
      session.id,
      agentConfig,
      cwd,
      false
    )

    // Update session with agentSessionId
    if (this.sessionStore) {
      session = await this.sessionStore.updateMeta(session.id, {
        agentSessionId,
        status: 'active'
      })
    } else {
      session.agentSessionId = agentSessionId
      this.inMemorySession = session
    }

    console.log(`[SessionLifecycle] Created session: ${session.id} (agent: ${agentSessionId})`)

    return session
  }

  /**
   * Load a session without starting its agent (lazy loading)
   */
  async load(sessionId: string): Promise<MulticaSession> {
    if (!this.sessionStore) {
      throw new Error('Session loading not available in CLI mode')
    }

    const data = await this.sessionStore.get(sessionId)
    if (!data) {
      throw new Error(`Session not found: ${sessionId}`)
    }

    return data.session
  }

  /**
   * Resume an existing session (starts a new agent process for it)
   */
  async resume(sessionId: string): Promise<MulticaSession> {
    if (!this.sessionStore) {
      throw new Error('Session resumption not available in CLI mode')
    }

    const data = await this.sessionStore.get(sessionId)
    if (!data) {
      throw new Error(`Session not found: ${sessionId}`)
    }

    // If session already has a running agent, return as-is
    if (this.agentProcessManager.get(sessionId)) {
      console.log(`[SessionLifecycle] Session ${sessionId} already has a running agent`)
      return data.session
    }

    // Get agent config
    const agentConfig = DEFAULT_AGENTS[data.session.agentId]
    if (!agentConfig) {
      throw new Error(`Unknown agent: ${data.session.agentId}`)
    }

    // Start a new agent process for this session (isResumed = true)
    const cwd = data.session.workingDirectory
    if (!cwd) {
      throw new Error(`Session ${sessionId} has no working directory`)
    }
    const { agentSessionId } = await this.agentProcessManager.start(
      sessionId,
      agentConfig,
      cwd,
      true // Resumed session needs history replay
    )

    // Update agentSessionId (new ACP session)
    const updatedSession = await this.sessionStore.updateMeta(sessionId, {
      agentSessionId,
      status: 'active'
    })

    console.log(
      `[SessionLifecycle] Resumed session: ${sessionId} (new agent session: ${agentSessionId})`
    )

    return updatedSession
  }

  /**
   * Delete a session and stop its agent
   */
  async delete(sessionId: string): Promise<void> {
    // Stop the session's agent process first
    await this.agentProcessManager.stop(sessionId)

    // Delete from store
    if (this.sessionStore) {
      await this.sessionStore.delete(sessionId)
    }
    if (this.inMemorySession?.id === sessionId) {
      this.inMemorySession = null
    }
  }

  /**
   * Update session metadata
   */
  async updateMeta(sessionId: string, updates: Partial<MulticaSession>): Promise<MulticaSession> {
    if (!this.sessionStore) {
      throw new Error('Session update not available in CLI mode')
    }
    return this.sessionStore.updateMeta(sessionId, updates)
  }

  /**
   * Start agent for a session (if not already running)
   * Used when selecting historical sessions to ensure agent is running
   */
  async startAgent(sessionId: string): Promise<MulticaSession> {
    // If already running, return current session
    if (this.agentProcessManager.get(sessionId)) {
      const data = await this.sessionStore!.get(sessionId)
      return data!.session
    }

    if (!this.sessionStore) {
      throw new Error('Session start not available in CLI mode')
    }

    // Load session
    const data = await this.sessionStore.get(sessionId)
    if (!data) {
      throw new Error(`Session not found: ${sessionId}`)
    }

    // Get agent config
    const agentConfig = DEFAULT_AGENTS[data.session.agentId]
    if (!agentConfig) {
      throw new Error(`Unknown agent: ${data.session.agentId}`)
    }

    console.log(`[SessionLifecycle] Starting agent for session ${sessionId}`)

    // Start agent (isResumed = true for history replay)
    const cwd = data.session.workingDirectory
    if (!cwd) {
      throw new Error(`Session ${sessionId} has no working directory`)
    }
    const { agentSessionId } = await this.agentProcessManager.start(
      sessionId,
      agentConfig,
      cwd,
      true
    )

    // Update session with new agentSessionId
    const updatedSession = await this.sessionStore.updateMeta(sessionId, {
      agentSessionId,
      status: 'active'
    })

    // Notify frontend
    if (this.events.onSessionMetaUpdated) {
      this.events.onSessionMetaUpdated(updatedSession)
    }

    console.log(
      `[SessionLifecycle] Started agent for session: ${sessionId} (agent session: ${agentSessionId})`
    )

    return updatedSession
  }

  /**
   * Switch a session's agent (stops current, starts new)
   */
  async switchAgent(sessionId: string, newAgentId: string): Promise<MulticaSession> {
    if (!this.sessionStore) {
      throw new Error('Session agent switch not available in CLI mode')
    }

    const data = await this.sessionStore.get(sessionId)
    if (!data) {
      throw new Error(`Session not found: ${sessionId}`)
    }

    // Get new agent config
    const newAgentConfig = DEFAULT_AGENTS[newAgentId]
    if (!newAgentConfig) {
      throw new Error(`Unknown agent: ${newAgentId}`)
    }

    console.log(
      `[SessionLifecycle] Switching session ${sessionId} from ${data.session.agentId} to ${newAgentId}`
    )

    // Stop current agent if running
    await this.agentProcessManager.stop(sessionId)

    // Update session's agentId
    let updatedSession = await this.sessionStore.updateMeta(sessionId, {
      agentId: newAgentId
    })

    // Start new agent (isResumed = true to replay history)
    const cwd = data.session.workingDirectory
    if (!cwd) {
      throw new Error(`Session ${sessionId} has no working directory`)
    }
    const { agentSessionId } = await this.agentProcessManager.start(
      sessionId,
      newAgentConfig,
      cwd,
      true
    )

    // Update agentSessionId
    updatedSession = await this.sessionStore.updateMeta(sessionId, {
      agentSessionId,
      status: 'active'
    })

    // Notify frontend
    if (this.events.onSessionMetaUpdated) {
      this.events.onSessionMetaUpdated(updatedSession)
    }

    console.log(
      `[SessionLifecycle] Session ${sessionId} switched to ${newAgentId} (agent session: ${agentSessionId})`
    )

    return updatedSession
  }

  /**
   * List sessions with optional filtering
   */
  async list(options?: ListSessionsOptions): Promise<MulticaSession[]> {
    if (!this.sessionStore) {
      return this.inMemorySession ? [this.inMemorySession] : []
    }
    return this.sessionStore.list(options)
  }

  /**
   * Get complete session data
   */
  async getData(sessionId: string): Promise<SessionData | null> {
    if (!this.sessionStore) {
      return null
    }
    return this.sessionStore.get(sessionId)
  }

  /**
   * Find Multica session ID by agent session ID
   */
  getSessionIdByAgentSessionId(agentSessionId: string): string | null {
    // First check in-memory sessions (running agents)
    for (const sessionId of this.agentProcessManager.getRunningSessionIds()) {
      const sessionAgent = this.agentProcessManager.get(sessionId)
      if (sessionAgent?.agentSessionId === agentSessionId) {
        return sessionId
      }
    }

    // Fallback to session store lookup
    if (this.sessionStore) {
      const session = this.sessionStore.getByAgentSessionId(agentSessionId)
      return session?.id ?? null
    }

    return null
  }

  /**
   * Ensure an agent is running for a session (start if needed)
   * This is used by PromptHandler to lazy-start agents on first prompt.
   */
  async ensureAgentForSession(sessionId: string): Promise<SessionAgent> {
    // If already running, return existing
    const existing = this.agentProcessManager.get(sessionId)
    if (existing) {
      return existing
    }

    // Load session data
    if (!this.sessionStore) {
      throw new Error('Cannot auto-start agent in CLI mode')
    }

    const data = await this.sessionStore.get(sessionId)
    if (!data) {
      throw new Error(`Session not found: ${sessionId}`)
    }

    // Get agent config
    const agentConfig = DEFAULT_AGENTS[data.session.agentId]
    if (!agentConfig) {
      throw new Error(`Unknown agent: ${data.session.agentId}`)
    }

    console.log(`[SessionLifecycle] Lazy-starting agent for session ${sessionId}`)

    // Start agent (isResumed = true for lazy start of existing session)
    const cwd = data.session.workingDirectory
    if (!cwd) {
      throw new Error(`Session ${sessionId} has no working directory`)
    }
    const { agentSessionId } = await this.agentProcessManager.start(
      sessionId,
      agentConfig,
      cwd,
      true // Existing session needs history replay
    )

    // Update session with new agentSessionId
    const updatedSession = await this.sessionStore.updateMeta(sessionId, {
      agentSessionId,
      status: 'active'
    })

    // Notify frontend of session metadata change (important for agentSessionId update)
    // This must happen BEFORE sending the prompt so frontend can receive messages
    if (this.events.onSessionMetaUpdated) {
      this.events.onSessionMetaUpdated(updatedSession)
    }

    return this.agentProcessManager.get(sessionId)!
  }

  /**
   * Get in-memory session (for CLI mode)
   */
  getInMemorySession(): MulticaSession | null {
    return this.inMemorySession
  }
}
