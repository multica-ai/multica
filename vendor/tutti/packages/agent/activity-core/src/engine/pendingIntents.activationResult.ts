import type { AgentActivitySessionInput } from "../sessionNormalization.ts";
import type { AgentActivityTurn } from "../types.ts";
import type {
  PendingActivationIntentRecord,
  PendingActivationStatus
} from "./pendingIntents.types.ts";
import {
  decodeSessionProjection,
  decodeTurnProjection
} from "./sessionProjection.validation.ts";
import type { EngineCommandResultContract, EngineIntent } from "./types.ts";

export type ActivationCommandSettlement =
  | {
      errorCode: string | null;
      errorMessage: string | null;
      kind: "acknowledged";
      projectionIntent: EngineIntent | null;
    }
  | {
      errorCode: string | null;
      errorMessage: string | null;
      kind: "failed";
      projectionIntent: null;
    }
  | {
      errorCode: "invalid_command_result";
      errorMessage: null;
      kind: "invalid";
      projectionIntent: null;
    };

/**
 * Accepts legacy activation acknowledgements without a projection, while
 * validating and extracting the richer result required by typed hosts.
 */
export function validateActivationCommandResult(
  value: unknown,
  record: PendingActivationIntentRecord,
  resultContract: EngineCommandResultContract | undefined,
  observedAtUnixMs?: number
): ActivationCommandSettlement {
  if (!value || typeof value !== "object") return invalid();
  const result = value as {
    activation?: { mode?: unknown; status?: unknown };
    detail?: unknown;
    error?: { code?: unknown; message?: unknown } | null;
    session?: unknown;
  };
  const status = result.activation?.status;
  if (typeof status !== "string") return invalid();
  const authoritativeResult = resultContract === "activation-v1";
  if (authoritativeResult && result.activation?.mode !== record.mode) {
    return invalid();
  }
  if (status === "failed") {
    return {
      errorCode: normalizedString(result.error?.code),
      errorMessage: normalizedString(result.error?.message),
      kind: "failed",
      projectionIntent: null
    };
  }
  if (!authoritativeResult) {
    return {
      errorCode: null,
      errorMessage: null,
      kind: "acknowledged",
      projectionIntent: null
    };
  }
  const projectionIntent =
    record.mode === "new"
      ? newSessionProjection(result, record, observedAtUnixMs)
      : existingSessionProjection(result, record, observedAtUnixMs);
  if (projectionIntent === false) return invalid();
  return {
    errorCode: null,
    errorMessage: null,
    kind: "acknowledged",
    projectionIntent
  };
}

function newSessionProjection(
  result: {
    activation?: { mode?: unknown; status?: unknown };
    session?: unknown;
  },
  record: PendingActivationIntentRecord,
  observedAtUnixMs?: number
): EngineIntent | null | false {
  if (result.activation?.status !== "attached") return false;
  const session = decodeSessionProjection(result.session, record);
  if (!session) return false;
  return {
    session,
    ...(observedAtUnixMs === undefined ? {} : { observedAtUnixMs }),
    type: "session/upserted"
  };
}

function existingSessionProjection(
  result: {
    activation?: { mode?: unknown; status?: unknown };
    detail?: unknown;
    session?: unknown;
  },
  record: PendingActivationIntentRecord,
  observedAtUnixMs?: number
): EngineIntent | null | false {
  if (result.activation?.status !== "already_attached") return false;
  if (!decodeSessionProjection(result.session, record)) return false;
  if (!result.detail || typeof result.detail !== "object") return false;
  const detail = result.detail as {
    childSessions?: unknown;
    lifecycleCapabilitiesProjected?: unknown;
    projection?: unknown;
    session?: unknown;
    turns?: unknown;
  };
  const session = decodeSessionProjection(detail.session, record);
  const entities = session
    ? scopedDetailEntities(detail.childSessions, detail.turns, record)
    : null;
  if (
    detail.projection !== "authoritative" ||
    detail.lifecycleCapabilitiesProjected !== true ||
    session === null ||
    entities === null
  ) {
    return false;
  }
  return {
    childSessions: entities.childSessions,
    session,
    turns: entities.turns,
    type: "session/detailSnapshotReceived",
    ...(observedAtUnixMs === undefined ? {} : { observedAtUnixMs }),
    workspaceId: record.workspaceId
  };
}

function scopedDetailEntities(
  childSessions: unknown,
  turns: unknown,
  record: Pick<PendingActivationIntentRecord, "agentSessionId" | "workspaceId">
): {
  childSessions: readonly AgentActivitySessionInput[];
  turns: readonly AgentActivityTurn[];
} | null {
  if (!Array.isArray(childSessions) || !Array.isArray(turns)) return null;
  const sessionIds = new Set([record.agentSessionId]);
  const decodedChildSessions: AgentActivitySessionInput[] = [];
  for (const childSession of childSessions) {
    const decoded = decodeSessionProjection(childSession, {
      workspaceId: record.workspaceId
    });
    const childSessionId = decoded?.agentSessionId.trim() ?? "";
    if (
      !decoded ||
      childSessionId === record.agentSessionId ||
      sessionIds.has(childSessionId)
    ) {
      return null;
    }
    sessionIds.add(childSessionId);
    decodedChildSessions.push(decoded);
  }
  const decodedTurns: AgentActivityTurn[] = [];
  for (const turn of turns) {
    const decoded = decodeTurnProjection(turn, sessionIds);
    if (!decoded) return null;
    decodedTurns.push(decoded);
  }
  return {
    childSessions: decodedChildSessions,
    turns: decodedTurns
  };
}

function normalizedString(value: unknown): string | null {
  return typeof value === "string" ? value.trim() || null : null;
}

function invalid(): ActivationCommandSettlement {
  return {
    errorCode: "invalid_command_result",
    errorMessage: null,
    kind: "invalid",
    projectionIntent: null
  };
}

export function activationCanAcceptCommandResult(
  status: PendingActivationStatus
): boolean {
  return (
    status === "requested" || status === "uncertain" || status === "confirmed"
  );
}
