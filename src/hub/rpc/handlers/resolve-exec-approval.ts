import type { RpcHandler } from "../dispatcher.js";
import { RpcError } from "../dispatcher.js";
import type { ExecApprovalManager } from "../../exec-approval-manager.js";
import type { ApprovalDecision } from "../../../agent/tools/exec-approval-types.js";

interface ResolveExecApprovalParams {
  approvalId: string;
  decision: ApprovalDecision;
}

const VALID_DECISIONS = new Set<ApprovalDecision>(["allow-once", "allow-always", "deny"]);

export function createResolveExecApprovalHandler(
  approvalManager: ExecApprovalManager,
): RpcHandler {
  return async (params: unknown) => {
    const { approvalId, decision } = (params ?? {}) as ResolveExecApprovalParams;

    if (!approvalId || typeof approvalId !== "string") {
      throw new RpcError("INVALID_PARAMS", "approvalId is required");
    }

    if (!decision || !VALID_DECISIONS.has(decision)) {
      throw new RpcError("INVALID_PARAMS", `Invalid decision: ${decision}. Must be allow-once, allow-always, or deny`);
    }

    const resolved = approvalManager.resolveApproval(approvalId, decision);
    if (!resolved) {
      throw new RpcError("NOT_FOUND", "Approval request not found or already resolved");
    }

    return { ok: true };
  };
}
