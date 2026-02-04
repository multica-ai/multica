/**
 * System Prompt Engineering — Type Definitions
 *
 * Provides structured, mode-aware system prompt assembly
 * inspired by OpenClaw's conditional prompt system.
 */

import type { ProfileConfig } from "../profile/types.js";

/**
 * Controls which sections are included in the system prompt.
 * - "full": All sections (default, for main agents)
 * - "minimal": Reduced sections (safety, tooling, runtime, subagent context) — for subagents
 * - "none": Identity line + safety + subagent task only — for bare subagents
 */
export type SystemPromptMode = "full" | "minimal" | "none";

/** Runtime environment information */
export interface RuntimeInfo {
  /** Agent display name */
  agentName?: string | undefined;
  /** Machine hostname */
  hostName?: string | undefined;
  /** LLM provider (e.g. "anthropic") */
  provider?: string | undefined;
  /** Model ID (e.g. "claude-3.5-sonnet") */
  model?: string | undefined;
  /** OS platform (e.g. "darwin") */
  os?: string | undefined;
  /** CPU architecture (e.g. "arm64") */
  arch?: string | undefined;
  /** Node.js version (e.g. "v22.0.0") */
  nodeVersion?: string | undefined;
  /** Current working directory */
  cwd?: string | undefined;
}

/** Subagent context for minimal/none modes */
export interface SubagentContext {
  /** Parent session that spawned this subagent */
  requesterSessionId: string;
  /** This subagent's session ID */
  childSessionId: string;
  /** Optional human-readable label */
  label?: string | undefined;
  /** The task this subagent must complete */
  task: string;
}

/** Profile content subset used by the prompt builder */
export interface ProfileContent {
  soul?: string | undefined;
  user?: string | undefined;
  workspace?: string | undefined;
  memory?: string | undefined;
  config?: ProfileConfig | undefined;
}

/** Input options for buildSystemPrompt() */
export interface SystemPromptOptions {
  /** Prompt mode — full for main agents, minimal for subagents, none for bare */
  mode: SystemPromptMode;
  /** Agent profile content */
  profile?: ProfileContent | undefined;
  /** Profile directory path (so the agent knows where files live) */
  profileDir?: string | undefined;
  /** Active tool names (after policy filtering) */
  tools?: string[] | undefined;
  /** Skills prompt (pre-built by SkillManager) */
  skillsPrompt?: string | undefined;
  /** Runtime context */
  runtime?: RuntimeInfo | undefined;
  /** Subagent context (for minimal/none modes) */
  subagent?: SubagentContext | undefined;
  /** Extra system prompt to append */
  extraSystemPrompt?: string | undefined;
  /** Whether to include the safety constitution (default: true) */
  includeSafety?: boolean | undefined;
}

/** A named section of the system prompt */
export interface PromptSection {
  /** Section identifier */
  name: string;
  /** Section content (joined lines) */
  content: string;
}

/** Report entry for a single section */
export interface SectionReport {
  name: string;
  chars: number;
  lines: number;
  included: boolean;
}

/** Telemetry report about a generated system prompt */
export interface SystemPromptReport {
  mode: SystemPromptMode;
  totalChars: number;
  totalLines: number;
  sections: SectionReport[];
  toolCount: number;
  skillsIncluded: boolean;
  safetyIncluded: boolean;
}
