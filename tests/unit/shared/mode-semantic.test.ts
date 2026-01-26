/**
 * Tests for mode-semantic module
 */
import { describe, expect, it } from 'vitest'
import {
  filterVisibleModes,
  getSemanticType,
  isModeVisible,
  getModeConfig,
  getNextModeId,
  type SemanticType
} from '../../../src/shared/mode-semantic'
import type { SessionMode } from '@agentclientprotocol/sdk'

// Helper to create mock SessionMode
function createMode(id: string, name?: string): SessionMode {
  return { id, name: name ?? id }
}

describe('filterVisibleModes', () => {
  it('shows configured visible modes', () => {
    const modes: SessionMode[] = [
      createMode('default'),
      createMode('acceptEdits'),
      createMode('plan')
    ]
    const result = filterVisibleModes(modes)
    expect(result).toHaveLength(3)
    expect(result.map((m) => m.id)).toEqual(['default', 'acceptEdits', 'plan'])
  })

  it('hides configured hidden modes', () => {
    const modes: SessionMode[] = [
      createMode('default'),
      createMode('dontAsk'),
      createMode('bypassPermissions')
    ]
    const result = filterVisibleModes(modes)
    expect(result).toHaveLength(1)
    expect(result[0].id).toBe('default')
  })

  it('hides unknown modes by default', () => {
    const modes: SessionMode[] = [
      createMode('default'),
      createMode('unknownMode'),
      createMode('anotherUnknown')
    ]
    const result = filterVisibleModes(modes)
    expect(result).toHaveLength(1)
    expect(result[0].id).toBe('default')
  })

  it('handles empty array', () => {
    const result = filterVisibleModes([])
    expect(result).toHaveLength(0)
  })

  it('filters Codex modes correctly', () => {
    const modes: SessionMode[] = [
      createMode('read-only'),
      createMode('auto'),
      createMode('full-access')
    ]
    const result = filterVisibleModes(modes)
    expect(result).toHaveLength(2)
    expect(result.map((m) => m.id)).toEqual(['read-only', 'auto'])
  })
})

describe('getSemanticType', () => {
  it('returns correct semantic for Claude Code modes', () => {
    expect(getSemanticType('default')).toBe('default')
    expect(getSemanticType('acceptEdits')).toBe('auto')
    expect(getSemanticType('plan')).toBe('plan')
  })

  it('returns correct semantic for Codex modes', () => {
    expect(getSemanticType('read-only')).toBe('readonly')
    expect(getSemanticType('auto')).toBe('auto')
  })

  it('returns correct semantic for OpenCode modes', () => {
    expect(getSemanticType('build')).toBe('default')
    expect(getSemanticType('plan')).toBe('plan')
  })

  it('returns default for unknown modes', () => {
    expect(getSemanticType('unknownMode')).toBe('default')
    expect(getSemanticType('')).toBe('default')
  })
})

describe('isModeVisible', () => {
  it('returns true for visible modes', () => {
    expect(isModeVisible('default')).toBe(true)
    expect(isModeVisible('acceptEdits')).toBe(true)
    expect(isModeVisible('plan')).toBe(true)
    expect(isModeVisible('read-only')).toBe(true)
  })

  it('returns false for hidden modes', () => {
    expect(isModeVisible('dontAsk')).toBe(false)
    expect(isModeVisible('bypassPermissions')).toBe(false)
    expect(isModeVisible('full-access')).toBe(false)
  })

  it('returns false for unknown modes', () => {
    expect(isModeVisible('unknownMode')).toBe(false)
    expect(isModeVisible('')).toBe(false)
  })
})

describe('getModeConfig', () => {
  it('returns config for known modes', () => {
    const config = getModeConfig('default')
    expect(config).toBeDefined()
    expect(config?.visible).toBe(true)
    expect(config?.semantic).toBe('default')
  })

  it('returns undefined for unknown modes', () => {
    expect(getModeConfig('unknownMode')).toBeUndefined()
  })
})

describe('getNextModeId', () => {
  it('cycles through visible modes', () => {
    const modes: SessionMode[] = [
      createMode('default'),
      createMode('acceptEdits'),
      createMode('plan')
    ]
    expect(getNextModeId(modes, 'default')).toBe('acceptEdits')
    expect(getNextModeId(modes, 'acceptEdits')).toBe('plan')
    expect(getNextModeId(modes, 'plan')).toBe('default') // wraps around
  })

  it('skips hidden modes in cycle', () => {
    const modes: SessionMode[] = [createMode('default'), createMode('dontAsk'), createMode('plan')]
    expect(getNextModeId(modes, 'default')).toBe('plan')
    expect(getNextModeId(modes, 'plan')).toBe('default')
  })

  it('returns first visible mode if current not found', () => {
    const modes: SessionMode[] = [createMode('default'), createMode('plan')]
    expect(getNextModeId(modes, 'unknownMode')).toBe('default')
  })

  it('returns null for empty modes array', () => {
    expect(getNextModeId([], 'default')).toBeNull()
  })

  it('returns null if all modes are hidden', () => {
    const modes: SessionMode[] = [createMode('dontAsk'), createMode('bypassPermissions')]
    expect(getNextModeId(modes, 'default')).toBeNull()
  })
})
