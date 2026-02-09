import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'

import { AskUserQuestionHandler } from '../../../../src/main/permission/AskUserQuestionHandler'
import type { Conductor } from '../../../../src/main/conductor/Conductor'
import type { G3HandlerParams } from '../../../../src/main/permission/types'

describe('AskUserQuestionHandler', () => {
  let handler: AskUserQuestionHandler
  let mockConductor: Conductor
  const originalConsoleWarn = console.warn
  const originalConsoleLog = console.log
  const originalConsoleError = console.error
  const originalSetImmediate = global.setImmediate

  beforeEach(() => {
    vi.useFakeTimers()

    // Mock console methods
    console.warn = vi.fn()
    console.log = vi.fn()
    console.error = vi.fn()

    // Mock setImmediate to schedule callback
    global.setImmediate = vi.fn((callback: (...args: unknown[]) => void) => {
      return setTimeout(callback, 0) as unknown as NodeJS.Immediate
    }) as unknown as typeof global.setImmediate

    mockConductor = {
      getMulticaSessionIdByAcp: vi.fn().mockReturnValue('multica-session-123'),
      addPendingAnswer: vi.fn(),
      cancelRequest: vi.fn().mockResolvedValue(undefined),
      isSessionProcessing: vi.fn().mockReturnValue(false),
      sendPrompt: vi.fn().mockResolvedValue(undefined)
    } as unknown as Conductor

    handler = new AskUserQuestionHandler(mockConductor)
  })

  afterEach(() => {
    vi.useRealTimers()
    console.warn = originalConsoleWarn
    console.log = originalConsoleLog
    console.error = originalConsoleError
    global.setImmediate = originalSetImmediate
    vi.clearAllMocks()
  })

  describe('handle - multi-question format', () => {
    it('should store multiple answers and re-prompt with formatted text', async () => {
      const params: G3HandlerParams = {
        acpSessionId: 'acp-session-456',
        toolCall: { rawInput: {} },
        responseData: {
          answers: [
            { question: 'What is your name?', answer: 'John' },
            { question: 'What is your age?', answer: '30' }
          ]
        }
      }

      await handler.handle(params)

      // Should map ACP session to Multica session
      expect(mockConductor.getMulticaSessionIdByAcp).toHaveBeenCalledWith('acp-session-456')

      // Should store each answer
      expect(mockConductor.addPendingAnswer).toHaveBeenCalledTimes(2)
      expect(mockConductor.addPendingAnswer).toHaveBeenNthCalledWith(
        1,
        'multica-session-123',
        'What is your name?',
        'John'
      )
      expect(mockConductor.addPendingAnswer).toHaveBeenNthCalledWith(
        2,
        'multica-session-123',
        'What is your age?',
        '30'
      )

      // Should log the processing
      expect(console.log).toHaveBeenCalledWith(expect.stringContaining('Processing 2 answers'))
    })

    it('should handle single answer in multi-question format', async () => {
      const params: G3HandlerParams = {
        acpSessionId: 'acp-session-456',
        toolCall: { rawInput: {} },
        responseData: {
          answers: [{ question: 'What is your name?', answer: 'John' }]
        }
      }

      await handler.handle(params)

      expect(mockConductor.addPendingAnswer).toHaveBeenCalledTimes(1)
      expect(mockConductor.addPendingAnswer).toHaveBeenCalledWith(
        'multica-session-123',
        'What is your name?',
        'John'
      )
    })

    it('should handle empty answers array', async () => {
      const params: G3HandlerParams = {
        acpSessionId: 'acp-session-456',
        toolCall: { rawInput: {} },
        responseData: {
          answers: []
        }
      }

      await handler.handle(params)

      // Should not add any answers, should fall through to backward compatibility
      expect(mockConductor.addPendingAnswer).not.toHaveBeenCalled()
    })
  })

  describe('handle - backward compatibility (single question)', () => {
    it('should handle single question with selectedOption', async () => {
      const params: G3HandlerParams = {
        acpSessionId: 'acp-session-456',
        toolCall: {
          rawInput: {
            questions: [{ question: 'Do you agree?' }]
          }
        },
        responseData: {
          selectedOption: 'Yes'
        }
      }

      await handler.handle(params)

      expect(mockConductor.addPendingAnswer).toHaveBeenCalledWith(
        'multica-session-123',
        'Do you agree?',
        'Yes'
      )
    })

    it('should handle single question with selectedOptions array', async () => {
      const params: G3HandlerParams = {
        acpSessionId: 'acp-session-456',
        toolCall: {
          rawInput: {
            questions: [{ question: 'Select colors:' }]
          }
        },
        responseData: {
          selectedOptions: ['Red', 'Blue']
        }
      }

      await handler.handle(params)

      expect(mockConductor.addPendingAnswer).toHaveBeenCalledWith(
        'multica-session-123',
        'Select colors:',
        'Red, Blue'
      )
    })

    it('should handle single question with customText', async () => {
      const params: G3HandlerParams = {
        acpSessionId: 'acp-session-456',
        toolCall: {
          rawInput: {
            questions: [{ question: 'Enter your feedback:' }]
          }
        },
        responseData: {
          customText: 'Great service!'
        }
      }

      await handler.handle(params)

      expect(mockConductor.addPendingAnswer).toHaveBeenCalledWith(
        'multica-session-123',
        'Enter your feedback:',
        'Great service!'
      )
    })

    it('should return early if no questions in rawInput', async () => {
      const params: G3HandlerParams = {
        acpSessionId: 'acp-session-456',
        toolCall: { rawInput: {} },
        responseData: {}
      }

      await handler.handle(params)

      expect(mockConductor.addPendingAnswer).not.toHaveBeenCalled()
    })

    it('should return early if no answer provided', async () => {
      const params: G3HandlerParams = {
        acpSessionId: 'acp-session-456',
        toolCall: {
          rawInput: {
            questions: [{ question: 'Do you agree?' }]
          }
        },
        responseData: {}
      }

      await handler.handle(params)

      expect(mockConductor.addPendingAnswer).not.toHaveBeenCalled()
    })
  })

  describe('handle - session mapping', () => {
    it('should warn and return if no Multica session found', async () => {
      vi.mocked(mockConductor.getMulticaSessionIdByAcp).mockReturnValue(null)

      const params: G3HandlerParams = {
        acpSessionId: 'unknown-acp-session',
        toolCall: { rawInput: {} },
        responseData: { answers: [{ question: 'Q?', answer: 'A' }] }
      }

      await handler.handle(params)

      expect(console.warn).toHaveBeenCalledWith(
        expect.stringContaining('Could not find Multica session ID')
      )
      expect(mockConductor.addPendingAnswer).not.toHaveBeenCalled()
    })
  })

  describe('handle - cancel and re-prompt flow', () => {
    it('should cancel current turn and re-prompt with answer', async () => {
      const params: G3HandlerParams = {
        acpSessionId: 'acp-session-456',
        toolCall: { rawInput: {} },
        responseData: {
          answers: [{ question: 'What?', answer: 'Answer text' }]
        }
      }

      await handler.handle(params)

      // Run setImmediate callbacks
      await vi.runAllTimersAsync()

      // Should cancel the request
      expect(mockConductor.cancelRequest).toHaveBeenCalledWith('multica-session-123')

      // Should send re-prompt with internal flag
      expect(mockConductor.sendPrompt).toHaveBeenCalledWith(
        'multica-session-123',
        [{ type: 'text', text: 'Answer text' }],
        { internal: true }
      )
    })

    it('should poll while session is processing', async () => {
      // Session is processing initially, then stops after a few polls
      let pollCount = 0
      vi.mocked(mockConductor.isSessionProcessing).mockImplementation(() => {
        pollCount++
        return pollCount < 3 // Stop processing after 2 polls
      })

      const params: G3HandlerParams = {
        acpSessionId: 'acp-session-456',
        toolCall: { rawInput: {} },
        responseData: {
          answers: [{ question: 'What?', answer: 'Yes' }]
        }
      }

      await handler.handle(params)

      // Run all timers to complete the async flow
      await vi.runAllTimersAsync()

      // Should have checked processing state multiple times
      expect(mockConductor.isSessionProcessing).toHaveBeenCalledTimes(3)
      expect(mockConductor.sendPrompt).toHaveBeenCalled()
    })

    it('should handle timeout waiting for cancel', async () => {
      // Session always processing (timeout scenario after 2s = 20 polls at 100ms each)
      vi.mocked(mockConductor.isSessionProcessing).mockReturnValue(true)

      const params: G3HandlerParams = {
        acpSessionId: 'acp-session-456',
        toolCall: { rawInput: {} },
        responseData: {
          answers: [{ question: 'What?', answer: 'Yes' }]
        }
      }

      await handler.handle(params)

      // Advance timers to trigger timeout (maxWaitMs = 2000, pollIntervalMs = 100)
      await vi.advanceTimersByTimeAsync(2500)

      // Should still proceed after timeout
      expect(mockConductor.sendPrompt).toHaveBeenCalled()
    })

    it('should log error if cancel/re-prompt fails', async () => {
      vi.mocked(mockConductor.cancelRequest).mockRejectedValue(new Error('Cancel failed'))

      const params: G3HandlerParams = {
        acpSessionId: 'acp-session-456',
        toolCall: { rawInput: {} },
        responseData: {
          answers: [{ question: 'What?', answer: 'Yes' }]
        }
      }

      await handler.handle(params)

      // Run all timers to complete the async flow
      await vi.runAllTimersAsync()

      expect(console.error).toHaveBeenCalledWith(
        expect.stringContaining('Error during cancel/re-prompt'),
        expect.any(Error)
      )
    })

    it('should format multiple answers for re-prompt', async () => {
      const params: G3HandlerParams = {
        acpSessionId: 'acp-session-456',
        toolCall: { rawInput: {} },
        responseData: {
          answers: [
            { question: 'Name?', answer: 'John' },
            { question: 'Age?', answer: '30' }
          ]
        }
      }

      await handler.handle(params)

      // Run all timers to complete the async flow
      await vi.runAllTimersAsync()

      const expectedText = 'Q1: Name?\nA1: John\n\nQ2: Age?\nA2: 30'
      expect(mockConductor.sendPrompt).toHaveBeenCalledWith(
        'multica-session-123',
        [{ type: 'text', text: expectedText }],
        { internal: true }
      )
    })
  })
})
