/**
 * Draft store for managing per-session input drafts
 *
 * Stores draft text and images for each session, allowing users to switch
 * between sessions without losing their unsent messages.
 */
import { create } from 'zustand'
import type { ImageContentItem } from '../../../shared/types/message'

export interface SessionDraft {
  text: string
  images: ImageContentItem[]
}

interface DraftStore {
  /** Drafts indexed by session ID */
  drafts: Record<string, SessionDraft>

  /** Get draft for a session (returns empty draft if none exists) */
  getDraft: (sessionId: string) => SessionDraft

  /** Update draft for a session (partial update supported) */
  setDraft: (sessionId: string, draft: Partial<SessionDraft>) => void

  /** Clear draft for a session */
  clearDraft: (sessionId: string) => void
}

const createEmptyDraft = (): SessionDraft => ({ text: '', images: [] })

export const useDraftStore = create<DraftStore>((set, get) => ({
  drafts: {},

  getDraft: (sessionId) => {
    const draft = get().drafts[sessionId]
    if (!draft) return createEmptyDraft()
    return { text: draft.text, images: [...draft.images] }
  },

  setDraft: (sessionId, draft) =>
    set((state) => {
      const currentDraft = state.drafts[sessionId] ?? createEmptyDraft()
      return {
        drafts: {
          ...state.drafts,
          [sessionId]: {
            text: draft.text ?? currentDraft.text,
            images: draft.images ? [...draft.images] : currentDraft.images
          }
        }
      }
    }),

  clearDraft: (sessionId) =>
    set((state) => {
      const nextDrafts = { ...state.drafts }
      delete nextDrafts[sessionId]
      return { drafts: nextDrafts }
    })
}))
