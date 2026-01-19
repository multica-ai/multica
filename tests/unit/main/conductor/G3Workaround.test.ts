import { describe, it, expect, vi, beforeEach } from 'vitest'
import { G3Workaround } from '../../../../src/main/conductor/G3Workaround'
import type { ISessionStore, ConductorEvents } from '../../../../src/main/conductor/types'

describe('G3Workaround', () => {
  let g3: G3Workaround
  let mockSessionStore: ISessionStore
  let mockEvents: ConductorEvents

  beforeEach(() => {
    mockSessionStore = {
      initialize: vi.fn(),
      create: vi.fn(),
      get: vi.fn(),
      list: vi.fn(),
      updateMeta: vi.fn(),
      delete: vi.fn(),
      appendUpdate: vi.fn().mockResolvedValue({ timestamp: new Date().toISOString() }),
      getByAgentSessionId: vi.fn()
    }

    mockEvents = {
      onSessionUpdate: vi.fn(),
      onStatusChange: vi.fn()
    }

    g3 = new G3Workaround({
      sessionStore: mockSessionStore,
      events: mockEvents
    })
  })

  describe('pendingAnswers', () => {
    it('should add pending answer for session', () => {
      g3.addPendingAnswer('session-1', 'What color?', 'Blue')

      const answers = g3.getPendingAnswers('session-1')
      expect(answers).toHaveLength(1)
      expect(answers[0]).toEqual({ question: 'What color?', answer: 'Blue' })
    })

    it('should accumulate multiple answers for same session', () => {
      g3.addPendingAnswer('session-1', 'Question 1?', 'Answer 1')
      g3.addPendingAnswer('session-1', 'Question 2?', 'Answer 2')

      const answers = g3.getPendingAnswers('session-1')
      expect(answers).toHaveLength(2)
      expect(answers[0]).toEqual({ question: 'Question 1?', answer: 'Answer 1' })
      expect(answers[1]).toEqual({ question: 'Question 2?', answer: 'Answer 2' })
    })

    it('should return empty array for unknown session', () => {
      const answers = g3.getPendingAnswers('unknown-session')
      expect(answers).toEqual([])
    })

    it('should keep answers separate per session', () => {
      g3.addPendingAnswer('session-1', 'Q1?', 'A1')
      g3.addPendingAnswer('session-2', 'Q2?', 'A2')

      expect(g3.getPendingAnswers('session-1')).toHaveLength(1)
      expect(g3.getPendingAnswers('session-2')).toHaveLength(1)
      expect(g3.getPendingAnswers('session-1')[0].answer).toBe('A1')
      expect(g3.getPendingAnswers('session-2')[0].answer).toBe('A2')
    })

    it('should clear pending answers for session', () => {
      g3.addPendingAnswer('session-1', 'Q?', 'A')
      expect(g3.getPendingAnswers('session-1')).toHaveLength(1)

      g3.clearPendingAnswers('session-1')
      expect(g3.getPendingAnswers('session-1')).toEqual([])
    })

    it('should not affect other sessions when clearing', () => {
      g3.addPendingAnswer('session-1', 'Q1?', 'A1')
      g3.addPendingAnswer('session-2', 'Q2?', 'A2')

      g3.clearPendingAnswers('session-1')

      expect(g3.getPendingAnswers('session-1')).toEqual([])
      expect(g3.getPendingAnswers('session-2')).toHaveLength(1)
    })
  })

  describe('storeResponse', () => {
    it('should persist response when sessionStore is available', async () => {
      const response = {
        selectedOption: 'Option A',
        answers: [{ question: 'Q?', answer: 'A' }]
      }

      await g3.storeResponse('session-1', 'tool-call-123', response)

      expect(mockSessionStore.appendUpdate).toHaveBeenCalledWith('session-1', {
        sessionId: 'session-1',
        update: {
          sessionUpdate: 'askuserquestion_response',
          toolCallId: 'tool-call-123',
          response
        }
      })
    })

    it('should emit onSessionUpdate event after storing', async () => {
      const response = { selectedOption: 'Option B' }

      await g3.storeResponse('session-1', 'tool-call-456', response)

      expect(mockEvents.onSessionUpdate).toHaveBeenCalledWith(
        {
          sessionId: 'session-1',
          update: {
            sessionUpdate: 'askuserquestion_response',
            toolCallId: 'tool-call-456',
            response
          }
        },
        undefined
      )
    })

    it('should skip persistence when sessionStore is null', async () => {
      const g3NoStore = new G3Workaround({
        sessionStore: null,
        events: mockEvents
      })

      await g3NoStore.storeResponse('session-1', 'tool-call', { selectedOption: 'A' })

      // Should not throw and should not call appendUpdate
      expect(mockSessionStore.appendUpdate).not.toHaveBeenCalled()
    })

    it('should not emit event when sessionStore is null', async () => {
      const g3NoStore = new G3Workaround({
        sessionStore: null,
        events: mockEvents
      })

      await g3NoStore.storeResponse('session-1', 'tool-call', { selectedOption: 'A' })

      // Should not emit since we can't persist
      expect(mockEvents.onSessionUpdate).not.toHaveBeenCalled()
    })
  })
})
