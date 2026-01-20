import { describe, it, expect, vi, beforeEach } from 'vitest'

// Store exit callbacks and error states for testing
const testState = {
  exitCallbacks: new Map<string, (code: number | null, signal: string | null) => void>(),
  instanceCounter: 0,
  mockStartError: null as Error | null,
  mockInitializeError: null as Error | null,
  mockNewSessionError: null as Error | null
}

// Mock AgentProcess to avoid spawning real processes
vi.mock('../../../../src/main/conductor/AgentProcess', () => {
  return {
    AgentProcess: class MockAgentProcess {
      private instanceId: string

      constructor() {
        this.instanceId = `instance-${testState.instanceCounter++}`
      }

      start = vi.fn().mockImplementation(() => {
        if (testState.mockStartError) {
          return Promise.reject(testState.mockStartError)
        }
        return Promise.resolve(undefined)
      })

      stop = vi.fn().mockResolvedValue(undefined)
      isRunning = vi.fn().mockReturnValue(true)

      onExit = vi.fn((cb: (code: number | null, signal: string | null) => void) => {
        testState.exitCallbacks.set(this.instanceId, cb)
      })

      getPid = vi.fn().mockReturnValue(12345)
      getStdinWeb = vi.fn().mockReturnValue(new WritableStream())
      getStdoutWeb = vi.fn().mockReturnValue(new ReadableStream())
    }
  }
})

// Mock ACP SDK
vi.mock('@agentclientprotocol/sdk', () => {
  return {
    PROTOCOL_VERSION: '1.0',
    ndJsonStream: vi.fn().mockReturnValue({}),
    ClientSideConnection: class MockClientSideConnection {
      initialize = vi.fn().mockImplementation(() => {
        if (testState.mockInitializeError) {
          return Promise.reject(testState.mockInitializeError)
        }
        return Promise.resolve({
          protocolVersion: '1.0',
          agentInfo: { name: 'mock-agent' }
        })
      })

      newSession = vi.fn().mockImplementation(() => {
        if (testState.mockNewSessionError) {
          return Promise.reject(testState.mockNewSessionError)
        }
        return Promise.resolve({
          sessionId: 'mock-acp-session-id'
        })
      })

      prompt = vi.fn().mockResolvedValue({
        stopReason: 'end_turn'
      })

      cancel = vi.fn().mockResolvedValue(undefined)
    }
  }
})

// Import after mocks are set up
import { AgentProcessManager } from '../../../../src/main/conductor/AgentProcessManager'
import type { ISessionStore, ConductorEvents } from '../../../../src/main/conductor/types'
import type { AgentConfig } from '../../../../src/shared/types'

describe('AgentProcessManager', () => {
  let manager: AgentProcessManager
  let mockSessionStore: ISessionStore
  let mockEvents: ConductorEvents

  const mockAgentConfig: AgentConfig = {
    id: 'opencode',
    name: 'OpenCode',
    command: 'opencode',
    args: ['--mcp'],
    enabled: true
  }

  beforeEach(() => {
    // Reset all test state
    testState.exitCallbacks.clear()
    testState.instanceCounter = 0
    testState.mockStartError = null
    testState.mockInitializeError = null
    testState.mockNewSessionError = null

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

    manager = new AgentProcessManager({
      sessionStore: mockSessionStore,
      events: mockEvents
    })
  })

  describe('start', () => {
    it('should start agent process and return connection info', async () => {
      const result = await manager.start('session-1', mockAgentConfig, '/test/project')

      expect(result.agentSessionId).toBe('mock-acp-session-id')
      expect(result.connection).toBeDefined()
    })

    it('should store session in sessions map', async () => {
      await manager.start('session-1', mockAgentConfig, '/test/project')

      expect(manager.get('session-1')).toBeDefined()
      expect(manager.get('session-1')?.agentConfig).toBe(mockAgentConfig)
    })

    it('should set needsHistoryReplay for resumed sessions', async () => {
      await manager.start('session-1', mockAgentConfig, '/test/project', true)

      const session = manager.get('session-1')
      expect(session?.needsHistoryReplay).toBe(true)
    })

    it('should not set needsHistoryReplay for new sessions', async () => {
      await manager.start('session-1', mockAgentConfig, '/test/project', false)

      const session = manager.get('session-1')
      expect(session?.needsHistoryReplay).toBe(false)
    })
  })

  describe('stop', () => {
    it('should stop agent process and remove from map', async () => {
      await manager.start('session-1', mockAgentConfig, '/test/project')
      expect(manager.get('session-1')).toBeDefined()

      await manager.stop('session-1')
      expect(manager.get('session-1')).toBeUndefined()
    })

    it('should handle stopping non-existent session gracefully', async () => {
      await expect(manager.stop('non-existent')).resolves.not.toThrow()
    })
  })

  describe('stopAll', () => {
    it('should stop all running agent processes', async () => {
      await manager.start('session-1', mockAgentConfig, '/test/project')
      await manager.start('session-2', mockAgentConfig, '/test/project2')
      expect(manager.getRunningSessionIds()).toHaveLength(2)

      await manager.stopAll()
      expect(manager.getRunningSessionIds()).toHaveLength(0)
    })
  })

  describe('state queries', () => {
    it('should return running session IDs', async () => {
      await manager.start('session-1', mockAgentConfig, '/test/project')
      await manager.start('session-2', mockAgentConfig, '/test/project2')

      const runningIds = manager.getRunningSessionIds()
      expect(runningIds).toContain('session-1')
      expect(runningIds).toContain('session-2')
    })

    it('should check if session is running', async () => {
      await manager.start('session-1', mockAgentConfig, '/test/project')

      expect(manager.isRunning('session-1')).toBe(true)
      expect(manager.isRunning('non-existent')).toBe(false)
    })

    it('should return agent config for session', async () => {
      await manager.start('session-1', mockAgentConfig, '/test/project')

      expect(manager.getAgentConfig('session-1')).toBe(mockAgentConfig)
      expect(manager.getAgentConfig('non-existent')).toBeNull()
    })
  })

  describe('set and remove', () => {
    it('should allow setting session agent directly', async () => {
      const mockSessionAgent = {
        // eslint-disable-next-line @typescript-eslint/no-explicit-any
        agentProcess: {} as any,
        // eslint-disable-next-line @typescript-eslint/no-explicit-any
        connection: {} as any,
        agentConfig: mockAgentConfig,
        agentSessionId: 'acp-123',
        needsHistoryReplay: false
      }

      manager.set('session-1', mockSessionAgent)
      expect(manager.get('session-1')).toBe(mockSessionAgent)
    })

    it('should allow removing session agent directly', async () => {
      await manager.start('session-1', mockAgentConfig, '/test/project')
      expect(manager.get('session-1')).toBeDefined()

      manager.remove('session-1')
      expect(manager.get('session-1')).toBeUndefined()
    })
  })

  describe('onExit callback', () => {
    it('should clean up session when agent process exits', async () => {
      await manager.start('session-1', mockAgentConfig, '/test/project')
      expect(manager.get('session-1')).toBeDefined()

      // Get the exit callback that was registered
      const exitCallback = testState.exitCallbacks.get('instance-0')
      expect(exitCallback).toBeDefined()

      // Simulate agent process exit with code 0
      exitCallback!(0, null)

      // Session should be cleaned up
      expect(manager.get('session-1')).toBeUndefined()
    })

    it('should clean up session on signal exit', async () => {
      await manager.start('session-1', mockAgentConfig, '/test/project')
      expect(manager.get('session-1')).toBeDefined()

      // Simulate signal exit (e.g., SIGTERM)
      const exitCallback = testState.exitCallbacks.get('instance-0')
      exitCallback!(null, 'SIGTERM')

      expect(manager.get('session-1')).toBeUndefined()
    })

    it('should clean up session on non-zero exit code', async () => {
      await manager.start('session-1', mockAgentConfig, '/test/project')
      expect(manager.get('session-1')).toBeDefined()

      // Simulate crash with exit code 1
      const exitCallback = testState.exitCallbacks.get('instance-0')
      exitCallback!(1, null)

      expect(manager.get('session-1')).toBeUndefined()
    })
  })

  describe('error handling', () => {
    it('should propagate error when agent process fails to start', async () => {
      testState.mockStartError = new Error('Failed to spawn process')

      await expect(manager.start('session-1', mockAgentConfig, '/test/project')).rejects.toThrow(
        'Failed to spawn process'
      )

      // Session should not be in the map
      expect(manager.get('session-1')).toBeUndefined()
    })

    it('should propagate error when ACP initialization fails', async () => {
      testState.mockInitializeError = new Error('Protocol version mismatch')

      await expect(manager.start('session-1', mockAgentConfig, '/test/project')).rejects.toThrow(
        'Protocol version mismatch'
      )

      // Session should not be in the map
      expect(manager.get('session-1')).toBeUndefined()
    })

    it('should propagate error when ACP session creation fails', async () => {
      testState.mockNewSessionError = new Error('Session limit exceeded')

      await expect(manager.start('session-1', mockAgentConfig, '/test/project')).rejects.toThrow(
        'Session limit exceeded'
      )

      // Session should not be in the map
      expect(manager.get('session-1')).toBeUndefined()
    })
  })
})
