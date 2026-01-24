import { describe, it, expect, vi, beforeEach } from 'vitest'
import { PromptHandler } from '../../../../src/main/conductor/PromptHandler'
import type {
  ISessionStore,
  IAgentProcessManager,
  IG3Workaround,
  ConductorEvents,
  SessionAgent
} from '../../../../src/main/conductor/types'
import type { AgentConfig } from '../../../../src/shared/types'

describe('PromptHandler', () => {
  let handler: PromptHandler
  let mockSessionStore: ISessionStore
  let mockAgentProcessManager: IAgentProcessManager
  let mockG3Workaround: IG3Workaround
  let mockEvents: ConductorEvents
  let mockEnsureAgent: ReturnType<typeof vi.fn>
  let mockSessionAgent: SessionAgent

  const mockAgentConfig: AgentConfig = {
    id: 'opencode',
    name: 'OpenCode',
    command: 'opencode',
    args: ['--mcp'],
    enabled: true
  }

  beforeEach(() => {
    // Create mock connection with prompt method
    const mockConnection = {
      prompt: vi.fn().mockResolvedValue({ stopReason: 'end_turn' }),
      cancel: vi.fn().mockResolvedValue(undefined)
    }

    mockSessionAgent = {
      // eslint-disable-next-line @typescript-eslint/no-explicit-any
      agentProcess: {} as any,
      // eslint-disable-next-line @typescript-eslint/no-explicit-any
      connection: mockConnection as any,
      agentConfig: mockAgentConfig,
      agentSessionId: 'acp-session-123',
      needsHistoryReplay: false
    }

    mockSessionStore = {
      initialize: vi.fn(),
      create: vi.fn(),
      get: vi.fn().mockResolvedValue({ session: {}, updates: [] }),
      list: vi.fn(),
      updateMeta: vi.fn(),
      delete: vi.fn(),
      appendUpdate: vi.fn().mockResolvedValue({ timestamp: new Date().toISOString() }),
      getByAgentSessionId: vi.fn()
    }

    mockAgentProcessManager = {
      start: vi.fn(),
      stop: vi.fn(),
      stopAll: vi.fn(),
      get: vi.fn().mockReturnValue(mockSessionAgent),
      set: vi.fn(),
      remove: vi.fn(),
      isRunning: vi.fn().mockReturnValue(true),
      getRunningSessionIds: vi.fn().mockReturnValue(['session-1']),
      getAgentConfig: vi.fn().mockReturnValue(mockAgentConfig)
    }

    mockG3Workaround = {
      addPendingAnswer: vi.fn(),
      getPendingAnswers: vi.fn().mockReturnValue([]),
      clearPendingAnswers: vi.fn(),
      storeResponse: vi.fn()
    }

    mockEvents = {
      onSessionUpdate: vi.fn(),
      onStatusChange: vi.fn()
    }

    mockEnsureAgent = vi.fn().mockResolvedValue(mockSessionAgent)

    handler = new PromptHandler({
      sessionStore: mockSessionStore,
      agentProcessManager: mockAgentProcessManager,
      g3Workaround: mockG3Workaround,
      events: mockEvents,
      ensureAgent: mockEnsureAgent
    })
  })

  describe('send', () => {
    it('should send prompt and return stop reason', async () => {
      const result = await handler.send('session-1', [{ type: 'text', text: 'Hello' }])

      expect(result).toBe('end_turn')
      expect(mockEnsureAgent).toHaveBeenCalledWith('session-1')
    })

    it('should convert MessageContent to ACP format', async () => {
      await handler.send('session-1', [
        { type: 'text', text: 'Hello' },
        { type: 'image', data: 'base64data', mimeType: 'image/png' }
      ])

      // eslint-disable-next-line @typescript-eslint/no-explicit-any
      const connection = mockSessionAgent.connection as any
      expect(connection.prompt).toHaveBeenCalledWith({
        sessionId: 'acp-session-123',
        prompt: [
          { type: 'text', text: 'Hello' },
          { type: 'image', data: 'base64data', mimeType: 'image/png' }
        ]
      })
    })

    it('should store user message before sending', async () => {
      await handler.send('session-1', [{ type: 'text', text: 'Hello' }])

      expect(mockSessionStore.appendUpdate).toHaveBeenCalledWith('session-1', {
        sessionId: 'acp-session-123',
        update: {
          sessionUpdate: 'user_message',
          content: [{ type: 'text', text: 'Hello' }],
          _internal: false
        }
      })
    })

    it('should mark internal messages with _internal flag', async () => {
      await handler.send('session-1', [{ type: 'text', text: 'Hello' }], { internal: true })

      expect(mockSessionStore.appendUpdate).toHaveBeenCalledWith('session-1', {
        sessionId: 'acp-session-123',
        update: {
          sessionUpdate: 'user_message',
          content: [{ type: 'text', text: 'Hello' }],
          _internal: true
        }
      })
    })

    it('should update processing state', async () => {
      expect(handler.isProcessing('session-1')).toBe(false)

      const sendPromise = handler.send('session-1', [{ type: 'text', text: 'Hello' }])

      // During send, should be processing
      expect(handler.isProcessing('session-1')).toBe(true)
      expect(mockEvents.onStatusChange).toHaveBeenCalled()

      await sendPromise

      // After send, should not be processing
      expect(handler.isProcessing('session-1')).toBe(false)
    })

    it('should inject pending G-3 answers', async () => {
      mockG3Workaround.getPendingAnswers = vi.fn().mockReturnValue([
        { question: 'What color?', answer: 'Blue' },
        { question: 'What size?', answer: 'Large' }
      ])

      await handler.send('session-1', [{ type: 'text', text: 'Hello' }])

      // eslint-disable-next-line @typescript-eslint/no-explicit-any
      const connection = mockSessionAgent.connection as any
      const callArgs = connection.prompt.mock.calls[0][0]

      // First element should be the injected answers
      expect(callArgs.prompt[0].type).toBe('text')
      expect(callArgs.prompt[0].text).toContain('[User\'s answer to "What color?"]: Blue')
      expect(callArgs.prompt[0].text).toContain('[User\'s answer to "What size?"]: Large')

      // Pending answers should be cleared
      expect(mockG3Workaround.clearPendingAnswers).toHaveBeenCalledWith('session-1')
    })

    it('should handle errors gracefully', async () => {
      // eslint-disable-next-line @typescript-eslint/no-explicit-any
      const mockConnection = mockSessionAgent.connection as any
      mockConnection.prompt.mockRejectedValue(new Error('Test error'))

      const result = await handler.send('session-1', [{ type: 'text', text: 'Hello' }])

      expect(result).toBe('error')
      expect(mockEvents.onSessionUpdate).toHaveBeenCalledWith(
        expect.objectContaining({
          sessionId: 'acp-session-123',
          update: expect.objectContaining({
            sessionUpdate: 'agent_message_chunk',
            content: expect.objectContaining({
              type: 'text',
              text: expect.stringContaining('Error:')
            })
          })
        }),
        'session-1' // multicaSessionId
      )
    })

    it('should clear processing state on error', async () => {
      // eslint-disable-next-line @typescript-eslint/no-explicit-any
      const mockConnection = mockSessionAgent.connection as any
      mockConnection.prompt.mockRejectedValue(new Error('Test error'))

      await handler.send('session-1', [{ type: 'text', text: 'Hello' }])

      expect(handler.isProcessing('session-1')).toBe(false)
    })

    it('should stop agent process on error to allow recovery', async () => {
      // eslint-disable-next-line @typescript-eslint/no-explicit-any
      const mockConnection = mockSessionAgent.connection as any
      mockConnection.prompt.mockRejectedValue({
        code: -32603,
        message: 'Internal error: only prompt commands are supported in streaming mode'
      })

      await handler.send('session-1', [{ type: 'text', text: 'Hello' }])

      // Should stop the agent so next request starts a fresh process
      expect(mockAgentProcessManager.stop).toHaveBeenCalledWith('session-1')
    })
  })

  describe('cancel', () => {
    it('should cancel ongoing request', async () => {
      await handler.cancel('session-1')

      // eslint-disable-next-line @typescript-eslint/no-explicit-any
      const connection = mockSessionAgent.connection as any
      expect(connection.cancel).toHaveBeenCalledWith({ sessionId: 'acp-session-123' })
    })

    it('should handle non-existent session gracefully', async () => {
      mockAgentProcessManager.get = vi.fn().mockReturnValue(undefined)

      await expect(handler.cancel('non-existent')).resolves.not.toThrow()
    })
  })

  describe('processing state', () => {
    it('should return processing session IDs', async () => {
      const sendPromise = handler.send('session-1', [{ type: 'text', text: 'Hello' }])

      expect(handler.getProcessingSessionIds()).toContain('session-1')

      await sendPromise
    })

    it('should track multiple processing sessions', async () => {
      // Start two prompts
      const mockSessionAgent2 = {
        ...mockSessionAgent,
        agentSessionId: 'acp-session-456'
      }
      mockEnsureAgent
        .mockResolvedValueOnce(mockSessionAgent)
        .mockResolvedValueOnce(mockSessionAgent2)

      const promise1 = handler.send('session-1', [{ type: 'text', text: 'Hello 1' }])
      const promise2 = handler.send('session-2', [{ type: 'text', text: 'Hello 2' }])

      expect(handler.getProcessingSessionIds()).toContain('session-1')
      expect(handler.getProcessingSessionIds()).toContain('session-2')

      await Promise.all([promise1, promise2])
    })
  })

  describe('history replay', () => {
    it('should prepend history for resumed sessions', async () => {
      const resumedSessionAgent = {
        ...mockSessionAgent,
        needsHistoryReplay: true
      }
      mockEnsureAgent.mockResolvedValue(resumedSessionAgent)

      mockSessionStore.get = vi.fn().mockResolvedValue({
        session: {},
        updates: [
          {
            timestamp: new Date().toISOString(),
            update: {
              update: {
                sessionUpdate: 'user_message',
                content: [{ type: 'text', text: 'Previous message' }]
              }
            }
          },
          {
            timestamp: new Date().toISOString(),
            update: {
              update: {
                sessionUpdate: 'agent_message_chunk',
                content: { type: 'text', text: 'Previous response' }
              }
            }
          }
        ]
      })

      await handler.send('session-1', [{ type: 'text', text: 'New message' }])

      // eslint-disable-next-line @typescript-eslint/no-explicit-any
      const connection = mockSessionAgent.connection as any
      const callArgs = connection.prompt.mock.calls[0][0]

      // First element should be the history
      expect(callArgs.prompt[0].type).toBe('text')
      expect(callArgs.prompt[0].text).toContain('[Session History')

      // Should mark as replayed
      expect(resumedSessionAgent.needsHistoryReplay).toBe(false)
    })

    it('should skip history replay if no meaningful history', async () => {
      const resumedSessionAgent = {
        ...mockSessionAgent,
        needsHistoryReplay: true
      }
      mockEnsureAgent.mockResolvedValue(resumedSessionAgent)

      mockSessionStore.get = vi.fn().mockResolvedValue({
        session: {},
        updates: [] // Empty history
      })

      await handler.send('session-1', [{ type: 'text', text: 'New message' }])

      // eslint-disable-next-line @typescript-eslint/no-explicit-any
      const connection = mockSessionAgent.connection as any
      const callArgs = connection.prompt.mock.calls[0][0]

      // Should just have the user message
      expect(callArgs.prompt).toHaveLength(1)
      expect(callArgs.prompt[0].text).toBe('New message')
    })

    it('should continue without history when sessionStore.get throws', async () => {
      const resumedSessionAgent = {
        ...mockSessionAgent,
        needsHistoryReplay: true
      }
      mockEnsureAgent.mockResolvedValue(resumedSessionAgent)

      // Simulate sessionStore.get throwing an error
      mockSessionStore.get = vi.fn().mockRejectedValue(new Error('Database connection failed'))

      const result = await handler.send('session-1', [{ type: 'text', text: 'New message' }])

      // Should still send the prompt successfully
      expect(result).toBe('end_turn')

      // Should mark as replayed even on error (to prevent repeated attempts)
      expect(resumedSessionAgent.needsHistoryReplay).toBe(false)

      // eslint-disable-next-line @typescript-eslint/no-explicit-any
      const connection = mockSessionAgent.connection as any
      const callArgs = connection.prompt.mock.calls[0][0]

      // Should just have the user message (no history)
      expect(callArgs.prompt).toHaveLength(1)
      expect(callArgs.prompt[0].text).toBe('New message')
    })
  })

  describe('error parsing', () => {
    it('should parse MCP missing environment variable error', async () => {
      // eslint-disable-next-line @typescript-eslint/no-explicit-any
      const mockConnection = mockSessionAgent.connection as any
      mockConnection.prompt.mockRejectedValue(
        new Error('Missing environment variables: OPENAI_API_KEY')
      )

      await handler.send('session-1', [{ type: 'text', text: 'Hello' }])

      expect(mockEvents.onSessionUpdate).toHaveBeenCalledWith(
        expect.objectContaining({
          update: expect.objectContaining({
            content: expect.objectContaining({
              text: expect.stringContaining(
                'MCP server requires environment variable: OPENAI_API_KEY'
              )
            })
          })
        }),
        'session-1' // multicaSessionId
      )
    })

    it('should parse MaxFileReadTokenExceededError', async () => {
      // eslint-disable-next-line @typescript-eslint/no-explicit-any
      const mockConnection = mockSessionAgent.connection as any
      mockConnection.prompt.mockRejectedValue(
        new Error('MaxFileReadTokenExceededError: file too large')
      )

      await handler.send('session-1', [{ type: 'text', text: 'Hello' }])

      expect(mockEvents.onSessionUpdate).toHaveBeenCalledWith(
        expect.objectContaining({
          update: expect.objectContaining({
            content: expect.objectContaining({
              text: expect.stringContaining('File is too large to read')
            })
          })
        }),
        'session-1' // multicaSessionId
      )
    })

    it('should parse mcp-config-invalid error', async () => {
      // eslint-disable-next-line @typescript-eslint/no-explicit-any
      const mockConnection = mockSessionAgent.connection as any
      mockConnection.prompt.mockRejectedValue(new Error('mcp-config-invalid: server name missing'))

      await handler.send('session-1', [{ type: 'text', text: 'Hello' }])

      expect(mockEvents.onSessionUpdate).toHaveBeenCalledWith(
        expect.objectContaining({
          update: expect.objectContaining({
            content: expect.objectContaining({
              text: expect.stringContaining('MCP server configuration is invalid')
            })
          })
        }),
        'session-1' // multicaSessionId
      )
    })

    it('should show error message in fallback for unknown Error instances', async () => {
      // eslint-disable-next-line @typescript-eslint/no-explicit-any
      const mockConnection = mockSessionAgent.connection as any
      mockConnection.prompt.mockRejectedValue(new Error('Some completely unknown error'))

      await handler.send('session-1', [{ type: 'text', text: 'Hello' }])

      expect(mockEvents.onSessionUpdate).toHaveBeenCalledWith(
        expect.objectContaining({
          update: expect.objectContaining({
            content: expect.objectContaining({
              text: expect.stringContaining('Agent error: Some completely unknown error')
            })
          })
        }),
        'session-1' // multicaSessionId
      )
    })

    it('should extract message from plain object errors (ACP errors)', async () => {
      // eslint-disable-next-line @typescript-eslint/no-explicit-any
      const mockConnection = mockSessionAgent.connection as any
      mockConnection.prompt.mockRejectedValue({
        code: -32603,
        message: 'Internal error: only prompt commands are supported in streaming mode'
      })

      await handler.send('session-1', [{ type: 'text', text: 'Hello' }])

      const callArgs = mockEvents.onSessionUpdate.mock.calls[0][0]
      const errorText = callArgs.update.content.text
      // Should NOT contain [object Object]
      expect(errorText).not.toContain('[object Object]')
      // Should contain the actual error message
      expect(errorText).toContain('only prompt commands are supported in streaming mode')
    })

    it('should JSON-stringify object errors without message property', async () => {
      // eslint-disable-next-line @typescript-eslint/no-explicit-any
      const mockConnection = mockSessionAgent.connection as any
      mockConnection.prompt.mockRejectedValue({ code: -32603, data: { details: 'something' } })

      await handler.send('session-1', [{ type: 'text', text: 'Hello' }])

      const callArgs = mockEvents.onSessionUpdate.mock.calls[0][0]
      const errorText = callArgs.update.content.text
      // Should NOT contain [object Object]
      expect(errorText).not.toContain('[object Object]')
      // Should contain stringified JSON
      expect(errorText).toContain('-32603')
    })

    it('should handle string errors', async () => {
      // eslint-disable-next-line @typescript-eslint/no-explicit-any
      const mockConnection = mockSessionAgent.connection as any
      mockConnection.prompt.mockRejectedValue('Raw string error')

      await handler.send('session-1', [{ type: 'text', text: 'Hello' }])

      const callArgs = mockEvents.onSessionUpdate.mock.calls[0][0]
      const errorText = callArgs.update.content.text
      expect(errorText).toContain('Raw string error')
    })

    it('should truncate long error messages', async () => {
      // eslint-disable-next-line @typescript-eslint/no-explicit-any
      const mockConnection = mockSessionAgent.connection as any
      const longError = 'A'.repeat(200)
      mockConnection.prompt.mockRejectedValue(new Error(longError))

      await handler.send('session-1', [{ type: 'text', text: 'Hello' }])

      expect(mockEvents.onSessionUpdate).toHaveBeenCalledWith(
        expect.objectContaining({
          update: expect.objectContaining({
            content: expect.objectContaining({
              // Error message should be truncated and end with "..."
              text: expect.stringMatching(/Agent error: A+\.\.\./)
            })
          })
        }),
        'session-1' // multicaSessionId
      )

      // Verify the error text doesn't exceed expected length
      const callArgs = mockEvents.onSessionUpdate.mock.calls[0][0]
      const errorText = callArgs.update.content.text
      expect(errorText.length).toBeLessThanOrEqual(200)
    })

    it('should parse ECONNREFUSED error', async () => {
      // eslint-disable-next-line @typescript-eslint/no-explicit-any
      const mockConnection = mockSessionAgent.connection as any
      mockConnection.prompt.mockRejectedValue(new Error('connect ECONNREFUSED 127.0.0.1:8080'))

      await handler.send('session-1', [{ type: 'text', text: 'Hello' }])

      expect(mockEvents.onSessionUpdate).toHaveBeenCalledWith(
        expect.objectContaining({
          update: expect.objectContaining({
            content: expect.objectContaining({
              text: expect.stringContaining('Failed to connect to agent')
            })
          })
        }),
        'session-1' // multicaSessionId
      )
    })

    it('should parse timeout error', async () => {
      // eslint-disable-next-line @typescript-eslint/no-explicit-any
      const mockConnection = mockSessionAgent.connection as any
      mockConnection.prompt.mockRejectedValue(new Error('Request timeout after 30000ms'))

      await handler.send('session-1', [{ type: 'text', text: 'Hello' }])

      expect(mockEvents.onSessionUpdate).toHaveBeenCalledWith(
        expect.objectContaining({
          update: expect.objectContaining({
            content: expect.objectContaining({
              text: expect.stringContaining('Request timed out')
            })
          })
        }),
        'session-1' // multicaSessionId
      )
    })

    it('should parse rate limit error', async () => {
      // eslint-disable-next-line @typescript-eslint/no-explicit-any
      const mockConnection = mockSessionAgent.connection as any
      mockConnection.prompt.mockRejectedValue(new Error('429 Too Many Requests'))

      await handler.send('session-1', [{ type: 'text', text: 'Hello' }])

      expect(mockEvents.onSessionUpdate).toHaveBeenCalledWith(
        expect.objectContaining({
          update: expect.objectContaining({
            content: expect.objectContaining({
              text: expect.stringContaining('Rate limit exceeded')
            })
          })
        }),
        'session-1' // multicaSessionId
      )
    })

    it('should parse authentication error', async () => {
      // eslint-disable-next-line @typescript-eslint/no-explicit-any
      const mockConnection = mockSessionAgent.connection as any
      mockConnection.prompt.mockRejectedValue(new Error('401 Unauthorized: Invalid API key'))

      await handler.send('session-1', [{ type: 'text', text: 'Hello' }])

      expect(mockEvents.onSessionUpdate).toHaveBeenCalledWith(
        expect.objectContaining({
          update: expect.objectContaining({
            content: expect.objectContaining({
              text: expect.stringContaining('Authentication failed')
            })
          })
        }),
        'session-1' // multicaSessionId
      )
    })
  })
})
