/**
 * Conductor - Central orchestrator for ACP agent communication
 *
 * Responsibilities:
 * - Session lifecycle management (create, resume, load, delete)
 * - Agent process orchestration
 * - Prompt handling with history replay
 */
import {
  ClientSideConnection,
  ndJsonStream,
  PROTOCOL_VERSION,
  type SessionNotification,
  type RequestPermissionRequest,
  type RequestPermissionResponse,
} from '@agentclientprotocol/sdk'
import { AgentProcess } from './AgentProcess'
import { SessionStore } from '../session/SessionStore'
import { DEFAULT_AGENTS } from '../config/defaults'
import { createAcpClient } from './AcpClientFactory'
import type {
  AgentConfig,
  MulticaSession,
  SessionData,
  ListSessionsOptions,
} from '../../shared/types'
import type { MessageContent, MessageContentItem } from '../../shared/types/message'
import { formatHistoryForReplay, hasReplayableHistory } from './historyReplay'

export interface SessionUpdateCallback {
  (update: SessionNotification): void
}

export interface ConductorEvents {
  onSessionUpdate?: SessionUpdateCallback
  onPermissionRequest?: (
    params: RequestPermissionRequest
  ) => Promise<RequestPermissionResponse>
  onStatusChange?: () => void
  /** Called when session metadata changes (e.g., agentSessionId after lazy start) */
  onSessionMetaUpdated?: (session: MulticaSession) => void
}

export interface ConductorOptions {
  events?: ConductorEvents
  /** Skip session persistence (for CLI mode) */
  skipPersistence?: boolean
  /** Custom storage path for sessions */
  storagePath?: string
}

/** Per-session agent state */
interface SessionAgent {
  agentProcess: AgentProcess
  connection: ClientSideConnection
  agentConfig: AgentConfig
  agentSessionId: string
  /** Whether the first prompt needs history prepended (for resumed sessions) */
  needsHistoryReplay: boolean
}

export class Conductor {
  private events: ConductorEvents
  private sessionStore: SessionStore | null = null
  private skipPersistence: boolean

  // Map of Multica sessionId -> agent state (each session has its own process)
  private sessions: Map<string, SessionAgent> = new Map()
  // Set of session IDs currently processing a request
  private processingSessions: Set<string> = new Set()
  // In-memory session for CLI mode (when persistence is skipped)
  private inMemorySession: MulticaSession | null = null

  constructor(options: ConductorOptions = {}) {
    this.events = options.events ?? {}
    this.skipPersistence = options.skipPersistence ?? false
    if (!this.skipPersistence) {
      this.sessionStore = new SessionStore(options.storagePath)
    }
  }

  /**
   * Initialize the conductor (must be called before use in GUI mode)
   */
  async initialize(): Promise<void> {
    if (this.sessionStore) {
      await this.sessionStore.initialize()
    }
  }

  /**
   * Start an agent process for a session (internal helper)
   * @param isResumed - Whether this is resuming an existing session (needs history replay)
   */
  private async startAgentForSession(
    sessionId: string,
    config: AgentConfig,
    cwd: string,
    isResumed: boolean = false
  ): Promise<{ connection: ClientSideConnection; agentSessionId: string }> {
    const startTime = Date.now()
    console.log(`[Conductor] Starting agent for session ${sessionId}: ${config.name}`)

    // Start the agent subprocess
    const agentProcess = new AgentProcess(config)
    const t1 = Date.now()
    await agentProcess.start()
    console.log(`[Conductor] [TIMING] AgentProcess.start() took ${Date.now() - t1}ms`)

    // Create ACP connection using the SDK
    const t2 = Date.now()
    const stream = ndJsonStream(
      agentProcess.getStdinWeb(),
      agentProcess.getStdoutWeb()
    )
    console.log(`[Conductor] [TIMING] ndJsonStream() took ${Date.now() - t2}ms`)

    // Create client-side connection with our Client implementation
    const t3 = Date.now()
    const connection = new ClientSideConnection(
      (_agent) =>
        createAcpClient(sessionId, {
          sessionStore: this.sessionStore,
          callbacks: {
            onSessionUpdate: this.events.onSessionUpdate,
            onPermissionRequest: this.events.onPermissionRequest,
          },
        }),
      stream
    )
    console.log(`[Conductor] [TIMING] ClientSideConnection() took ${Date.now() - t3}ms`)

    console.log(`[Conductor] Sending ACP initialize request (protocol v${PROTOCOL_VERSION})`)

    // Initialize the ACP connection
    const t4 = Date.now()
    const initResult = await connection.initialize({
      protocolVersion: PROTOCOL_VERSION,
      clientCapabilities: {
        fs: {
          readTextFile: false,
          writeTextFile: false,
        },
        terminal: false,
      },
    })
    console.log(`[Conductor] [TIMING] connection.initialize() took ${Date.now() - t4}ms`)

    console.log(`[Conductor] ACP connected to ${config.name}`)
    console.log(`[Conductor]   Protocol version: ${initResult.protocolVersion}`)
    console.log(`[Conductor]   Agent info:`, initResult.agentInfo)

    // Create ACP session
    const t5 = Date.now()
    const acpResult = await connection.newSession({
      cwd,
      mcpServers: [],
    })
    console.log(`[Conductor] [TIMING] connection.newSession() took ${Date.now() - t5}ms`)
    console.log(`[Conductor] [TIMING] Total startAgentForSession() took ${Date.now() - startTime}ms`)

    // Handle agent process exit
    agentProcess.onExit((code, signal) => {
      console.log(`[Conductor] Agent for session ${sessionId} exited (code: ${code}, signal: ${signal})`)
      this.sessions.delete(sessionId)
    })

    // Store in sessions map
    this.sessions.set(sessionId, {
      agentProcess,
      connection,
      agentConfig: config,
      agentSessionId: acpResult.sessionId,
      needsHistoryReplay: isResumed, // True when resuming, agent needs conversation context
    })

    return { connection, agentSessionId: acpResult.sessionId }
  }

  /**
   * Stop a session's agent process
   */
  async stopSession(sessionId: string): Promise<void> {
    const sessionAgent = this.sessions.get(sessionId)
    if (sessionAgent) {
      console.log(`[Conductor] Stopping session ${sessionId} agent: ${sessionAgent.agentConfig.name}`)
      await sessionAgent.agentProcess.stop()
      this.sessions.delete(sessionId)
      console.log(`[Conductor] Session ${sessionId} agent stopped`)
    }
  }

  /**
   * Stop all session agents
   */
  async stopAllSessions(): Promise<void> {
    const sessionIds = Array.from(this.sessions.keys())
    for (const sessionId of sessionIds) {
      await this.stopSession(sessionId)
    }
  }

  /**
   * Create a new session with a new agent process
   */
  async createSession(cwd: string, agentConfig: AgentConfig): Promise<MulticaSession> {
    let session: MulticaSession

    if (this.sessionStore) {
      // Create session record first to get the ID
      session = await this.sessionStore.create({
        agentSessionId: '', // Will be updated after agent starts
        agentId: agentConfig.id,
        workingDirectory: cwd,
      })
    } else {
      // CLI mode: in-memory session
      const { randomUUID } = await import('crypto')
      const now = new Date().toISOString()
      session = {
        id: randomUUID(),
        agentSessionId: '',
        agentId: agentConfig.id,
        workingDirectory: cwd,
        createdAt: now,
        updatedAt: now,
        status: 'active',
        messageCount: 0,
      }
      this.inMemorySession = session
    }

    // Start agent process for this session
    const { agentSessionId } = await this.startAgentForSession(session.id, agentConfig, cwd)

    // Update session with agentSessionId
    if (this.sessionStore) {
      session = await this.sessionStore.updateMeta(session.id, { agentSessionId })
    } else {
      session.agentSessionId = agentSessionId
      this.inMemorySession = session
    }

    console.log(`[Conductor] Created session: ${session.id} (agent: ${agentSessionId})`)

    return session
  }

  /**
   * Resume an existing session (starts a new agent process for it)
   */
  async resumeSession(sessionId: string): Promise<MulticaSession> {
    if (!this.sessionStore) {
      throw new Error('Session resumption not available in CLI mode')
    }

    const data = await this.sessionStore.get(sessionId)
    if (!data) {
      throw new Error(`Session not found: ${sessionId}`)
    }

    // If session already has a running agent, return as-is
    if (this.sessions.has(sessionId)) {
      console.log(`[Conductor] Session ${sessionId} already has a running agent`)
      return data.session
    }

    // Get agent config
    const agentConfig = DEFAULT_AGENTS[data.session.agentId]
    if (!agentConfig) {
      throw new Error(`Unknown agent: ${data.session.agentId}`)
    }

    // Start a new agent process for this session (isResumed = true)
    const { agentSessionId } = await this.startAgentForSession(
      sessionId,
      agentConfig,
      data.session.workingDirectory,
      true // Resumed session needs history replay
    )

    // Update agentSessionId (new ACP session)
    const updatedSession = await this.sessionStore.updateMeta(sessionId, {
      agentSessionId,
      status: 'active',
    })

    console.log(`[Conductor] Resumed session: ${sessionId} (new agent session: ${agentSessionId})`)

    return updatedSession
  }

  /**
   * Send a prompt to the agent (supports text and images)
   */
  async sendPrompt(sessionId: string, content: MessageContent): Promise<string> {
    // Ensure agent is running (lazy start if needed)
    const sessionAgent = await this.ensureAgentForSession(sessionId)
    const { connection, agentSessionId } = sessionAgent

    // Convert MessageContent to ACP SDK format
    const convertToAcpFormat = (items: MessageContent): Array<{ type: string; text?: string; data?: string; mimeType?: string }> => {
      return items.map((item: MessageContentItem) => {
        if (item.type === 'text') {
          return { type: 'text', text: item.text }
        } else if (item.type === 'image') {
          return { type: 'image', data: item.data, mimeType: item.mimeType }
        }
        return { type: 'text', text: '' } // fallback
      })
    }

    // Build prompt content array
    let promptContent = convertToAcpFormat(content)

    // If this is a resumed session, prepend conversation history to first prompt
    if (sessionAgent.needsHistoryReplay && this.sessionStore) {
      try {
        const data = await this.sessionStore.get(sessionId)
        if (data && hasReplayableHistory(data.updates)) {
          const history = formatHistoryForReplay(data.updates)
          if (history) {
            console.log(`[Conductor] Prepending conversation history (${data.updates.length} updates)`)
            // Prepend history as text block before other content
            promptContent = [{ type: 'text', text: history }, ...promptContent]
          }
        }
      } catch (error) {
        console.error(`[Conductor] Failed to load history for replay:`, error)
        // Continue without history - better than blocking the prompt
      } finally {
        // Always mark as replayed to prevent repeated attempts
        sessionAgent.needsHistoryReplay = false
      }
    }

    // Log prompt info
    const textContent = content.find((c: MessageContentItem) => c.type === 'text')
    const imageCount = content.filter((c: MessageContentItem) => c.type === 'image').length
    console.log(`[Conductor] Sending prompt to session ${agentSessionId}`)
    if (textContent && textContent.type === 'text') {
      console.log(`[Conductor]   Text: ${textContent.text.slice(0, 100)}${textContent.text.length > 100 ? '...' : ''}`)
    }
    if (imageCount > 0) {
      console.log(`[Conductor]   Images: ${imageCount}`)
    }

    // Store user message before sending (so it appears in history)
    if (this.sessionStore) {
      const userUpdate = {
        sessionId: agentSessionId,
        update: {
          sessionUpdate: 'user_message',
          content: content, // Store full MessageContent array
        },
      }
      await this.sessionStore.appendUpdate(sessionId, userUpdate as any)
    }

    // Mark session as processing and broadcast status change
    this.processingSessions.add(sessionId)
    this.events.onStatusChange?.()

    try {
      const result = await connection.prompt({
        sessionId: agentSessionId,
        prompt: promptContent as any,
      })

      console.log(`[Conductor] Prompt completed with stopReason: ${result.stopReason}`)

      return result.stopReason
    } finally {
      // Always remove from processing when done (success or error)
      this.processingSessions.delete(sessionId)
      this.events.onStatusChange?.()
    }
  }

  /**
   * Cancel an ongoing request
   */
  async cancelRequest(sessionId: string): Promise<void> {
    const sessionAgent = this.sessions.get(sessionId)
    if (!sessionAgent) {
      return
    }

    const { connection, agentSessionId } = sessionAgent
    console.log(`[Conductor] Cancelling request for session ${agentSessionId}`)
    await connection.cancel({ sessionId: agentSessionId })
    console.log(`[Conductor] Cancel request sent`)
  }

  /**
   * Get session list
   */
  async listSessions(options?: ListSessionsOptions): Promise<MulticaSession[]> {
    if (!this.sessionStore) {
      return this.inMemorySession ? [this.inMemorySession] : []
    }
    return this.sessionStore.list(options)
  }

  /**
   * Get session complete data (including message history)
   */
  async getSessionData(sessionId: string): Promise<SessionData | null> {
    if (!this.sessionStore) {
      return null
    }
    return this.sessionStore.get(sessionId)
  }

  /**
   * Load a session without starting its agent (lazy loading)
   */
  async loadSession(sessionId: string): Promise<MulticaSession> {
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
   * Ensure an agent is running for a session (start if needed)
   */
  private async ensureAgentForSession(sessionId: string): Promise<SessionAgent> {
    // If already running, return existing
    const existing = this.sessions.get(sessionId)
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

    console.log(`[Conductor] Lazy-starting agent for session ${sessionId}`)

    // Start agent (isResumed = true for lazy start of existing session)
    const { agentSessionId } = await this.startAgentForSession(
      sessionId,
      agentConfig,
      data.session.workingDirectory,
      true // Existing session needs history replay
    )

    // Update session with new agentSessionId
    const updatedSession = await this.sessionStore.updateMeta(sessionId, {
      agentSessionId,
      status: 'active',
    })

    // Notify frontend of session metadata change (important for agentSessionId update)
    // This must happen BEFORE sending the prompt so frontend can receive messages
    if (this.events.onSessionMetaUpdated) {
      this.events.onSessionMetaUpdated(updatedSession)
    }

    return this.sessions.get(sessionId)!
  }

  /**
   * Delete a session
   */
  async deleteSession(sessionId: string): Promise<void> {
    // Stop the session's agent process first
    await this.stopSession(sessionId)

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
  async updateSessionMeta(
    sessionId: string,
    updates: Partial<MulticaSession>
  ): Promise<MulticaSession> {
    if (!this.sessionStore) {
      throw new Error('Session update not available in CLI mode')
    }
    return this.sessionStore.updateMeta(sessionId, updates)
  }

  /**
   * Switch a session's agent (stops current, updates, starts new)
   */
  async switchSessionAgent(sessionId: string, newAgentId: string): Promise<MulticaSession> {
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

    console.log(`[Conductor] Switching session ${sessionId} from ${data.session.agentId} to ${newAgentId}`)

    // Stop current agent if running
    await this.stopSession(sessionId)

    // Update session's agentId
    let updatedSession = await this.sessionStore.updateMeta(sessionId, {
      agentId: newAgentId,
    })

    // Start new agent (isResumed = true to replay history)
    const { agentSessionId } = await this.startAgentForSession(
      sessionId,
      newAgentConfig,
      data.session.workingDirectory,
      true
    )

    // Update agentSessionId
    updatedSession = await this.sessionStore.updateMeta(sessionId, {
      agentSessionId,
      status: 'active',
    })

    // Notify frontend
    if (this.events.onSessionMetaUpdated) {
      this.events.onSessionMetaUpdated(updatedSession)
    }

    console.log(`[Conductor] Session ${sessionId} switched to ${newAgentId} (agent session: ${agentSessionId})`)

    return updatedSession
  }

  /**
   * Get agent config for a session
   */
  getSessionAgent(sessionId: string): AgentConfig | null {
    return this.sessions.get(sessionId)?.agentConfig ?? null
  }

  /**
   * Check if a session has a running agent
   */
  isSessionRunning(sessionId: string): boolean {
    const sessionAgent = this.sessions.get(sessionId)
    return sessionAgent?.agentProcess.isRunning() ?? false
  }

  /**
   * Get all running session IDs (sessions with agent process running)
   */
  getRunningSessionIds(): string[] {
    return Array.from(this.sessions.keys())
  }

  /**
   * Get all processing session IDs (sessions currently handling a request)
   */
  getProcessingSessionIds(): string[] {
    return Array.from(this.processingSessions)
  }

  /**
   * Find Multica session ID by ACP agent session ID
   */
  getSessionIdByAgentSessionId(agentSessionId: string): string | null {
    for (const [sessionId, sessionAgent] of this.sessions) {
      if (sessionAgent.agentSessionId === agentSessionId) {
        return sessionId
      }
    }
    return null
  }
}
