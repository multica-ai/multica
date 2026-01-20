/**
 * G3Workaround - Handles AskUserQuestion answer injection
 *
 * The G-3 mechanism handles a limitation in ACP's AskUserQuestion tool:
 * the tool only returns "User has answered the question(s)" to the agent,
 * without the actual user selection.
 *
 * This module stores user answers so they can be injected into the next
 * prompt as context, allowing the agent to see the actual user selection.
 *
 * Flow:
 * 1. User selects an option in AskUserQuestion UI
 * 2. Answer is stored via addPendingAnswer()
 * 3. Current agent turn is cancelled (G-3 mechanism)
 * 4. Re-prompt is sent, which injects these answers as context
 * 5. Agent now sees the actual user selection
 */
import type { SessionNotification } from '@agentclientprotocol/sdk'
import type { AskUserQuestionResponseData } from '../../shared/types'
import type { IG3Workaround, G3WorkaroundOptions, PendingAnswer, ISessionStore } from './types'

export class G3Workaround implements IG3Workaround {
  private sessionStore: ISessionStore | null
  private events: G3WorkaroundOptions['events']

  /**
   * Pending answers from AskUserQuestion tool
   * Map of sessionId -> array of {question, answer}
   */
  private pendingAnswers: Map<string, PendingAnswer[]> = new Map()

  constructor(options: G3WorkaroundOptions) {
    this.sessionStore = options.sessionStore
    this.events = options.events
  }

  /**
   * Store a user's answer from AskUserQuestion for later injection
   * The answer will be prepended to the next prompt as context
   */
  addPendingAnswer(sessionId: string, question: string, answer: string): void {
    if (!this.pendingAnswers.has(sessionId)) {
      this.pendingAnswers.set(sessionId, [])
    }
    this.pendingAnswers.get(sessionId)!.push({ question, answer })
  }

  /**
   * Get pending answers for a session
   */
  getPendingAnswers(sessionId: string): PendingAnswer[] {
    return this.pendingAnswers.get(sessionId) || []
  }

  /**
   * Clear pending answers for a session (after injection)
   */
  clearPendingAnswers(sessionId: string): void {
    this.pendingAnswers.delete(sessionId)
  }

  /**
   * Store AskUserQuestion response for persistence
   * This allows the completed state to be restored after app restart.
   * The response is stored as a session update with type 'askuserquestion_response'.
   */
  async storeResponse(
    sessionId: string,
    toolCallId: string,
    response: AskUserQuestionResponseData
  ): Promise<void> {
    if (!this.sessionStore) {
      console.log('[G3Workaround] Skipping AskUserQuestion response storage (no session store)')
      return
    }

    const update = {
      sessionId,
      update: {
        sessionUpdate: 'askuserquestion_response',
        toolCallId,
        response
      }
    }

    console.log(`[G3Workaround] Storing AskUserQuestion response for toolCallId=${toolCallId}`)
    await this.sessionStore.appendUpdate(sessionId, update as unknown as SessionNotification)

    // Also notify frontend so it appears in sessionUpdates immediately
    if (this.events.onSessionUpdate) {
      this.events.onSessionUpdate(
        {
          sessionId,
          update: update.update
        } as SessionNotification,
        sessionId // Pass Multica session ID for stable filtering
      )
    }
  }
}
