import { describe, it, expect } from 'vitest'
import { mergeSessionUpdates } from '../../../../src/renderer/src/utils/sessionUpdates'
import type { StoredSessionUpdate } from '../../../../src/shared/types'

function makeUpdate(
  overrides: Partial<StoredSessionUpdate> & { update: StoredSessionUpdate['update'] }
): StoredSessionUpdate {
  return {
    timestamp: new Date().toISOString(),
    ...overrides
  }
}

describe('mergeSessionUpdates', () => {
  it('should return existing when incoming is empty', () => {
    const existing = [
      makeUpdate({ update: { sessionId: 's1', update: { sessionUpdate: 'text_delta' } } })
    ]
    const result = mergeSessionUpdates(existing, [])
    expect(result).toBe(existing)
  })

  it('should combine non-overlapping updates', () => {
    const existing = [
      makeUpdate({ update: { sessionId: 's1', update: { sessionUpdate: 'text_delta' } } })
    ]
    const incoming = [
      makeUpdate({ update: { sessionId: 's1', update: { sessionUpdate: 'end_turn' } } })
    ]
    const result = mergeSessionUpdates(existing, incoming)
    expect(result).toHaveLength(2)
  })

  it('should deduplicate by sequence number', () => {
    const update = {
      sessionId: 's1',
      update: { sessionUpdate: 'text_delta' }
    } as StoredSessionUpdate['update']

    const existing = [makeUpdate({ sequenceNumber: 1, update })]
    const incoming = [makeUpdate({ sequenceNumber: 1, update })]
    const result = mergeSessionUpdates(existing, incoming)
    expect(result).toHaveLength(1)
  })

  it('should deduplicate by payload when no sequence number', () => {
    const update = {
      sessionId: 's1',
      update: { sessionUpdate: 'text_delta' }
    } as StoredSessionUpdate['update']

    const existing = [makeUpdate({ update })]
    const incoming = [makeUpdate({ update })]
    const result = mergeSessionUpdates(existing, incoming)
    expect(result).toHaveLength(1)
  })

  it('should replace payload-keyed item with sequence-numbered version', () => {
    const update = {
      sessionId: 's1',
      update: {
        sessionUpdate: 'user_message',
        content: [{ type: 'text', text: 'hello' }],
        _internal: false
      }
    } as StoredSessionUpdate['update']

    const optimistic = makeUpdate({ update })
    const fromDb = makeUpdate({ sequenceNumber: 5, update })

    const result = mergeSessionUpdates([optimistic], [fromDb])
    expect(result).toHaveLength(1)
    expect(result[0].sequenceNumber).toBe(5)
  })

  it('should deduplicate optimistic user_message with DB version when _internal matches', () => {
    const content = [{ type: 'text', text: 'hello world' }]
    const agentSessionId = 'agent-session-1'

    // Optimistic update (frontend) - with _internal: false to match DB
    const optimistic = makeUpdate({
      update: {
        sessionId: agentSessionId,
        update: {
          sessionUpdate: 'user_message',
          content,
          _internal: false
        }
      } as StoredSessionUpdate['update']
    })

    // DB-stored version (returned by focus sync)
    const fromDb = makeUpdate({
      sequenceNumber: 10,
      update: {
        sessionId: agentSessionId,
        update: {
          sessionUpdate: 'user_message',
          content,
          _internal: false
        }
      } as StoredSessionUpdate['update']
    })

    const result = mergeSessionUpdates([optimistic], [fromDb])
    expect(result).toHaveLength(1)
    expect(result[0].sequenceNumber).toBe(10)
  })

  it('should NOT deduplicate when _internal field is missing (the bug scenario)', () => {
    const content = [{ type: 'text', text: 'hello world' }]
    const agentSessionId = 'agent-session-1'

    // Optimistic update WITHOUT _internal (the old buggy format)
    const optimistic = makeUpdate({
      update: {
        sessionId: agentSessionId,
        update: {
          sessionUpdate: 'user_message',
          content
        }
      } as StoredSessionUpdate['update']
    })

    // DB-stored version WITH _internal
    const fromDb = makeUpdate({
      sequenceNumber: 10,
      update: {
        sessionId: agentSessionId,
        update: {
          sessionUpdate: 'user_message',
          content,
          _internal: false
        }
      } as StoredSessionUpdate['update']
    })

    // This demonstrates the bug: mismatched payloads cause duplication
    const result = mergeSessionUpdates([optimistic], [fromDb])
    expect(result).toHaveLength(2)
  })

  it('should skip payload-only item when seq-keyed version already exists', () => {
    const update = {
      sessionId: 's1',
      update: { sessionUpdate: 'text_delta' }
    } as StoredSessionUpdate['update']

    const fromDb = makeUpdate({ sequenceNumber: 1, update })
    const duplicate = makeUpdate({ update })

    const result = mergeSessionUpdates([fromDb], [duplicate])
    expect(result).toHaveLength(1)
    expect(result[0].sequenceNumber).toBe(1)
  })

  it('should preserve order of non-duplicate updates', () => {
    const u1 = makeUpdate({
      sequenceNumber: 1,
      update: {
        sessionId: 's1',
        update: { sessionUpdate: 'text_delta', text: 'a' }
      } as StoredSessionUpdate['update']
    })
    const u2 = makeUpdate({
      sequenceNumber: 2,
      update: {
        sessionId: 's1',
        update: { sessionUpdate: 'text_delta', text: 'b' }
      } as StoredSessionUpdate['update']
    })
    const u3 = makeUpdate({
      sequenceNumber: 3,
      update: {
        sessionId: 's1',
        update: { sessionUpdate: 'end_turn' }
      } as StoredSessionUpdate['update']
    })

    const result = mergeSessionUpdates([u1], [u2, u3])
    expect(result).toHaveLength(3)
    expect(result[0].sequenceNumber).toBe(1)
    expect(result[1].sequenceNumber).toBe(2)
    expect(result[2].sequenceNumber).toBe(3)
  })
})
