import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { spawn } from 'node:child_process'
import { EventEmitter } from 'node:events'
import { generateSessionTitle } from '../../../../src/main/conductor/TitleGenerator'

// Mock child_process spawn
vi.mock('node:child_process', () => ({
  spawn: vi.fn()
}))

// Mock path utility
vi.mock('../../../../src/main/utils/path', () => ({
  getEnhancedPath: vi.fn().mockReturnValue('/usr/local/bin:/usr/bin:/bin')
}))

describe('TitleGenerator', () => {
  const mockSpawn = spawn as unknown as ReturnType<typeof vi.fn>

  const createMockProcess = (options: {
    stdout?: string
    stderr?: string
    code?: number | null
    signal?: NodeJS.Signals | null
    error?: Error
    autoClose?: boolean
  }): { stdout?: EventEmitter; stderr?: EventEmitter; kill: ReturnType<typeof vi.fn> } => {
    const child = new EventEmitter() as unknown as {
      stdout?: EventEmitter
      stderr?: EventEmitter
      kill: ReturnType<typeof vi.fn>
    }
    child.stdout = new EventEmitter()
    child.stderr = new EventEmitter()
    child.kill = vi.fn()

    if (options.autoClose !== false) {
      process.nextTick(() => {
        if (options.error) {
          child.emit('error', options.error)
          return
        }
        if (options.stdout) {
          child.stdout?.emit('data', Buffer.from(options.stdout))
        }
        if (options.stderr) {
          child.stderr?.emit('data', Buffer.from(options.stderr))
        }
        child.emit('close', options.code ?? 0, options.signal ?? null)
      })
    }

    return child
  }

  beforeEach(() => {
    vi.clearAllMocks()
  })

  afterEach(() => {
    vi.resetAllMocks()
  })

  describe('generateSessionTitle', () => {
    describe('Claude Code agent', () => {
      it('should generate title using claude CLI', async () => {
        mockSpawn.mockReturnValue(createMockProcess({ stdout: '实现用户登录功能\n', stderr: '' }))

        const title = await generateSessionTitle('claude-code', '帮我实现一个用户登录功能')

        expect(title).toBe('实现用户登录功能')
        expect(mockSpawn).toHaveBeenCalledWith(
          'claude',
          expect.any(Array),
          expect.objectContaining({ stdio: ['ignore', 'pipe', 'pipe'] })
        )
      })

      it('should use correct CLI arguments for Claude', async () => {
        mockSpawn.mockReturnValue(createMockProcess({ stdout: 'Test Title', stderr: '' }))

        await generateSessionTitle('claude-code', 'Test message')

        const [command, args] = mockSpawn.mock.calls[0]
        expect(command).toBe('claude')
        expect(args).toContain('-p')
        expect(args).toContain('--output-format')
        expect(args).toContain('text')
        expect(args).toContain('--tools')
        expect(args).toContain('--permission-mode')
        expect(args).toContain('dontAsk')
        expect(args).toContain('--no-session-persistence')
      })
    })

    describe('OpenCode agent', () => {
      it('should generate title using opencode CLI', async () => {
        const jsonOutput = [
          '{"type":"step_start","timestamp":123}',
          '{"type":"text","part":{"text":"实现用户登录功能"}}',
          '{"type":"step_finish","timestamp":456}'
        ].join('\n')

        mockSpawn.mockReturnValue(createMockProcess({ stdout: jsonOutput, stderr: '' }))

        const title = await generateSessionTitle('opencode', '帮我实现一个用户登录功能')

        expect(title).toBe('实现用户登录功能')
      })

      it('should use opencode/gpt-5-nano model and title agent', async () => {
        mockSpawn.mockReturnValue(
          createMockProcess({ stdout: '{"type":"text","part":{"text":"Title"}}', stderr: '' })
        )

        await generateSessionTitle('opencode', 'Test message')

        const [command, args] = mockSpawn.mock.calls[0]
        expect(command).toBe('opencode')
        expect(args).toContain('run')
        expect(args).toContain('-m')
        expect(args).toContain('opencode/gpt-5-nano')
        expect(args).toContain('--agent')
        expect(args).toContain('title')
        expect(args).toContain('--format')
        expect(args).toContain('json')
      })
    })

    describe('Codex agent', () => {
      it('should generate title using codex CLI', async () => {
        const jsonOutput = [
          '{"type":"thread.started","thread_id":"123"}',
          '{"type":"turn.started"}',
          '{"type":"item.completed","item":{"type":"reasoning","text":"Thinking..."}}',
          '{"type":"item.completed","item":{"type":"agent_message","text":"实现用户登录功能"}}',
          '{"type":"turn.completed"}'
        ].join('\n')

        mockSpawn.mockReturnValue(createMockProcess({ stdout: jsonOutput, stderr: '' }))

        const title = await generateSessionTitle('codex', '帮我实现一个用户登录功能')

        expect(title).toBe('实现用户登录功能')
      })

      it('should use correct CLI arguments for Codex with read-only sandbox', async () => {
        mockSpawn.mockReturnValue(
          createMockProcess({
            stdout: '{"type":"item.completed","item":{"type":"agent_message","text":"Title"}}',
            stderr: ''
          })
        )

        await generateSessionTitle('codex', 'Test message')

        const [command, args] = mockSpawn.mock.calls[0]
        expect(command).toBe('codex')
        expect(args).toContain('exec')
        expect(args).toContain('--sandbox')
        expect(args).toContain('read-only')
        expect(args).toContain('--json')
      })
    })

    describe('failure behavior (returns null)', () => {
      it('should return null for unknown agent', async () => {
        const title = await generateSessionTitle('unknown-agent', '帮我实现一个用户登录功能')

        expect(title).toBeNull()
        expect(mockSpawn).not.toHaveBeenCalled()
      })

      it('should return null on CLI error', async () => {
        mockSpawn.mockReturnValue(
          createMockProcess({ stderr: 'Error', error: new Error('Command failed') })
        )

        const title = await generateSessionTitle('claude-code', '帮我实现一个用户登录功能')

        expect(title).toBeNull()
      })

      it('should return null on CLI timeout', async () => {
        vi.useFakeTimers()
        mockSpawn.mockReturnValue(createMockProcess({ autoClose: false }))
        const titlePromise = generateSessionTitle('claude-code', '帮我实现一个用户登录功能')
        await vi.advanceTimersByTimeAsync(60000)
        const title = await titlePromise

        expect(title).toBeNull()
        vi.useRealTimers()
      })

      it('should return null on empty output', async () => {
        mockSpawn.mockReturnValue(createMockProcess({ stdout: '', stderr: '' }))

        const title = await generateSessionTitle('claude-code', '帮我实现一个用户登录功能')

        expect(title).toBeNull()
      })
    })

    describe('prompt building', () => {
      it('should include user message in prompt', async () => {
        mockSpawn.mockReturnValue(createMockProcess({ stdout: 'Title', stderr: '' }))

        await generateSessionTitle('claude-code', 'My specific message')

        const [, args] = mockSpawn.mock.calls[0]
        const promptIndex = args.indexOf('-p') + 1
        expect(args[promptIndex]).toContain('My specific message')
      })

      it('should escape single quotes in user message', async () => {
        mockSpawn.mockReturnValue(createMockProcess({ stdout: 'Title', stderr: '' }))

        await generateSessionTitle('claude-code', "User's message with 'quotes'")

        const [, args] = mockSpawn.mock.calls[0]
        const promptIndex = args.indexOf('-p') + 1
        expect(args[promptIndex]).toContain("User's message with 'quotes'")
      })
    })

    describe('JSON parsing', () => {
      it('should handle malformed JSON in OpenCode output', async () => {
        const badJsonOutput = [
          '{"type":"text","part":{"text":"Good Title"}}',
          'not valid json',
          '{"incomplete'
        ].join('\n')

        mockSpawn.mockReturnValue(createMockProcess({ stdout: badJsonOutput, stderr: '' }))

        const title = await generateSessionTitle('opencode', 'Test')

        expect(title).toBe('Good Title')
      })

      it('should handle malformed JSON in Codex output', async () => {
        const badJsonOutput = [
          '{"type":"item.completed","item":{"type":"agent_message","text":"Good Title"}}',
          'garbage',
          '{'
        ].join('\n')

        mockSpawn.mockReturnValue(createMockProcess({ stdout: badJsonOutput, stderr: '' }))

        const title = await generateSessionTitle('codex', 'Test')

        expect(title).toBe('Good Title')
      })

      it('should return null when no valid JSON events found', async () => {
        mockSpawn.mockReturnValue(createMockProcess({ stdout: 'not json at all', stderr: '' }))

        const title = await generateSessionTitle('opencode', 'Test message')

        expect(title).toBeNull()
      })
    })
  })
})
