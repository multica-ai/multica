import { describe, expect, it } from 'vitest'
import { isValidPath } from '../../../../src/main/utils/pathValidation'

describe('isValidPath', () => {
  it('accepts absolute POSIX paths', () => {
    expect(isValidPath('/tmp/project')).toBe(true)
  })

  it('accepts absolute POSIX paths with trailing slash', () => {
    expect(isValidPath('/tmp/project/')).toBe(true)
  })

  it('rejects relative paths', () => {
    expect(isValidPath('tmp/project')).toBe(false)
  })

  it('rejects POSIX traversal segments', () => {
    expect(isValidPath('/tmp/../secret')).toBe(false)
  })

  it('accepts absolute Windows paths', () => {
    expect(isValidPath('C:\\\\Users\\\\me\\\\project')).toBe(true)
  })

  it('rejects Windows traversal segments', () => {
    expect(isValidPath('C:\\\\Users\\\\..\\\\project')).toBe(false)
  })
})
