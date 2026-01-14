/**
 * Conductor - Central orchestrator for ACP agent communication
 */
import {
  ClientSideConnection,
  ndJsonStream,
  PROTOCOL_VERSION,
  type Client,
  type SessionNotification,
  type RequestPermissionRequest,
  type RequestPermissionResponse,
} from '@agentclientprotocol/sdk'
import { AgentProcess } from './AgentProcess'
import { SessionStore } from '../session/SessionStore'
import { DEFAULT_AGENTS } from '../config/defaults'
import type {
  AgentConfig,
  MulticaSession,
  SessionData,
  ListSessionsOptions,
} from '../../shared/types'

export interface SessionUpdateCallback {
  (update: SessionNotification): void
}

export interface ConductorEvents {
  onSessionUpdate?: SessionUpdateCallback
  onPermissionRequest?: (
    params: RequestPermissionRequest
  ) => Promise<RequestPermissionResponse>
  onStatusChange?: () => void
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
   */
  private async startAgentForSession(
    sessionId: string,
    config: AgentConfig,
    cwd: string
  ): Promise<{ connection: ClientSideConnection; agentSessionId: string }> {
    console.log(`[Conductor] Starting agent for session ${sessionId}: ${config.name}`)

    // Start the agent subprocess
    const agentProcess = new AgentProcess(config)
    await agentProcess.start()

    // Create ACP connection using the SDK
    const stream = ndJsonStream(
      agentProcess.getStdinWeb(),
      agentProcess.getStdoutWeb()
    )

    // Create client-side connection with our Client implementation
    const connection = new ClientSideConnection(
      (_agent) => this.createClient(sessionId),
      stream
    )

    console.log(`[Conductor] Sending ACP initialize request (protocol v${PROTOCOL_VERSION})`)

    // Initialize the ACP connection
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

    console.log(`[Conductor] ACP connected to ${config.name}`)
    console.log(`[Conductor]   Protocol version: ${initResult.protocolVersion}`)
    console.log(`[Conductor]   Agent info:`, initResult.agentInfo)

    // Create ACP session
    const acpResult = await connection.newSession({
      cwd,
      mcpServers: [],
    })

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

    // Start a new agent process for this session
    const { agentSessionId } = await this.startAgentForSession(
      sessionId,
      agentConfig,
      data.session.workingDirectory
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
   * Send a prompt to the agent
   */
  async sendPrompt(sessionId: string, content: string): Promise<string> {
    // Ensure agent is running (lazy start if needed)
    const sessionAgent = await this.ensureAgentForSession(sessionId)
    const { connection, agentSessionId } = sessionAgent

    console.log(`[Conductor] Sending prompt to session ${agentSessionId}`)
    console.log(`[Conductor]   Content: ${content.slice(0, 100)}${content.length > 100 ? '...' : ''}`)

    // Store user message before sending (so it appears in history)
    if (this.sessionStore) {
      const userUpdate = {
        sessionId: agentSessionId,
        update: {
          sessionUpdate: 'user_message',
          content: { type: 'text', text: content },
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
        prompt: [{ type: 'text', text: content }],
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

    // Start agent
    const { agentSessionId } = await this.startAgentForSession(
      sessionId,
      agentConfig,
      data.session.workingDirectory
    )

    // Update session with new agentSessionId
    await this.sessionStore.updateMeta(sessionId, {
      agentSessionId,
      status: 'active',
    })

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
   * Create the Client implementation for ACP SDK
   */
  private createClient(sessionId: string): Client {
    return {
      // Handle session updates from agent
      sessionUpdate: async (params: SessionNotification) => {
        // Log the update type
        const update = params.update
        if ('sessionUpdate' in update) {
          const updateType = update.sessionUpdate
          if (updateType === 'agent_message_chunk') {
            const contentType = update.content?.type || 'unknown'
            console.log(`[ACP] Session ${sessionId} update: ${updateType} (${contentType})`)
          } else if (updateType === 'tool_call') {
            console.log(`[ACP] Session ${sessionId} update: ${updateType} - ${update.title} [${update.status}]`)
          } else if (updateType === 'tool_call_update') {
            console.log(`[ACP] Session ${sessionId} update: ${updateType} [${update.status}]`)
          } else {
            console.log(`[ACP] Session ${sessionId} update: ${updateType}`, update)
          }
        } else {
          console.log(`[ACP] Session ${sessionId} update (raw):`, params)
        }

        // Store raw update to SessionStore (if available)
        if (this.sessionStore) {
          try {
            await this.sessionStore.appendUpdate(sessionId, params)
          } catch (err) {
            console.error('[Conductor] Failed to store session update:', err)
          }
        }

        // Trigger UI callback
        if (this.events.onSessionUpdate) {
          this.events.onSessionUpdate(params)
        }
      },

      // Handle permission requests from agent
      requestPermission: async (
        params: RequestPermissionRequest
      ): Promise<RequestPermissionResponse> => {
        if (this.events.onPermissionRequest) {
          return this.events.onPermissionRequest(params)
        }
        // Default: auto-approve (V1 simplification)
        console.log(`[Conductor] Auto-approving: ${params.toolCall.title}`)
        return {
          outcome: {
            outcome: 'selected',
            optionId: params.options[0]?.optionId ?? '',
          },
        }
      },
    }
  }
}
