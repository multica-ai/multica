import { describe, it, expect, vi, beforeEach } from 'vitest'
import { SessionLifecycle } from '../../../../src/main/conductor/SessionLifecycle'
import type {
  ISessionStore,
  IAgentProcessManager,
  ConductorEvents,
  SessionAgent
} from '../../../../src/main/conductor/types'
import type { AgentConfig, MulticaSession } from '../../../../src/shared/types'

describe('SessionLifecycle', () => {
  let lifecycle: SessionLifecycle
  let mockSessionStore: ISessionStore
  let mockAgentProcessManager: IAgentProcessManager
  let mockEvents: ConductorEvents

  const mockAgentConfig: AgentConfig = {
    id: 'opencode',
    name: 'OpenCode',
    command: 'opencode',
    args: ['--mcp'],
    enabled: true
  }

  const mockProject = {
    id: 'project-123',
    name: 'Test Project',
    workingDirectory: '/test/project',
    createdAt: '2024-01-01T00:00:00Z',
    updatedAt: '2024-01-01T00:00:00Z',
    isExpanded: true
  }

  const mockSession: MulticaSession = {
    id: 'session-123',
    agentSessionId: 'acp-123',
    projectId: 'project-123',
    agentId: 'opencode',
    workingDirectory: '/test/project',
    createdAt: '2024-01-01T00:00:00Z',
    updatedAt: '2024-01-01T00:00:00Z',
    status: 'active',
    messageCount: 0
  }

  const mockSessionAgent: SessionAgent = {
    // eslint-disable-next-line @typescript-eslint/no-explicit-any
    agentProcess: {} as any,
    // eslint-disable-next-line @typescript-eslint/no-explicit-any
    connection: {} as any,
    agentConfig: mockAgentConfig,
    agentSessionId: 'acp-123',
    needsHistoryReplay: false
  }

  beforeEach(() => {
    mockSessionStore = {
      initialize: vi.fn().mockResolvedValue(undefined),
      create: vi.fn().mockResolvedValue(mockSession),
      get: vi.fn().mockResolvedValue({ session: mockSession, updates: [] } as SessionData),
      list: vi.fn().mockResolvedValue([mockSession]),
      updateMeta: vi.fn().mockResolvedValue(mockSession),
      delete: vi.fn().mockResolvedValue(undefined),
      appendUpdate: vi.fn().mockResolvedValue({ timestamp: new Date().toISOString() }),
      getByAgentSessionId: vi.fn().mockReturnValue(mockSession),
      getOrCreateProject: vi.fn().mockResolvedValue(mockProject)
    }

    mockAgentProcessManager = {
      start: vi.fn().mockResolvedValue({ connection: {}, agentSessionId: 'acp-new-123' }),
      stop: vi.fn().mockResolvedValue(undefined),
      stopAll: vi.fn().mockResolvedValue(undefined),
      get: vi.fn().mockReturnValue(undefined), // Default: no running agent
      set: vi.fn(),
      remove: vi.fn(),
      isRunning: vi.fn().mockReturnValue(false),
      getRunningSessionIds: vi.fn().mockReturnValue([]),
      getAgentConfig: vi.fn().mockReturnValue(mockAgentConfig)
    }

    mockEvents = {
      onSessionUpdate: vi.fn(),
      onStatusChange: vi.fn(),
      onSessionMetaUpdated: vi.fn()
    }

    lifecycle = new SessionLifecycle({
      sessionStore: mockSessionStore,
      agentProcessManager: mockAgentProcessManager,
      events: mockEvents
    })
  })

  describe('initialize', () => {
    it('should initialize session store', async () => {
      await lifecycle.initialize()
      expect(mockSessionStore.initialize).toHaveBeenCalled()
    })

    it('should handle missing session store (CLI mode)', async () => {
      const cliLifecycle = new SessionLifecycle({
        sessionStore: null,
        agentProcessManager: mockAgentProcessManager,
        events: mockEvents
      })

      await expect(cliLifecycle.initialize()).resolves.not.toThrow()
    })
  })

  describe('create', () => {
    it('should create session with persistence', async () => {
      const session = await lifecycle.create('/test/project', mockAgentConfig)

      expect(mockSessionStore.getOrCreateProject).toHaveBeenCalledWith('/test/project')
      expect(mockSessionStore.create).toHaveBeenCalledWith({
        projectId: 'project-123',
        agentSessionId: '',
        agentId: 'opencode'
      })
      expect(session).toEqual(mockSession)
    })

    it('should create in-memory session in CLI mode', async () => {
      const cliLifecycle = new SessionLifecycle({
        sessionStore: null,
        agentProcessManager: mockAgentProcessManager,
        events: mockEvents
      })

      const session = await cliLifecycle.create('/test/project', mockAgentConfig)

      expect(session.agentId).toBe('opencode')
      expect(session.workingDirectory).toBe('/test/project')
      expect(session.status).toBe('active')
      expect(cliLifecycle.getInMemorySession()).toBe(session)
    })
  })

  describe('load', () => {
    it('should load session without starting agent', async () => {
      const session = await lifecycle.load('session-123')

      expect(session).toEqual(mockSession)
      expect(mockAgentProcessManager.start).not.toHaveBeenCalled()
    })

    it('should throw for non-existent session', async () => {
      mockSessionStore.get = vi.fn().mockResolvedValue(null)

      await expect(lifecycle.load('non-existent')).rejects.toThrow('Session not found')
    })

    it('should throw in CLI mode', async () => {
      const cliLifecycle = new SessionLifecycle({
        sessionStore: null,
        agentProcessManager: mockAgentProcessManager,
        events: mockEvents
      })

      await expect(cliLifecycle.load('session-123')).rejects.toThrow('not available in CLI mode')
    })
  })

  describe('resume', () => {
    it('should start agent for existing session', async () => {
      await lifecycle.resume('session-123')

      expect(mockAgentProcessManager.start).toHaveBeenCalledWith(
        'session-123',
        expect.objectContaining({ id: 'opencode' }),
        '/test/project',
        true // isResumed
      )
      expect(mockSessionStore.updateMeta).toHaveBeenCalledWith('session-123', {
        agentSessionId: 'acp-new-123',
        status: 'active'
      })
    })

    it('should return existing session if agent already running', async () => {
      mockAgentProcessManager.get = vi.fn().mockReturnValue(mockSessionAgent)

      const result = await lifecycle.resume('session-123')

      expect(mockAgentProcessManager.start).not.toHaveBeenCalled()
      expect(result).toEqual(mockSession)
    })

    it('should throw for unknown agent', async () => {
      mockSessionStore.get = vi.fn().mockResolvedValue({
        session: { ...mockSession, agentId: 'unknown-agent' },
        updates: []
      })

      await expect(lifecycle.resume('session-123')).rejects.toThrow('Unknown agent')
    })
  })

  describe('delete', () => {
    it('should stop agent and delete from store', async () => {
      await lifecycle.delete('session-123')

      expect(mockAgentProcessManager.stop).toHaveBeenCalledWith('session-123')
      expect(mockSessionStore.delete).toHaveBeenCalledWith('session-123')
    })

    it('should clear in-memory session in CLI mode', async () => {
      const cliLifecycle = new SessionLifecycle({
        sessionStore: null,
        agentProcessManager: mockAgentProcessManager,
        events: mockEvents
      })

      await cliLifecycle.create('/test/project', mockAgentConfig)
      const inMemory = cliLifecycle.getInMemorySession()
      expect(inMemory).not.toBeNull()

      await cliLifecycle.delete(inMemory!.id)
      expect(cliLifecycle.getInMemorySession()).toBeNull()
    })
  })

  describe('updateMeta', () => {
    it('should update session metadata', async () => {
      await lifecycle.updateMeta('session-123', { title: 'New Title' })

      expect(mockSessionStore.updateMeta).toHaveBeenCalledWith('session-123', {
        title: 'New Title'
      })
    })

    it('should throw in CLI mode', async () => {
      const cliLifecycle = new SessionLifecycle({
        sessionStore: null,
        agentProcessManager: mockAgentProcessManager,
        events: mockEvents
      })

      await expect(cliLifecycle.updateMeta('session-123', {})).rejects.toThrow(
        'not available in CLI mode'
      )
    })
  })

  describe('switchAgent', () => {
    it('should stop current agent and start new one', async () => {
      await lifecycle.switchAgent('session-123', 'claude-code')

      expect(mockAgentProcessManager.stop).toHaveBeenCalledWith('session-123')
      expect(mockSessionStore.updateMeta).toHaveBeenCalledWith('session-123', {
        agentId: 'claude-code'
      })
      expect(mockAgentProcessManager.start).toHaveBeenCalledWith(
        'session-123',
        expect.objectContaining({ id: 'claude-code' }),
        '/test/project',
        true
      )
    })

    it('should notify frontend of metadata change', async () => {
      await lifecycle.switchAgent('session-123', 'claude-code')

      expect(mockEvents.onSessionMetaUpdated).toHaveBeenCalled()
    })

    it('should throw for unknown new agent', async () => {
      await expect(lifecycle.switchAgent('session-123', 'unknown-agent')).rejects.toThrow(
        'Unknown agent'
      )
    })

    it('should throw when session not found', async () => {
      mockSessionStore.get = vi.fn().mockResolvedValue(null)

      await expect(lifecycle.switchAgent('non-existent', 'claude-code')).rejects.toThrow(
        'Session not found'
      )
    })

    it('should throw in CLI mode', async () => {
      const cliLifecycle = new SessionLifecycle({
        sessionStore: null,
        agentProcessManager: mockAgentProcessManager,
        events: mockEvents
      })

      await expect(cliLifecycle.switchAgent('session-123', 'claude-code')).rejects.toThrow(
        'not available in CLI mode'
      )
    })
  })

  describe('list', () => {
    it('should list sessions from store', async () => {
      const sessions = await lifecycle.list()

      expect(sessions).toEqual([mockSession])
      expect(mockSessionStore.list).toHaveBeenCalled()
    })

    it('should return in-memory session in CLI mode', async () => {
      const cliLifecycle = new SessionLifecycle({
        sessionStore: null,
        agentProcessManager: mockAgentProcessManager,
        events: mockEvents
      })

      await cliLifecycle.create('/test/project', mockAgentConfig)
      const sessions = await cliLifecycle.list()

      expect(sessions).toHaveLength(1)
      expect(sessions[0].workingDirectory).toBe('/test/project')
    })

    it('should return empty array in CLI mode with no session', async () => {
      const cliLifecycle = new SessionLifecycle({
        sessionStore: null,
        agentProcessManager: mockAgentProcessManager,
        events: mockEvents
      })

      const sessions = await cliLifecycle.list()
      expect(sessions).toEqual([])
    })
  })

  describe('getData', () => {
    it('should return complete session data', async () => {
      const data = await lifecycle.getData('session-123')

      expect(data).toEqual({ session: mockSession, updates: [] })
    })

    it('should return null in CLI mode', async () => {
      const cliLifecycle = new SessionLifecycle({
        sessionStore: null,
        agentProcessManager: mockAgentProcessManager,
        events: mockEvents
      })

      const data = await cliLifecycle.getData('session-123')
      expect(data).toBeNull()
    })
  })

  describe('getSessionIdByAgentSessionId', () => {
    it('should find session ID from running agents', () => {
      mockAgentProcessManager.getRunningSessionIds = vi.fn().mockReturnValue(['session-123'])
      mockAgentProcessManager.get = vi.fn().mockReturnValue(mockSessionAgent)

      const sessionId = lifecycle.getSessionIdByAgentSessionId('acp-123')
      expect(sessionId).toBe('session-123')
    })

    it('should fallback to session store', () => {
      mockAgentProcessManager.getRunningSessionIds = vi.fn().mockReturnValue([])

      const sessionId = lifecycle.getSessionIdByAgentSessionId('acp-123')
      expect(sessionId).toBe('session-123')
      expect(mockSessionStore.getByAgentSessionId).toHaveBeenCalledWith('acp-123')
    })

    it('should return null if not found', () => {
      mockAgentProcessManager.getRunningSessionIds = vi.fn().mockReturnValue([])
      mockSessionStore.getByAgentSessionId = vi.fn().mockReturnValue(null)

      const sessionId = lifecycle.getSessionIdByAgentSessionId('unknown')
      expect(sessionId).toBeNull()
    })
  })

  describe('ensureAgentForSession', () => {
    it('should return existing agent if already running', async () => {
      mockAgentProcessManager.get = vi.fn().mockReturnValue(mockSessionAgent)

      const result = await lifecycle.ensureAgentForSession('session-123')

      expect(result).toBe(mockSessionAgent)
      expect(mockAgentProcessManager.start).not.toHaveBeenCalled()
    })

    it('should start agent if not running', async () => {
      mockAgentProcessManager.get = vi
        .fn()
        .mockReturnValueOnce(undefined) // First call: not running
        .mockReturnValueOnce(mockSessionAgent) // Second call: after start

      const sessionAgent = await lifecycle.ensureAgentForSession('session-123')

      expect(sessionAgent).toBe(mockSessionAgent)
      expect(mockAgentProcessManager.start).toHaveBeenCalledWith(
        'session-123',
        expect.objectContaining({ id: 'opencode' }),
        '/test/project',
        true
      )
      expect(mockEvents.onSessionMetaUpdated).toHaveBeenCalled()
    })

    it('should throw in CLI mode', async () => {
      const cliLifecycle = new SessionLifecycle({
        sessionStore: null,
        agentProcessManager: mockAgentProcessManager,
        events: mockEvents
      })

      await expect(cliLifecycle.ensureAgentForSession('session-123')).rejects.toThrow(
        'Cannot auto-start agent in CLI mode'
      )
    })

    it('should throw when session not found', async () => {
      mockAgentProcessManager.get = vi.fn().mockReturnValue(undefined)
      mockSessionStore.get = vi.fn().mockResolvedValue(null)

      await expect(lifecycle.ensureAgentForSession('non-existent')).rejects.toThrow(
        'Session not found'
      )
    })

    it('should throw when agent config is unknown', async () => {
      mockAgentProcessManager.get = vi.fn().mockReturnValue(undefined)
      mockSessionStore.get = vi.fn().mockResolvedValue({
        session: { ...mockSession, agentId: 'unknown-agent' },
        updates: []
      })

      await expect(lifecycle.ensureAgentForSession('session-123')).rejects.toThrow('Unknown agent')
    })
  })

  describe('getInMemorySession', () => {
    it('should return null when no in-memory session exists', () => {
      expect(lifecycle.getInMemorySession()).toBeNull()
    })

    it('should return in-memory session after creation in CLI mode', async () => {
      const cliLifecycle = new SessionLifecycle({
        sessionStore: null,
        agentProcessManager: mockAgentProcessManager,
        events: mockEvents
      })

      expect(cliLifecycle.getInMemorySession()).toBeNull()

      await cliLifecycle.create('/test/project', mockAgentConfig)

      const inMemory = cliLifecycle.getInMemorySession()
      expect(inMemory).not.toBeNull()
      expect(inMemory?.workingDirectory).toBe('/test/project')
      expect(inMemory?.agentId).toBe('opencode')
    })
  })
})
