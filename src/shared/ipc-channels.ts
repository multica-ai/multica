/**
 * IPC Channel definitions for communication between main and renderer processes
 */
export const IPC_CHANNELS = {
  // Agent communication (per-session)
  AGENT_PROMPT: 'agent:prompt',
  AGENT_CANCEL: 'agent:cancel',
  AGENT_MESSAGE: 'agent:message',
  AGENT_ERROR: 'agent:error',
  AGENT_STATUS: 'agent:status', // Returns status of all running sessions

  // Session management
  SESSION_CREATE: 'session:create',
  SESSION_LIST: 'session:list',
  SESSION_GET: 'session:get',
  SESSION_LOAD: 'session:load', // Load session without starting agent
  SESSION_START_AGENT: 'session:start-agent', // Start agent for a session
  SESSION_RESUME: 'session:resume',
  SESSION_DELETE: 'session:delete',
  SESSION_UPDATE: 'session:update',
  SESSION_SWITCH_AGENT: 'session:switch-agent',
  SESSION_META_UPDATED: 'session:meta-updated', // Push event when session metadata changes (e.g., agentSessionId)
  SESSION_GET_MODES: 'session:get-modes', // Get current session's available modes
  SESSION_GET_MODELS: 'session:get-models', // Get current session's available models
  SESSION_SET_MODE: 'session:set-mode', // Set session mode
  SESSION_SET_MODEL: 'session:set-model', // Set session model
  SESSION_GET_COMMANDS: 'session:get-commands', // Get current session's available slash commands

  // Configuration
  CONFIG_GET: 'config:get',
  CONFIG_UPDATE: 'config:update',

  // Dialog
  DIALOG_SELECT_DIRECTORY: 'dialog:select-directory',

  // System
  SYSTEM_CHECK_AGENTS: 'system:check-agents',
  SYSTEM_CHECK_AGENT: 'system:check-agent',

  // Agent installation
  AGENT_INSTALL: 'agent:install',
  AGENT_INSTALL_PROGRESS: 'agent:install-progress',

  // File system (V2)
  FILE_APPROVAL_REQUEST: 'file:approval-request',
  FILE_APPROVAL_RESPONSE: 'file:approval-response',

  // Permission request (ACP)
  PERMISSION_REQUEST: 'permission:request',
  PERMISSION_RESPONSE: 'permission:response',

  // File tree
  FS_LIST_DIRECTORY: 'fs:list-directory',
  FS_DETECT_APPS: 'fs:detect-apps',
  FS_OPEN_WITH: 'fs:open-with',

  // File watcher
  FS_WATCH_START: 'fs:watch-start',
  FS_WATCH_STOP: 'fs:watch-stop',
  FS_FILE_CHANGED: 'fs:file-changed',

  // Terminal
  TERMINAL_RUN: 'terminal:run',

  // App lifecycle
  APP_FOCUS: 'app:focus',

  // Auto-update
  UPDATE_CHECK: 'update:check',
  UPDATE_DOWNLOAD: 'update:download',
  UPDATE_INSTALL: 'update:install',
  UPDATE_STATUS: 'update:status'
} as const

export type IPCChannel = (typeof IPC_CHANNELS)[keyof typeof IPC_CHANNELS]
