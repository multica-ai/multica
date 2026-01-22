import { describe, it, expect, vi, beforeEach } from 'vitest'

// Mock child_process with exec
vi.mock('node:child_process', () => ({
  exec: vi.fn()
}))

// Mock os module
vi.mock('node:os', () => ({
  platform: vi.fn().mockReturnValue('darwin')
}))

// Mock path module
vi.mock('../../../../src/main/utils/path', () => ({
  getEnhancedPath: vi.fn().mockReturnValue('/usr/local/bin:/usr/bin:/bin')
}))

// Mock version utilities
vi.mock('../../../../src/main/utils/agent-version', () => ({
  getInstalledVersion: vi.fn().mockResolvedValue(undefined),
  checkAgentVersions: vi.fn()
}))

import { exec } from 'node:child_process'
import { platform } from 'node:os'
import { commandExists, checkAgents } from '../../../../src/main/utils/agent-check'
import { getInstalledVersion } from '../../../../src/main/utils/agent-version'

const mockExec = vi.mocked(exec)
const mockPlatform = vi.mocked(platform)
const mockGetInstalledVersion = vi.mocked(getInstalledVersion)

// Helper to create a mock exec implementation
function mockExecSuccess(stdout: string): void {
  mockExec.mockImplementation((_cmd, _opts, callback) => {
    if (typeof _opts === 'function') {
      callback = _opts
    }
    if (callback) {
      callback(null, { stdout, stderr: '' } as never)
    }
    return {} as ReturnType<typeof exec>
  })
}

function mockExecFailure(error: Error): void {
  mockExec.mockImplementation((_cmd, _opts, callback) => {
    if (typeof _opts === 'function') {
      callback = _opts
    }
    if (callback) {
      callback(error, { stdout: '', stderr: '' } as never)
    }
    return {} as ReturnType<typeof exec>
  })
}

describe('agent-check', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    mockPlatform.mockReturnValue('darwin')
    mockGetInstalledVersion.mockResolvedValue(undefined)
  })

  describe('commandExists', () => {
    it('should return exists: true when command is found', async () => {
      mockExecSuccess('/usr/local/bin/node\n')

      const result = await commandExists('node')

      expect(result).toEqual({
        exists: true,
        path: '/usr/local/bin/node'
      })
    })

    it('should return exists: false when command is not found', async () => {
      mockExecFailure(new Error('Command not found'))

      const result = await commandExists('nonexistent')

      expect(result).toEqual({ exists: false })
    })

    it('should use "where" command on Windows', async () => {
      mockPlatform.mockReturnValue('win32')
      mockExecSuccess('C:\\Program Files\\node\\node.exe\n')

      await commandExists('node')

      expect(mockExec).toHaveBeenCalledWith('where node', expect.any(Object), expect.any(Function))
    })

    it('should use "which" command on Unix-like systems', async () => {
      mockPlatform.mockReturnValue('darwin')
      mockExecSuccess('/usr/local/bin/node\n')

      await commandExists('node')

      expect(mockExec).toHaveBeenCalledWith('which node', expect.any(Object), expect.any(Function))
    })

    it('should handle multiline which output', async () => {
      mockExecSuccess('/usr/local/bin/node\n/usr/bin/node\n')

      const result = await commandExists('node')

      // Should return first path
      expect(result.path).toBe('/usr/local/bin/node')
    })
  })

  describe('checkAgents', () => {
    it('should check all default agents', async () => {
      // Mock all commands as not found for simplicity
      mockExecFailure(new Error('Command not found'))

      const results = await checkAgents()

      // Should have entries for each configured agent
      expect(results.length).toBeGreaterThan(0)
      expect(results.every((r) => r.id && r.name && r.command)).toBe(true)
    })

    it('should mark installed agents correctly', async () => {
      // Mock 'opencode' command as found, rest as not found
      mockExec.mockImplementation((cmd, _opts, callback) => {
        if (typeof _opts === 'function') {
          callback = _opts
        }
        // Check if this is looking for 'opencode'
        const cmdStr = String(cmd)
        if (cmdStr.includes('opencode')) {
          if (callback) {
            callback(null, { stdout: '/usr/local/bin/opencode\n', stderr: '' } as never)
          }
        } else {
          if (callback) {
            callback(new Error('not found'), { stdout: '', stderr: '' } as never)
          }
        }
        return {} as ReturnType<typeof exec>
      })
      mockGetInstalledVersion.mockImplementation((command) =>
        Promise.resolve(command === 'opencode' ? '1.2.3' : undefined)
      )

      const results = await checkAgents()

      // The opencode agent should be marked as installed
      const opencodeAgent = results.find((r) => r.id === 'opencode')
      expect(opencodeAgent?.installed).toBe(true)
      expect(opencodeAgent?.commands?.[0]?.version).toBe('1.2.3')
    })

    it('should include install hints', async () => {
      mockExecFailure(new Error('Command not found'))

      const results = await checkAgents()

      // Check that at least some have install hints
      const withHints = results.filter((r) => r.installHint)
      expect(withHints.length).toBeGreaterThan(0)
    })
  })
})
