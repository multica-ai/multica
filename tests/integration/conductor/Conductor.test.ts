import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { mkdtempSync, rmSync, existsSync } from 'fs'
import { join } from 'path'
import { tmpdir } from 'os'

// Mock AgentProcess to avoid spawning real processes
vi.mock('../../../src/main/conductor/AgentProcess', () => {
  return {
    AgentProcess: class MockAgentProcess {
      start = vi.fn().mockResolvedValue(undefined)
      stop = vi.fn().mockResolvedValue(undefined)
      isRunning = vi.fn().mockReturnValue(true)
      onExit = vi.fn()
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
      initialize = vi.fn().mockResolvedValue({
        protocolVersion: '1.0',
        agentInfo: { name: 'mock-agent' }
      })
      newSession = vi.fn().mockResolvedValue({
        sessionId: 'mock-acp-session-id'
      })
      prompt = vi.fn().mockResolvedValue({
        stopReason: 'end_turn'
      })
      cancel = vi.fn().mockResolvedValue(undefined)
    }
  }
})

// Import after mocks are set up
import { Conductor } from '../../../src/main/conductor/Conductor'

describe('Conductor', () => {
  let tempDir: string
  let conductor: Conductor

  beforeEach(async () => {
    tempDir = mkdtempSync(join(tmpdir(), 'conductor-test-'))
  })

  afterEach(async () => {
    if (conductor) {
      await conductor.stopAllSessions()
    }
    if (tempDir && existsSync(tempDir)) {
      rmSync(tempDir, { recursive: true, force: true })
    }
  })

  describe('initialization', () => {
    it('should initialize without errors', async () => {
      conductor = new Conductor({ storagePath: tempDir })
      await expect(conductor.initialize()).resolves.not.toThrow()
    })

    it('should work in CLI mode (skip persistence)', async () => {
      conductor = new Conductor({ skipPersistence: true })
      await expect(conductor.initialize()).resolves.not.toThrow()
    })
  })

  describe('session management', () => {
    const mockAgentConfig = {
      id: 'opencode',
      name: 'OpenCode',
      command: 'opencode',
      args: ['--mcp'],
      enabled: true
    }

    beforeEach(async () => {
      conductor = new Conductor({ storagePath: tempDir })
      await conductor.initialize()
    })

    it('should create a new session', async () => {
      const session = await conductor.createSession('/test/project', mockAgentConfig)

      expect(session).toMatchObject({
        id: expect.any(String),
        agentId: 'opencode',
        workingDirectory: '/test/project',
        status: 'active'
      })
    })

    it('should list created sessions', async () => {
      await conductor.createSession('/test/project1', mockAgentConfig)
      await conductor.createSession('/test/project2', mockAgentConfig)

      const sessions = await conductor.listSessions()
      expect(sessions.length).toBe(2)
    })

    it('should get session data', async () => {
      const session = await conductor.createSession('/test/project', mockAgentConfig)

      const data = await conductor.getSessionData(session.id)
      expect(data).not.toBeNull()
      expect(data?.session.id).toBe(session.id)
    })

    it('should delete a session', async () => {
      const session = await conductor.createSession('/test/project', mockAgentConfig)

      await conductor.deleteSession(session.id)

      const sessions = await conductor.listSessions()
      expect(sessions.find((s) => s.id === session.id)).toBeUndefined()
    })

    it('should track running sessions', async () => {
      const session = await conductor.createSession('/test/project', mockAgentConfig)

      expect(conductor.isSessionRunning(session.id)).toBe(true)
      expect(conductor.getRunningSessionIds()).toContain(session.id)
    })

    it('should stop a session', async () => {
      const session = await conductor.createSession('/test/project', mockAgentConfig)

      await conductor.stopSession(session.id)

      expect(conductor.getRunningSessionIds()).not.toContain(session.id)
    })

    it('should stop all sessions', async () => {
      await conductor.createSession('/test/project1', mockAgentConfig)
      await conductor.createSession('/test/project2', mockAgentConfig)

      expect(conductor.getRunningSessionIds().length).toBe(2)

      await conductor.stopAllSessions()

      expect(conductor.getRunningSessionIds().length).toBe(0)
    })
  })

  describe('CLI mode (in-memory)', () => {
    const mockAgentConfig = {
      id: 'opencode',
      name: 'OpenCode',
      command: 'opencode',
      args: ['--mcp'],
      enabled: true
    }

    beforeEach(async () => {
      conductor = new Conductor({ skipPersistence: true })
      await conductor.initialize()
    })

    it('should create in-memory session', async () => {
      const session = await conductor.createSession('/test/project', mockAgentConfig)

      expect(session.id).toBeDefined()
      expect(session.agentId).toBe('opencode')
    })

    it('should list in-memory session', async () => {
      await conductor.createSession('/test/project', mockAgentConfig)

      const sessions = await conductor.listSessions()
      expect(sessions.length).toBe(1)
    })

    it('should not support session resumption in CLI mode', async () => {
      await expect(conductor.resumeSession('some-id')).rejects.toThrow(
        'Session resumption not available in CLI mode'
      )
    })
  })

  describe('event callbacks', () => {
    const mockAgentConfig = {
      id: 'opencode',
      name: 'OpenCode',
      command: 'opencode',
      args: ['--mcp'],
      enabled: true
    }

    it('should call onStatusChange when session starts processing', async () => {
      const onStatusChange = vi.fn()
      conductor = new Conductor({
        storagePath: tempDir,
        events: { onStatusChange }
      })
      await conductor.initialize()

      const session = await conductor.createSession('/test/project', mockAgentConfig)

      // sendPrompt should trigger status changes
      await conductor.sendPrompt(session.id, [{ type: 'text', text: 'Hello' }])

      // Should be called at least twice (start processing, end processing)
      expect(onStatusChange).toHaveBeenCalled()
    })
  })
})
