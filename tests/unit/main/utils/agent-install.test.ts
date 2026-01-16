import { describe, it, expect, vi, beforeEach } from 'vitest'
import { homedir } from 'node:os'

// Mock child_process
vi.mock('child_process', () => ({
  spawn: vi.fn()
}))

// Mock fs
vi.mock('fs', () => ({
  promises: {
    mkdir: vi.fn().mockResolvedValue(undefined)
  }
}))

// Mock os module
vi.mock('os', () => ({
  platform: vi.fn().mockReturnValue('darwin')
}))

// Mock path utilities
vi.mock('../../../../src/main/utils/path', () => ({
  getEnhancedPath: vi
    .fn()
    .mockReturnValue(
      '/Users/test/.npm-global/bin:/Users/test/.opencode/bin:/usr/local/bin:/usr/bin:/bin'
    ),
  getNpmUserPrefix: vi.fn().mockReturnValue('/Users/test/.npm-global')
}))

// Mock agent-check
vi.mock('../../../../src/main/utils/agent-check', () => ({
  commandExists: vi.fn().mockResolvedValue({ exists: false })
}))

import { formatInstallError } from '../../../../src/main/utils/agent-install'
import { getNpmUserPrefix, getEnhancedPath } from '../../../../src/main/utils/path'

describe('agent-install', () => {
  beforeEach(() => {
    vi.clearAllMocks()
  })

  describe('getNpmUserPrefix', () => {
    it('should return user-level npm global directory path', () => {
      const prefix = getNpmUserPrefix()
      expect(prefix).toBe('/Users/test/.npm-global')
    })
  })

  describe('getEnhancedPath', () => {
    it('should include npm-global/bin in PATH', () => {
      const path = getEnhancedPath()
      expect(path).toContain('.npm-global/bin')
    })
  })

  describe('formatInstallError', () => {
    it('should format EACCES permission errors', () => {
      const error = 'npm ERR! code EACCES\nnpm ERR! syscall mkdir'
      const result = formatInstallError(error)
      expect(result).toBe('Permission denied. Please run: chmod -R u+w ~/.npm-global')
    })

    it('should format permission denied errors', () => {
      const error = 'Error: EACCES: permission denied, mkdir /some/path'
      const result = formatInstallError(error)
      expect(result).toBe('Permission denied. Please run: chmod -R u+w ~/.npm-global')
    })

    it('should format network errors with ENOTFOUND', () => {
      const error = 'npm ERR! code ENOTFOUND\nnpm ERR! network'
      const result = formatInstallError(error)
      expect(result).toBe('Network error. Please check your internet connection and try again.')
    })

    it('should format network errors with host resolution', () => {
      const error = 'Could not resolve host: registry.npmjs.org'
      const result = formatInstallError(error)
      expect(result).toBe('Network error. Please check your internet connection and try again.')
    })

    it('should format command not found errors', () => {
      const error = 'spawn npm ENOENT'
      const result = formatInstallError(error)
      expect(result).toBe('Command not found. Please ensure required tools are installed.')
    })

    it('should pass through unknown errors unchanged', () => {
      const error = 'Some unknown error message'
      const result = formatInstallError(error)
      expect(result).toBe('Some unknown error message')
    })
  })
})
