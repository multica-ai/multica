/**
 * Exec Approval Manager — Hub-side approval tracking
 *
 * Manages pending approval requests, sends them to connected clients,
 * and resolves them when clients respond via RPC.
 */

import { v7 as uuidv7 } from "uuid";
import type {
  ExecApprovalRequest,
  ApprovalDecision,
  ApprovalResult,
} from "../agent/tools/exec-approval-types.js";
import { DEFAULT_APPROVAL_TIMEOUT_MS } from "../agent/tools/exec-approval-types.js";

interface PendingEntry {
  resolve: (result: ApprovalResult) => void;
  timer: NodeJS.Timeout;
  request: ExecApprovalRequest;
}

/**
 * Callback type for sending approval requests to clients.
 * The Hub wires this to Gateway message sending.
 */
export type SendApprovalToClient = (
  agentId: string,
  payload: ExecApprovalRequest,
) => void;

export class ExecApprovalManager {
  private readonly pending = new Map<string, PendingEntry>();

  constructor(
    private readonly sendToClient: SendApprovalToClient,
    private readonly defaultTimeoutMs: number = DEFAULT_APPROVAL_TIMEOUT_MS,
  ) {}

  /**
   * Create an approval request and send it to the client.
   * Returns a Promise that resolves when the client responds or times out.
   */
  requestApproval(params: {
    agentId: string;
    command: string;
    cwd?: string;
    riskLevel: "safe" | "needs-review" | "dangerous";
    riskReasons: string[];
    timeoutMs?: number;
    askFallback?: "deny" | "allowlist" | "full";
    allowlistSatisfied?: boolean;
  }): Promise<ApprovalResult> {
    const approvalId = uuidv7();
    const timeoutMs = params.timeoutMs ?? this.defaultTimeoutMs;
    const expiresAtMs = Date.now() + timeoutMs;

    const request: ExecApprovalRequest = {
      approvalId,
      agentId: params.agentId,
      command: params.command,
      cwd: params.cwd,
      riskLevel: params.riskLevel,
      riskReasons: params.riskReasons,
      expiresAtMs,
    };

    return new Promise<ApprovalResult>((resolve) => {
      // Timeout: follow askFallback (default: fail-closed)
      const timer = setTimeout(() => {
        if (this.pending.has(approvalId)) {
          this.pending.delete(approvalId);
          const fallback = params.askFallback ?? "deny";
          const decision =
            fallback === "full" ||
            (fallback === "allowlist" && params.allowlistSatisfied)
              ? "allow-once"
              : "deny";
          resolve({ approved: decision !== "deny", decision });
        }
      }, timeoutMs);

      this.pending.set(approvalId, { resolve, timer, request });

      // Send to client via Gateway
      try {
        this.sendToClient(params.agentId, request);
      } catch (err) {
        // If sending fails, auto-deny (fail-closed)
        clearTimeout(timer);
        this.pending.delete(approvalId);
        console.error(`[ExecApprovalManager] Failed to send approval request: ${err}`);
        resolve({ approved: false, decision: "deny" });
      }
    });
  }

  /**
   * Resolve a pending approval with a client decision.
   * Returns true if the approval was found and resolved, false otherwise.
   */
  resolveApproval(approvalId: string, decision: ApprovalDecision): boolean {
    const entry = this.pending.get(approvalId);
    if (!entry) return false;

    clearTimeout(entry.timer);
    this.pending.delete(approvalId);

    entry.resolve({
      approved: decision !== "deny",
      decision,
    });

    return true;
  }

  /**
   * Cancel all pending approvals for an agent (e.g., on agent close).
   * All pending requests are resolved as denied.
   */
  cancelPending(agentId: string): void {
    for (const [id, entry] of this.pending) {
      if (entry.request.agentId === agentId) {
        clearTimeout(entry.timer);
        this.pending.delete(id);
        entry.resolve({ approved: false, decision: "deny" });
      }
    }
  }

  /**
   * Get a snapshot of a pending approval request (for debugging).
   */
  getSnapshot(approvalId: string): ExecApprovalRequest | null {
    const entry = this.pending.get(approvalId);
    return entry ? { ...entry.request } : null;
  }

  /**
   * Get count of pending approvals (for monitoring).
   */
  get pendingCount(): number {
    return this.pending.size;
  }
}
