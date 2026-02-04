/**
 * System Prompt Engineering — Public API
 */

export { buildSystemPrompt, buildSystemPromptWithReport } from "./builder.js";
export { collectRuntimeInfo, formatRuntimeLine } from "./runtime-info.js";
export { formatPromptReport } from "./report.js";
export { SAFETY_CONSTITUTION } from "./constitution.js";

export type {
  ProfileContent,
  PromptSection,
  RuntimeInfo,
  SectionReport,
  SubagentContext,
  SystemPromptMode,
  SystemPromptOptions,
  SystemPromptReport,
} from "./types.js";
