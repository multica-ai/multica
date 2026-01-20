/**
 * Slash command state management
 *
 * Stores available commands for the current session, updated via:
 * 1. Initial fetch when switching sessions (getSessionCommands)
 * 2. Real-time updates from available_commands_update events
 */
import { create } from 'zustand'
import type { AvailableCommand } from '../../../shared/types'

interface CommandStore {
  /** Available slash commands for the current session */
  availableCommands: AvailableCommand[]

  /** Set available commands (replaces all) */
  setAvailableCommands: (commands: AvailableCommand[]) => void

  /** Clear available commands (on session switch) */
  clearCommands: () => void
}

export const useCommandStore = create<CommandStore>((set) => ({
  availableCommands: [],

  setAvailableCommands: (commands) => {
    set({ availableCommands: commands })
  },

  clearCommands: () => {
    set({ availableCommands: [] })
  }
}))
