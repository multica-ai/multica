/**
 * Slash command utility functions
 */
import type { AvailableCommand } from '../../../shared/types'

/**
 * Parse slash command from input text
 * Returns the command name and optional argument if input starts with "/"
 * Supports multiline input: the first line contains the command, and any
 * remaining content (after the command name and optional space) is the argument.
 */
export function parseSlashCommand(text: string): { command?: string; argument?: string } | null {
  if (!text.startsWith('/')) return null
  const match = text.match(/^\/(\S*)(?:\s+([\s\S]*))?$/)
  if (!match) return null
  return { command: match[1], argument: match[2] }
}

/**
 * Validate if a command is available
 * Returns error message if invalid, null if valid
 */
export function validateCommand(
  text: string,
  availableCommands: AvailableCommand[]
): string | null {
  const parsed = parseSlashCommand(text)
  if (!parsed) return null // Not a command

  // Empty command name is ok during typing
  if (!parsed.command) return null

  // If no commands available, skip validation (agent doesn't support slash commands)
  if (availableCommands.length === 0) return null

  // Check if command exists
  const isValid = availableCommands.some((cmd) => cmd.name === parsed.command)
  if (!isValid) {
    return `/${parsed.command} is not a valid command`
  }

  return null
}
