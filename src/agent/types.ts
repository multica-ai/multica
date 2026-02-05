import type { ThinkingLevel } from "@mariozechner/pi-agent-core";
import type { SkillsConfig } from "./skills/types.js";
import type { ToolsConfig } from "./tools/policy.js";
import type { ExecApprovalCallback, ExecApprovalConfig } from "./tools/exec-approval-types.js";

/** Controls how reasoning/thinking content blocks are handled */
export type ReasoningMode = "off" | "on" | "stream";

export type AgentRunResult = {
  text: string;
  /** Extracted thinking/reasoning content (when reasoningMode !== "off") */
  thinking?: string | undefined;
  error?: string | undefined;
};

export type AgentLogger = {
  stdout?: NodeJS.WritableStream | undefined;
  stderr?: NodeJS.WritableStream | undefined;
};

export type AgentOptions = {
  /** Agent Profile ID - loads predefined identity, personality, memory and other configurations */
  profileId?: string | undefined;
  /** Profile base directory, defaults to ~/.super-multica/agent-profiles */
  profileBaseDir?: string | undefined;
  provider?: string | undefined;
  model?: string | undefined;
  /** Custom API key (overrides environment variable) */
  apiKey?: string | undefined;
  /** Pin a specific auth profile ID (e.g. "anthropic:backup"). Disables rotation. */
  authProfileId?: string | undefined;
  /** Custom base URL for the provider endpoint */
  baseUrl?: string | undefined;
  /** System prompt, if profileId is set will auto-construct from profile */
  systemPrompt?: string | undefined;
  thinkingLevel?: ThinkingLevel | undefined;
  /** Controls how reasoning/thinking content is displayed: off, on, stream (default: stream) */
  reasoningMode?: ReasoningMode | undefined;
  /** Command execution directory */
  cwd?: string | undefined;
  sessionId?: string | undefined;
  logger?: AgentLogger | undefined;

  // === Context Window Guard Configuration ===
  /** Manually specify context window token count (overrides model value) */
  contextWindowTokens?: number | undefined;
  /** Tokens reserved for response generation, defaults to 1024 */
  reserveTokens?: number | undefined;
  /**
   * Compaction mode:
   * - "count": uses legacy message count
   * - "tokens": uses token awareness (default)
   * - "summary": uses LLM to generate summary
   */
  compactionMode?: "count" | "tokens" | "summary" | undefined;
  /** Compaction target utilization ratio (0-1), defaults to 0.5 */
  compactionTargetRatio?: number | undefined;
  /** Minimum messages to keep, defaults to 10 */
  minKeepMessages?: number | undefined;

  // === Summary Compaction Configuration ===
  /** Custom summary generation instructions */
  summaryInstructions?: string | undefined;

  /** Enable debug logging */
  debug?: boolean | undefined;

  // === Skills Configuration ===
  /** Enable skills system (default: true) */
  enableSkills?: boolean | undefined;
  /** Full skills configuration */
  skills?: SkillsConfig | undefined;

  // === Tools Configuration ===
  /** Tools policy configuration (profile, allow/deny, byProvider) */
  tools?: ToolsConfig | undefined;
  /** Whether this is a subagent (applies restricted tool set) */
  isSubagent?: boolean | undefined;

  // === Exec Approval Configuration ===
  /** Callback invoked when exec tool needs approval before running a command */
  onExecApprovalNeeded?: ExecApprovalCallback | undefined;
  /** Exec approval configuration (security level, ask mode, allowlist) */
  execApproval?: ExecApprovalConfig | undefined;
};

export interface Message {
  readonly id: string;
  readonly content: string;
}
