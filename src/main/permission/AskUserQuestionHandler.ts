/**
 * AskUserQuestion G-3 Handler
 *
 * Handles the G-3 mechanism for AskUserQuestion tool responses.
 *
 * Problem: ACP's AskUserQuestion tool has a design limitation where it only
 *          returns "User has answered the question(s)" to the Agent, without
 *          including the actual user selection. The Agent never knows which
 *          option the user picked.
 *
 * Solution (G-3 - Cancel + Re-prompt):
 * 1. Store the user's answer(s) in Conductor's pendingAnswers map
 * 2. Return the permission response to complete the tool call
 * 3. Asynchronously cancel the current agent turn
 * 4. Send a new prompt with the actual user answer(s)
 * 5. Agent now sees the user's selection(s) both from pendingAnswers and the message
 */
import type { Conductor } from '../conductor/Conductor'
import type { G3HandlerParams } from './types'

export class AskUserQuestionHandler {
  constructor(private conductor: Conductor) {}

  /**
   * Handle AskUserQuestion response using G-3 mechanism
   */
  async handle(params: G3HandlerParams): Promise<void> {
    const { acpSessionId, toolCall, responseData } = params

    const multicaSessionId = this.conductor.getMulticaSessionIdByAcp(acpSessionId)
    if (!multicaSessionId) {
      console.warn(`[G-3] Could not find Multica session ID for ACP session ${acpSessionId}`)
      return
    }

    // Check for multi-question answers first (new format)
    const answers = responseData.answers
    let userAnswerText: string

    if (answers && answers.length > 0) {
      // Multi-question support: store all Q&A pairs
      console.log(`[G-3] Processing ${answers.length} answers for session ${multicaSessionId}`)

      for (const item of answers) {
        this.conductor.addPendingAnswer(multicaSessionId, item.question, item.answer)
        console.log(`[G-3] Stored answer: "${item.question}" -> "${item.answer}"`)
      }

      // Build user answer text for re-prompt
      if (answers.length === 1) {
        userAnswerText = answers[0].answer
      } else {
        // Format multiple answers as numbered list
        userAnswerText = answers
          .map((item, idx) => `Q${idx + 1}: ${item.question}\nA${idx + 1}: ${item.answer}`)
          .join('\n\n')
      }
    } else {
      // Backward compatibility: single answer (old format)
      const rawInput = toolCall.rawInput as { questions?: Array<{ question: string }> } | undefined
      const questions = rawInput?.questions || []
      if (questions.length === 0) return

      const question = questions[0]?.question || 'Unknown question'
      const answer =
        responseData.selectedOptions?.join(', ') ||
        responseData.selectedOption ||
        responseData.customText ||
        ''
      if (!answer) return

      this.conductor.addPendingAnswer(multicaSessionId, question, answer)
      console.log(
        `[G-3] Stored answer for session ${multicaSessionId}: "${question}" -> "${answer}"`
      )
      userAnswerText = answer
    }

    // Cancel current turn and re-prompt (async, after permission response is sent)
    setImmediate(async () => {
      try {
        console.log(`[G-3] Cancelling current turn...`)
        await this.conductor.cancelRequest(multicaSessionId)

        // Wait for processing to complete (poll with timeout)
        // This ensures the ACP SDK has fully cleaned up its internal state
        const maxWaitMs = 2000
        const pollIntervalMs = 100
        let waited = 0
        while (this.conductor.isSessionProcessing(multicaSessionId) && waited < maxWaitMs) {
          await new Promise((r) => setTimeout(r, pollIntervalMs))
          waited += pollIntervalMs
        }

        if (waited >= maxWaitMs) {
          console.warn(`[G-3] Timed out waiting for cancel to complete, proceeding anyway`)
        } else {
          console.log(`[G-3] Cancel completed after ${waited}ms`)
        }

        // Additional delay for ACP SDK internal cleanup
        await new Promise((r) => setTimeout(r, 200))

        // Re-prompt with the actual user answer as message content
        // Use internal: true so the message is sent to agent but not displayed in UI
        await this.conductor.sendPrompt(
          multicaSessionId,
          [{ type: 'text', text: userAnswerText }],
          { internal: true }
        )
        console.log(
          `[G-3] Re-prompt sent with user answer: "${userAnswerText.slice(0, 100)}${userAnswerText.length > 100 ? '...' : ''}"`
        )
      } catch (error) {
        console.error(`[G-3] Error during cancel/re-prompt:`, error)
      }
    })
  }
}
