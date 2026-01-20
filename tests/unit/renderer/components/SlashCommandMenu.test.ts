/**
 * Tests for slash command utility functions
 */
import { describe, it, expect } from 'vitest'
import { parseSlashCommand, validateCommand } from '../../../../src/renderer/src/utils/slashCommand'
import type { AvailableCommand } from '../../../../src/shared/types'

describe('SlashCommandMenu', () => {
  describe('parseSlashCommand', () => {
    it('should return null for non-command input', () => {
      expect(parseSlashCommand('hello')).toBeNull()
      expect(parseSlashCommand(' /hello')).toBeNull()
      expect(parseSlashCommand('')).toBeNull()
    })

    it('should parse command without argument', () => {
      expect(parseSlashCommand('/')).toEqual({ command: '', argument: undefined })
      expect(parseSlashCommand('/help')).toEqual({ command: 'help', argument: undefined })
      expect(parseSlashCommand('/create_plan')).toEqual({
        command: 'create_plan',
        argument: undefined
      })
    })

    it('should parse command with argument', () => {
      expect(parseSlashCommand('/help me')).toEqual({ command: 'help', argument: 'me' })
      expect(parseSlashCommand('/search hello world')).toEqual({
        command: 'search',
        argument: 'hello world'
      })
    })

    it('should handle command with empty argument after space', () => {
      expect(parseSlashCommand('/help ')).toEqual({ command: 'help', argument: '' })
    })
  })

  describe('validateCommand', () => {
    const mockCommands: AvailableCommand[] = [
      { name: 'help', description: 'Show help' },
      { name: 'search', description: 'Search the codebase' },
      { name: 'create_plan', description: 'Create a plan' }
    ]

    it('should return null for non-command input', () => {
      expect(validateCommand('hello', mockCommands)).toBeNull()
      expect(validateCommand('', mockCommands)).toBeNull()
    })

    it('should return null for valid command', () => {
      expect(validateCommand('/help', mockCommands)).toBeNull()
      expect(validateCommand('/help me', mockCommands)).toBeNull()
      expect(validateCommand('/search query', mockCommands)).toBeNull()
    })

    it('should return null for incomplete command (empty name)', () => {
      expect(validateCommand('/', mockCommands)).toBeNull()
    })

    it('should return error for invalid command', () => {
      expect(validateCommand('/invalid', mockCommands)).toBe('/invalid is not a valid command')
      expect(validateCommand('/unknown command', mockCommands)).toBe(
        '/unknown is not a valid command'
      )
    })

    it('should return null when no commands available', () => {
      expect(validateCommand('/help', [])).toBeNull()
    })
  })
})
