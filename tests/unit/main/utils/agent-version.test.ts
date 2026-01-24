import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'

vi.mock('node:child_process', () => ({
  exec: vi.fn()
}))

vi.mock('../../../../src/main/utils/path', () => ({
  getEnhancedPath: vi.fn().mockReturnValue('/usr/local/bin:/usr/bin:/bin')
}))

import { exec } from 'node:child_process'
import { checkAgentVersions, isNewerVersion } from '../../../../src/main/utils/agent-version'

const mockExec = vi.mocked(exec)
const originalFetch = globalThis.fetch

function mockFetchResponse(data: unknown): { ok: boolean; json: () => Promise<unknown> } {
  return {
    ok: true,
    json: async () => data
  }
}

describe('agent-version', () => {
  beforeEach(() => {
    vi.clearAllMocks()
  })

  afterEach(() => {
    globalThis.fetch = originalFetch
  })

  describe('isNewerVersion', () => {
    it('should detect newer versions', () => {
      expect(isNewerVersion('1.2.3', '1.2.4')).toBe(true)
      expect(isNewerVersion('1.2.3', '2.0.0')).toBe(true)
      expect(isNewerVersion('1.2.3', '1.2.3')).toBe(false)
      expect(isNewerVersion('1.2.3', '1.2.2')).toBe(false)
    })
  })

  describe('checkAgentVersions', () => {
    it('should return installed and latest versions with update flag', async () => {
      mockExec.mockImplementation((cmd, _opts, callback) => {
        if (typeof _opts === 'function') {
          callback = _opts
        }
        const cmdString = String(cmd)
        if (cmdString.startsWith('codex --version')) {
          callback?.(null, { stdout: 'codex-cli 0.1.0\n', stderr: '' } as never)
        } else if (cmdString.includes('npm list -g @zed-industries/codex-acp')) {
          callback?.(null, { stdout: '@zed-industries/codex-acp@0.1.0\n', stderr: '' } as never)
        } else {
          callback?.(new Error('not found'), { stdout: '', stderr: '' } as never)
        }
        return {} as ReturnType<typeof exec>
      })

      const fetchMock = vi.fn(async (url: string) => {
        if (url === 'https://registry.npmjs.org/@openai/codex') {
          return mockFetchResponse({ 'dist-tags': { latest: '0.2.0' } })
        }
        if (url === 'https://registry.npmjs.org/@zed-industries/codex-acp') {
          return mockFetchResponse({ 'dist-tags': { latest: '0.1.0' } })
        }
        if (url === 'https://registry.npmjs.org/@anthropic-ai/claude-code') {
          return mockFetchResponse({ 'dist-tags': { latest: '2.0.0' } })
        }
        if (url === 'https://registry.npmjs.org/@zed-industries/claude-code-acp') {
          return mockFetchResponse({ 'dist-tags': { latest: '0.9.9' } })
        }
        if (url === 'https://api.github.com/repos/anomalyco/opencode/releases/latest') {
          return mockFetchResponse({ tag_name: 'v1.1.0' })
        }
        return mockFetchResponse({})
      })

      globalThis.fetch = fetchMock as unknown as typeof fetch

      const results = await checkAgentVersions('codex', ['codex', 'codex-acp'])

      expect(results).toEqual([
        {
          command: 'codex',
          installedVersion: '0.1.0',
          latestVersion: '0.2.0',
          hasUpdate: true
        },
        {
          command: 'codex-acp',
          installedVersion: '0.1.0',
          latestVersion: '0.1.0',
          hasUpdate: false
        }
      ])
    })
  })
})
