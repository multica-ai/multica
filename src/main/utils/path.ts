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
    `${home}/.npm-global/bin`, // User-level npm global directory
    `${home}/.opencode/bin`,
    `${home}/.claude/local/bin`,
    `${home}/.claude/local`,
    `${home}/.bun/bin`,
    `${home}/.local/bin`,
    '/opt/homebrew/bin',
    '/usr/local/bin'
  ]
  return `${customPaths.join(':')}:${process.env.PATH || ''}`
}

/**
 * Get the user-level npm global prefix directory
 * Used for installing npm packages without requiring sudo
 */
export function getNpmUserPrefix(): string {
  const home = homedir()
  return `${home}/.npm-global`
}
