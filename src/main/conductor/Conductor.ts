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
import type { AgentConfig, SessionInfo } from '../../shared/types'

export interface SessionUpdateCallback {
  (update: SessionNotification): void
}

export interface ConductorEvents {
  onSessionUpdate?: SessionUpdateCallback
  onPermissionRequest?: (
    params: RequestPermissionRequest
  ) => Promise<RequestPermissionResponse>
}

export class Conductor {
  private agentProcess: AgentProcess | null = null
  private connection: ClientSideConnection | null = null
  private currentAgentConfig: AgentConfig | null = null
  private sessions: Map<string, SessionInfo> = new Map()
  private events: ConductorEvents

  constructor(events: ConductorEvents = {}) {
    this.events = events
  }

  /**
   * Start an ACP agent
   */
  async startAgent(config: AgentConfig): Promise<void> {
    // Stop existing agent if running
    await this.stopAgent()

    // Start the agent subprocess
    this.agentProcess = new AgentProcess(config)
    await this.agentProcess.start()

    // Create ACP connection using the SDK
    const stream = ndJsonStream(
      this.agentProcess.getStdinWeb(),
      this.agentProcess.getStdoutWeb()
    )

    // Create client-side connection with our Client implementation
    this.connection = new ClientSideConnection(
      (_agent) => this.createClient(),
      stream
    )

    // Initialize the ACP connection
    const initResult = await this.connection.initialize({
      protocolVersion: PROTOCOL_VERSION,
      clientCapabilities: {
        // Declare what capabilities we support
        fs: {
          readTextFile: false, // V2: implement file system access
          writeTextFile: false,
        },
        terminal: false, // V2: implement terminal support
      },
    })

    console.log(
      `[Conductor] Connected to ${config.name} (protocol v${initResult.protocolVersion})`
    )
    this.currentAgentConfig = config

    // Handle agent process exit
    this.agentProcess.onExit((code, signal) => {
      console.log(`[Conductor] Agent exited (code: ${code}, signal: ${signal})`)
      this.connection = null
      this.agentProcess = null
    })
  }

  /**
   * Stop the current agent
   */
  async stopAgent(): Promise<void> {
    if (this.agentProcess) {
      await this.agentProcess.stop()
      this.agentProcess = null
      this.connection = null
      this.currentAgentConfig = null
      this.sessions.clear()
    }
  }

  /**
   * Create a new session with the agent
   */
  async createSession(cwd: string): Promise<SessionInfo> {
    if (!this.connection || !this.currentAgentConfig) {
      throw new Error('No agent is running')
    }

    const result = await this.connection.newSession({
      cwd,
      mcpServers: [], // V2: support MCP servers
    })

    const session: SessionInfo = {
      id: result.sessionId,
      workingDirectory: cwd,
      agentId: this.currentAgentConfig.id,
      createdAt: new Date().toISOString(),
      isActive: true,
    }

    this.sessions.set(session.id, session)
    console.log(`[Conductor] Created session: ${session.id}`)

    return session
  }

  /**
   * Send a prompt to the agent
   */
  async sendPrompt(sessionId: string, content: string): Promise<string> {
    if (!this.connection) {
      throw new Error('No agent is running')
    }

    const session = this.sessions.get(sessionId)
    if (!session) {
      throw new Error(`Session not found: ${sessionId}`)
    }

    const result = await this.connection.prompt({
      sessionId,
      prompt: [{ type: 'text', text: content }],
    })

    return result.stopReason
  }

  /**
   * Cancel an ongoing request
   */
  async cancelRequest(sessionId: string): Promise<void> {
    if (!this.connection) {
      return
    }

    await this.connection.cancel({ sessionId })
  }

  /**
   * Get current agent info
   */
  getCurrentAgent(): AgentConfig | null {
    return this.currentAgentConfig
  }

  /**
   * Check if an agent is running
   */
  isAgentRunning(): boolean {
    return this.agentProcess?.isRunning() ?? false
  }

  /**
   * Get all sessions
   */
  getSessions(): SessionInfo[] {
    return Array.from(this.sessions.values())
  }

  /**
   * Create the Client implementation for ACP SDK
   */
  private createClient(): Client {
    return {
      // Handle session updates from agent
      sessionUpdate: async (params: SessionNotification) => {
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
        // In production, this should prompt the user
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
