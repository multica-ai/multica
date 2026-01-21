/**
 * File change state management
 * Tracks file changes from the file system watcher (native fs.watch)
 */
import { create } from 'zustand'

interface FileChangeStore {
  // Counter that increments when files change - used to trigger refresh
  refreshCounter: number

  // Active session being watched
  watchedSessionId: string | null

  // Handle file system change event from watcher
  handleFileChange: (sessionId: string) => void

  // Set watched session
  setWatchedSession: (sessionId: string | null) => void
}

export const useFileChangeStore = create<FileChangeStore>((set, get) => ({
  refreshCounter: 0,
  watchedSessionId: null,

  handleFileChange: (sessionId: string) => {
    const state = get()
    // Only trigger refresh if the change is for the watched session
    if (state.watchedSessionId === sessionId) {
      set((state) => ({
        refreshCounter: state.refreshCounter + 1
      }))
    }
  },

  setWatchedSession: (sessionId: string | null) => {
    set({ watchedSessionId: sessionId })
  }
}))
