import { describe, it, expect, vi, beforeEach } from 'vitest'

// Mock child_process
vi.mock('child_process', () => ({
  execFile: vi.fn()
}))

// Mock fs
vi.mock('fs', () => ({
  existsSync: vi.fn(),
  statSync: vi.fn(),
  readFileSync: vi.fn()
}))

import { execFile } from 'child_process'
import { existsSync, readFileSync, statSync } from 'fs'
import { getGitBranch, getGitHeadPath } from '../../../../src/main/utils/git'

const mockExecFile = vi.mocked(execFile)
const mockExistsSync = vi.mocked(existsSync)
const mockStatSync = vi.mocked(statSync)
const mockReadFileSync = vi.mocked(readFileSync)

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

  describe('getGitHeadPath', () => {
    it('should return undefined when .git does not exist', () => {
      mockExistsSync.mockReturnValue(false)

      const result = getGitHeadPath('/repo')

      expect(result).toBeUndefined()
    })

    it('should return HEAD path for a standard repo', () => {
      mockExistsSync.mockImplementation(
        (input) => input === '/repo/.git' || input === '/repo/.git/HEAD'
      )
      mockStatSync.mockReturnValue({
        isDirectory: () => true,
        isFile: () => false
      })

      const result = getGitHeadPath('/repo')

      expect(result).toBe('/repo/.git/HEAD')
    })

    it('should return HEAD path for a worktree repo with absolute gitdir', () => {
      mockExistsSync.mockImplementation(
        (input) => input === '/repo/.git' || input === '/gitdir/worktrees/wt/HEAD'
      )
      mockStatSync.mockReturnValue({
        isDirectory: () => false,
        isFile: () => true
      })
      mockReadFileSync.mockReturnValue('gitdir: /gitdir/worktrees/wt\n')

      const result = getGitHeadPath('/repo')

      expect(result).toBe('/gitdir/worktrees/wt/HEAD')
    })

    it('should resolve relative gitdir paths', () => {
      mockExistsSync.mockImplementation(
        (input) => input === '/repo/.git' || input === '/repo/.git/worktrees/wt/HEAD'
      )
      mockStatSync.mockReturnValue({
        isDirectory: () => false,
        isFile: () => true
      })
      mockReadFileSync.mockReturnValue('gitdir: .git/worktrees/wt\n')

      const result = getGitHeadPath('/repo')

      expect(result).toBe('/repo/.git/worktrees/wt/HEAD')
    })

    it('should return undefined when gitdir file is malformed', () => {
      mockExistsSync.mockReturnValue(true)
      mockStatSync.mockReturnValue({
        isDirectory: () => false,
        isFile: () => true
      })
      mockReadFileSync.mockReturnValue('not-a-gitdir-file')

      const result = getGitHeadPath('/repo')

      expect(result).toBeUndefined()
    })

    it('should return undefined when HEAD path does not exist', () => {
      mockExistsSync.mockImplementation((input) => input === '/repo/.git')
      mockStatSync.mockReturnValue({
        isDirectory: () => true,
        isFile: () => false
      })

      const result = getGitHeadPath('/repo')

      expect(result).toBeUndefined()
    })
  })
})
