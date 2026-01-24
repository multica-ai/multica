/**
 * Tests for renderer utils
 */
import { describe, expect, it } from 'vitest'
import { formatDuration, formatLocalizedDatetime } from '../../../../src/renderer/src/lib/utils'

describe('formatDuration', () => {
  it('returns seconds with 1 decimal place for durations under 1 minute', () => {
    expect(formatDuration(0)).toBe('0.0s')
    expect(formatDuration(1000)).toBe('1.0s')
    expect(formatDuration(1230)).toBe('1.2s')
    expect(formatDuration(45000)).toBe('45.0s')
    expect(formatDuration(59500)).toBe('59.5s')
  })

  it('returns minutes only when seconds is zero', () => {
    expect(formatDuration(60000)).toBe('1m')
    expect(formatDuration(120000)).toBe('2m')
    expect(formatDuration(300000)).toBe('5m')
  })

  it('returns minutes and seconds for durations over 1 minute', () => {
    expect(formatDuration(61000)).toBe('1m 1s')
    expect(formatDuration(89000)).toBe('1m 29s')
    expect(formatDuration(125000)).toBe('2m 5s')
    expect(formatDuration(3661000)).toBe('61m 1s')
  })
})

describe('formatLocalizedDatetime', () => {
  it('formats timestamp number to localized string', () => {
    const timestamp = new Date('2026-01-21T15:03:00').getTime()
    const result = formatLocalizedDatetime(timestamp)
    // Check that it contains expected parts (locale-independent assertions)
    expect(result).toContain('2026')
    expect(result).toContain('21')
  })

  it('formats ISO string to localized string', () => {
    const result = formatLocalizedDatetime('2026-01-21T15:03:00')
    expect(result).toContain('2026')
    expect(result).toContain('21')
  })

  it('handles different timestamp formats', () => {
    // ISO string with timezone
    const result1 = formatLocalizedDatetime('2026-01-21T15:03:00Z')
    expect(result1).toContain('2026')

    // Timestamp number
    const result2 = formatLocalizedDatetime(Date.now())
    expect(typeof result2).toBe('string')
    expect(result2.length).toBeGreaterThan(0)
  })

  it('returns "Unknown time" for invalid dates', () => {
    expect(formatLocalizedDatetime('invalid-date')).toBe('Unknown time')
    expect(formatLocalizedDatetime(NaN)).toBe('Unknown time')
  })
})
