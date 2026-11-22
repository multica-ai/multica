/**
 * Installing AI coding tools from onboarding. The renderer reads these
 * through `desktopAPI.codingTools`; the views package declares the same
 * shapes in `@multica/core/onboarding` (coding-tools.ts), so keep both in
 * step.
 */

export const CODING_TOOLS_PROBE_CHANNEL = "coding-tools:probe";
export const CODING_TOOLS_START_CHANNEL = "coding-tools:start";
export const CODING_TOOLS_SUBMIT_CODE_CHANNEL = "coding-tools:submit-code";
export const CODING_TOOLS_CANCEL_CHANNEL = "coding-tools:cancel";
export const CODING_TOOLS_EVENT_CHANNEL = "coding-tools:event";

export type CodingToolId = "claude" | "codex" | "cursor";

export const CODING_TOOL_IDS: readonly CodingToolId[] = ["claude", "codex", "cursor"];

export function isCodingToolId(value: unknown): value is CodingToolId {
  return typeof value === "string" && (CODING_TOOL_IDS as readonly string[]).includes(value);
}

export interface CodingToolProbe {
  id: CodingToolId;
  installed: boolean;
  version: string | null;
  loggedIn: boolean | null;
}

export type CodingToolRunKind = "install" | "login";
export type CodingToolPhase = "download" | "install" | "login" | "verify";

export type CodingToolErrorCode =
  | "network"
  | "permission"
  | "unsupported_platform"
  | "busy"
  | "login_failed"
  | "verify_failed"
  | "cancelled"
  | "unknown";

export interface CodingToolError {
  code: CodingToolErrorCode;
  message: string;
}

export interface CodingToolRunEvent {
  runId: string;
  toolId: CodingToolId;
  kind: CodingToolRunKind;
  phase: CodingToolPhase;
  status: "running" | "done" | "failed";
  /** 0–1 while a download reports its size. */
  progress?: number;
  loginUrl?: string;
  needsCode?: boolean;
  error?: CodingToolError;
  logTail?: string;
  finished?: boolean;
}

export type CodingToolStartResult = { runId: string } | { error: CodingToolError };
