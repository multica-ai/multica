// TECH-3642 — per-agent capabilities card: client + detail-tab factory.
export {
  getAgentCapabilities,
  AgentCapabilitiesSchema,
  type AgentCapabilities,
  type AgentCapabilityExecOption,
  type AgentCapabilityRuntimeOptions,
  type AgentCapabilitySystemPromptSupport,
} from "./api";
export {
  getAgentCapabilitySwap,
  AgentCapabilitySwapSchema,
  type AgentCapabilitySwap,
  type AgentCapabilitySwapImpact,
  type AgentCapabilitySwapFieldTransition,
  type AgentCapabilitySwapSystemPrompt,
} from "./swap-api";
export {
  createAgentCapabilitiesTabs,
  CerebroCapabilitiesTab,
  type AgentCapabilitiesTabExtension,
} from "./views";
