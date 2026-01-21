/**
 * Global UI state management using Zustand
 * Stores all UI-related state like panel visibility, etc.
 */
import { create } from 'zustand'
import { persist } from 'zustand/middleware'

// Width constraints
export const SIDEBAR_MIN_WIDTH = 200
export const SIDEBAR_MAX_WIDTH = 400
export const SIDEBAR_DEFAULT_WIDTH = 256

export const RIGHT_PANEL_MIN_WIDTH = 240
export const RIGHT_PANEL_MAX_WIDTH = 480
export const RIGHT_PANEL_DEFAULT_WIDTH = 320

interface UIStore {
  // Sidebar state
  sidebarOpen: boolean
  sidebarWidth: number
  toggleSidebar: () => void
  setSidebarOpen: (open: boolean) => void
  setSidebarWidth: (width: number) => void

  // Right panel state
  rightPanelOpen: boolean
  rightPanelWidth: number
  toggleRightPanel: () => void
  setRightPanelOpen: (open: boolean) => void
  setRightPanelWidth: (width: number) => void

  // Hidden files visibility
  showHiddenFiles: boolean
  toggleShowHiddenFiles: () => void
}

export const useUIStore = create<UIStore>()(
  persist(
    (set) => ({
      // Sidebar - default open
      sidebarOpen: true,
      sidebarWidth: SIDEBAR_DEFAULT_WIDTH,
      toggleSidebar: () => set((state) => ({ sidebarOpen: !state.sidebarOpen })),
      setSidebarOpen: (open) => set({ sidebarOpen: open }),
      setSidebarWidth: (width) =>
        set({ sidebarWidth: Math.max(SIDEBAR_MIN_WIDTH, Math.min(SIDEBAR_MAX_WIDTH, width)) }),

      // Right panel - default open
      rightPanelOpen: true,
      rightPanelWidth: RIGHT_PANEL_DEFAULT_WIDTH,
      toggleRightPanel: () => set((state) => ({ rightPanelOpen: !state.rightPanelOpen })),
      setRightPanelOpen: (open) => set({ rightPanelOpen: open }),
      setRightPanelWidth: (width) =>
        set({
          rightPanelWidth: Math.max(RIGHT_PANEL_MIN_WIDTH, Math.min(RIGHT_PANEL_MAX_WIDTH, width))
        }),

      // Hidden files - default hidden
      showHiddenFiles: false,
      toggleShowHiddenFiles: () => set((state) => ({ showHiddenFiles: !state.showHiddenFiles }))
    }),
    {
      name: 'ui-state' // localStorage key
    }
  )
)
