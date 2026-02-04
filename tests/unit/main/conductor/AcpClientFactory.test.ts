/**
 * Tests for AcpClientFactory - creates ACP SDK Client implementations
 */
import { describe, it, expect, vi, beforeEach } from 'vitest'
import type { SessionNotification, RequestPermissionRequest } from '@agentclientprotocol/sdk'
import {
  createAcpClient,
  AcpClientCallbacks,
  AcpClientFactoryOptions
} from '../../../../src/main/conductor/AcpClientFactory'
import type { ISessionStore, StoredSessionUpdate } from '../../../../src/main/conductor/types'

describe('AcpClientFactory', () => {
  let mockSessionStore: ISessionStore
  let callbacks: AcpClientCallbacks

  beforeEach(() => {
    vi.clearAllMocks()

    // Create mock session store
    mockSessionStore = {
      initialize: vi.fn(),
      createProject: vi.fn(),
      getOrCreateProject: vi.fn(),
      getProject: vi.fn(),
      listProjects: vi.fn(),
      listProjectsWithSessions: vi.fn(),
      updateProject: vi.fn(),
      toggleProjectExpanded: vi.fn(),
      reorderProjects: vi.fn(),
      deleteProject: vi.fn(),
      create: vi.fn(),
      get: vi.fn(),
      list: vi.fn(),
      updateMeta: vi.fn(),
      delete: vi.fn(),
      archiveSession: vi.fn(),
      unarchiveSession: vi.fn(),
      listArchivedSessions: vi.fn(),
      appendUpdate: vi.fn().mockResolvedValue({
        sequenceNumber: 1,
        timestamp: new Date().toISOString(),
        update: {}
      } as StoredSessionUpdate),
      getByAgentSessionId: vi.fn()
    } as unknown as ISessionStore

    callbacks = {
      onSessionUpdate: vi.fn(),
      onPermissionRequest: vi.fn(),
      onModeUpdate: vi.fn(),
      onModelUpdate: vi.fn(),
      onAvailableCommandsUpdate: vi.fn(),
      onUpdateStarted: vi.fn()
    }
  })

  describe('createAcpClient', () => {
    it('should create a client with sessionUpdate and requestPermission methods', () => {
      const options: AcpClientFactoryOptions = {
        sessionStore: mockSessionStore,
        callbacks
      }

      const client = createAcpClient('session-123', options)

      expect(client).toBeDefined()
      expect(typeof client.sessionUpdate).toBe('function')
      expect(typeof client.requestPermission).toBe('function')
    })

    it('should work with null sessionStore', () => {
      const options: AcpClientFactoryOptions = {
        sessionStore: null,
        callbacks
      }

      const client = createAcpClient('session-123', options)
      expect(client).toBeDefined()
    })
  })

  describe('sessionUpdate', () => {
    it('should store update and trigger callback with sequence number', async () => {
      const options: AcpClientFactoryOptions = {
        sessionStore: mockSessionStore,
        callbacks
      }

      const client = createAcpClient('session-123', options)

      const update: SessionNotification = {
        update: {
          sessionUpdate: 'agent_message_chunk',
          content: { type: 'text', text: 'Hello world' }
        }
      }

      await client.sessionUpdate(update)

      expect(mockSessionStore.appendUpdate).toHaveBeenCalledWith('session-123', update)
      expect(callbacks.onSessionUpdate).toHaveBeenCalledWith(update, 'session-123', 1)
    })

    it('should handle agent_thought_chunk updates', async () => {
      const options: AcpClientFactoryOptions = {
        sessionStore: mockSessionStore,
        callbacks
      }

      const client = createAcpClient('session-123', options)

      const update: SessionNotification = {
        update: {
          sessionUpdate: 'agent_thought_chunk',
          content: { type: 'text', text: 'Thinking...' }
        }
      }

      await client.sessionUpdate(update)

      expect(callbacks.onSessionUpdate).toHaveBeenCalled()
    })

    it('should handle tool_call updates', async () => {
      const options: AcpClientFactoryOptions = {
        sessionStore: mockSessionStore,
        callbacks
      }

      const client = createAcpClient('session-123', options)

      const update: SessionNotification = {
        update: {
          sessionUpdate: 'tool_call',
          toolCallId: 'tool-1',
          title: 'Read file',
          status: 'pending'
        }
      }

      await client.sessionUpdate(update)

      expect(callbacks.onSessionUpdate).toHaveBeenCalled()
    })

    it('should handle tool_call_update updates', async () => {
      const options: AcpClientFactoryOptions = {
        sessionStore: mockSessionStore,
        callbacks
      }

      const client = createAcpClient('session-123', options)

      const update: SessionNotification = {
        update: {
          sessionUpdate: 'tool_call_update',
          toolCallId: 'tool-1',
          status: 'complete'
        }
      }

      await client.sessionUpdate(update)

      expect(callbacks.onSessionUpdate).toHaveBeenCalled()
    })

    it('should handle current_mode_update and trigger onModeUpdate callback', async () => {
      const options: AcpClientFactoryOptions = {
        sessionStore: mockSessionStore,
        callbacks
      }

      const client = createAcpClient('session-123', options)

      const update: SessionNotification = {
        update: {
          sessionUpdate: 'current_mode_update',
          currentModeId: 'code-mode'
        }
      }

      await client.sessionUpdate(update)

      expect(callbacks.onModeUpdate).toHaveBeenCalledWith('code-mode')
    })

    it('should handle available_commands_update and trigger callback', async () => {
      const options: AcpClientFactoryOptions = {
        sessionStore: mockSessionStore,
        callbacks
      }

      const client = createAcpClient('session-123', options)

      const commands = [
        { name: '/help', description: 'Show help', input: null },
        { name: '/clear', description: 'Clear chat', input: null }
      ]

      const update: SessionNotification = {
        update: {
          sessionUpdate: 'available_commands_update',
          availableCommands: commands
        }
      }

      await client.sessionUpdate(update)

      expect(callbacks.onAvailableCommandsUpdate).toHaveBeenCalledWith(commands)
    })

    it('should track pending updates via onUpdateStarted', async () => {
      const options: AcpClientFactoryOptions = {
        sessionStore: mockSessionStore,
        callbacks
      }

      const client = createAcpClient('session-123', options)

      const update: SessionNotification = {
        update: {
          sessionUpdate: 'agent_message_chunk',
          content: { type: 'text', text: 'Hello' }
        }
      }

      await client.sessionUpdate(update)

      expect(callbacks.onUpdateStarted).toHaveBeenCalled()
      // The callback should receive a promise
      const call = (callbacks.onUpdateStarted as ReturnType<typeof vi.fn>).mock.calls[0]
      expect(call[0]).toBeInstanceOf(Promise)
    })

    it('should work without sessionStore (null)', async () => {
      const options: AcpClientFactoryOptions = {
        sessionStore: null,
        callbacks
      }

      const client = createAcpClient('session-123', options)

      const update: SessionNotification = {
        update: {
          sessionUpdate: 'agent_message_chunk',
          content: { type: 'text', text: 'Hello' }
        }
      }

      await client.sessionUpdate(update)

      // Should still trigger callback, but without sequence number
      expect(callbacks.onSessionUpdate).toHaveBeenCalledWith(update, 'session-123', undefined)
    })

    it('should handle storage errors gracefully', async () => {
      const consoleErrorSpy = vi.spyOn(console, 'error').mockImplementation(() => {})

      const failingStore = {
        ...mockSessionStore,
        appendUpdate: vi.fn().mockRejectedValue(new Error('Storage failed'))
      } as unknown as ISessionStore

      const options: AcpClientFactoryOptions = {
        sessionStore: failingStore,
        callbacks
      }

      const client = createAcpClient('session-123', options)

      const update: SessionNotification = {
        update: {
          sessionUpdate: 'agent_message_chunk',
          content: { type: 'text', text: 'Hello' }
        }
      }

      // Should not throw
      await client.sessionUpdate(update)

      // Should log error
      expect(consoleErrorSpy).toHaveBeenCalledWith(
        '[Conductor] Failed to store session update:',
        expect.any(Error)
      )

      // Should still trigger callback (without sequence number since storage failed)
      expect(callbacks.onSessionUpdate).toHaveBeenCalled()

      consoleErrorSpy.mockRestore()
    })
  })

  describe('requestPermission', () => {
    it('should delegate to callback when provided', async () => {
      const permissionCallback = vi.fn().mockResolvedValue({
        outcome: { outcome: 'selected', optionId: 'option-1' }
      })

      const options: AcpClientFactoryOptions = {
        sessionStore: mockSessionStore,
        callbacks: {
          ...callbacks,
          onPermissionRequest: permissionCallback
        }
      }

      const client = createAcpClient('session-123', options)

      const request: RequestPermissionRequest = {
        toolCall: {
          toolCallId: 'tool-1',
          title: 'Execute command',
          input: { command: 'ls -la' }
        },
        options: [
          { optionId: 'approve', label: 'Approve' },
          { optionId: 'deny', label: 'Deny' }
        ]
      }

      const response = await client.requestPermission(request)

      expect(permissionCallback).toHaveBeenCalledWith(request)
      expect(response).toEqual({
        outcome: { outcome: 'selected', optionId: 'option-1' }
      })
    })

    it('should auto-approve when no callback provided', async () => {
      const consoleLogSpy = vi.spyOn(console, 'log').mockImplementation(() => {})

      const options: AcpClientFactoryOptions = {
        sessionStore: mockSessionStore,
        callbacks: {
          // No onPermissionRequest callback
        }
      }

      const client = createAcpClient('session-123', options)

      const request: RequestPermissionRequest = {
        toolCall: {
          toolCallId: 'tool-1',
          title: 'Execute command',
          input: {}
        },
        options: [
          { optionId: 'approve', label: 'Approve' },
          { optionId: 'deny', label: 'Deny' }
        ]
      }

      const response = await client.requestPermission(request)

      expect(consoleLogSpy).toHaveBeenCalledWith('[Conductor] Auto-approving: Execute command')
      expect(response).toEqual({
        outcome: { outcome: 'selected', optionId: 'approve' }
      })

      consoleLogSpy.mockRestore()
    })

    it('should handle empty options array gracefully', async () => {
      const options: AcpClientFactoryOptions = {
        sessionStore: mockSessionStore,
        callbacks: {}
      }

      const client = createAcpClient('session-123', options)

      const request: RequestPermissionRequest = {
        toolCall: {
          toolCallId: 'tool-1',
          title: 'Risky operation',
          input: {}
        },
        options: []
      }

      const response = await client.requestPermission(request)

      expect(response).toEqual({
        outcome: { outcome: 'selected', optionId: '' }
      })
    })
  })

  describe('raw updates', () => {
    it('should log raw updates that are not sessionUpdate type', async () => {
      const consoleLogSpy = vi.spyOn(console, 'log').mockImplementation(() => {})

      const options: AcpClientFactoryOptions = {
        sessionStore: mockSessionStore,
        callbacks
      }

      const client = createAcpClient('session-123', options)

      // Create an update that doesn't have sessionUpdate property
      const update = {
        update: {
          // Some other update type
          customUpdate: 'value'
        }
      } as unknown as SessionNotification

      await client.sessionUpdate(update)

      expect(consoleLogSpy).toHaveBeenCalledWith('[ACP] raw update:', update)

      consoleLogSpy.mockRestore()
    })
  })
})
