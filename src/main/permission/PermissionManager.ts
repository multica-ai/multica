/**
 * Permission Manager
 *
 * Central orchestration for permission requests between main process and renderer.
 * Manages pending requests, generates request IDs, routes responses, and delegates
 * to specialized handlers for AskUserQuestion and question tool workarounds.
 */
import { app, type BrowserWindow } from 'electron'
import type { Conductor } from '../conductor/Conductor'
import type { PermissionResponse } from '../../shared/electron-api'
import { IPC_CHANNELS } from '../../shared/ipc-channels'
import { isQuestionTool } from '../../shared/tool-names'
import { AskUserQuestionHandler } from './AskUserQuestionHandler'
import { QuestionToolWorkaround } from './QuestionToolWorkaround'
import type { PermissionRequestParams, PermissionOutcome, QuestionToolUpdate } from './types'

export class PermissionManager {
  private pendingRequests = new Map<string, (response: PermissionResponse) => void>()
  private askUserQuestionHandler: AskUserQuestionHandler
  private questionToolWorkaround: QuestionToolWorkaround
  private getMainWindow: () => BrowserWindow | null
  private conductor: Conductor

  constructor(conductor: Conductor, getMainWindow: () => BrowserWindow | null) {
    this.conductor = conductor
    this.getMainWindow = getMainWindow
    this.askUserQuestionHandler = new AskUserQuestionHandler(conductor)
    this.questionToolWorkaround = new QuestionToolWorkaround(conductor)
  }

  /**
   * Update dock badge count to show pending permission requests (macOS only)
   */
  private updateBadgeCount(): void {
    if (process.platform === 'darwin') {
      app.setBadgeCount(this.pendingRequests.size)
    }
  }

  /**
   * Handle permission request from ACP
   */
  async handlePermissionRequest(params: PermissionRequestParams): Promise<PermissionOutcome> {
    // Generate unique request ID
    const { randomUUID } = await import('crypto')
    const requestId = randomUUID()

    console.log(`[Permission] Request ${requestId}: ${params.toolCall.title}`)
    console.log(
      `[Permission]   Options:`,
      params.options.map((o) => `${o.name} (${o.optionId})`).join(', ')
    )

    const mainWindow = this.getMainWindow()
    // Get the multica session ID (internal) from ACP session ID
    const multicaSessionId = this.conductor.getMulticaSessionIdByAcp(params.sessionId)

    // Send permission request to renderer
    if (mainWindow && !mainWindow.isDestroyed()) {
      mainWindow.webContents.send(IPC_CHANNELS.PERMISSION_REQUEST, {
        requestId,
        sessionId: params.sessionId,
        multicaSessionId: multicaSessionId || params.sessionId, // Use multica session ID for filtering
        toolCall: {
          toolCallId: params.toolCall.toolCallId,
          title: params.toolCall.title,
          kind: params.toolCall.kind,
          status: params.toolCall.status,
          rawInput: params.toolCall.rawInput
        },
        options: params.options.map((o) => ({
          optionId: o.optionId,
          name: o.name,
          kind: o.kind
        }))
      })
    }

    // Wait for response from renderer
    return new Promise((resolve) => {
      this.pendingRequests.set(requestId, (response) => {
        console.log(`[Permission] Response ${requestId}: ${response.optionId}`)

        // Handle AskUserQuestion/question using G-3 mechanism
        const isQuestion = isQuestionTool(params.toolCall.title)
        console.log(
          `[Permission] Tool title check: "${params.toolCall.title}" isQuestionTool=${isQuestion}`
        )

        if (response.data && isQuestion) {
          // Log response data for debugging
          console.log(`[Permission] AskUserQuestion response data:`, {
            hasAnswers: !!response.data.answers,
            answersCount: response.data.answers?.length,
            hasSelectedOptions: !!response.data.selectedOptions,
            selectedOptionsCount: response.data.selectedOptions?.length,
            selectedOption: response.data.selectedOption,
            customText: response.data.customText
          })

          // Check if we have any answer
          const hasAnswer =
            (response.data.answers && response.data.answers.length > 0) ||
            response.data.selectedOptions ||
            response.data.selectedOption ||
            response.data.customText

          if (hasAnswer) {
            // Store the response for persistence (allows state restoration after restart)
            const multicaSessionId = this.conductor.getMulticaSessionIdByAcp(params.sessionId)
            if (multicaSessionId) {
              this.conductor.storeAskUserQuestionResponse(
                multicaSessionId,
                params.toolCall.toolCallId,
                response.data
              )
            }

            // Pass the full response data to G-3 handler
            this.askUserQuestionHandler.handle({
              acpSessionId: params.sessionId,
              toolCall: params.toolCall,
              responseData: response.data
            })
          }
        }

        // Return ACP compliant response with _meta for user answers
        const userAnswers = response.data?.answers
        const userAnswer =
          userAnswers?.map((a) => a.answer).join(', ') ||
          response.data?.selectedOptions?.join(', ') ||
          response.data?.selectedOption ||
          response.data?.customText

        resolve({
          outcome: {
            outcome: 'selected',
            optionId: response.optionId,
            _meta: response.data
              ? {
                  userAnswer,
                  userAnswers,
                  answerType:
                    userAnswers && userAnswers.length > 1
                      ? 'multi-question'
                      : response.data.selectedOptions
                        ? 'multi-selected'
                        : response.data.selectedOption
                          ? 'selected'
                          : 'custom'
                }
              : undefined
          }
        })
      })

      // Update badge count after adding pending request
      this.updateBadgeCount()

      // Timeout after 5 minutes (auto-deny)
      setTimeout(
        () => {
          if (this.pendingRequests.has(requestId)) {
            console.log(`[Permission] Timeout ${requestId}, auto-denying`)
            this.pendingRequests.delete(requestId)
            this.updateBadgeCount()
            // Find a deny option or use first option
            const denyOption =
              params.options.find((o) => (o.kind as string) === 'deny') || params.options[0]
            resolve({
              outcome: {
                outcome: 'selected',
                optionId: denyOption?.optionId ?? ''
              }
            })
          }
        },
        5 * 60 * 1000
      )
    })
  }

  /**
   * Handle permission response from renderer
   */
  handlePermissionResponse(response: PermissionResponse): void {
    console.log(`[Permission] Received response for ${response.requestId}: ${response.optionId}`)
    console.log(`[Permission] Response data:`, JSON.stringify(response.data, null, 2))

    if (response.data?.selectedOptions) {
      console.log(
        `[Permission] Multi-select detected: ${response.data.selectedOptions.length} options`
      )
    }

    const resolver = this.pendingRequests.get(response.requestId)
    if (resolver) {
      this.pendingRequests.delete(response.requestId)
      this.updateBadgeCount()
      resolver(response)
    } else {
      console.warn(`[Permission] No resolver found for requestId: ${response.requestId}`)
    }
  }

  /**
   * Handle session update for question tool workaround
   */
  handleSessionUpdate(params: { sessionId: string; update: QuestionToolUpdate }): void {
    this.questionToolWorkaround.handleToolUpdate(params)
  }
}
