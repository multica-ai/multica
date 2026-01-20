/**
 * PromptHandler - Handles prompt sending and processing
 *
 * Responsibilities:
 * - Converting message content to ACP format
 * - History replay for resumed sessions
 * - Injecting G-3 pending answers
 * - Tracking processing state
 * - Error handling with user-friendly messages
 */
import type { SessionNotification } from '@agentclientprotocol/sdk'
import type { MessageContent, MessageContentItem } from '../../shared/types/message'
import { formatHistoryForReplay, hasReplayableHistory } from './historyReplay'
import log from '../logger'
import type {
  IPromptHandler,
  PromptHandlerOptions,
  SessionAgent,
  ISessionStore,
  IG3Workaround,
  IAgentProcessManager,
  ConductorEvents
} from './types'

/**
 * Extended options for PromptHandler including the ensureAgent callback
 */
export interface PromptHandlerFullOptions extends PromptHandlerOptions {
  /**
   * Callback to ensure an agent is running for a session.
   * This is provided by the parent Conductor/SessionLifecycle module.
   * Returns the SessionAgent for the session.
   */
  ensureAgent: (sessionId: string) => Promise<SessionAgent>
}

export class PromptHandler implements IPromptHandler {
  private sessionStore: ISessionStore | null
  private agentProcessManager: IAgentProcessManager
  private g3Workaround: IG3Workaround
  private events: ConductorEvents
  private ensureAgent: (sessionId: string) => Promise<SessionAgent>

  /**
   * Set of session IDs currently processing a request
   */
  private processingSessions: Set<string> = new Set()

  constructor(options: PromptHandlerFullOptions) {
    this.sessionStore = options.sessionStore
    this.agentProcessManager = options.agentProcessManager
    this.g3Workaround = options.g3Workaround
    this.events = options.events
    this.ensureAgent = options.ensureAgent
  }

  /**
   * Send a prompt to the agent
   * @param sessionId - Multica session ID
   * @param content - Message content (text, images)
   * @param options.internal - If true, message is not displayed in UI (G-3 mechanism)
   * @returns Stop reason from the agent
   */
  async send(
    sessionId: string,
    content: MessageContent,
    options?: { internal?: boolean }
  ): Promise<string> {
    // Mark session as processing immediately (before any await)
    // This ensures isProcessing() returns true as soon as send() is called
    this.processingSessions.add(sessionId)
    this.events.onStatusChange?.()

    try {
      return await this.sendInternal(sessionId, content, options)
    } finally {
      // Always remove from processing when done (success or error)
      this.processingSessions.delete(sessionId)
      this.events.onStatusChange?.()
    }
  }

  /**
   * Internal implementation of send (separated for cleaner processing state management)
   */
  private async sendInternal(
    sessionId: string,
    content: MessageContent,
    options?: { internal?: boolean }
  ): Promise<string> {
    // Ensure agent is running (lazy start if needed)
    const sessionAgent = await this.ensureAgent(sessionId)
    const { connection, agentSessionId } = sessionAgent

    // Convert MessageContent to ACP SDK format
    const convertToAcpFormat = (
      items: MessageContent
    ): Array<{ type: string; text?: string; data?: string; mimeType?: string }> => {
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
            console.log(
              `[PromptHandler] Prepending conversation history (${data.updates.length} updates)`
            )
            // Prepend history as text block before other content
            promptContent = [{ type: 'text', text: history }, ...promptContent]
          }
        }
      } catch (error) {
        console.error(`[PromptHandler] Failed to load history for replay:`, error)
        // Continue without history - better than blocking the prompt
      } finally {
        // Always mark as replayed to prevent repeated attempts
        sessionAgent.needsHistoryReplay = false
      }
    }

    // Inject pending user answers from AskUserQuestion (G-3 workaround)
    const pendingAnswers = this.g3Workaround.getPendingAnswers(sessionId)
    if (pendingAnswers.length > 0) {
      const answerContext = pendingAnswers
        .map((a) => `[User's answer to "${a.question}"]: ${a.answer}`)
        .join('\n')

      console.log(
        `[PromptHandler] Injecting ${pendingAnswers.length} pending answer(s) for session ${sessionId}`
      )

      // Prepend answers as context before user's message
      promptContent = [{ type: 'text', text: `---\n${answerContext}\n---\n` }, ...promptContent]

      // Clear pending answers after injection
      this.g3Workaround.clearPendingAnswers(sessionId)
    }

    // Log prompt info
    const textContent = content.find((c: MessageContentItem) => c.type === 'text')
    const imageCount = content.filter((c: MessageContentItem) => c.type === 'image').length
    console.log(`[PromptHandler] Sending prompt to session ${agentSessionId}`)
    if (textContent && textContent.type === 'text') {
      console.log(
        `[PromptHandler]   Text: ${textContent.text.slice(0, 100)}${textContent.text.length > 100 ? '...' : ''}`
      )
    }
    if (imageCount > 0) {
      console.log(`[PromptHandler]   Images: ${imageCount}`)
    }

    // Store user message before sending (so it appears in history)
    // Internal messages are stored with _internal flag for filtering in UI
    if (this.sessionStore) {
      const userUpdate = {
        sessionId: agentSessionId,
        update: {
          sessionUpdate: 'user_message',
          content: content, // Store full MessageContent array
          _internal: options?.internal ?? false // G-3: internal messages not shown in UI
        }
      }
      await this.sessionStore.appendUpdate(sessionId, userUpdate as unknown as SessionNotification)
    }

    try {
      // Cast promptContent to satisfy ACP SDK types - our MessageContentItem maps to ContentBlock
      const result = await connection.prompt({
        sessionId: agentSessionId,
        // eslint-disable-next-line @typescript-eslint/no-explicit-any -- ACP SDK types don't match our MessageContentItem
        prompt: promptContent as any
      })

      console.log(`[PromptHandler] Prompt completed with stopReason: ${result.stopReason}`)

      return result.stopReason
    } catch (error) {
      log.error(`[PromptHandler] ACP error for session ${sessionId}:`, error)

      // Parse ACP error to user-friendly message
      const message = this.parseAcpError(error)

      // DESIGN DECISION: Event-driven error handling instead of throwing
      //
      // Why not throw?
      // - Throwing causes IPC handler to fail, frontend receives an error response
      // - This typically triggers a toast/dialog, interrupting user flow
      //
      // Why emit an event?
      // - Error appears inline in the chat as a message, keeping context visible
      // - User can read the error and continue the conversation naturally
      // - Consistent with how agent responses are already delivered (via events)
      if (this.events.onSessionUpdate) {
        this.events.onSessionUpdate(
          {
            sessionId: agentSessionId,
            update: {
              sessionUpdate: 'agent_message_chunk',
              content: {
                type: 'text',
                text: `\n\n**Error:** ${message}\n`
              }
            }
          } as SessionNotification,
          sessionId // Pass Multica session ID for stable filtering
        )
      }

      // Return 'error' as stopReason - IPC succeeds, caller knows it was an error
      return 'error'
    }
  }

  /**
   * Cancel an ongoing request
   */
  async cancel(sessionId: string): Promise<void> {
    const sessionAgent = this.agentProcessManager.get(sessionId)
    if (!sessionAgent) {
      return
    }

    const { connection, agentSessionId } = sessionAgent
    console.log(`[PromptHandler] Cancelling request for session ${agentSessionId}`)
    await connection.cancel({ sessionId: agentSessionId })
    console.log(`[PromptHandler] Cancel request sent`)
  }

  /**
   * Check if a session is currently processing
   */
  isProcessing(sessionId: string): boolean {
    return this.processingSessions.has(sessionId)
  }

  /**
   * Get all session IDs currently processing
   */
  getProcessingSessionIds(): string[] {
    return Array.from(this.processingSessions)
  }

  /**
   * Parse ACP error and return user-friendly message
   */
  private parseAcpError(error: unknown): string {
    const errorStr = String(error)

    // MCP server missing environment variables
    if (errorStr.includes('Missing environment variables')) {
      const match = errorStr.match(/Missing environment variables: ([A-Z_]+)/)
      return `MCP server requires environment variable: ${match?.[1] || 'unknown'}`
    }

    // File too large to read
    if (errorStr.includes('MaxFileReadTokenExceededError')) {
      return 'File is too large to read. Try reading smaller portions.'
    }

    // MCP configuration invalid
    if (errorStr.includes('mcp-config-invalid')) {
      return 'MCP server configuration is invalid. Check your settings.'
    }

    // Connection/network errors
    if (errorStr.includes('ECONNREFUSED') || errorStr.includes('ECONNRESET')) {
      return 'Failed to connect to agent. Please check if the agent is running.'
    }

    // Timeout errors
    if (errorStr.includes('ETIMEDOUT') || errorStr.includes('timeout')) {
      return 'Request timed out. Please try again.'
    }

    // Process exit errors
    if (errorStr.includes('process exited') || errorStr.includes('spawn')) {
      return 'Agent process terminated unexpectedly.'
    }

    // API authentication errors
    if (
      errorStr.includes('401') ||
      errorStr.includes('authentication') ||
      errorStr.includes('API key')
    ) {
      return 'Authentication failed. Please check your API credentials.'
    }

    // Rate limit errors
    if (errorStr.includes('429') || errorStr.includes('rate limit')) {
      return 'Rate limit exceeded. Please wait and try again.'
    }

    // Improved fallback: show first line of error (truncated to 150 chars)
    const firstLine = errorStr.split('\n')[0].trim()
    const truncated = firstLine.length > 150 ? firstLine.slice(0, 150) + '...' : firstLine
    return `Agent error: ${truncated}`
  }
}
