import { describe, expect, it } from 'vitest'
import { getBaseName } from '../../../../src/renderer/src/utils/path'

describe('getBaseName', () => {
  it('returns the last segment for POSIX paths', () => {
    expect(getBaseName('/Users/me/project')).toBe('project')
  })

  it('handles POSIX paths with trailing slashes', () => {
    expect(getBaseName('/Users/me/project/')).toBe('project')
  })

  it('returns the last segment for Windows paths', () => {
    expect(getBaseName('C:\\\\Users\\\\me\\\\project')).toBe('project')
  })

  it('handles Windows paths with trailing slashes', () => {
    expect(getBaseName('C:\\\\Users\\\\me\\\\project\\\\')).toBe('project')
  })

  it('returns a fallback for root paths', () => {
    expect(getBaseName('/')).toBe('Root')
  })
})
