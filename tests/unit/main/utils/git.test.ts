import { describe, it, expect, vi, beforeEach } from 'vitest'

// Mock child_process
vi.mock('child_process', () => ({
  execFile: vi.fn()
}))

// Mock fs
vi.mock('fs', () => ({
  existsSync: vi.fn()
}))

import { execFile } from 'child_process'
import { existsSync } from 'fs'
import { getGitBranch } from '../../../../src/main/utils/git'

const mockExecFile = vi.mocked(execFile)
const mockExistsSync = vi.mocked(existsSync)

describe('git utils', () => {
  beforeEach(() => {
    vi.clearAllMocks()
  })

  describe('getGitBranch', () => {
    it('should return undefined when .git directory does not exist', async () => {
      mockExistsSync.mockReturnValue(false)

      const result = await getGitBranch('/some/directory')

      expect(result).toBeUndefined()
      expect(mockExecFile).not.toHaveBeenCalled()
    })

    it('should return branch name when git repo exists', async () => {
      mockExistsSync.mockReturnValue(true)
      mockExecFile.mockImplementation((_cmd, _args, _opts, callback) => {
        if (typeof callback === 'function') {
          callback(null, 'main\n', '')
        }
        return {} as ReturnType<typeof execFile>
      })

      const result = await getGitBranch('/some/git-repo')

      expect(result).toBe('main')
      expect(mockExecFile).toHaveBeenCalledWith(
        'git',
        ['rev-parse', '--abbrev-ref', 'HEAD'],
        { cwd: '/some/git-repo', timeout: 3000 },
        expect.any(Function)
      )
    })

    it('should return undefined when git command fails', async () => {
      mockExistsSync.mockReturnValue(true)
      mockExecFile.mockImplementation((_cmd, _args, _opts, callback) => {
        if (typeof callback === 'function') {
          callback(new Error('git error'), '', '')
        }
        return {} as ReturnType<typeof execFile>
      })

      const result = await getGitBranch('/some/directory')

      expect(result).toBeUndefined()
    })

    it('should return undefined when git returns empty string', async () => {
      mockExistsSync.mockReturnValue(true)
      mockExecFile.mockImplementation((_cmd, _args, _opts, callback) => {
        if (typeof callback === 'function') {
          callback(null, '', '')
        }
        return {} as ReturnType<typeof execFile>
      })

      const result = await getGitBranch('/some/directory')

      expect(result).toBeUndefined()
    })

    it('should trim whitespace from branch name', async () => {
      mockExistsSync.mockReturnValue(true)
      mockExecFile.mockImplementation((_cmd, _args, _opts, callback) => {
        if (typeof callback === 'function') {
          callback(null, '  feature/my-branch  \n', '')
        }
        return {} as ReturnType<typeof execFile>
      })

      const result = await getGitBranch('/some/git-repo')

      expect(result).toBe('feature/my-branch')
    })

    it('should handle detached HEAD state', async () => {
      mockExistsSync.mockReturnValue(true)
      mockExecFile.mockImplementation((_cmd, _args, _opts, callback) => {
        if (typeof callback === 'function') {
          callback(null, 'HEAD\n', '')
        }
        return {} as ReturnType<typeof execFile>
      })

      const result = await getGitBranch('/some/git-repo')

      expect(result).toBe('HEAD')
    })
  })
})
