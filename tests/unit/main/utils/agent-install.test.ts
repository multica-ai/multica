import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import type { ChildProcess } from 'child_process'
import type { EventEmitter } from 'events'

// Mock child_process
vi.mock('child_process', () => ({
  spawn: vi.fn()
}))

// Mock os module
vi.mock('os', () => ({
  platform: vi.fn().mockReturnValue('darwin')
}))

import { spawn } from 'child_process'
import { platform } from 'os'
import {
  installAgent,
  openTerminalWithCommand,
  INSTALL_COMMANDS
} from '../../../../src/main/utils/agent-install'

const mockSpawn = vi.mocked(spawn)
const mockPlatform = vi.mocked(platform)

// Helper to create a mock child process
function createMockProcess(): {
  proc: Partial<ChildProcess>
  emit: (event: string, ...args: unknown[]) => void
} {
  const listeners: Record<string, ((...args: unknown[]) => void)[]> = {}

  const proc: Partial<ChildProcess> = {
    on: vi.fn((event: string, callback: (...args: unknown[]) => void) => {
      if (!listeners[event]) {
        listeners[event] = []
      }
      listeners[event].push(callback)
      return proc as ChildProcess
    }),
    unref: vi.fn()
  }

  const emit = (event: string, ...args: unknown[]): void => {
    const callbacks = listeners[event] || []
    callbacks.forEach((cb) => cb(...args))
  }

  return { proc, emit }
}

describe('agent-install', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    mockPlatform.mockReturnValue('darwin')
  })

  afterEach(() => {
    vi.useRealTimers()
  })

  describe('INSTALL_COMMANDS', () => {
    it('should have install commands for supported agents', () => {
      // Claude Code: CLI + ACP
      expect(INSTALL_COMMANDS['claude-code']).toBe(
        'curl -fsSL https://claude.ai/install.sh | bash && npm install -g @zed-industries/claude-code-acp'
      )
      // OpenCode: official install script
      expect(INSTALL_COMMANDS['opencode']).toBe('curl -fsSL https://opencode.ai/install | bash')
      // Codex: CLI + ACP in one command
      expect(INSTALL_COMMANDS['codex']).toBe(
        'npm install -g @openai/codex @zed-industries/codex-acp'
      )
      // Gemini: single package
      expect(INSTALL_COMMANDS['gemini']).toBe('npm install -g @google/gemini-cli')
    })
  })

  describe('openTerminalWithCommand', () => {
    describe('macOS', () => {
      beforeEach(() => {
        mockPlatform.mockReturnValue('darwin')
      })

      it('should use osascript to open Terminal.app', async () => {
        const { proc, emit } = createMockProcess()
        mockSpawn.mockReturnValue(proc as ChildProcess)

        const promise = openTerminalWithCommand('echo hello')

        // Simulate successful spawn
        emit('close', 0)

        const result = await promise

        expect(mockSpawn).toHaveBeenCalledWith('osascript', ['-e', expect.any(String)])
        expect(result.success).toBe(true)
      })

      it('should escape double quotes in command', async () => {
        const { proc, emit } = createMockProcess()
        mockSpawn.mockReturnValue(proc as ChildProcess)

        const promise = openTerminalWithCommand('echo "hello world"')
        emit('close', 0)
        await promise

        const osascriptArg = mockSpawn.mock.calls[0][1][1]
        expect(osascriptArg).toContain('echo \\"hello world\\"')
      })

      it('should return error when osascript fails', async () => {
        const { proc, emit } = createMockProcess()
        mockSpawn.mockReturnValue(proc as ChildProcess)

        const promise = openTerminalWithCommand('echo hello')
        emit('close', 1)

        const result = await promise

        expect(result.success).toBe(false)
        expect(result.error).toBe('osascript exited with code 1')
      })

      it('should handle spawn error', async () => {
        const { proc, emit } = createMockProcess()
        mockSpawn.mockReturnValue(proc as ChildProcess)

        const promise = openTerminalWithCommand('echo hello')
        emit('error', new Error('spawn ENOENT'))

        const result = await promise

        expect(result.success).toBe(false)
        expect(result.error).toBe('spawn ENOENT')
      })
    })

    describe('Windows', () => {
      beforeEach(() => {
        mockPlatform.mockReturnValue('win32')
      })

      it('should use cmd.exe to open terminal', async () => {
        const { proc, emit } = createMockProcess()
        mockSpawn.mockReturnValue(proc as ChildProcess)

        const promise = openTerminalWithCommand('npm install')
        emit('close', 0)

        const result = await promise

        expect(mockSpawn).toHaveBeenCalledWith(
          'cmd.exe',
          ['/c', 'start', 'cmd.exe', '/K', 'npm install'],
          { shell: true }
        )
        expect(result.success).toBe(true)
      })

      it('should return error when cmd fails', async () => {
        const { proc, emit } = createMockProcess()
        mockSpawn.mockReturnValue(proc as ChildProcess)

        const promise = openTerminalWithCommand('npm install')
        emit('close', 1)

        const result = await promise

        expect(result.success).toBe(false)
        expect(result.error).toBe('cmd exited with code 1')
      })
    })

    describe('Linux', () => {
      beforeEach(() => {
        mockPlatform.mockReturnValue('linux')
        vi.useFakeTimers()
      })

      it('should try gnome-terminal first', async () => {
        const { proc } = createMockProcess()
        mockSpawn.mockReturnValue(proc as ChildProcess)

        const promise = openTerminalWithCommand('npm install')

        // Fast-forward timers to trigger the success timeout
        vi.advanceTimersByTime(100)

        const result = await promise

        expect(mockSpawn).toHaveBeenCalledWith(
          'gnome-terminal',
          ['--', 'bash', '-c', 'npm install; exec bash'],
          { detached: true, stdio: 'ignore' }
        )
        expect(result.success).toBe(true)
      })

      it('should try next terminal if gnome-terminal fails', async () => {
        let callCount = 0
        mockSpawn.mockImplementation((cmd) => {
          callCount++
          const { proc, emit } = createMockProcess()

          // First call (gnome-terminal) fails, second (konsole) succeeds
          if (cmd === 'gnome-terminal') {
            setTimeout(() => emit('error', new Error('ENOENT')), 0)
          }

          return proc as ChildProcess
        })

        const promise = openTerminalWithCommand('npm install')

        // Run pending timers to trigger error and retry
        await vi.advanceTimersByTimeAsync(0)
        // Advance to trigger success timeout
        vi.advanceTimersByTime(100)

        const result = await promise

        // Should have tried gnome-terminal first, then konsole
        expect(callCount).toBeGreaterThanOrEqual(2)
        expect(result.success).toBe(true)
      })

      it('should return error when no terminal is found', async () => {
        mockSpawn.mockImplementation(() => {
          const { proc, emit } = createMockProcess()
          setTimeout(() => emit('error', new Error('ENOENT')), 0)
          return proc as ChildProcess
        })

        const promise = openTerminalWithCommand('npm install')

        // Run all timers to exhaust terminal options
        await vi.runAllTimersAsync()

        const result = await promise

        expect(result.success).toBe(false)
        expect(result.error).toBe('No supported terminal emulator found')
      })
    })
  })

  describe('installAgent', () => {
    beforeEach(() => {
      mockPlatform.mockReturnValue('darwin')
    })

    it('should open terminal with correct command for claude-code', async () => {
      const { proc, emit } = createMockProcess()
      mockSpawn.mockReturnValue(proc as ChildProcess)

      const promise = installAgent('claude-code')
      emit('close', 0)

      const result = await promise

      expect(result.success).toBe(true)
      const osascriptArg = mockSpawn.mock.calls[0][1][1]
      // Should install both CLI and ACP
      expect(osascriptArg).toContain('curl -fsSL https://claude.ai/install.sh | bash')
      expect(osascriptArg).toContain('npm install -g @zed-industries/claude-code-acp')
    })

    it('should open terminal with correct command for opencode', async () => {
      const { proc, emit } = createMockProcess()
      mockSpawn.mockReturnValue(proc as ChildProcess)

      const promise = installAgent('opencode')
      emit('close', 0)

      const result = await promise

      expect(result.success).toBe(true)
      const osascriptArg = mockSpawn.mock.calls[0][1][1]
      expect(osascriptArg).toContain('curl -fsSL https://opencode.ai/install | bash')
    })

    it('should open terminal with correct command for codex', async () => {
      const { proc, emit } = createMockProcess()
      mockSpawn.mockReturnValue(proc as ChildProcess)

      const promise = installAgent('codex')
      emit('close', 0)

      const result = await promise

      expect(result.success).toBe(true)
      const osascriptArg = mockSpawn.mock.calls[0][1][1]
      // Should install both CLI and ACP in one command
      expect(osascriptArg).toContain('npm install -g @openai/codex @zed-industries/codex-acp')
    })

    it('should return error for unsupported agent', async () => {
      const result = await installAgent('unsupported-agent')

      expect(result.success).toBe(false)
      expect(result.error).toBe('Installation not supported for: unsupported-agent')
      expect(mockSpawn).not.toHaveBeenCalled()
    })

    it('should return error when terminal fails to open', async () => {
      const { proc, emit } = createMockProcess()
      mockSpawn.mockReturnValue(proc as ChildProcess)

      const promise = installAgent('claude-code')
      emit('error', new Error('spawn failed'))

      const result = await promise

      expect(result.success).toBe(false)
      expect(result.error).toBe('spawn failed')
    })
  })
})
