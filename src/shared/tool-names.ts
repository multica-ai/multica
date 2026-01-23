/**
 * Centralized tool name constants and utilities
 *
 * This module provides a single source of truth for tool name detection,
 * supporting both Claude Code's 'AskUserQuestion' and OpenCode's 'question' tool.
 */

// Tool name constants
export const TOOL_NAMES = {
  // Question tools (both Claude Code and OpenCode variants)
  ASK_USER_QUESTION: 'AskUserQuestion',
  QUESTION: 'question',
  MCP_CONDUCTOR_ASK_USER_QUESTION: 'mcp__conductor__askuserquestion',

  // File operations
  READ: 'Read',
  WRITE: 'Write',
  EDIT: 'Edit',

  // Shell/terminal
  BASH: 'Bash',
  EXECUTE: 'execute',

  // Search tools
  GREP: 'Grep',
  GLOB: 'Glob',
  SEARCH: 'Search',

  // Web tools
  WEB_SEARCH: 'WebSearch',
  WEB_FETCH: 'WebFetch',

  // Agent tools
  TASK: 'Task',
  TODO_WRITE: 'TodoWrite',

  // Plan tools
  EXIT_PLAN_MODE: 'ExitPlanMode',
  ENTER_PLAN_MODE: 'EnterPlanMode',
  PLAN_EXIT: 'plan_exit' // OpenCode variant
} as const

// Type for tool names
export type ToolName = (typeof TOOL_NAMES)[keyof typeof TOOL_NAMES]

/**
 * Check if a tool name/title represents a question tool
 * Supports both exact match and case-insensitive comparison
 *
 * @param toolName - The tool name or title to check
 * @returns true if this is a question-related tool
 */
export function isQuestionTool(toolName: string | undefined | null): boolean {
  if (!toolName) return false

  const normalized = toolName.toLowerCase()
  return (
    toolName === TOOL_NAMES.ASK_USER_QUESTION ||
    toolName === TOOL_NAMES.QUESTION ||
    normalized === 'askuserquestion' ||
    normalized === 'question' ||
    normalized === TOOL_NAMES.MCP_CONDUCTOR_ASK_USER_QUESTION
  )
}

/**
 * Check if a tool name/title represents a plan approval tool (ExitPlanMode)
 * These tools require user approval before executing the plan
 *
 * @param toolName - The tool name or title to check
 * @returns true if this is a plan approval tool
 */
export function isPlanApprovalTool(toolName: string | undefined | null): boolean {
  if (!toolName) return false

  const normalized = toolName.toLowerCase()
  return (
    toolName === TOOL_NAMES.EXIT_PLAN_MODE ||
    toolName === TOOL_NAMES.PLAN_EXIT ||
    normalized === 'exitplanmode' ||
    normalized === 'plan_exit' ||
    normalized.includes('ready to code') // Claude Code's display title
  )
}
