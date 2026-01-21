/**
 * AgentProcessManager - Manages agent process lifecycle
 *
 * Responsibilities:
 * - Starting agent subprocesses for sessions
 * - Managing agent process pool (sessions Map)
 * - Stopping individual or all agent processes
 * - Tracking running sessions
 */
import { ClientSideConnection, ndJsonStream, PROTOCOL_VERSION } from '@agentclientprotocol/sdk'
import { AgentProcess } from './AgentProcess'
import { createAcpClient } from './AcpClientFactory'
import type { AvailableCommand } from '@agentclientprotocol/sdk/dist/schema/types.gen'
import type { AgentConfig } from '../../shared/types'
import type {
  IAgentProcessManager,
  AgentProcessManagerOptions,
  SessionAgent,
  AgentStartResult,
  ISessionStore
} from './types'

export class AgentProcessManager implements IAgentProcessManager {
  private sessionStore: ISessionStore | null
  private events: AgentProcessManagerOptions['events']
  private pendingAvailableCommands: Map<string, AvailableCommand[]> = new Map()

  /**
   * Map of Multica sessionId -> agent state (each session has its own process)
   */
  private sessions: Map<string, SessionAgent> = new Map()

  constructor(options: AgentProcessManagerOptions) {
    this.sessionStore = options.sessionStore
    this.events = options.events
  }

  /**
   * Start an agent process for a session
   * @param sessionId - Multica session ID
   * @param config - Agent configuration
   * @param cwd - Working directory
   * @param isResumed - Whether this is a resumed session (needs history replay)
   */
  async start(
    sessionId: string,
    config: AgentConfig,
    cwd: string,
    isResumed: boolean = false
  ): Promise<AgentStartResult> {
    console.log(`[AgentProcessManager] Starting agent for session ${sessionId}: ${config.name}`)

    // Start the agent subprocess
    const agentProcess = new AgentProcess(config)
    await agentProcess.start()

    // Create ACP connection using the SDK
    const stream = ndJsonStream(agentProcess.getStdinWeb(), agentProcess.getStdoutWeb())

    // Create client-side connection with our Client implementation
    const connection = new ClientSideConnection(
      () =>
        createAcpClient(sessionId, {
          sessionStore: this.sessionStore, // Type is compatible with ISessionStore
          callbacks: {
            onSessionUpdate: this.events.onSessionUpdate,
            onPermissionRequest: this.events.onPermissionRequest,
            // Handle server-initiated mode updates
            onModeUpdate: (modeId) => {
              const sessionAgent = this.sessions.get(sessionId)
              if (sessionAgent?.sessionModeState) {
                sessionAgent.sessionModeState.currentModeId = modeId
              }
            },
            // Handle server-initiated model updates
            onModelUpdate: (modelId) => {
              const sessionAgent = this.sessions.get(sessionId)
              if (sessionAgent?.sessionModelState) {
                sessionAgent.sessionModelState.currentModelId = modelId
              }
            },
            // Handle available commands updates
            onAvailableCommandsUpdate: (commands) => {
              const sessionAgent = this.sessions.get(sessionId)
              if (sessionAgent) {
                sessionAgent.availableCommands = commands
                console.log(
                  `[AgentProcessManager] Available commands updated for session ${sessionId} (${commands.length})`
                )
                return
              }

              // Some agents (e.g., Codex) may emit available_commands_update before
              // the session is fully registered. Cache and apply after session creation.
              console.log(
                `[AgentProcessManager] Caching available commands for session ${sessionId} (${commands.length})`
              )
              this.pendingAvailableCommands.set(sessionId, commands)
            },
            // Track pending updates for completion detection
            onUpdateStarted: (promise) => {
              const sessionAgent = this.sessions.get(sessionId)
              if (sessionAgent) {
                sessionAgent.pendingUpdates.push(promise)
              }
            }
          }
        }),
      stream
    )

    // Initialize the ACP connection
    const initResult = await connection.initialize({
      protocolVersion: PROTOCOL_VERSION,
      clientCapabilities: {
        fs: {
          readTextFile: false,
          writeTextFile: false
        },
        terminal: false
      }
    })

    console.log(
      `[AgentProcessManager] ACP connected to ${config.name} (protocol v${initResult.protocolVersion})`
    )

    // Create ACP session
    const acpResult = await connection.newSession({
      cwd,
      mcpServers: []
    })

    // Handle agent process exit
    agentProcess.onExit((code, signal) => {
      console.log(
        `[AgentProcessManager] Agent for session ${sessionId} exited (code: ${code}, signal: ${signal})`
      )
      this.sessions.delete(sessionId)
      this.pendingAvailableCommands.delete(sessionId)
    })

    // Extract modes and models from ACP response (like Zed does)
    const sessionModeState = acpResult.modes ?? null
    const sessionModelState = acpResult.models ?? null

    // Store in sessions map
    const sessionAgent: SessionAgent = {
      agentProcess,
      connection,
      agentConfig: config,
      agentSessionId: acpResult.sessionId,
      needsHistoryReplay: isResumed, // True when resuming, agent needs conversation context
      sessionModeState,
      sessionModelState,
      availableCommands: [], // Will be populated by available_commands_update
      pendingUpdates: [] // Track pending sessionUpdate promises for completion detection
    }
    this.sessions.set(sessionId, sessionAgent)

    const pendingCommands = this.pendingAvailableCommands.get(sessionId)
    if (pendingCommands) {
      sessionAgent.availableCommands = pendingCommands
      this.pendingAvailableCommands.delete(sessionId)
      console.log(
        `[AgentProcessManager] Applied cached available commands for session ${sessionId} (${pendingCommands.length})`
      )
    }

    return { connection, agentSessionId: acpResult.sessionId }
  }

  /**
   * Stop a session's agent process
   */
  async stop(sessionId: string): Promise<void> {
    const sessionAgent = this.sessions.get(sessionId)
    if (sessionAgent) {
      console.log(
        `[AgentProcessManager] Stopping session ${sessionId} agent: ${sessionAgent.agentConfig.name}`
      )
      await sessionAgent.agentProcess.stop()
      this.sessions.delete(sessionId)
      this.pendingAvailableCommands.delete(sessionId)
      console.log(`[AgentProcessManager] Session ${sessionId} agent stopped`)
    }
  }

  /**
   * Stop all running agent processes
   */
  async stopAll(): Promise<void> {
    const sessionIds = Array.from(this.sessions.keys())
    for (const sessionId of sessionIds) {
      await this.stop(sessionId)
    }
  }

  /**
   * Get session agent state by session ID
   */
  get(sessionId: string): SessionAgent | undefined {
    return this.sessions.get(sessionId)
  }

  /**
   * Set session agent state (used by other modules when needed)
   */
  set(sessionId: string, sessionAgent: SessionAgent): void {
    this.sessions.set(sessionId, sessionAgent)
  }

  /**
   * Remove session agent state
   */
  remove(sessionId: string): void {
    this.sessions.delete(sessionId)
    this.pendingAvailableCommands.delete(sessionId)
  }

  /**
   * Check if a session has a running agent
   */
  isRunning(sessionId: string): boolean {
    const sessionAgent = this.sessions.get(sessionId)
    return sessionAgent?.agentProcess.isRunning() ?? false
  }

  /**
   * Get all running session IDs
   */
  getRunningSessionIds(): string[] {
    return Array.from(this.sessions.keys())
  }

  /**
   * Get agent config for a session
   */
  getAgentConfig(sessionId: string): AgentConfig | null {
    return this.sessions.get(sessionId)?.agentConfig ?? null
  }
}
