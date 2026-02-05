/**
 * Tools module - provides tool creation and policy-based filtering.
 */

// Tool implementations
export { createExecTool } from "./exec.js";
export { createProcessTool } from "./process.js";
export { createGlobTool } from "./glob.js";
export { createWebFetchTool, createWebSearchTool } from "./web/index.js";

// Tool groups
export {
  TOOL_NAME_ALIASES,
  TOOL_GROUPS,
  DEFAULT_SUBAGENT_TOOL_DENY,
  normalizeToolName,
  normalizeToolList,
  expandToolGroups,
} from "./groups.js";

// Tool policy system
export {
  type ToolPolicy,
  type ToolsConfig,
  type FilterToolsOptions,
  isToolAllowed,
  filterToolsByPolicy,
  filterTools,
  getSubagentPolicy,
  wouldToolBeAllowed,
} from "./policy.js";

// Exec approval system
export type {
  ExecSecurity,
  ExecAsk,
  ApprovalDecision,
  ExecApprovalRequest,
  ExecApprovalConfig,
  ExecAllowlistEntry,
  ExecApprovalCallback,
  ApprovalResult,
  SafetyEvaluation,
} from "./exec-approval-types.js";
export { DEFAULT_APPROVAL_TIMEOUT_MS } from "./exec-approval-types.js";
export { evaluateCommandSafety, requiresApproval, minSecurity, maxAsk, DEFAULT_SAFE_BINS } from "./exec-safety.js";
export { matchAllowlist, addAllowlistEntry, recordAllowlistUse, removeAllowlistEntry, normalizeAllowlist } from "./exec-allowlist.js";
export { createCliApprovalCallback } from "./exec-approval-cli.js";
