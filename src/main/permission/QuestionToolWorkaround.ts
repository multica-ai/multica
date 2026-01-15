/**
 * Question Tool Workaround Handler
 *
 * Handles OpenCode's 'question' tool which hangs forever in ACP mode because:
 * 1. It uses an internal HTTP-based Question system, not ACP's requestPermission
 * 2. The ACP agent doesn't forward 'question.asked' events to clients
 * 3. The tool hangs forever waiting for an HTTP response that never comes
 *
 * Solution: Detect when question tool starts (in_progress) and cancel + notify
 */
import type { Conductor } from '../conductor/Conductor'
import type { QuestionToolUpdate } from './types'

/** Delay (ms) after cancel to let it settle before re-prompting */
const CANCEL_SETTLE_DELAY_MS = 200

/** Time (ms) to keep toolCallId in handled set before cleanup */
const HANDLED_CLEANUP_DELAY_MS = 60_000

export class QuestionToolWorkaround {
  /** Track handled toolCallIds to prevent duplicate processing */
  private handledToolCalls = new Set<string>()

  constructor(private conductor: Conductor) {}

  /**
   * Handle session update to detect and workaround question tool
   */
  handleToolUpdate(params: { sessionId: string; update: QuestionToolUpdate }): void {
    const { sessionId, update } = params

    // Check if this is a question tool starting
    if (
      update.sessionUpdate === 'tool_call_update' &&
      update.title === 'question' &&
      update.status === 'in_progress'
    ) {
      // Skip if already handled (prevents duplicate processing from multiple events)
      const toolCallId = update.toolCallId
      if (toolCallId && this.handledToolCalls.has(toolCallId)) {
        console.log(`[Question] Already handled toolCallId=${toolCallId}, skipping`)
        return
      }

      const multicaSessionId = this.conductor.getMulticaSessionIdByAcp(sessionId)
      if (!multicaSessionId) return

      // Mark as handled to prevent duplicate processing
      if (toolCallId) {
        this.handledToolCalls.add(toolCallId)
        // Cleanup after delay to prevent memory leak
        setTimeout(() => {
          this.handledToolCalls.delete(toolCallId)
        }, HANDLED_CLEANUP_DELAY_MS)
      }

      // Extract original question from rawInput
      const questions = update.rawInput?.questions || []
      const questionTexts = questions
        .map((q, i) => {
          let text = `${i + 1}. ${q.question}`
          if (q.options && q.options.length > 0) {
            text += '\n   Options: ' + q.options.map((o) => o.label).join(', ')
          }
          return text
        })
        .join('\n')

      // Use setImmediate to avoid blocking the event handler
      setImmediate(async () => {
        try {
          console.log(
            '[Question] Tool detected (will hang forever), cancelling and notifying agent'
          )
          console.log('[Question] Original questions:', questionTexts)

          // Cancel the current turn to stop the hanging question tool
          await this.conductor.cancelRequest(multicaSessionId)

          // Wait for cancel to settle before re-prompting
          await new Promise((resolve) => setTimeout(resolve, CANCEL_SETTLE_DELAY_MS))

          // Send internal notification to agent (not shown to user)
          // Agent will then ask questions naturally as plain text
          const prompt = questionTexts
            ? `The "question" tool is not available in this environment. You tried to ask:\n\n${questionTexts}\n\nPlease ask these questions directly in the conversation (as plain text) so the user can respond.`
            : 'The "question" tool is not available in this environment. Please ask your question directly in the conversation instead of using the question tool.'

          await this.conductor.sendPrompt(multicaSessionId, [{ type: 'text', text: prompt }], {
            internal: true
          })
          console.log('[Question] Agent notified internally to ask directly')
        } catch (error) {
          console.error('[Question] Failed to handle question tool:', error)
        }
      })
    }
  }
}
