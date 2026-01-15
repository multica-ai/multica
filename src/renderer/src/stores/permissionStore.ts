/**
 * Permission request state management
 *
 * Tracks both pending and responded requests to support showing
 * the user's selection in completed state (especially for AskUserQuestion).
 *
 * Multi-question support:
 * When AskUserQuestion contains multiple questions, the store tracks:
 * - currentQuestionIndex: which question is being displayed
 * - collectedAnswers: answers collected so far
 * The IPC response is only sent after ALL questions are answered.
 */
import { create } from 'zustand'
import type {
  PermissionRequest,
  PermissionResponse,
  PermissionResponseData,
  QuestionAnswer
} from '../../../shared/electron-api'

// AskUserQuestion rawInput structure (for type safety)
interface QuestionOption {
  label: string
  description?: string
}

interface Question {
  question: string
  header?: string
  options: QuestionOption[]
  multiSelect?: boolean
}

interface AskUserQuestionInput {
  questions?: Question[]
}

// Responded request with user's selection (exported for use in components)
export interface RespondedRequest {
  request: PermissionRequest
  response: {
    optionId: string
    selectedOption?: string // Single selection
    selectedOptions?: string[] // Multi-selection
    customText?: string
    answers?: QuestionAnswer[] // Multi-question answers
    timestamp: number
  }
}

interface PermissionStore {
  // Pending permission requests queue (supports concurrent requests)
  pendingRequests: PermissionRequest[]

  // Responded requests (requestId -> responded info)
  respondedRequests: Map<string, RespondedRequest>

  // Last responded request (for showing completed state in chat)
  lastRespondedRequest: RespondedRequest | null

  // === Multi-question support ===
  // Current question index (0-based) for multi-question AskUserQuestion
  currentQuestionIndex: number

  // Collected answers for multi-question AskUserQuestion
  collectedAnswers: QuestionAnswer[]

  // Get current request (first in queue)
  getCurrentRequest: () => PermissionRequest | null

  // Add pending request to queue
  addPendingRequest: (request: PermissionRequest) => void

  // Respond to permission request (with optional data for AskUserQuestion)
  respondToRequest: (optionId: string, data?: PermissionResponseData) => void

  // Answer current question and advance to next (for multi-question support)
  answerCurrentQuestion: (answer: string, isCustom?: boolean) => void

  // Get responded request by ID
  getRespondedRequest: (requestId: string) => RespondedRequest | undefined

  // Get responded request by tool call ID (for ToolCallItem to check AskUserQuestion status)
  getRespondedByToolCallId: (toolCallId: string) => RespondedRequest | undefined

  // Clear last responded (after it's been displayed)
  clearLastResponded: () => void
}

export const usePermissionStore = create<PermissionStore>((set, get) => ({
  pendingRequests: [],
  respondedRequests: new Map(),
  lastRespondedRequest: null,
  currentQuestionIndex: 0,
  collectedAnswers: [],

  getCurrentRequest: () => {
    return get().pendingRequests[0] || null
  },

  addPendingRequest: (request) => {
    set((state) => {
      const isQueueEmpty = state.pendingRequests.length === 0
      return {
        pendingRequests: [...state.pendingRequests, request],
        // Only reset multi-question state when queue was empty
        ...(isQueueEmpty
          ? {
              lastRespondedRequest: null,
              currentQuestionIndex: 0,
              collectedAnswers: []
            }
          : {})
      }
    })
  },

  respondToRequest: (optionId, data) => {
    const { pendingRequests, respondedRequests } = get()
    const currentRequest = pendingRequests[0]

    if (!currentRequest) {
      console.warn('[PermissionStore] No pending request to respond to')
      return
    }

    console.log('[PermissionStore] respondToRequest called:', {
      optionId,
      data,
      hasSelectedOptions: !!data?.selectedOptions,
      selectedOptionsLength: data?.selectedOptions?.length,
      hasAnswers: !!data?.answers,
      answersLength: data?.answers?.length,
      queueLength: pendingRequests.length
    })

    // Create responded request object
    const respondedRequest: RespondedRequest = {
      request: currentRequest,
      response: {
        optionId,
        selectedOption: data?.selectedOption,
        selectedOptions: data?.selectedOptions,
        customText: data?.customText,
        answers: data?.answers,
        timestamp: Date.now()
      }
    }

    // Store the responded request for later reference
    const newResponded = new Map(respondedRequests)
    newResponded.set(currentRequest.requestId, respondedRequest)

    const response: PermissionResponse = {
      requestId: currentRequest.requestId,
      optionId,
      data
    }

    // Send response to main process
    window.electronAPI.respondToPermission(response)

    // Remove current request from queue (shift), store responded
    // Reset multi-question state for the next request
    set({
      pendingRequests: pendingRequests.slice(1),
      respondedRequests: newResponded,
      lastRespondedRequest: respondedRequest,
      currentQuestionIndex: 0,
      collectedAnswers: []
    })
  },

  answerCurrentQuestion: (answer, isCustom = false) => {
    const { pendingRequests, currentQuestionIndex, collectedAnswers } = get()
    const currentRequest = pendingRequests[0]

    if (!currentRequest) {
      console.warn('[PermissionStore] No pending request for answerCurrentQuestion')
      return
    }

    // Get questions from rawInput
    const rawInput = currentRequest.toolCall.rawInput as AskUserQuestionInput | undefined
    const questions = rawInput?.questions || []
    const currentQuestion = questions[currentQuestionIndex]

    if (!currentQuestion) {
      console.warn('[PermissionStore] No question at index', currentQuestionIndex)
      return
    }

    // Collect current answer
    const newAnswer: QuestionAnswer = {
      question: currentQuestion.question,
      answer,
      isCustom
    }
    const newAnswers = [...collectedAnswers, newAnswer]
    const nextIndex = currentQuestionIndex + 1

    console.log(
      `[PermissionStore] answerCurrentQuestion: index=${currentQuestionIndex}, total=${questions.length}, nextIndex=${nextIndex}`
    )

    // Check if there are more questions
    if (nextIndex < questions.length) {
      // More questions remaining, advance to next
      console.log(`[PermissionStore] Advancing to question ${nextIndex + 1} of ${questions.length}`)
      set({
        currentQuestionIndex: nextIndex,
        collectedAnswers: newAnswers
      })
    } else {
      // All questions answered, send the response
      console.log(`[PermissionStore] All ${questions.length} questions answered, sending response`)

      // Find allow option
      const allowOption =
        currentRequest.options.find((o) => o.kind === 'allow_once') ||
        currentRequest.options.find((o) => o.kind === 'allow') ||
        currentRequest.options[0]

      // Build response data with all answers
      const responseData: PermissionResponseData = {
        answers: newAnswers,
        // For backward compatibility with single question
        selectedOption:
          newAnswers.length === 1 && !newAnswers[0].isCustom ? newAnswers[0].answer : undefined,
        customText:
          newAnswers.length === 1 && newAnswers[0].isCustom ? newAnswers[0].answer : undefined
      }

      // Use respondToRequest to send the final response
      get().respondToRequest(allowOption.optionId, responseData)
    }
  },

  getRespondedRequest: (requestId) => {
    return get().respondedRequests.get(requestId)
  },

  getRespondedByToolCallId: (toolCallId) => {
    const { respondedRequests } = get()
    // Search through respondedRequests to find one matching the toolCallId
    for (const responded of respondedRequests.values()) {
      if (responded.request.toolCall?.toolCallId === toolCallId) {
        return responded
      }
    }
    return undefined
  },

  clearLastResponded: () => {
    set({ lastRespondedRequest: null })
  }
}))
