/**
 * Exec Approval Actions — WebSocket protocol types for exec approval flow
 */

/** Action name for exec approval requests (Hub → Client) */
export const ExecApprovalRequestAction = "exec-approval-request" as const;

/** Approval decision types */
export type ApprovalDecision = "allow-once" | "allow-always" | "deny";

/** Payload for exec approval request (Hub → Client) */
export interface ExecApprovalRequestPayload {
  /** Unique approval ID */
  approvalId: string;
  /** Agent that initiated the command */
  agentId: string;
  /** Shell command requiring approval */
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

/** Params for resolveExecApproval RPC (Client → Hub) */
export interface ResolveExecApprovalParams {
  /** The approval ID to resolve */
  approvalId: string;
  /** User decision */
  decision: ApprovalDecision;
}

/** Result of resolveExecApproval RPC */
export interface ResolveExecApprovalResult {
  ok: boolean;
}
