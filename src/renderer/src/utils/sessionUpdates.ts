/**
 * Session update utilities
 */
import type { StoredSessionUpdate } from '../../../shared/types'

/**
 * Merge two arrays of session updates, deduplicating by sequence number or payload.
 * Updates with sequence numbers take precedence over updates identified by payload.
 */
export function mergeSessionUpdates(
  existing: StoredSessionUpdate[],
  incoming: StoredSessionUpdate[]
): StoredSessionUpdate[] {
  if (incoming.length === 0) {
    return existing
  }

  const merged: Array<StoredSessionUpdate | null> = []
  const keyToIndex = new Map<string, number>()
  const payloadToKey = new Map<string, string>()

  const buildPayloadKey = (item: StoredSessionUpdate): string => {
    try {
      return JSON.stringify(item.update)
    } catch {
      return `unstringifiable:${item.timestamp}`
    }
  }

  const addItem = (item: StoredSessionUpdate): void => {
    const payloadKey = buildPayloadKey(item)
    if (item.sequenceNumber !== undefined) {
      const seqKey = `seq:${item.sequenceNumber}`
      const existingPayloadKey = payloadToKey.get(payloadKey)
      if (existingPayloadKey?.startsWith('payload:')) {
        const existingIndex = keyToIndex.get(existingPayloadKey)
        if (existingIndex !== undefined) {
          merged[existingIndex] = null
        }
        keyToIndex.delete(existingPayloadKey)
      }

      const existingIndex = keyToIndex.get(seqKey)
      if (existingIndex !== undefined) {
        merged[existingIndex] = item
      } else {
        merged.push(item)
        keyToIndex.set(seqKey, merged.length - 1)
      }
      payloadToKey.set(payloadKey, seqKey)
      return
    }

    const payloadKeyWithPrefix = `payload:${payloadKey}`
    const existingPayloadKey = payloadToKey.get(payloadKey)
    if (existingPayloadKey?.startsWith('seq:')) {
      return
    }

    const existingIndex = keyToIndex.get(payloadKeyWithPrefix)
    if (existingIndex !== undefined) {
      merged[existingIndex] = item
    } else {
      merged.push(item)
      keyToIndex.set(payloadKeyWithPrefix, merged.length - 1)
    }
    payloadToKey.set(payloadKey, payloadKeyWithPrefix)
  }

  for (const item of existing) {
    addItem(item)
  }
  for (const item of incoming) {
    addItem(item)
  }

  return merged.filter((item): item is StoredSessionUpdate => item !== null)
}
