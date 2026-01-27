/**
 * Tests for AgentProcess - manages subprocess lifecycle
 *
 * These tests mock child_process.spawn at the module level to avoid spawning real processes.
 */
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { EventEmitter } from 'events'
import { Readable, Writable } from 'stream'

// Create a mock process factory
function createMockProcess() {
  const proc = new EventEmitter() as EventEmitter & {
    stdin: Writable
    stdout: Readable
    pid: number
    kill: ReturnType<typeof vi.fn>
  }

  proc.stdin = new Writable({
    write(_chunk, _encoding, callback) {
      callback()
    }
  })
  proc.stdout = new Readable({
    read() {
      // Empty implementation
    }
  })
  proc.pid = 12345
  proc.kill = vi.fn()

  return proc
}

// Global mock state
let mockProcess: ReturnType<typeof createMockProcess>
const mockSpawn = vi.fn()

// Mock modules BEFORE importing the module under test
vi.mock('node:child_process', () => ({
  spawn: (...args: unknown[]) => mockSpawn(...args)
}))

vi.mock('../../../../src/main/utils/path', () => ({
  getEnhancedPath: vi.fn(() => '/custom/path:/usr/bin')
}))

// Import after mocking
import { AgentProcess } from '../../../../src/main/conductor/AgentProcess'

describe('AgentProcess', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    mockProcess = createMockProcess()
    mockSpawn.mockReturnValue(mockProcess)
  })

  afterEach(() => {
    vi.clearAllMocks()
  })

  describe('constructor', () => {
    it('should create an AgentProcess with config', () => {
      const config = {
        id: 'test-agent',
        name: 'Test Agent',
        command: 'node',
        args: ['--version'],
        env: { TEST_VAR: 'value' }
      }

      const agent = new AgentProcess(config)
      expect(agent).toBeDefined()
      expect(agent.isRunning()).toBe(false)
    })
  })

  describe('start', () => {
    it('should spawn the process with correct config', async () => {
      const config = {
        id: 'test-agent',
        name: 'Test Agent',
        command: 'node',
        args: ['--version'],
        env: { TEST_VAR: 'value' }
      }

      const agent = new AgentProcess(config)

      // Start the agent
      const startPromise = agent.start()

      // Wait for the startup timeout (100ms in the actual code)
      await new Promise((resolve) => setTimeout(resolve, 150))
      await startPromise

      expect(mockSpawn).toHaveBeenCalledWith('node', ['--version'], {
        stdio: ['pipe', 'pipe', 'inherit'],
        env: expect.objectContaining({
          TEST_VAR: 'value',
          PATH: '/custom/path:/usr/bin'
        })
      })

      expect(agent.isRunning()).toBe(true)
    })

    it('should throw if already running', async () => {
      const config = {
        id: 'test-agent',
        name: 'Test Agent',
        command: 'node',
        args: []
      }

      const agent = new AgentProcess(config)

      // Start the first time
      const startPromise = agent.start()
      await new Promise((resolve) => setTimeout(resolve, 150))
      await startPromise

      // Try to start again
      await expect(agent.start()).rejects.toThrow('Agent process already running')
    })

    it('should handle spawn error', async () => {
      const config = {
        id: 'test-agent',
        name: 'Test Agent',
        command: 'nonexistent-command',
        args: []
      }

      const agent = new AgentProcess(config)

      // Simulate spawn error after a short delay
      const startPromise = agent.start()

      // Emit error before the 100ms timeout
      setTimeout(() => {
        mockProcess.emit('error', new Error('spawn ENOENT'))
      }, 10)

      await expect(startPromise).rejects.toThrow('spawn ENOENT')
    })
  })

  describe('stop', () => {
    it('should do nothing if not running', async () => {
      const config = {
        id: 'test-agent',
        name: 'Test Agent',
        command: 'node',
        args: []
      }

      const agent = new AgentProcess(config)

      // Should not throw
      await agent.stop()
      expect(mockProcess.kill).not.toHaveBeenCalled()
    })

    it('should send SIGTERM and wait for exit', async () => {
      const config = {
        id: 'test-agent',
        name: 'Test Agent',
        command: 'node',
        args: []
      }

      const agent = new AgentProcess(config)

      // Start the process
      const startPromise = agent.start()
      await new Promise((resolve) => setTimeout(resolve, 150))
      await startPromise

      // Start stopping
      const stopPromise = agent.stop()

      // Simulate process exit
      setTimeout(() => {
        mockProcess.emit('exit', 0, null)
      }, 50)

      await stopPromise

      expect(mockProcess.kill).toHaveBeenCalledWith('SIGTERM')
    })

    it('should force kill with SIGKILL after timeout', async () => {
      vi.useFakeTimers()

      const config = {
        id: 'test-agent',
        name: 'Test Agent',
        command: 'node',
        args: []
      }

      const agent = new AgentProcess(config)

      // Start the process
      const startPromise = agent.start()
      await vi.advanceTimersByTimeAsync(150)
      await startPromise

      const stopPromise = agent.stop()

      // Advance past the 5000ms timeout
      await vi.advanceTimersByTimeAsync(5000)

      expect(mockProcess.kill).toHaveBeenCalledWith('SIGTERM')
      expect(mockProcess.kill).toHaveBeenCalledWith('SIGKILL')

      // Simulate exit to resolve the promise
      mockProcess.emit('exit', 0, 'SIGKILL')
      await stopPromise

      vi.useRealTimers()
    })
  })

  describe('getStdinWeb / getStdoutWeb', () => {
    it('should throw if process not running', () => {
      const config = {
        id: 'test-agent',
        name: 'Test Agent',
        command: 'node',
        args: []
      }

      const agent = new AgentProcess(config)

      expect(() => agent.getStdinWeb()).toThrow('Agent process not running')
      expect(() => agent.getStdoutWeb()).toThrow('Agent process not running')
    })

    it('should return web streams when running', async () => {
      const config = {
        id: 'test-agent',
        name: 'Test Agent',
        command: 'node',
        args: []
      }

      const agent = new AgentProcess(config)

      // Start the process
      const startPromise = agent.start()
      await new Promise((resolve) => setTimeout(resolve, 150))
      await startPromise

      const stdin = agent.getStdinWeb()
      const stdout = agent.getStdoutWeb()

      expect(stdin).toBeInstanceOf(WritableStream)
      expect(stdout).toBeInstanceOf(ReadableStream)
    })
  })

  describe('onExit', () => {
    it('should register exit callbacks and call them on exit', async () => {
      const config = {
        id: 'test-agent',
        name: 'Test Agent',
        command: 'node',
        args: []
      }

      const agent = new AgentProcess(config)
      const exitCallback = vi.fn()
      agent.onExit(exitCallback)

      // Start the process
      const startPromise = agent.start()
      await new Promise((resolve) => setTimeout(resolve, 150))
      await startPromise

      // Simulate exit
      mockProcess.emit('exit', 0, null)

      expect(exitCallback).toHaveBeenCalledWith(0, null)
      expect(agent.isRunning()).toBe(false)
    })

    it('should call multiple exit callbacks', async () => {
      const config = {
        id: 'test-agent',
        name: 'Test Agent',
        command: 'node',
        args: []
      }

      const agent = new AgentProcess(config)
      const callback1 = vi.fn()
      const callback2 = vi.fn()
      agent.onExit(callback1)
      agent.onExit(callback2)

      // Start the process
      const startPromise = agent.start()
      await new Promise((resolve) => setTimeout(resolve, 150))
      await startPromise

      // Simulate exit with signal
      mockProcess.emit('exit', 1, 'SIGTERM')

      expect(callback1).toHaveBeenCalledWith(1, 'SIGTERM')
      expect(callback2).toHaveBeenCalledWith(1, 'SIGTERM')
    })
  })

  describe('getPid', () => {
    it('should return undefined when not running', () => {
      const config = {
        id: 'test-agent',
        name: 'Test Agent',
        command: 'node',
        args: []
      }

      const agent = new AgentProcess(config)
      expect(agent.getPid()).toBeUndefined()
    })

    it('should return pid when running', async () => {
      const config = {
        id: 'test-agent',
        name: 'Test Agent',
        command: 'node',
        args: []
      }

      const agent = new AgentProcess(config)

      // Start the process
      const startPromise = agent.start()
      await new Promise((resolve) => setTimeout(resolve, 150))
      await startPromise

      expect(agent.getPid()).toBe(12345)
    })
  })

  describe('isRunning', () => {
    it('should return false initially', () => {
      const config = {
        id: 'test-agent',
        name: 'Test Agent',
        command: 'node',
        args: []
      }

      const agent = new AgentProcess(config)
      expect(agent.isRunning()).toBe(false)
    })

    it('should return true after start', async () => {
      const config = {
        id: 'test-agent',
        name: 'Test Agent',
        command: 'node',
        args: []
      }

      const agent = new AgentProcess(config)

      const startPromise = agent.start()
      await new Promise((resolve) => setTimeout(resolve, 150))
      await startPromise

      expect(agent.isRunning()).toBe(true)
    })

    it('should return false after exit', async () => {
      const config = {
        id: 'test-agent',
        name: 'Test Agent',
        command: 'node',
        args: []
      }

      const agent = new AgentProcess(config)

      const startPromise = agent.start()
      await new Promise((resolve) => setTimeout(resolve, 150))
      await startPromise

      expect(agent.isRunning()).toBe(true)

      // Simulate exit
      mockProcess.emit('exit', 0, null)

      expect(agent.isRunning()).toBe(false)
    })
  })
})
