/**
 * Tests for slash command utility functions
 */
import { describe, it, expect } from 'vitest'
import { parseSlashCommand, validateCommand } from '../../../../src/renderer/src/utils/slashCommand'
import type { AvailableCommand } from '../../../../src/shared/types'

/**
 * Helper to extract the current line at a given cursor position,
 * mirroring the logic in MessageInput.tsx
 */
function getCurrentLineAtCursor(text: string, cursorPosition: number): string {
  const textBeforeCursor = text.slice(0, cursorPosition)
  const lastNewline = textBeforeCursor.lastIndexOf('\n')
  return textBeforeCursor.slice(lastNewline + 1)
}

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

    it('should parse command with multiline argument', () => {
      expect(parseSlashCommand('/help line1\nline2')).toEqual({
        command: 'help',
        argument: 'line1\nline2'
      })
      expect(parseSlashCommand('/search hello\nworld\nfoo')).toEqual({
        command: 'search',
        argument: 'hello\nworld\nfoo'
      })
    })

    it('should parse command name when followed by newline without space', () => {
      // "/help\n" — no space before newline, so command is "help\n..." which is non-whitespace
      // Actually \n is whitespace, so \S* stops at \n. Let's verify behavior:
      // The regex ^\/(\S*)(?:\s+([\s\S]*))?$ with input "/help\nmore"
      // \S* matches "help", then \s+ matches \n, then [\s\S]* matches "more"
      expect(parseSlashCommand('/help\nmore')).toEqual({
        command: 'help',
        argument: 'more'
      })
    })

    it('should parse standalone command with trailing newline', () => {
      expect(parseSlashCommand('/help\n')).toEqual({
        command: 'help',
        argument: ''
      })
    })
  })

  describe('getCurrentLineAtCursor (multiline slash command detection)', () => {
    it('should return the full text when there is no newline', () => {
      expect(getCurrentLineAtCursor('/help', 5)).toBe('/help')
      expect(getCurrentLineAtCursor('/', 1)).toBe('/')
    })

    it('should return the current line when cursor is on a new line', () => {
      // User typed "hello\n/" - cursor at end (position 7)
      expect(getCurrentLineAtCursor('hello\n/', 7)).toBe('/')
      // User typed "hello\n/he" - cursor at end (position 9)
      expect(getCurrentLineAtCursor('hello\n/he', 9)).toBe('/he')
    })

    it('should return the current line for multi-line input', () => {
      const text = 'line1\nline2\n/search'
      // Cursor at end (position 19)
      expect(getCurrentLineAtCursor(text, 19)).toBe('/search')
    })

    it('should detect slash command on any line with cursor', () => {
      const text = 'some text\n/help\nmore text'
      // Cursor after "/help" (position 15)
      expect(getCurrentLineAtCursor(text, 15)).toBe('/help')
    })

    it('should not detect slash command when cursor is on a non-command line', () => {
      const text = '/help\nnon-command line'
      // Cursor at end of second line (position 21)
      const currentLine = getCurrentLineAtCursor(text, 21)
      expect(parseSlashCommand(currentLine)).toBeNull()
    })

    it('should detect slash command when cursor is in the middle of command text', () => {
      // User typed "/hel" and cursor is at position 4
      const text = 'hello\n/hel'
      expect(getCurrentLineAtCursor(text, 10)).toBe('/hel')
      expect(parseSlashCommand(getCurrentLineAtCursor(text, 10))).toEqual({
        command: 'hel',
        argument: undefined
      })
    })

    it('should handle empty line after newline', () => {
      expect(getCurrentLineAtCursor('hello\n', 6)).toBe('')
    })

    it('should handle cursor at position 0', () => {
      expect(getCurrentLineAtCursor('/help', 0)).toBe('')
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

    it('should validate command with multiline argument', () => {
      expect(validateCommand('/help line1\nline2', mockCommands)).toBeNull()
      expect(validateCommand('/search query\nmore', mockCommands)).toBeNull()
      expect(validateCommand('/invalid line1\nline2', mockCommands)).toBe(
        '/invalid is not a valid command'
      )
    })
  })
})
