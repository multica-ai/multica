export { agentCapabilitiesSettingsTab } from "./views";
// TECH-3642 — per-agent capabilities card: client + detail-tab factory.
export {
  getAgentCapabilities,
  AgentCapabilitiesSchema,
  type AgentCapabilities,
} from "./api";
export {
  createAgentCapabilitiesTabs,
  CerebroCapabilitiesTab,
  type AgentCapabilitiesTabExtension,
} from "./views";
