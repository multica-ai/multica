/**
 * Default agent configurations
 */
import type { AgentConfig, AppConfig } from '../../shared/types'

export const DEFAULT_AGENTS: Record<string, AgentConfig> = {
  opencode: {
    id: 'opencode',
    name: 'OpenCode',
    command: 'opencode',
    args: ['acp'],
    enabled: true,
  },
  codex: {
    id: 'codex',
    name: 'Codex CLI',
    command: 'codex',
    args: ['--acp'],
    enabled: true,
  },
  gemini: {
    id: 'gemini',
    name: 'Gemini CLI',
    command: 'gemini',
    args: ['acp'],
    enabled: true,
  },
}

export const DEFAULT_CONFIG: AppConfig = {
  version: '0.1.0',
  activeAgentId: 'opencode',
  agents: DEFAULT_AGENTS,
  ui: {
    theme: 'system',
    fontSize: 14,
  },
}
