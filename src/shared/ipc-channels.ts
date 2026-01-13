/**
 * IPC Channel definitions for communication between main and renderer processes
 */
export const IPC_CHANNELS = {
  // Agent communication
  AGENT_PROMPT: 'agent:prompt',
  AGENT_CANCEL: 'agent:cancel',
  AGENT_SWITCH: 'agent:switch',
  AGENT_START: 'agent:start',
  AGENT_STOP: 'agent:stop',
  AGENT_MESSAGE: 'agent:message',
  AGENT_ERROR: 'agent:error',
  AGENT_STATUS: 'agent:status',

  // Session management
  SESSION_CREATE: 'session:create',
  SESSION_LIST: 'session:list',
  SESSION_GET: 'session:get',
  SESSION_RESUME: 'session:resume',
  SESSION_DELETE: 'session:delete',
  SESSION_UPDATE: 'session:update',

  // Configuration
  CONFIG_GET: 'config:get',
  CONFIG_UPDATE: 'config:update',

  // Dialog
  DIALOG_SELECT_DIRECTORY: 'dialog:select-directory',

  // System
  SYSTEM_CHECK_AGENTS: 'system:check-agents',

  // File system (V2)
  FILE_APPROVAL_REQUEST: 'file:approval-request',
  FILE_APPROVAL_RESPONSE: 'file:approval-response',
} as const

export type IPCChannel = (typeof IPC_CHANNELS)[keyof typeof IPC_CHANNELS]
