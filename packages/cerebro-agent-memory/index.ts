// FIR-1794 — per-agent memory read/write toggle: client + detail-tab factory.
export {
  getAgentMemorySettings,
  setAgentMemorySettings,
  type AgentMemorySettings,
} from "./api";
export {
  createAgentMemoryTabs,
  CerebroAgentMemoryTab,
  type AgentMemoryTabExtension,
} from "./views";
