// TECH-3642 — per-agent capabilities detail tab (skills/tools/credentials/limits).
// FIR-3091 slice 3: the workspace "Agent capabilities" settings tab was retired;
// its living cards now live in the unified Permissions screen
// (@multica/cerebro-tool-policy → CapabilityIsolationSections). Only the
// per-agent detail tab remains here.
export {
  createAgentCapabilitiesTabs,
  CerebroCapabilitiesTab,
  type AgentCapabilitiesTabExtension,
} from "./capabilities-tab";
