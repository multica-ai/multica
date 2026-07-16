// FIR-3212 — production prompt snapshot: client + detail-tab factory.
export {
  listAgentPromptSnapshots,
  getAgentPromptSnapshot,
  type PromptSnapshot,
  type PromptSnapshotLayer,
  type PromptSnapshotRef,
} from "./api";
export {
  createAgentPromptSnapshotTabs,
  CerebroAgentPromptSnapshotTab,
  type AgentPromptSnapshotTabExtension,
} from "./views";
