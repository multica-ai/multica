import { ipcRenderer, contextBridge } from 'electron'

// ============================================================================
// Type definitions for IPC API
// ============================================================================

export interface HubStatus {
  hubId: string
  status: string
  agentCount: number
  gatewayConnected: boolean
  gatewayUrl?: string
  defaultAgent?: {
    agentId: string
    status: string
  } | null
}

export interface AgentInfo {
  agentId: string
  status: string
}

export interface ToolInfo {
  name: string
  group: string
  enabled: boolean
}

export interface SkillInfo {
  id: string
  name: string
  description: string
  version: string
  enabled: boolean
  source: 'bundled' | 'global' | 'profile'
  triggers: string[]
}

export interface ProfileData {
  profileId: string | undefined
  name: string | undefined
  style: string | undefined
  userContent: string | undefined
}

export interface ProviderStatus {
  id: string
  name: string
  authMethod: 'api-key' | 'oauth'
  available: boolean
  configured: boolean
  current: boolean
  defaultModel: string
  models: string[]
  loginUrl?: string
  loginCommand?: string
  loginInstructions?: string
}

export interface CurrentProviderInfo {
  provider: string
  model: string | undefined
  providerName: string | undefined
  available: boolean
}

// Local chat event types (for direct IPC communication without Gateway)
export interface LocalChatEvent {
  agentId: string
  streamId?: string
  type?: 'error'
  content?: string
  event?: {
    type: 'message_start' | 'message_update' | 'message_end' | 'tool_execution_start' | 'tool_execution_end' | 'compaction_start' | 'compaction_end'
    id?: string
    message?: {
      role: string
      content?: Array<{ type: string; text?: string }>
    }
    [key: string]: unknown
  }
}

// Local chat approval request (mirrors ExecApprovalRequestPayload from @multica/sdk)
export interface LocalChatApproval {
  approvalId: string
  agentId: string
  command: string
  cwd?: string
  riskLevel: 'safe' | 'needs-review' | 'dangerous'
  riskReasons: string[]
  expiresAtMs: number
}

// Available style options
export const AGENT_STYLES = ['concise', 'warm', 'playful', 'professional'] as const
export type AgentStyle = (typeof AGENT_STYLES)[number]

// ============================================================================
// Expose typed API to Renderer process
// ============================================================================

const electronAPI = {
  // Hub management
  hub: {
    init: () => ipcRenderer.invoke('hub:init'),
    getStatus: (): Promise<HubStatus> => ipcRenderer.invoke('hub:getStatus'),
    getAgentInfo: (): Promise<AgentInfo | null> => ipcRenderer.invoke('hub:getAgentInfo'),
    info: () => ipcRenderer.invoke('hub:info'),
    reconnect: (url: string) => ipcRenderer.invoke('hub:reconnect', url),
    listAgents: () => ipcRenderer.invoke('hub:listAgents'),
    createAgent: (id?: string) => ipcRenderer.invoke('hub:createAgent', id),
    getAgent: (id: string) => ipcRenderer.invoke('hub:getAgent', id),
    closeAgent: (id: string) => ipcRenderer.invoke('hub:closeAgent', id),
    sendMessage: (agentId: string, content: string) =>
      ipcRenderer.invoke('hub:sendMessage', agentId, content),
    registerToken: (token: string, agentId: string, expiresAt: number) =>
      ipcRenderer.invoke('hub:registerToken', token, agentId, expiresAt),
    onDeviceConfirmRequest: (callback: (deviceId: string, meta?: { userAgent?: string; platform?: string; language?: string }) => void) => {
      ipcRenderer.on('hub:device-confirm-request', (_event, deviceId: string, meta?: { userAgent?: string; platform?: string; language?: string }) => callback(deviceId, meta))
    },
    offDeviceConfirmRequest: () => {
      ipcRenderer.removeAllListeners('hub:device-confirm-request')
    },
    deviceConfirmResponse: (deviceId: string, allowed: boolean) => {
      ipcRenderer.send('hub:device-confirm-response', deviceId, allowed)
    },
    listDevices: () => ipcRenderer.invoke('hub:listDevices'),
    revokeDevice: (deviceId: string) => ipcRenderer.invoke('hub:revokeDevice', deviceId),
    onConnectionStateChanged: (callback: (state: string) => void) => {
      ipcRenderer.on('hub:connection-state-changed', (_event, state: string) => callback(state))
    },
    offConnectionStateChanged: () => {
      ipcRenderer.removeAllListeners('hub:connection-state-changed')
    },
    onDevicesChanged: (callback: () => void) => {
      ipcRenderer.on('hub:devices-changed', () => callback())
    },
    offDevicesChanged: () => {
      ipcRenderer.removeAllListeners('hub:devices-changed')
    },
  },

  // Tools management
  tools: {
    list: (): Promise<ToolInfo[]> => ipcRenderer.invoke('tools:list'),
    toggle: (name: string) => ipcRenderer.invoke('tools:toggle', name),
    setStatus: (name: string, enabled: boolean) =>
      ipcRenderer.invoke('tools:setStatus', name, enabled),
    active: () => ipcRenderer.invoke('tools:active'),
    reload: () => ipcRenderer.invoke('tools:reload'),
  },

  // Skills management
  skills: {
    list: (): Promise<SkillInfo[]> => ipcRenderer.invoke('skills:list'),
    get: (id: string) => ipcRenderer.invoke('skills:get', id),
    toggle: (id: string) => ipcRenderer.invoke('skills:toggle', id),
    setStatus: (id: string, enabled: boolean) =>
      ipcRenderer.invoke('skills:setStatus', id, enabled),
    reload: () => ipcRenderer.invoke('skills:reload'),
    add: (source: string, options?: { name?: string; force?: boolean }) =>
      ipcRenderer.invoke('skills:add', source, options),
    remove: (name: string) => ipcRenderer.invoke('skills:remove', name),
  },

  // Agent management
  agent: {
    status: () => ipcRenderer.invoke('agent:status'),
  },

  // Profile management
  profile: {
    get: (): Promise<ProfileData> => ipcRenderer.invoke('profile:get'),
    updateName: (name: string) => ipcRenderer.invoke('profile:updateName', name),
    updateStyle: (style: string) => ipcRenderer.invoke('profile:updateStyle', style),
    updateUser: (content: string) => ipcRenderer.invoke('profile:updateUser', content),
  },

  // Provider management
  provider: {
    /** List all providers with their status */
    list: (): Promise<ProviderStatus[]> => ipcRenderer.invoke('provider:list'),
    /** List only available (configured) providers */
    listAvailable: (): Promise<ProviderStatus[]> => ipcRenderer.invoke('provider:listAvailable'),
    /** Get current provider and model from the active agent */
    current: (): Promise<CurrentProviderInfo> => ipcRenderer.invoke('provider:current'),
    /** Switch the agent to a different provider and/or model */
    set: (providerId: string, modelId?: string): Promise<{ ok: boolean; provider?: string; model?: string; error?: string }> =>
      ipcRenderer.invoke('provider:set', providerId, modelId),
    /** Get metadata for a specific provider */
    getMeta: (providerId: string) => ipcRenderer.invoke('provider:getMeta', providerId),
    /** Check if a specific provider is available */
    isAvailable: (providerId: string): Promise<boolean> => ipcRenderer.invoke('provider:isAvailable', providerId),
    /** Save API key for a provider */
    saveApiKey: (providerId: string, apiKey: string): Promise<{ ok: boolean; error?: string }> =>
      ipcRenderer.invoke('provider:saveApiKey', providerId, apiKey),
    /** Import OAuth credentials from CLI tools (claude-code, codex) */
    importOAuth: (providerId: string): Promise<{ ok: boolean; expiresAt?: number; error?: string }> =>
      ipcRenderer.invoke('provider:importOAuth', providerId),
  },

  // Local chat (direct IPC, no Gateway required)
  localChat: {
    /** Subscribe to agent events for local direct chat */
    subscribe: (agentId: string) => ipcRenderer.invoke('localChat:subscribe', agentId),
    /** Unsubscribe from agent events */
    unsubscribe: (agentId: string) => ipcRenderer.invoke('localChat:unsubscribe', agentId),
    /** Get message history for local chat (returns raw AgentMessageItem[]) */
    getHistory: (agentId: string) => ipcRenderer.invoke('localChat:getHistory', agentId),
    /** Send message to agent via direct IPC (no Gateway) */
    send: (agentId: string, content: string) => ipcRenderer.invoke('localChat:send', agentId, content),
    /** Resolve an exec approval request */
    resolveExecApproval: (approvalId: string, decision: string) =>
      ipcRenderer.invoke('localChat:resolveExecApproval', approvalId, decision),
    /** Listen for agent events */
    onEvent: (callback: (event: LocalChatEvent) => void) => {
      ipcRenderer.on('localChat:event', (_event, data: LocalChatEvent) => callback(data))
    },
    /** Remove event listener */
    offEvent: () => {
      ipcRenderer.removeAllListeners('localChat:event')
    },
    /** Listen for exec approval requests */
    onApproval: (callback: (approval: LocalChatApproval) => void) => {
      ipcRenderer.on('localChat:approval', (_event, data: LocalChatApproval) => callback(data))
    },
    /** Remove approval listener */
    offApproval: () => {
      ipcRenderer.removeAllListeners('localChat:approval')
    },
  },
}

// Expose to renderer
contextBridge.exposeInMainWorld('electronAPI', electronAPI)

// Also expose ipcRenderer for backward compatibility
contextBridge.exposeInMainWorld('ipcRenderer', {
  on(...args: Parameters<typeof ipcRenderer.on>) {
    const [channel, listener] = args
    return ipcRenderer.on(channel, (event, ...args) => listener(event, ...args))
  },
  off(...args: Parameters<typeof ipcRenderer.off>) {
    const [channel, ...omit] = args
    return ipcRenderer.off(channel, ...omit)
  },
  send(...args: Parameters<typeof ipcRenderer.send>) {
    const [channel, ...omit] = args
    return ipcRenderer.send(channel, ...omit)
  },
  invoke(...args: Parameters<typeof ipcRenderer.invoke>) {
    const [channel, ...omit] = args
    return ipcRenderer.invoke(channel, ...omit)
  },
})

// Type declaration for window object
export type ElectronAPI = typeof electronAPI
