/**
 * Core type definitions shared between main and renderer processes
 */

// Agent configuration
export interface AgentConfig {
  id: string
  name: string
  command: string
  args: string[]
  env?: Record<string, string>
  enabled: boolean
}

// Session state
export interface SessionInfo {
  id: string
  workingDirectory: string
  agentId: string
  createdAt: string
  isActive: boolean
}

// Message types for UI
export interface ChatMessage {
  id: string
  role: 'user' | 'assistant'
  content: MessageContent[]
  timestamp: string
  status: 'pending' | 'streaming' | 'complete' | 'error'
}

export type MessageContent =
  | { type: 'text'; text: string }
  | { type: 'code'; language: string; code: string }
  | { type: 'diff'; filePath: string; hunks: DiffHunk[] }
  | { type: 'tool_call'; name: string; status: 'pending' | 'approved' | 'denied' | 'complete' }

export interface DiffHunk {
  oldStart: number
  oldLines: number
  newStart: number
  newLines: number
  content: string
}

// Agent status
export type AgentStatus =
  | { state: 'stopped' }
  | { state: 'starting'; agentId: string }
  | { state: 'running'; agentId: string; sessionCount: number }
  | { state: 'error'; error: string }

// Application configuration
export interface AppConfig {
  version: string
  activeAgentId: string
  agents: Record<string, AgentConfig>
  ui: UIConfig
}

export interface UIConfig {
  theme: 'light' | 'dark' | 'system'
  fontSize: number
}

// File operation approval (V2)
export interface FileApprovalRequest {
  requestId: string
  operation: 'read' | 'write' | 'delete'
  path: string
  content?: string
  reason?: string
}

export interface FileApprovalResponse {
  requestId: string
  approved: boolean
  remember?: 'once' | 'session' | 'always'
}

// Re-export session types
export * from './types/session'

// Re-export mode/model types from ACP SDK for frontend use
export * from './types/mode'
export * from './types/model'

// Re-export AvailableCommand type from ACP SDK
export type { AvailableCommand } from '@agentclientprotocol/sdk/dist/schema/types.gen'
