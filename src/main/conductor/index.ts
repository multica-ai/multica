// Main facade
export { Conductor } from './Conductor'
export type { SessionUpdateCallback, ConductorEvents, ConductorOptions } from './Conductor'

// Modules (for direct use if needed)
export { AgentProcessManager } from './AgentProcessManager'
export { PromptHandler } from './PromptHandler'
export { G3Workaround } from './G3Workaround'
export { SessionLifecycle } from './SessionLifecycle'

// Utilities
export { AgentProcess } from './AgentProcess'
export { createAcpClient } from './AcpClientFactory'
export type { AcpClientCallbacks, AcpClientFactoryOptions } from './AcpClientFactory'

// Types
export type {
  SessionAgent,
  PendingAnswer,
  AgentStartResult,
  ISessionStore,
  IAgentProcessManager,
  IPromptHandler,
  IG3Workaround,
  ISessionLifecycle
} from './types'
