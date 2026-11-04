import type { AgentActivityMessage } from "../types.ts";
import type {
  EngineCommand,
  EngineIntent,
  EngineReducerResult
} from "./types.ts";
import type {
  PromptQueueIntent,
  PromptQueuePendingSendNow,
  PromptQueueRecord,
  PromptQueueState
} from "./promptQueue.types.ts";
import type {
  SendInputResultValidation,
  ScopedSessionResultValidation
} from "./commandResult.validation.ts";
import { promptQueuePromptIdForClientSubmit } from "./promptQueue.lookup.ts";
import {
  clonePromptCapabilityReferences,
  clonePromptRequiredSettingsPatch,
  normalizeQueuedPrompt
} from "./promptQueue.prompt.ts";
import {
  compactQueueRecord,
  emptyQueueRecord,
  queueSendCommandId
} from "./promptQueue.record.ts";
import {
  canRequestQueuedPromptSendNow,
  type PromptQueueSendNowStrategy
} from "./promptQueue.sendNow.ts";
import {
  activeTurnIdForSession,
  resolveQueueDrainDecision
} from "./promptQueue.drainDecision.ts";
import {
  deriveCanonicalSubmitAvailability,
  type CanonicalSessionLifecycleView
} from "./sessionLifecycle.availability.ts";
import { canonicalTurnKey } from "./sessionEntityKeys.ts";
import { promptVisibleInQueueAdmission } from "./promptQueue.admission.ts";
import { affectedPromptQueueSessionIds } from "./promptQueue.affectedSessions.ts";
import {
  pendingSendNowFromSubmit,
  resolvePendingSendNow,
  setPendingSendNowForPrompt
} from "./promptQueue.pendingSendNow.ts";
import { queuedPromptFromSubmitIntent } from "./promptQueue.submit.ts";
import {
  requestPromptExecution,
  settlePromptSettingsPrecondition
} from "./promptQueue.precondition.ts";
import { settleOwnedQueueReconcile } from "./promptQueue.ownedReconcile.ts";
import { isPreTurnSendFailure } from "./promptSendFailure.ts";
import { settleQueueCommand } from "./promptQueue.settle.ts";
import type { RootEngineReducerResult } from "./rootReducer.types.ts";

const NO_COMMANDS: readonly EngineCommand[] = [];
// Send on an inactive Claude/Codex session includes full Resume/Start before
// Exec acceptance. Keep this aligned with new-session activation (90s) so a
// large restore is not aborted inside the 120s prompt confirmation window.
const QUEUE_SEND_TIMEOUT_MS = 90_000;

export { createInitialPromptQueueState } from "./promptQueue.initialState.ts";

export interface PromptQueueReducerContext {
  lifecycle: CanonicalSessionLifecycleView;
  deletedSessionIds: Readonly<Record<string, true>>;
  submitRequestAccepted?: boolean;
  sendResultValidation?: SendInputResultValidation | null;
  sendNowStrategy?: PromptQueueSendNowStrategy | null;
  settingsPreconditionPromptCommandId?: string | null;
  settingsResultValidation?: ScopedSessionResultValidation | null;
}

export function promptQueueReducer(
  state: PromptQueueState,
  intent: EngineIntent,
  context: PromptQueueReducerContext
): RootEngineReducerResult<PromptQueueState> {
  const reduced = reduceQueueOwnedState(state, intent, context);
  if (intent.type === "submit/requested" && intent.routing === "immediate") {
    return reduced;
  }
  if (isPreTurnSendFailure(intent)) {
    return reduced;
  }
  return drainAffectedSessions(
    reduced,
    affectedPromptQueueSessionIds(state, intent, context),
    context.lifecycle
  );
}

function reduceQueueOwnedState(
  state: PromptQueueState,
  intent: EngineIntent,
  context: PromptQueueReducerContext
): RootEngineReducerResult<PromptQueueState> {
  switch (intent.type) {
    case "session/removed":
    case "queue/sessionCleaned":
      return removeQueue(state, intent.agentSessionId);
    case "queue/enqueued":
      return context.deletedSessionIds[intent.agentSessionId.trim()]
        ? unchanged(state)
        : enqueuePrompt(state, intent);
    case "submit/requested":
      if (
        context.submitRequestAccepted === false ||
        context.deletedSessionIds[intent.agentSessionId.trim()]
      ) {
        return unchanged(state);
      }
      if (intent.routing === "send_now") {
        if (!context.sendNowStrategy) return unchanged(state);
        const targetTurnId =
          intent.targetTurnId ??
          activeTurnIdForSession(context.lifecycle, intent.agentSessionId);
        return requestQueuedPromptSendNow(
          enqueueSubmit(state, intent, context.lifecycle).state,
          intent.agentSessionId,
          intent.clientSubmitId,
          context.sendNowStrategy,
          targetTurnId,
          pendingSendNowFromSubmit(
            intent,
            context.sendNowStrategy,
            targetTurnId
          )
        );
      }
      return enqueueSubmit(state, intent, context.lifecycle);
    case "submit/canceled":
      return removePrompt(
        state,
        intent.agentSessionId,
        promptQueuePromptIdForClientSubmit(
          state,
          intent.agentSessionId,
          intent.clientSubmitId
        ) ?? ""
      );
    case "message/snapshotReceived":
      return confirmDeliveredPrompts(state, intent.messages);
    case "queue/removed":
      return removePrompt(state, intent.agentSessionId, intent.promptId);
    case "queue/sendNowRequested":
      if (
        context.deletedSessionIds[intent.agentSessionId.trim()] ||
        !context.sendNowStrategy
      ) {
        return unchanged(state);
      }
      const activeTurnId = activeTurnIdForSession(
        context.lifecycle,
        intent.agentSessionId
      );
      const targetTurnId = intent.targetTurnId?.trim() || activeTurnId;
      return requestQueuedPromptSendNow(
        state,
        intent.agentSessionId,
        intent.promptId,
        context.sendNowStrategy,
        targetTurnId,
        context.sendNowStrategy === "await_capabilities" && targetTurnId
          ? {
              awaitingTurnExpiresAtUnixMs: intent.awaitingTurnExpiresAtUnixMs,
              cancelCommandId: intent.cancelCommandId,
              promptId: intent.promptId,
              targetTurnId,
              timeoutMs: intent.timeoutMs
            }
          : null
      );
    case "queue/suspended":
      return suspendQueue(state, intent.agentSessionId, intent.reason);
    case "session/stopRequested":
      return suspendQueue(state, intent.agentSessionId, "user_stop");
    case "queue/resumed":
      return context.deletedSessionIds[intent.agentSessionId.trim()]
        ? unchanged(state)
        : resumeQueue(state, intent.agentSessionId);
    case "engine/commandResult":
      if (intent.commandType === "queue/sendPrompt") {
        return settleQueueCommand(
          state,
          intent,
          context.sendResultValidation ?? null
        );
      }
      if (intent.commandType === "session/reconcile") {
        return settleOwnedQueueReconcile(state, intent);
      }
      return intent.commandType === "session/updateSettings" &&
        context.settingsPreconditionPromptCommandId
        ? settlePromptSettingsPrecondition(
            state,
            context.settingsPreconditionPromptCommandId,
            intent,
            context.settingsResultValidation ?? null
          )
        : unchanged(state);
    default:
      return unchanged(state);
  }
}

function enqueuePrompt(
  state: PromptQueueState,
  intent: Extract<PromptQueueIntent, { type: "queue/enqueued" }>,
  options?: { insertAtFront?: boolean }
): EngineReducerResult<PromptQueueState> {
  const agentSessionId = intent.agentSessionId.trim();
  const workspaceId = intent.workspaceId.trim();
  const prompt = normalizeQueuedPrompt(intent.prompt);
  if (!agentSessionId || !workspaceId || !prompt) return unchanged(state);
  const current =
    state.recordsBySessionId[agentSessionId] ??
    emptyQueueRecord(workspaceId, agentSessionId);
  if (current.prompts.some((candidate) => candidate.id === prompt.id)) {
    return unchanged(state);
  }
  return result(
    replaceRecord(state, agentSessionId, {
      ...current,
      prompts: options?.insertAtFront
        ? [prompt, ...current.prompts]
        : [...current.prompts, prompt]
    })
  );
}

function enqueueSubmit(
  state: PromptQueueState,
  intent: Extract<EngineIntent, { type: "submit/requested" }>,
  lifecycle: CanonicalSessionLifecycleView
): RootEngineReducerResult<PromptQueueState> {
  if (intent.routing === "immediate") {
    const command = sendCommandFromImmediateSubmit(intent);
    return command.requiredSettingsPatch
      ? requestPromptExecution(state, command)
      : { commands: [command], state };
  }
  const agentSessionId = intent.agentSessionId.trim();
  const current = state.recordsBySessionId[agentSessionId];
  const availability = deriveCanonicalSubmitAvailability(
    lifecycle,
    agentSessionId
  );
  const visibleInQueue = promptVisibleInQueueAdmission(
    current,
    availability.state
  );
  // An ordinary submit that resumes a queue the user just explicitly
  // stopped is the user's next instruction, not a continuation of whatever
  // was already queued before the stop: it jumps ahead of that stale
  // backlog instead of preserving FIFO order behind it.
  const resumingFromUserStop = current?.suspendReason === "user_stop";
  const resumed = resumeQueue(state, agentSessionId);
  return enqueuePrompt(
    resumed.state,
    {
      agentSessionId,
      prompt: queuedPromptFromSubmitIntent(intent, visibleInQueue),
      type: "queue/enqueued",
      workspaceId: intent.workspaceId
    },
    { insertAtFront: resumingFromUserStop }
  );
}

function sendCommandFromImmediateSubmit(
  intent: Extract<EngineIntent, { type: "submit/requested" }>
): Extract<EngineCommand, { type: "queue/sendPrompt" }> {
  return {
    agentSessionId: intent.agentSessionId,
    ...clonePromptCapabilityReferences(intent.capabilityRefs),
    commandId: `submit:send:${intent.clientSubmitId}`,
    clientSubmitId: intent.clientSubmitId,
    correlationId: intent.clientSubmitId,
    content: intent.runtimeContent ?? intent.content,
    ...(intent.displayPrompt ? { displayPrompt: intent.displayPrompt } : {}),
    ...(intent.submitDiagnostics
      ? { submitDiagnostics: intent.submitDiagnostics }
      : {}),
    ...(intent.targetTurnId?.trim()
      ? { targetTurnId: intent.targetTurnId.trim() }
      : {}),
    promptId: intent.clientSubmitId,
    ...clonePromptRequiredSettingsPatch(intent.requiredSettingsPatch),
    timeoutMs: QUEUE_SEND_TIMEOUT_MS,
    type: "queue/sendPrompt",
    workspaceId: intent.workspaceId
  };
}

function removePrompt(
  state: PromptQueueState,
  rawAgentSessionId: string,
  rawPromptId: string
): EngineReducerResult<PromptQueueState> {
  const agentSessionId = rawAgentSessionId.trim();
  const promptId = rawPromptId.trim();
  const current = state.recordsBySessionId[agentSessionId];
  if (
    !current ||
    !promptId ||
    current.inFlight?.promptId === promptId ||
    current.uncertainDelivery?.promptId === promptId ||
    !current.prompts.some((prompt) => prompt.id === promptId)
  ) {
    return unchanged(state);
  }
  const next = compactQueueRecord({
    ...current,
    failedPromptId:
      current.failedPromptId === promptId ? null : current.failedPromptId,
    failureMessage:
      current.failedPromptId === promptId ? null : current.failureMessage,
    prompts: current.prompts.filter((prompt) => prompt.id !== promptId),
    pendingSendNowByPromptId: setPendingSendNowForPrompt(
      current.pendingSendNowByPromptId,
      promptId,
      null
    ),
    sendNextPromptId:
      current.sendNextPromptId === promptId ? null : current.sendNextPromptId
  });
  return result(
    next
      ? replaceRecord(state, agentSessionId, next)
      : deleteRecord(state, agentSessionId)
  );
}

function requestQueuedPromptSendNow(
  state: PromptQueueState,
  rawAgentSessionId: string,
  rawPromptId: string,
  strategy: PromptQueueSendNowStrategy,
  targetTurnId?: string,
  pendingSendNow: PromptQueuePendingSendNow | null = null
): EngineReducerResult<PromptQueueState> {
  const agentSessionId = rawAgentSessionId.trim();
  const promptId = rawPromptId.trim();
  if (!canRequestQueuedPromptSendNow(state, agentSessionId, promptId)) {
    return unchanged(state);
  }
  const current = state.recordsBySessionId[agentSessionId]!;
  const index = current.prompts.findIndex((prompt) => prompt.id === promptId);
  if (index < 0) return unchanged(state);
  const prompts = [...current.prompts];
  const [selected] = prompts.splice(index, 1);
  const selectedWithoutGuidance = { ...selected! };
  delete selectedWithoutGuidance.guidance;
  delete selectedWithoutGuidance.targetTurnId;
  if (strategy === "await_capabilities") {
    prompts.splice(index, 0, selectedWithoutGuidance);
  } else {
    prompts.unshift(
      strategy === "native_guidance"
        ? {
            ...selectedWithoutGuidance,
            guidance: true,
            ...(targetTurnId?.trim()
              ? { targetTurnId: targetTurnId.trim() }
              : {})
          }
        : selectedWithoutGuidance
    );
  }
  return result(
    replaceRecord(state, agentSessionId, {
      ...current,
      failedPromptId:
        current.failedPromptId === promptId ? null : current.failedPromptId,
      failureMessage:
        current.failedPromptId === promptId ? null : current.failureMessage,
      pendingSendNowByPromptId: setPendingSendNowForPrompt(
        current.pendingSendNowByPromptId,
        promptId,
        strategy === "await_capabilities" ? pendingSendNow : null
      ),
      prompts,
      sendNextPromptId: strategy === "cancel_then_send" ? promptId : null,
      suspendReason: null
    })
  );
}

function suspendQueue(
  state: PromptQueueState,
  rawAgentSessionId: string,
  reason: PromptQueueRecord["suspendReason"]
): EngineReducerResult<PromptQueueState> {
  const agentSessionId = rawAgentSessionId.trim();
  const current = state.recordsBySessionId[agentSessionId];
  if (
    !current ||
    current.prompts.length === 0 ||
    current.suspendReason === reason
  ) {
    return unchanged(state);
  }
  return result(
    replaceRecord(state, agentSessionId, { ...current, suspendReason: reason })
  );
}

function resumeQueue(
  state: PromptQueueState,
  rawAgentSessionId: string
): EngineReducerResult<PromptQueueState> {
  const agentSessionId = rawAgentSessionId.trim();
  const current = state.recordsBySessionId[agentSessionId];
  return !current || current.suspendReason === null
    ? unchanged(state)
    : result(
        replaceRecord(state, agentSessionId, {
          ...current,
          suspendReason: null
        })
      );
}

function confirmDeliveredPrompts(
  state: PromptQueueState,
  messages: readonly AgentActivityMessage[]
): EngineReducerResult<PromptQueueState> {
  const confirmedTurnByClientSubmitId = exactConfirmedTurns(messages);
  if (confirmedTurnByClientSubmitId.size === 0) return unchanged(state);
  let next = state;
  for (const [agentSessionId, current] of Object.entries(
    state.recordsBySessionId
  )) {
    const matched = current.prompts.find((prompt) => {
      const turnId = prompt.clientSubmitId
        ? confirmedTurnByClientSubmitId.get(prompt.clientSubmitId)
        : undefined;
      return Boolean(turnId);
    });
    if (!matched?.clientSubmitId) continue;
    const turnId = confirmedTurnByClientSubmitId.get(matched.clientSubmitId);
    if (!turnId) continue;
    const record = compactQueueRecord({
      ...current,
      deliveryBarrierTurnId: turnId,
      failedPromptId:
        current.failedPromptId === matched.id ? null : current.failedPromptId,
      failureMessage:
        current.failedPromptId === matched.id ? null : current.failureMessage,
      inFlight:
        current.inFlight?.promptId === matched.id ? null : current.inFlight,
      prompts: current.prompts.filter((prompt) => prompt.id !== matched.id),
      pendingSendNowByPromptId: setPendingSendNowForPrompt(
        current.pendingSendNowByPromptId,
        matched.id,
        null
      ),
      sendNextPromptId:
        current.sendNextPromptId === matched.id
          ? null
          : current.sendNextPromptId,
      uncertainDelivery:
        current.uncertainDelivery?.promptId === matched.id
          ? null
          : current.uncertainDelivery
    });
    next = record
      ? replaceRecord(next, agentSessionId, record)
      : deleteRecord(next, agentSessionId);
  }
  return next === state ? unchanged(state) : result(next);
}

function exactConfirmedTurns(
  messages: readonly AgentActivityMessage[]
): ReadonlyMap<string, string> {
  const turnsBySubmitId = new Map<string, Set<string>>();
  for (const message of messages) {
    const clientSubmitId = message.payload?.clientSubmitId;
    const turnId = message.turnId?.trim() ?? "";
    if (
      typeof clientSubmitId !== "string" ||
      !clientSubmitId.trim() ||
      !turnId
    ) {
      continue;
    }
    const id = clientSubmitId.trim();
    const turns = turnsBySubmitId.get(id) ?? new Set<string>();
    turns.add(turnId);
    turnsBySubmitId.set(id, turns);
  }
  return new Map(
    [...turnsBySubmitId]
      .filter(([, turns]) => turns.size === 1)
      .map(([clientSubmitId, turns]) => [clientSubmitId, [...turns][0]!])
  );
}

function drainAffectedSessions(
  reduced: RootEngineReducerResult<PromptQueueState>,
  affected: readonly string[],
  lifecycle: CanonicalSessionLifecycleView
): RootEngineReducerResult<PromptQueueState> {
  let state = reduced.state;
  const commands = [...reduced.commands];
  const followUpIntents = [...(reduced.followUpIntents ?? [])];
  for (const agentSessionId of [...new Set(affected)].sort()) {
    const drained = drainSession(state, agentSessionId, lifecycle);
    state = drained.state;
    commands.push(...drained.commands);
    followUpIntents.push(...(drained.followUpIntents ?? []));
  }
  return state === reduced.state &&
    commands.length === reduced.commands.length &&
    followUpIntents.length === (reduced.followUpIntents?.length ?? 0)
    ? reduced
    : {
        commands,
        ...(followUpIntents.length > 0 ? { followUpIntents } : {}),
        state
      };
}

function drainSession(
  state: PromptQueueState,
  agentSessionId: string,
  lifecycle: CanonicalSessionLifecycleView
): RootEngineReducerResult<PromptQueueState> {
  const originalState = state;
  let record = state.recordsBySessionId[agentSessionId];
  if (!record) return unchanged(state);
  let barrierPending = false;
  if (record.deliveryBarrierTurnId) {
    const barrierTurn =
      lifecycle.turnsById[
        canonicalTurnKey(agentSessionId, record.deliveryBarrierTurnId)
      ];
    if (!barrierTurn || barrierTurn.phase !== "settled") {
      // The barrier only serializes plain new-turn sends; it does not gate
      // drain readiness on its own. Whether it blocks the head is decided
      // below, alongside every other blocker, so a guidance head steering
      // this very turn is not deadlocked behind its own barrier.
      barrierPending = true;
    } else {
      record = { ...record, deliveryBarrierTurnId: null };
      const compacted = compactQueueRecord(record);
      state = compacted
        ? replaceRecord(state, agentSessionId, compacted)
        : deleteRecord(state, agentSessionId);
      if (!compacted) return result(state);
    }
  }
  const availability = deriveCanonicalSubmitAvailability(
    lifecycle,
    agentSessionId
  );
  const pendingResolution = resolvePendingSendNow({
    activeTurnId: activeTurnIdForSession(lifecycle, agentSessionId),
    availability,
    capabilities: lifecycle.sessionsById[agentSessionId]?.capabilities,
    record
  });
  if (pendingResolution.kind === "waiting") {
    return state === originalState ? unchanged(state) : result(state);
  }
  if (pendingResolution.record !== record) {
    record = pendingResolution.record;
    state = replaceRecord(state, agentSessionId, record);
  }
  if (pendingResolution.kind === "request") {
    return {
      commands: NO_COMMANDS,
      followUpIntents: [pendingResolution.intent],
      state
    };
  }
  // The guidance flag was resolved when the prompt entered the queue; whether
  // it can steer is decided here, against the availability observed at drain
  // time. A prompt queued as guidance behind a turn that has since settled is
  // sent as a plain new-turn submit instead of a doomed steer.
  const decision = resolveQueueDrainDecision(
    record,
    availability,
    barrierPending
  );
  if (decision.kind === "blocked") {
    return state === originalState ? unchanged(state) : result(state);
  }
  const head = record.prompts[0]!;
  const sequence = state.nextCommandSequence;
  const commandId = queueSendCommandId(record.agentSessionId, sequence);
  const command = sendCommandFromQueuedPrompt(
    record,
    head,
    commandId,
    decision.guidance
  );
  const nextState = replaceRecord(
    { ...state, nextCommandSequence: sequence + 1 },
    record.agentSessionId,
    {
      ...record,
      inFlight: {
        commandId,
        ...(decision.guidance ? { guidance: true as const } : {}),
        kind: "send",
        promptId: head.id,
        stage: command.requiredSettingsPatch ? "preparingSettings" : "sending"
      }
    }
  );
  if (command.requiredSettingsPatch) {
    return requestPromptExecution(nextState, command);
  }
  return {
    commands: [command],
    state: nextState
  };
}

function sendCommandFromQueuedPrompt(
  record: PromptQueueRecord,
  head: PromptQueueRecord["prompts"][number],
  commandId: string,
  guidance: boolean
): Extract<EngineCommand, { type: "queue/sendPrompt" }> {
  return {
    agentSessionId: record.agentSessionId,
    ...clonePromptCapabilityReferences(head.capabilityRefs),
    commandId,
    ...(head.clientSubmitId ? { correlationId: head.clientSubmitId } : {}),
    clientSubmitId: head.clientSubmitId ?? head.id,
    content: head.runtimeContent ?? head.content,
    ...(head.displayPrompt ? { displayPrompt: head.displayPrompt } : {}),
    ...(guidance ? { guidance: true } : {}),
    ...(guidance && head.targetTurnId?.trim()
      ? { targetTurnId: head.targetTurnId.trim() }
      : {}),
    ...(head.submitDiagnostics
      ? { submitDiagnostics: head.submitDiagnostics }
      : {}),
    promptId: head.id,
    ...clonePromptRequiredSettingsPatch(head.requiredSettingsPatch),
    timeoutMs: QUEUE_SEND_TIMEOUT_MS,
    type: "queue/sendPrompt",
    workspaceId: record.workspaceId
  };
}

function removeQueue(
  state: PromptQueueState,
  rawAgentSessionId: string
): EngineReducerResult<PromptQueueState> {
  const agentSessionId = rawAgentSessionId.trim();
  return state.recordsBySessionId[agentSessionId]
    ? result(deleteRecord(state, agentSessionId))
    : unchanged(state);
}

function replaceRecord(
  state: PromptQueueState,
  agentSessionId: string,
  record: PromptQueueRecord
): PromptQueueState {
  return {
    ...state,
    recordsBySessionId: {
      ...state.recordsBySessionId,
      [agentSessionId]: record
    }
  };
}

function deleteRecord(
  state: PromptQueueState,
  agentSessionId: string
): PromptQueueState {
  const records = { ...state.recordsBySessionId };
  delete records[agentSessionId];
  return { ...state, recordsBySessionId: records };
}

function result(
  state: PromptQueueState
): EngineReducerResult<PromptQueueState> {
  return { commands: NO_COMMANDS, state };
}

function unchanged(
  state: PromptQueueState
): EngineReducerResult<PromptQueueState> {
  return { commands: NO_COMMANDS, state };
}
