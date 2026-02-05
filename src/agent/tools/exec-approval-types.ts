/**
 * Exec Approval System — Type Definitions
 *
 * Human-in-the-loop command execution approval for the exec tool.
 * Inspired by OpenClaw's defense-in-depth design.
 */

// ============ Security Policy ============

/** Security level for exec commands */
export type ExecSecurity = "deny" | "allowlist" | "full";

/** Ask mode — when to request human approval */
export type ExecAsk = "off" | "on-miss" | "always";

/** User decision for an approval request */
export type ApprovalDecision = "allow-once" | "allow-always" | "deny";

// ============ Approval Request/Response ============

/** Approval request sent to client (via WebSocket) or shown in CLI */
export interface ExecApprovalRequest {
  /** Unique approval ID (UUIDv7) */
  approvalId: string;
  /** Agent that initiated the command */
  agentId: string;
  /** Shell command to execute */
  command: string;
  /** Working directory */
  cwd?: string;
  /** Evaluated risk level */
  riskLevel: "safe" | "needs-review" | "dangerous";
  /** Reasons for the risk assessment */
  riskReasons: string[];
  /** When this approval expires (ms since epoch) */
  expiresAtMs: number;
}

/** Result returned after approval decision */
export interface ApprovalResult {
  approved: boolean;
  decision: ApprovalDecision;
}

// ============ Configuration ============

/** Exec approval configuration (stored in profile config) */
export interface ExecApprovalConfig {
  /** Security level: "deny" blocks all, "allowlist" requires matching, "full" allows all */
  security?: ExecSecurity;
  /** Ask mode: "off" never asks, "on-miss" asks when allowlist misses, "always" always asks */
  ask?: ExecAsk;
  /** Timeout before auto-deny in milliseconds (default: 60_000) */
  timeoutMs?: number;
  /** Fallback security level on timeout (default: "deny" — fail-closed) */
  askFallback?: ExecSecurity;
  /** Persistent allowlist of approved command patterns */
  allowlist?: ExecAllowlistEntry[];
}

/** Default timeout for approval requests (60 seconds) */
export const DEFAULT_APPROVAL_TIMEOUT_MS = 60_000;

// ============ Allowlist ============

/** A single allowlist entry */
export interface ExecAllowlistEntry {
  /** Unique entry ID (auto-generated UUID) */
  id?: string;
  /** Glob pattern to match against command binary or full command */
  pattern: string;
  /** Last time this entry was used (ms since epoch) */
  lastUsedAt?: number;
  /** Last command that matched this entry */
  lastUsedCommand?: string;
}

// ============ Callback ============

/**
 * Callback injected into the exec tool for approval flow.
 * Abstracts the communication channel (Hub WebSocket vs CLI readline).
 * Returns a promise that resolves when the user makes a decision.
 */
export type ExecApprovalCallback = (
  command: string,
  cwd: string | undefined,
) => Promise<ApprovalResult>;

// ============ Safety Evaluation ============

/** Result of command safety evaluation */
export interface SafetyEvaluation {
  /** Overall risk level */
  riskLevel: "safe" | "needs-review" | "dangerous";
  /** Reasons explaining the risk assessment */
  reasons: string[];
  /** Whether shell syntax analysis passed */
  analysisOk: boolean;
  /** Whether the command matched the allowlist */
  allowlistSatisfied: boolean;
}
