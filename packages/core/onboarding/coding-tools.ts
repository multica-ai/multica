/**
 * Contract between onboarding and a host that can install AI coding tools on
 * this machine. Only Desktop provides one (`window.desktopAPI.codingTools`);
 * its main-process implementation mirrors these types in
 * `apps/desktop/src/shared/coding-tools.ts`, so a change here must land there
 * too.
 */

/** Tools onboarding can install. Ids match the runtime `provider` they register as. */
export type CodingToolId = "claude" | "codex" | "cursor";

export const CODING_TOOL_IDS: readonly CodingToolId[] = ["claude", "codex", "cursor"];

export interface CodingToolProbe {
  id: CodingToolId;
  installed: boolean;
  version: string | null;
  /** `null` when the tool's status command failed or could not be read. */
  loggedIn: boolean | null;
}

export type CodingToolRunKind = "install" | "login";

/** An install run goes download → install → login → verify; a login run is login → verify. */
export type CodingToolPhase = "download" | "install" | "login" | "verify";

export type CodingToolErrorCode =
  | "network"
  | "permission"
  | "unsupported_platform"
  | "busy"
  | "login_failed"
  | "verify_failed"
  | "cancelled"
  /** Installed, but the daemon never registered it. Raised by the UI, not the host. */
  | "not_detected"
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
  /** Login phase: the sign-in page the CLI asked for, so the UI can reopen it. */
  loginUrl?: string;
  /** Login phase: the CLI is waiting for a code pasted from the browser (Claude Code). */
  needsCode?: boolean;
  error?: CodingToolError;
  /** Tail of the run's combined output, for the "view log" affordance. */
  logTail?: string;
  /** Set on the last event of a run. */
  finished?: boolean;
}

export type CodingToolStartResult = { runId: string } | { error: CodingToolError };

export interface CodingToolsBridge {
  probe(): Promise<CodingToolProbe[]>;
  start(toolId: CodingToolId, kind: CodingToolRunKind): Promise<CodingToolStartResult>;
  submitCode(runId: string, code: string): Promise<boolean>;
  cancel(runId: string): Promise<void>;
  onEvent(callback: (event: CodingToolRunEvent) => void): () => void;
}
