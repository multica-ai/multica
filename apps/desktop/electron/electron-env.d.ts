/// <reference types="vite-plugin-electron/electron-env" />

declare namespace NodeJS {
  interface ProcessEnv {
    /**
     * The built directory structure
     *
     * ```tree
     * ├─┬─┬ dist
     * │ │ └── index.html
     * │ │
     * │ ├─┬ dist-electron
     * │ │ ├── main.js
     * │ │ └── preload.js
     * │
     * ```
     */
    APP_ROOT: string
    /** /dist/ or /public/ */
    VITE_PUBLIC: string
  }
}

// ============================================================================
// ElectronAPI type definitions
// ============================================================================

interface HubStatus {
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

interface AgentInfo {
  agentId: string
  status: string
}

interface ToolInfo {
  name: string
  group: string
  enabled: boolean
}

interface SkillInfo {
  id: string
  name: string
  description: string
  version: string
  enabled: boolean
  source: 'bundled' | 'global' | 'profile'
  triggers: string[]
}

interface DeviceMeta {
  userAgent?: string
  platform?: string
  language?: string
}

interface DeviceEntryInfo {
  deviceId: string
  agentId: string
  addedAt: number
  meta?: DeviceMeta
}

interface SkillAddResult {
  ok: boolean
  message: string
  path?: string
  skills?: string[]
}

interface ProfileData {
  profileId: string | undefined
  name: string | undefined
  style: string | undefined
  userContent: string | undefined
}

interface LocalChatEvent {
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

interface LocalChatApproval {
  approvalId: string
  agentId: string
  command: string
  cwd?: string
  riskLevel: 'safe' | 'needs-review' | 'dangerous'
  riskReasons: string[]
  expiresAtMs: number
}

interface ProviderStatus {
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

interface CurrentProviderInfo {
  provider: string
  model: string | undefined
  providerName: string | undefined
  available: boolean
}

interface ElectronAPI {
  hub: {
    init: () => Promise<unknown>
    getStatus: () => Promise<HubStatus>
    getAgentInfo: () => Promise<AgentInfo | null>
    info: () => Promise<unknown>
    reconnect: (url: string) => Promise<unknown>
    listAgents: () => Promise<unknown>
    createAgent: (id?: string) => Promise<unknown>
    getAgent: (id: string) => Promise<unknown>
    closeAgent: (id: string) => Promise<unknown>
    sendMessage: (agentId: string, content: string) => Promise<unknown>
    registerToken: (token: string, agentId: string, expiresAt: number) => Promise<unknown>
    onDeviceConfirmRequest: (callback: (deviceId: string, meta?: DeviceMeta) => void) => void
    offDeviceConfirmRequest: () => void
    deviceConfirmResponse: (deviceId: string, allowed: boolean) => void
    listDevices: () => Promise<DeviceEntryInfo[]>
    revokeDevice: (deviceId: string) => Promise<{ ok: boolean }>
    onConnectionStateChanged: (callback: (state: string) => void) => void
    offConnectionStateChanged: () => void
    onDevicesChanged: (callback: () => void) => void
    offDevicesChanged: () => void
  }
  tools: {
    list: () => Promise<ToolInfo[]>
    toggle: (name: string) => Promise<unknown>
    setStatus: (name: string, enabled: boolean) => Promise<unknown>
    active: () => Promise<unknown>
    reload: () => Promise<unknown>
  }
  skills: {
    list: () => Promise<SkillInfo[]>
    get: (id: string) => Promise<unknown>
    toggle: (id: string) => Promise<unknown>
    setStatus: (id: string, enabled: boolean) => Promise<unknown>
    reload: () => Promise<unknown>
    add: (source: string, options?: { name?: string; force?: boolean }) => Promise<SkillAddResult>
    remove: (name: string) => Promise<SkillAddResult>
  }
  agent: {
    status: () => Promise<unknown>
  }
  profile: {
    get: () => Promise<ProfileData>
    updateName: (name: string) => Promise<unknown>
    updateStyle: (style: string) => Promise<unknown>
    updateUser: (content: string) => Promise<unknown>
  }
  provider: {
    list: () => Promise<ProviderStatus[]>
    listAvailable: () => Promise<ProviderStatus[]>
    current: () => Promise<CurrentProviderInfo>
    set: (providerId: string, modelId?: string) => Promise<{ ok: boolean; provider?: string; model?: string; error?: string }>
    getMeta: (providerId: string) => Promise<unknown>
    isAvailable: (providerId: string) => Promise<boolean>
    saveApiKey: (providerId: string, apiKey: string) => Promise<{ ok: boolean; error?: string }>
    importOAuth: (providerId: string) => Promise<{ ok: boolean; expiresAt?: number; error?: string }>
  }
  localChat: {
    subscribe: (agentId: string) => Promise<{ ok?: boolean; error?: string; alreadySubscribed?: boolean }>
    unsubscribe: (agentId: string) => Promise<{ ok: boolean }>
    getHistory: (agentId: string) => Promise<{ messages: unknown[] }>
    send: (agentId: string, content: string) => Promise<{ ok?: boolean; error?: string }>
    resolveExecApproval: (approvalId: string, decision: string) => Promise<{ ok: boolean }>
    onEvent: (callback: (event: LocalChatEvent) => void) => void
    offEvent: () => void
    onApproval: (callback: (approval: LocalChatApproval) => void) => void
    offApproval: () => void
  }
}

// Used in Renderer process, expose in `preload.ts`
interface Window {
  ipcRenderer: import('electron').IpcRenderer
  electronAPI: ElectronAPI
}
