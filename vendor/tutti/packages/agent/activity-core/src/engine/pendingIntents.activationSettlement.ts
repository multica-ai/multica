import type { AgentActivitySessionInput } from "../sessionNormalization.ts";
import type { AgentActivityTurn } from "../types.ts";
import { attachPendingActivationSettings } from "./pendingIntents.activationSettings.ts";
import {
  activationCanAcceptCommandResult,
  validateActivationCommandResult
} from "./pendingIntents.activationResult.ts";
import { replaceActivation } from "./pendingIntents.activationRecords.ts";
import {
  markSessionActive,
  unchanged
} from "./pendingIntents.inactiveSessions.ts";
import type {
  PendingActivationIntentRecord,
  PendingIntentsState
} from "./pendingIntents.types.ts";
import { confirmFromSessions } from "./pendingSubmit.reducer.ts";
import type {
  EngineCommand,
  EngineCommandResultIntent,
  EngineIntent,
  EngineReducerResult
} from "./types.ts";

const NO_COMMANDS: readonly EngineCommand[] = [];

export function createActivationRecoveryIntent(
  record: PendingActivationIntentRecord
): EngineIntent | null {
  if (record.mode !== "new") return null;
  return {
    agentSessionId: record.agentSessionId,
    needsMessages: false,
    needsState: true,
    type: "session/reconcileRequested",
    workspaceId: record.workspaceId
  };
}

export function settleActivationCommand(
  state: PendingIntentsState,
  intent: EngineCommandResultIntent
): EngineReducerResult<PendingIntentsState> {
  const requestId = intent.correlationId?.trim() ?? "";
  const record = state.activationsByRequestId[requestId];
  if (!record || !activationCanAcceptCommandResult(record.status)) {
    return unchanged(state);
  }
  const commandSettledAtUnixMs = finiteTimestamp(intent.settledAtUnixMs);
  if (intent.outcome === "succeeded") {
    const settlement = validateActivationCommandResult(
      intent.value,
      record,
      intent.resultContract,
      commandSettledAtUnixMs ?? undefined
    );
    const commandOutcome =
      settlement.kind === "acknowledged"
        ? "succeeded"
        : settlement.kind === "failed"
          ? "failed"
          : "invalid_result";
    const settledRecord: PendingActivationIntentRecord = {
      ...record,
      commandOutcome,
      commandSettledAtUnixMs,
      errorCode: settlement.errorCode,
      errorMessage: settlement.errorMessage,
      lastObservedStage:
        record.status === "confirmed" ? "confirmed" : "command_settled"
    };
    if (record.status === "confirmed") {
      return {
        commands: NO_COMMANDS,
        ...(settlement.kind === "acknowledged" &&
        settlement.projectionIntent !== null
          ? { followUpIntents: [settlement.projectionIntent] }
          : {}),
        state: replaceActivation(state, {
          ...record,
          commandOutcome,
          commandSettledAtUnixMs,
          lastObservedStage: "stale_command_result"
        })
      };
    }
    if (settlement.kind === "invalid") {
      const recoveryIntent = createActivationRecoveryIntent(record);
      return {
        commands: NO_COMMANDS,
        ...(recoveryIntent ? { followUpIntents: [recoveryIntent] } : {}),
        state: replaceActivation(state, {
          ...settledRecord,
          status: "uncertain"
        })
      };
    }
    const failed = settlement.kind === "failed";
    return {
      commands: NO_COMMANDS,
      ...(settlement.projectionIntent
        ? { followUpIntents: [settlement.projectionIntent] }
        : {}),
      state: replaceActivation(
        markSessionActive(state, record.agentSessionId),
        {
          ...settledRecord,
          status: failed ? "failed" : record.status
        }
      )
    };
  }
  if (record.status === "confirmed") {
    return {
      commands: NO_COMMANDS,
      state: replaceActivation(state, {
        ...record,
        commandOutcome: intent.outcome === "timedOut" ? "timed_out" : "failed",
        commandSettledAtUnixMs,
        lastObservedStage: "stale_command_result"
      })
    };
  }
  const recoveryIntent =
    intent.outcome === "timedOut"
      ? createActivationRecoveryIntent(record)
      : null;
  return {
    commands: NO_COMMANDS,
    ...(recoveryIntent ? { followUpIntents: [recoveryIntent] } : {}),
    state: replaceActivation(state, {
      ...record,
      commandOutcome: intent.outcome === "timedOut" ? "timed_out" : "failed",
      commandSettledAtUnixMs,
      errorCode: intent.errorCode ?? null,
      errorMessage:
        intent.outcome === "timedOut"
          ? null
          : intent.errorMessage?.trim() || null,
      lastObservedStage: "command_settled",
      status: intent.outcome === "timedOut" ? "uncertain" : "failed"
    })
  };
}

export function receiveSessionSnapshot(
  state: PendingIntentsState,
  sessions: readonly AgentActivitySessionInput[],
  turnsById: Readonly<Record<string, AgentActivityTurn>>,
  observedAtUnixMs?: number,
  workspaceMismatchSessionIds?: readonly string[]
): EngineReducerResult<PendingIntentsState> {
  const activationResult = confirmActivationsFromSessions(
    state,
    sessions,
    true,
    observedAtUnixMs,
    workspaceMismatchSessionIds
  );
  const submitResult = confirmFromSessions(activationResult.state, turnsById);
  const followUpIntents = [
    ...(activationResult.followUpIntents ?? []),
    ...(submitResult.followUpIntents ?? [])
  ];
  return {
    commands: [...activationResult.commands, ...submitResult.commands],
    ...(followUpIntents.length ? { followUpIntents } : {}),
    state: submitResult.state
  };
}

export function confirmActivationsFromSessions(
  state: PendingIntentsState,
  sessions: readonly AgentActivitySessionInput[],
  completeSnapshot: boolean,
  observedAtUnixMs?: number,
  workspaceMismatchSessionIds?: readonly string[]
): EngineReducerResult<PendingIntentsState> {
  const sessionsById = new Map(
    sessions.map((session) => [session.agentSessionId, session])
  );
  const commands: EngineCommand[] = [];
  const workspaceMismatchSessionIdSet = new Set(
    workspaceMismatchSessionIds?.map((id) => id.trim()).filter(Boolean) ?? []
  );
  let next = state;
  for (const record of Object.values(state.activationsByRequestId)) {
    if (record.status !== "requested" && record.status !== "uncertain")
      continue;
    const session = sessionsById.get(record.agentSessionId);
    if (!session) {
      if (completeSnapshot) {
        next = observeActivationSnapshot(
          next,
          record,
          workspaceMismatchSessionIdSet.has(record.agentSessionId)
            ? "workspace_mismatch"
            : "session_missing",
          observedAtUnixMs
        );
      }
      continue;
    }
    if (session.workspaceId.trim() !== record.workspaceId) {
      next = observeActivationSnapshot(
        next,
        record,
        "workspace_mismatch",
        observedAtUnixMs
      );
      continue;
    }
    if (
      record.mode === "new" &&
      !sessionConfirmsNewActivation(session, record.requestedAtUnixMs)
    ) {
      next = observeActivationSnapshot(
        next,
        record,
        "stale_for_new",
        observedAtUnixMs
      );
      continue;
    }
    const settingsUpdate = attachPendingActivationSettings(record);
    next = replaceActivation(markSessionActive(next, record.agentSessionId), {
      ...settingsUpdate.record,
      errorMessage: null,
      lastObservedStage: "confirmed",
      snapshotObservedAtUnixMs: finiteTimestamp(observedAtUnixMs),
      snapshotOutcome: "matched",
      status: "confirmed"
    });
    commands.push(...settingsUpdate.commands);
  }
  return next === state && commands.length === 0
    ? unchanged(state)
    : { commands, state: next };
}

function sessionConfirmsNewActivation(
  session: AgentActivitySessionInput,
  requestedAtUnixMs: number
): boolean {
  if (
    session.createdAtUnixMs !== undefined &&
    session.createdAtUnixMs >= requestedAtUnixMs
  ) {
    return true;
  }
  return (
    session.updatedAtUnixMs !== undefined &&
    session.updatedAtUnixMs >= requestedAtUnixMs
  );
}

function finiteTimestamp(value: number | undefined): number | null {
  return typeof value === "number" && Number.isFinite(value) ? value : null;
}

function observeActivationSnapshot(
  state: PendingIntentsState,
  record: PendingActivationIntentRecord,
  snapshotOutcome: Exclude<
    PendingActivationIntentRecord["snapshotOutcome"],
    "matched" | "not_observed"
  >,
  observedAtUnixMs?: number
): PendingIntentsState {
  if (
    record.lastObservedStage === "snapshot_observed" &&
    record.snapshotOutcome === snapshotOutcome
  ) {
    return state;
  }
  return replaceActivation(state, {
    ...record,
    lastObservedStage: "snapshot_observed",
    snapshotObservedAtUnixMs:
      record.snapshotOutcome === snapshotOutcome
        ? (record.snapshotObservedAtUnixMs ?? finiteTimestamp(observedAtUnixMs))
        : finiteTimestamp(observedAtUnixMs),
    snapshotOutcome
  });
}
