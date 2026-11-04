import type { AgentSessionEngineStateBase } from "./types.ts";
import type {
  PendingActivationIntentRecord,
  PendingSubmitIntentRecord
} from "./pendingIntents.types.ts";
import { promptQueuePromptIdForClientSubmit } from "./promptQueue.lookup.ts";
import { canonicalTurnKey } from "./sessionEntityKeys.ts";

const pendingActivationsCache = new WeakMap<
  Readonly<Record<string, PendingActivationIntentRecord>>,
  readonly PendingActivationIntentRecord[]
>();
const pendingSubmitsCache = new WeakMap<
  Readonly<Record<string, PendingSubmitIntentRecord>>,
  readonly PendingSubmitIntentRecord[]
>();

export function selectPendingActivations(
  state: AgentSessionEngineStateBase
): readonly PendingActivationIntentRecord[] {
  const records = state.pendingIntents.activationsByRequestId;
  const cached = pendingActivationsCache.get(records);
  if (cached) return cached;
  const selected = Object.values(records).sort(
    (left, right) =>
      left.requestedAtUnixMs - right.requestedAtUnixMs ||
      left.requestId.localeCompare(right.requestId)
  );
  pendingActivationsCache.set(records, selected);
  return selected;
}

export function selectPendingActivationByRequestId(
  state: AgentSessionEngineStateBase,
  requestId: string | null | undefined
): PendingActivationIntentRecord | null {
  const id = requestId?.trim() ?? "";
  return state.pendingIntents.activationsByRequestId[id] ?? null;
}

const EMPTY_PENDING_SUBMITS: readonly PendingSubmitIntentRecord[] = [];

export function selectPendingSubmits(
  state: AgentSessionEngineStateBase
): readonly PendingSubmitIntentRecord[] {
  const records = state.pendingIntents.submitsByClientSubmitId;
  const cached = pendingSubmitsCache.get(records);
  if (cached) return cached;
  const selected = Object.values(records).sort(
    (left, right) =>
      left.requestedAtUnixMs - right.requestedAtUnixMs ||
      left.clientSubmitId.localeCompare(right.clientSubmitId)
  );
  pendingSubmitsCache.set(records, selected);
  return selected;
}

export interface SessionActivationPresentation {
  errorCode: string | null;
  errorMessage: string | null;
  requestId: string | null;
  status: "inactive" | "activating" | "active" | "failed";
}

export function selectPendingSubmitsForSession(
  state: AgentSessionEngineStateBase,
  agentSessionId: string | null | undefined
): readonly PendingSubmitIntentRecord[] {
  const id = agentSessionId?.trim() ?? "";
  const matches = Object.values(
    state.pendingIntents.submitsByClientSubmitId
  ).filter((pending) => pending.agentSessionId === id);
  return matches.length > 0 ? matches : EMPTY_PENDING_SUBMITS;
}

export function pendingSubmitRecordListsEqual(
  left: readonly PendingSubmitIntentRecord[],
  right: readonly PendingSubmitIntentRecord[]
): boolean {
  return (
    left.length === right.length &&
    left.every((record, index) => record === right[index])
  );
}

export function selectSessionIsSubmitting(
  state: AgentSessionEngineStateBase,
  agentSessionId: string | null | undefined
): boolean {
  const id = agentSessionId?.trim() ?? "";
  if (!id) {
    return false;
  }
  const visibleQueuedSubmitIds = new Set(
    (state.promptQueue.recordsBySessionId[id]?.prompts ?? [])
      .filter((prompt) => prompt.visibleInQueue !== false)
      .map((prompt) => prompt.clientSubmitId)
      .filter((value): value is string => Boolean(value))
  );
  return selectPendingSubmitsForSession(state, id).some(
    (pending) =>
      (pending.status === "requested" || pending.status === "uncertain") &&
      !visibleQueuedSubmitIds.has(pending.clientSubmitId)
  );
}

export function selectSessionHasUnconfirmedSubmit(
  state: AgentSessionEngineStateBase,
  agentSessionId: string | null | undefined
): boolean {
  const id = agentSessionId?.trim() ?? "";
  const session = id ? state.sessionLifecycle.sessionsById[id] : undefined;
  return selectPendingSubmitsForSession(state, agentSessionId).some(
    (pending) => {
      if (pending.status !== "accepted") return false;
      const turnId = pending.turnId?.trim() ?? "";
      if (!turnId) return true;
      const turn =
        state.sessionLifecycle.turnsById[
          canonicalTurnKey(pending.agentSessionId, turnId)
        ];
      if (turn?.phase === "settled") return false;
      return session?.activeTurnId === turnId;
    }
  );
}

export function selectLatestPendingSubmitForSession(
  state: AgentSessionEngineStateBase,
  agentSessionId: string | null | undefined
): PendingSubmitIntentRecord | null {
  let latest: PendingSubmitIntentRecord | null = null;
  for (const pending of selectPendingSubmitsForSession(state, agentSessionId)) {
    if (!latest || pending.requestedAtUnixMs >= latest.requestedAtUnixMs) {
      latest = pending;
    }
  }
  return latest;
}

/**
 * Select the implicit stop target without changing the broader pending-submit
 * presentation semantics used by GUI consumers.
 *
 * A failed submit is proven not to produce a Turn. A submit with a known
 * settled canonical Turn is likewise complete. Missing canonical state is not
 * proof of completion because admission and activity events can arrive out of
 * order.
 */
export function selectLatestStopTargetSubmitForSession(
  state: AgentSessionEngineStateBase,
  agentSessionId: string | null | undefined
): PendingSubmitIntentRecord | null {
  let latest: PendingSubmitIntentRecord | null = null;
  for (const pending of selectPendingSubmitsForSession(state, agentSessionId)) {
    if (!stopTargetMayStillProduceUnsettledTurn(state, pending)) continue;
    if (
      !latest ||
      pending.requestedAtUnixMs > latest.requestedAtUnixMs ||
      (pending.requestedAtUnixMs === latest.requestedAtUnixMs &&
        pending.clientSubmitId.localeCompare(latest.clientSubmitId) > 0)
    ) {
      latest = pending;
    }
  }
  return latest;
}

/**
 * Whether an implicit Session stop can target a pending prompt admission.
 * Consumers use this presentation-safe fact instead of duplicating submit and
 * canonical Turn correlation rules.
 */
export function selectSessionHasPendingSubmitStopTarget(
  state: AgentSessionEngineStateBase,
  agentSessionId: string | null | undefined
): boolean {
  return selectLatestStopTargetSubmitForSession(state, agentSessionId) !== null;
}

function stopTargetMayStillProduceUnsettledTurn(
  state: AgentSessionEngineStateBase,
  pending: PendingSubmitIntentRecord
): boolean {
  if (pending.status === "failed") return false;
  const turnId = pending.turnId?.trim() ?? "";
  if (!turnId) {
    if (pending.submitDiagnostics?.queued !== true) return true;
    const queue = state.promptQueue.recordsBySessionId[pending.agentSessionId];
    const promptId = promptQueuePromptIdForClientSubmit(
      state.promptQueue,
      pending.agentSessionId,
      pending.clientSubmitId
    );
    return Boolean(
      promptId &&
      (queue?.inFlight?.promptId === promptId ||
        queue?.uncertainDelivery?.promptId === promptId)
    );
  }
  const turn =
    state.sessionLifecycle.turnsById[
      canonicalTurnKey(pending.agentSessionId, turnId)
    ];
  return turn?.phase !== "settled";
}

export function selectLatestActivationForSession(
  state: AgentSessionEngineStateBase,
  agentSessionId: string | null | undefined
): PendingActivationIntentRecord | null {
  const id = agentSessionId?.trim() ?? "";
  let latest: PendingActivationIntentRecord | null = null;
  for (const activation of Object.values(
    state.pendingIntents.activationsByRequestId
  )) {
    if (
      activation.agentSessionId === id &&
      (!latest || activation.requestedAtUnixMs >= latest.requestedAtUnixMs)
    ) {
      latest = activation;
    }
  }
  return latest;
}

export function selectSessionActivationPresentations(
  state: AgentSessionEngineStateBase
): Readonly<Record<string, SessionActivationPresentation>> {
  const latestBySessionId = new Map<string, PendingActivationIntentRecord>();
  for (const activation of Object.values(
    state.pendingIntents.activationsByRequestId
  )) {
    const current = latestBySessionId.get(activation.agentSessionId);
    if (!current || activation.requestedAtUnixMs >= current.requestedAtUnixMs) {
      latestBySessionId.set(activation.agentSessionId, activation);
    }
  }
  const result: Record<string, SessionActivationPresentation> = {};
  for (const [agentSessionId, activation] of latestBySessionId) {
    result[agentSessionId] = {
      errorCode: activation.errorCode,
      errorMessage: activation.errorMessage,
      requestId: activation.requestId,
      status:
        activation.settingsUpdateStatus === "failed"
          ? "failed"
          : activation.status === "confirmed"
            ? "active"
            : activation.status === "failed"
              ? "failed"
              : activation.status === "canceled"
                ? "inactive"
                : "activating"
    };
  }
  for (const agentSessionId of Object.keys(
    state.pendingIntents.inactiveSessionIds
  )) {
    result[agentSessionId] = {
      errorCode: null,
      errorMessage: null,
      requestId: null,
      status: "inactive"
    };
  }
  return result;
}

export function sessionActivationPresentationMapsEqual(
  left: Readonly<Record<string, SessionActivationPresentation>>,
  right: Readonly<Record<string, SessionActivationPresentation>>
): boolean {
  const leftKeys = Object.keys(left);
  const rightKeys = Object.keys(right);
  return (
    leftKeys.length === rightKeys.length &&
    leftKeys.every((key) => {
      const a = left[key];
      const b = right[key];
      return (
        b !== undefined &&
        a?.errorCode === b.errorCode &&
        a.errorMessage === b.errorMessage &&
        a.requestId === b.requestId &&
        a.status === b.status
      );
    })
  );
}
