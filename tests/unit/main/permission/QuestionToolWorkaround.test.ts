import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'

import { QuestionToolWorkaround } from '../../../../src/main/permission/QuestionToolWorkaround'
import type { Conductor } from '../../../../src/main/conductor/Conductor'
import type { QuestionToolUpdate } from '../../../../src/main/permission/types'

describe('QuestionToolWorkaround', () => {
  let workaround: QuestionToolWorkaround
  let mockConductor: Conductor
  const originalConsoleLog = console.log
  const originalConsoleError = console.error
  const originalSetImmediate = global.setImmediate
  const originalSetTimeout = global.setTimeout

  beforeEach(() => {
    vi.useFakeTimers()

    // Mock console methods
    console.log = vi.fn()
    console.error = vi.fn()

    // Mock setImmediate to execute callback immediately
    global.setImmediate = vi.fn((callback: (...args: unknown[]) => void) => {
      callback()
      return {} as NodeJS.Immediate
    }) as unknown as typeof global.setImmediate

    mockConductor = {
      getMulticaSessionIdByAcp: vi.fn().mockReturnValue('multica-session-123'),
      cancelRequest: vi.fn().mockResolvedValue(undefined),
      sendPrompt: vi.fn().mockResolvedValue(undefined)
    } as unknown as Conductor

    workaround = new QuestionToolWorkaround(mockConductor)
  })

  afterEach(() => {
    vi.useRealTimers()
    console.log = originalConsoleLog
    console.error = originalConsoleError
    global.setImmediate = originalSetImmediate
    global.setTimeout = originalSetTimeout
    vi.clearAllMocks()
  })

  describe('handleToolUpdate - detection', () => {
    it('should detect question tool in progress', async () => {
      const update: QuestionToolUpdate = {
        sessionUpdate: 'tool_call_update',
        toolCallId: 'tool-123',
        title: 'question',
        status: 'in_progress',
        rawInput: {
          questions: [{ question: 'What is your name?' }]
        }
      }

      workaround.handleToolUpdate({ sessionId: 'acp-session-456', update })

      expect(mockConductor.getMulticaSessionIdByAcp).toHaveBeenCalledWith('acp-session-456')
      expect(console.log).toHaveBeenCalledWith(
        expect.stringContaining('Tool detected (will hang forever)')
      )
    })

    it('should ignore non-question tools', () => {
      const update: QuestionToolUpdate = {
        sessionUpdate: 'tool_call_update',
        toolCallId: 'tool-123',
        title: 'bash',
        status: 'in_progress'
      }

      workaround.handleToolUpdate({ sessionId: 'acp-session-456', update })

      expect(mockConductor.getMulticaSessionIdByAcp).not.toHaveBeenCalled()
    })

    it('should ignore question tool with non-in_progress status', () => {
      const update: QuestionToolUpdate = {
        sessionUpdate: 'tool_call_update',
        toolCallId: 'tool-123',
        title: 'question',
        status: 'completed'
      }

      workaround.handleToolUpdate({ sessionId: 'acp-session-456', update })

      expect(mockConductor.getMulticaSessionIdByAcp).not.toHaveBeenCalled()
    })

    it('should ignore non-tool_call_update events', () => {
      const update: QuestionToolUpdate = {
        sessionUpdate: 'other_event',
        toolCallId: 'tool-123',
        title: 'question',
        status: 'in_progress'
      }

      workaround.handleToolUpdate({ sessionId: 'acp-session-456', update })

      expect(mockConductor.getMulticaSessionIdByAcp).not.toHaveBeenCalled()
    })

    it('should handle missing title', () => {
      const update: QuestionToolUpdate = {
        sessionUpdate: 'tool_call_update',
        toolCallId: 'tool-123',
        status: 'in_progress'
      }

      workaround.handleToolUpdate({ sessionId: 'acp-session-456', update })

      expect(mockConductor.getMulticaSessionIdByAcp).not.toHaveBeenCalled()
    })

    it('should handle missing status', () => {
      const update: QuestionToolUpdate = {
        sessionUpdate: 'tool_call_update',
        toolCallId: 'tool-123',
        title: 'question'
      }

      workaround.handleToolUpdate({ sessionId: 'acp-session-456', update })

      expect(mockConductor.getMulticaSessionIdByAcp).not.toHaveBeenCalled()
    })
  })

  describe('handleToolUpdate - duplicate prevention', () => {
    it('should skip already handled toolCallIds', () => {
      const update: QuestionToolUpdate = {
        sessionUpdate: 'tool_call_update',
        toolCallId: 'tool-123',
        title: 'question',
        status: 'in_progress',
        rawInput: { questions: [{ question: 'Q1?' }] }
      }

      // First call
      workaround.handleToolUpdate({ sessionId: 'acp-session-456', update })
      expect(mockConductor.cancelRequest).toHaveBeenCalledTimes(1)

      // Second call with same toolCallId
      workaround.handleToolUpdate({ sessionId: 'acp-session-456', update })
      expect(mockConductor.cancelRequest).toHaveBeenCalledTimes(1)

      expect(console.log).toHaveBeenCalledWith(
        expect.stringContaining('Already handled toolCallId=tool-123')
      )
    })

    it('should handle different toolCallIds separately', () => {
      const update1: QuestionToolUpdate = {
        sessionUpdate: 'tool_call_update',
        toolCallId: 'tool-123',
        title: 'question',
        status: 'in_progress',
        rawInput: { questions: [{ question: 'Q1?' }] }
      }

      const update2: QuestionToolUpdate = {
        sessionUpdate: 'tool_call_update',
        toolCallId: 'tool-456',
        title: 'question',
        status: 'in_progress',
        rawInput: { questions: [{ question: 'Q2?' }] }
      }

      workaround.handleToolUpdate({ sessionId: 'acp-session-456', update: update1 })
      workaround.handleToolUpdate({ sessionId: 'acp-session-456', update: update2 })

      expect(mockConductor.cancelRequest).toHaveBeenCalledTimes(2)
    })

    it('should handle missing toolCallId (still processes)', () => {
      const update: QuestionToolUpdate = {
        sessionUpdate: 'tool_call_update',
        title: 'question',
        status: 'in_progress',
        rawInput: { questions: [{ question: 'Q?' }] }
      }

      workaround.handleToolUpdate({ sessionId: 'acp-session-456', update })

      // Should still process even without toolCallId
      expect(mockConductor.cancelRequest).toHaveBeenCalled()
    })
  })

  describe('handleToolUpdate - session mapping', () => {
    it('should return early if no Multica session found', () => {
      vi.mocked(mockConductor.getMulticaSessionIdByAcp).mockReturnValue(null)

      const update: QuestionToolUpdate = {
        sessionUpdate: 'tool_call_update',
        toolCallId: 'tool-123',
        title: 'question',
        status: 'in_progress',
        rawInput: { questions: [{ question: 'Q?' }] }
      }

      workaround.handleToolUpdate({ sessionId: 'unknown-acp-session', update })

      expect(mockConductor.cancelRequest).not.toHaveBeenCalled()
    })
  })

  describe('handleToolUpdate - question formatting', () => {
    it('should format single question without options', async () => {
      const update: QuestionToolUpdate = {
        sessionUpdate: 'tool_call_update',
        toolCallId: 'tool-123',
        title: 'question',
        status: 'in_progress',
        rawInput: {
          questions: [{ question: 'What is your name?' }]
        }
      }

      workaround.handleToolUpdate({ sessionId: 'acp-session-456', update })

      expect(console.log).toHaveBeenCalledWith(
        '[Question] Original questions:',
        '1. What is your name?'
      )
    })

    it('should format question with options', async () => {
      const update: QuestionToolUpdate = {
        sessionUpdate: 'tool_call_update',
        toolCallId: 'tool-123',
        title: 'question',
        status: 'in_progress',
        rawInput: {
          questions: [
            {
              question: 'Select a color:',
              options: [
                { label: 'Red', description: 'The color red' },
                { label: 'Blue', description: 'The color blue' }
              ]
            }
          ]
        }
      }

      workaround.handleToolUpdate({ sessionId: 'acp-session-456', update })

      expect(console.log).toHaveBeenCalledWith(
        '[Question] Original questions:',
        '1. Select a color:\n   Options: Red, Blue'
      )
    })

    it('should format multiple questions', async () => {
      const update: QuestionToolUpdate = {
        sessionUpdate: 'tool_call_update',
        toolCallId: 'tool-123',
        title: 'question',
        status: 'in_progress',
        rawInput: {
          questions: [
            { question: 'What is your name?' },
            { question: 'What is your age?', options: [{ label: '18-25' }, { label: '26-35' }] }
          ]
        }
      }

      workaround.handleToolUpdate({ sessionId: 'acp-session-456', update })

      expect(console.log).toHaveBeenCalledWith(
        '[Question] Original questions:',
        '1. What is your name?\n2. What is your age?\n   Options: 18-25, 26-35'
      )
    })

    it('should handle missing questions array', async () => {
      const update: QuestionToolUpdate = {
        sessionUpdate: 'tool_call_update',
        toolCallId: 'tool-123',
        title: 'question',
        status: 'in_progress',
        rawInput: {}
      }

      workaround.handleToolUpdate({ sessionId: 'acp-session-456', update })

      expect(console.log).toHaveBeenCalledWith('[Question] Original questions:', '')
    })

    it('should handle missing rawInput', async () => {
      const update: QuestionToolUpdate = {
        sessionUpdate: 'tool_call_update',
        toolCallId: 'tool-123',
        title: 'question',
        status: 'in_progress'
      }

      workaround.handleToolUpdate({ sessionId: 'acp-session-456', update })

      expect(console.log).toHaveBeenCalledWith('[Question] Original questions:', '')
    })
  })

  describe('handleToolUpdate - cancel and notify flow', () => {
    it('should cancel request and send notification', async () => {
      const update: QuestionToolUpdate = {
        sessionUpdate: 'tool_call_update',
        toolCallId: 'tool-123',
        title: 'question',
        status: 'in_progress',
        rawInput: {
          questions: [{ question: 'What is your name?' }]
        }
      }

      workaround.handleToolUpdate({ sessionId: 'acp-session-456', update })

      // Run timers to execute setImmediate and setTimeout callbacks
      await vi.runAllTimersAsync()

      expect(mockConductor.cancelRequest).toHaveBeenCalledWith('multica-session-123')

      const expectedPrompt = `The "question" tool is not available in this environment. You tried to ask:

1. What is your name?

Please ask these questions directly in the conversation (as plain text) so the user can respond.`

      expect(mockConductor.sendPrompt).toHaveBeenCalledWith(
        'multica-session-123',
        [{ type: 'text', text: expectedPrompt }],
        { internal: true }
      )

      expect(console.log).toHaveBeenCalledWith(
        '[Question] Agent notified internally to ask directly'
      )
    })

    it('should use generic prompt when no questions provided', async () => {
      const update: QuestionToolUpdate = {
        sessionUpdate: 'tool_call_update',
        toolCallId: 'tool-123',
        title: 'question',
        status: 'in_progress',
        rawInput: {}
      }

      workaround.handleToolUpdate({ sessionId: 'acp-session-456', update })

      // Run timers to execute setImmediate and setTimeout callbacks
      await vi.runAllTimersAsync()

      const expectedPrompt =
        'The "question" tool is not available in this environment. Please ask your question directly in the conversation instead of using the question tool.'

      expect(mockConductor.sendPrompt).toHaveBeenCalledWith(
        'multica-session-123',
        [{ type: 'text', text: expectedPrompt }],
        { internal: true }
      )
    })

    it('should wait for cancel to settle before re-prompting', async () => {
      // Use real timers for this test
      vi.useRealTimers()

      let setImmediateCallback: (() => void) | null = null
      global.setImmediate = vi.fn((callback: (...args: unknown[]) => void) => {
        setImmediateCallback = callback as () => void
        return {} as NodeJS.Immediate
      }) as unknown as typeof global.setImmediate

      const update: QuestionToolUpdate = {
        sessionUpdate: 'tool_call_update',
        toolCallId: 'tool-123',
        title: 'question',
        status: 'in_progress',
        rawInput: { questions: [{ question: 'Q?' }] }
      }

      workaround.handleToolUpdate({ sessionId: 'acp-session-456', update })

      expect(setImmediateCallback).not.toBeNull()

      // Execute the async flow
      if (setImmediateCallback) {
        await setImmediateCallback()
      }

      // Cancel should be called before sendPrompt
      expect(mockConductor.cancelRequest).toHaveBeenCalledBefore(mockConductor.sendPrompt)
    })

    it('should log error if handling fails', async () => {
      vi.mocked(mockConductor.cancelRequest).mockRejectedValue(new Error('Cancel failed'))

      const update: QuestionToolUpdate = {
        sessionUpdate: 'tool_call_update',
        toolCallId: 'tool-123',
        title: 'question',
        status: 'in_progress',
        rawInput: { questions: [{ question: 'Q?' }] }
      }

      workaround.handleToolUpdate({ sessionId: 'acp-session-456', update })

      // Run timers to execute setImmediate and setTimeout callbacks
      await vi.runAllTimersAsync()

      expect(console.error).toHaveBeenCalledWith(
        '[Question] Failed to handle question tool:',
        expect.any(Error)
      )
    })
  })

  describe('handleToolUpdate - handled set cleanup', () => {
    it('should clean up handled toolCallIds after delay', () => {
      const update: QuestionToolUpdate = {
        sessionUpdate: 'tool_call_update',
        toolCallId: 'tool-123',
        title: 'question',
        status: 'in_progress',
        rawInput: { questions: [{ question: 'Q?' }] }
      }

      // First call
      workaround.handleToolUpdate({ sessionId: 'acp-session-456', update })
      expect(mockConductor.cancelRequest).toHaveBeenCalledTimes(1)

      // Advance timers by 60 seconds (cleanup happens at 60s)
      vi.advanceTimersByTime(60000)

      // Second call should process again after cleanup
      workaround.handleToolUpdate({ sessionId: 'acp-session-456', update })
      expect(mockConductor.cancelRequest).toHaveBeenCalledTimes(2)
    })

    it('should not clean up before delay', () => {
      const update: QuestionToolUpdate = {
        sessionUpdate: 'tool_call_update',
        toolCallId: 'tool-123',
        title: 'question',
        status: 'in_progress',
        rawInput: { questions: [{ question: 'Q?' }] }
      }

      // First call
      workaround.handleToolUpdate({ sessionId: 'acp-session-456', update })
      expect(mockConductor.cancelRequest).toHaveBeenCalledTimes(1)

      // Advance timers by 59 seconds (just before cleanup)
      vi.advanceTimersByTime(59000)

      // Second call should still be skipped
      workaround.handleToolUpdate({ sessionId: 'acp-session-456', update })
      expect(mockConductor.cancelRequest).toHaveBeenCalledTimes(1)
    })
  })
})
