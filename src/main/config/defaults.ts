/**
 * Default agent configurations
 */
import type { AgentConfig, AppConfig } from '../../shared/types'

export const DEFAULT_AGENTS: Record<string, AgentConfig> = {
  'claude-code': {
    id: 'claude-code',
    name: 'Claude Code',
    command: 'claude-code-acp',
    args: [],
    enabled: true
    // Note: Uses https://github.com/zed-industries/claude-code-acp
    // Requires ANTHROPIC_API_KEY environment variable
  },
  opencode: {
    id: 'opencode',
    name: 'OpenCode',
    command: 'opencode',
    args: ['acp'],
    enabled: true
  },
  codex: {
    id: 'codex',
    name: 'Codex',
    command: 'codex-acp',
    args: [],
    enabled: true
    // Note: Uses https://github.com/zed-industries/codex-acp
    // Official Codex CLI doesn't support ACP
  }
}

export const DEFAULT_CONFIG: AppConfig = {
  version: '0.1.0',
  activeAgentId: 'opencode',
  agents: DEFAULT_AGENTS,
  ui: {
    theme: 'system',
    fontSize: 14
  }
}
