/**
 * Tests for commandStore
 */
import { describe, it, expect, beforeEach } from 'vitest'
import { useCommandStore } from '../../../../src/renderer/src/stores/commandStore'
import type { AvailableCommand } from '../../../../src/shared/types'

describe('commandStore', () => {
  beforeEach(() => {
    // Reset store before each test
    useCommandStore.getState().clearCommands()
  })

  describe('setAvailableCommands', () => {
    it('should set available commands', () => {
      const commands: AvailableCommand[] = [
        { name: 'help', description: 'Show help' },
        { name: 'search', description: 'Search the codebase' }
      ]

      useCommandStore.getState().setAvailableCommands(commands)

      expect(useCommandStore.getState().availableCommands).toEqual(commands)
    })

    it('should replace existing commands', () => {
      const initialCommands: AvailableCommand[] = [{ name: 'help', description: 'Show help' }]
      const newCommands: AvailableCommand[] = [
        { name: 'search', description: 'Search the codebase' },
        { name: 'create', description: 'Create something' }
      ]

      useCommandStore.getState().setAvailableCommands(initialCommands)
      useCommandStore.getState().setAvailableCommands(newCommands)

      expect(useCommandStore.getState().availableCommands).toEqual(newCommands)
    })
  })

  describe('clearCommands', () => {
    it('should clear all commands', () => {
      const commands: AvailableCommand[] = [{ name: 'help', description: 'Show help' }]

      useCommandStore.getState().setAvailableCommands(commands)
      useCommandStore.getState().clearCommands()

      expect(useCommandStore.getState().availableCommands).toEqual([])
    })
  })

  describe('initial state', () => {
    it('should start with empty commands', () => {
      expect(useCommandStore.getState().availableCommands).toEqual([])
    })
  })
})
