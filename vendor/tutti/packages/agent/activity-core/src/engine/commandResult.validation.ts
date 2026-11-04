import type { AgentActivitySendInputResult } from "../types.ts";
import type { AgentActivitySession } from "../types.ts";
import type { AgentActivityTurn } from "../types.ts";
import type { PendingSubmitIntentRecord } from "./pendingIntents.types.ts";

export type SendInputResultValidation =
  | { kind: "valid"; result: AgentActivitySendInputResult }
  | { kind: "invalid"; reason: string };

export type ScopedSessionResultValidation =
  | { kind: "valid"; session: AgentActivitySession }
  | { kind: "invalid"; reason: string };

export function validateScopedSessionResult(
  value: unknown,
  expected: { agentSessionId: string; workspaceId: string } | undefined,
  requireTopLevelSessionId = false
): ScopedSessionResultValidation {
  if (!expected || !value || typeof value !== "object") {
    return { kind: "invalid", reason: "scoped_session_result_missing" };
  }
  const result = value as {
    agentSessionId?: unknown;
    session?: Partial<AgentActivitySession>;
  };
  const session = result.session;
  if (
    !session ||
    typeof session.agentSessionId !== "string" ||
    typeof session.workspaceId !== "string" ||
    session.agentSessionId.trim() !== expected.agentSessionId ||
    session.workspaceId.trim() !== expected.workspaceId ||
    (requireTopLevelSessionId &&
      (typeof result.agentSessionId !== "string" ||
        result.agentSessionId.trim() !== expected.agentSessionId))
  ) {
    return { kind: "invalid", reason: "scoped_session_result_mismatch" };
  }
  return { kind: "valid", session: session as AgentActivitySession };
}

export function validateSendInputResult(
  value: unknown,
  record: PendingSubmitIntentRecord | undefined
): SendInputResultValidation {
  if (!record) {
    return { kind: "invalid", reason: "submit_request_missing" };
  }
  if (!isSendInputResult(value)) {
    return {
      kind: "invalid",
      reason: "send_result_entities_missing"
    };
  }
  const sessionId = value.session.agentSessionId.trim();
  const workspaceId = value.session.workspaceId.trim();
  if (sessionId !== record.agentSessionId) {
    return {
      kind: "invalid",
      reason: "send_result_session_scope_mismatch"
    };
  }
  if (workspaceId !== record.workspaceId) {
    return {
      kind: "invalid",
      reason: "send_result_workspace_scope_mismatch"
    };
  }
  const turnSessionId = value.turn.agentSessionId.trim();
  const turnId = value.turnId.trim();
  if (!turnId || value.turn.turnId.trim() !== turnId) {
    return { kind: "invalid", reason: "send_result_turn_scope_mismatch" };
  }
  if (turnSessionId !== record.agentSessionId) {
    return {
      kind: "invalid",
      reason: "send_result_session_scope_mismatch"
    };
  }
  return { kind: "valid", result: value };
}

function isSendInputResult(
  value: unknown
): value is AgentActivitySendInputResult {
  if (!value || typeof value !== "object") return false;
  const result = value as {
    kind?: unknown;
    session?: Partial<AgentActivitySession>;
    turn?: Partial<AgentActivityTurn>;
    turnId?: unknown;
  };
  return Boolean(
    result.session &&
    result.turn &&
    typeof result.turnId === "string" &&
    typeof result.session.agentSessionId === "string" &&
    typeof result.session.workspaceId === "string" &&
    typeof result.turn.agentSessionId === "string" &&
    typeof result.turn.turnId === "string"
  );
}
