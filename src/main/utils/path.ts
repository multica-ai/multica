/**
 * Path utilities for agent process management
 */
import { homedir } from 'node:os'

/**
 * Get enhanced PATH that includes common custom installation directories
 * Used by both agent-check and AgentProcess to ensure consistent command resolution
 */
export function getEnhancedPath(): string {
  const home = homedir()
  const customPaths = [
    `${home}/.opencode/bin`,
    `${home}/.claude/local/bin`,
    `${home}/.local/bin`,
    '/opt/homebrew/bin',
    '/usr/local/bin'
  ]
  return `${customPaths.join(':')}:${process.env.PATH || ''}`
}
