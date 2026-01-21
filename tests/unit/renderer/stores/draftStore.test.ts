/**
 * Tests for draftStore
 */
import { beforeEach, describe, expect, it } from 'vitest'
import { useDraftStore } from '../../../../src/renderer/src/stores/draftStore'
import type { ImageContentItem } from '../../../../src/shared/types/message'

describe('draftStore', () => {
  beforeEach(() => {
    useDraftStore.setState({ drafts: {} })
  })

  it('returns an empty draft for missing sessions', () => {
    const draft = useDraftStore.getState().getDraft('missing')
    expect(draft).toEqual({ text: '', images: [] })
  })

  it('does not expose internal draft references', () => {
    const image: ImageContentItem = {
      type: 'image',
      data: 'abc',
      mimeType: 'image/png'
    }

    useDraftStore.getState().setDraft('session-a', { text: 'hello', images: [image] })

    const firstRead = useDraftStore.getState().getDraft('session-a')
    firstRead.images.push({
      type: 'image',
      data: 'mutated',
      mimeType: 'image/png'
    })

    const secondRead = useDraftStore.getState().getDraft('session-a')
    expect(secondRead.images).toHaveLength(1)
    expect(secondRead.images[0]).toEqual(image)
  })

  it('clones image arrays when setting drafts', () => {
    const images: ImageContentItem[] = [{ type: 'image', data: 'one', mimeType: 'image/png' }]

    useDraftStore.getState().setDraft('session-a', { images })
    images.push({ type: 'image', data: 'two', mimeType: 'image/png' })

    const stored = useDraftStore.getState().getDraft('session-a')
    expect(stored.images).toHaveLength(1)
  })

  it('clears drafts for a session', () => {
    useDraftStore.getState().setDraft('session-a', { text: 'hello' })
    useDraftStore.getState().clearDraft('session-a')

    expect(useDraftStore.getState().drafts['session-a']).toBeUndefined()
  })
})
