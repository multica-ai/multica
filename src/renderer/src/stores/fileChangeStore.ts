/**
 * File change state management
 * Tracks file changes from agent tool calls to trigger FileTree refresh
 */
import { create } from 'zustand'

interface FileChangeStore {
  // Counter that increments when files change - used to trigger refresh
  refreshCounter: number

  // Trigger a file refresh
  triggerRefresh: () => void
}

export const useFileChangeStore = create<FileChangeStore>((set) => ({
  refreshCounter: 0,

  triggerRefresh: () => {
    set((state) => ({ refreshCounter: state.refreshCounter + 1 }))
  }
}))
