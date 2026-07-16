// FIR-3212 — agent quality/satisfaction per config version: client + tab factory.
export { getAgentQuality, type AgentQualityVersion } from "./api";
export {
  createAgentQualityTabs,
  CerebroAgentQualityTab,
  type AgentQualityTabExtension,
} from "./views";
