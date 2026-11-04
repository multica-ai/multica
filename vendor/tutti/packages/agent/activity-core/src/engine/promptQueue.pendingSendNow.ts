import type { AgentActivitySessionCapabilities } from "../types.ts";
import type {
  PromptQueuePendingSendNow,
  PromptQueueRecord,
  PromptQueueSendNowRequestedIntent
} from "./promptQueue.types.ts";
import {
  resolvePromptSendNowStrategy,
  type PromptQueueSendNowStrategy
} from "./promptQueue.sendNow.ts";

const TURN_CANCEL_TIMEOUT_MS = 30_000;
import type { CanonicalSubmitAvailability } from "./sessionLifecycle.availability.ts";
import type { EngineIntent } from "./types.ts";

export function pendingSendNowFromSubmit(
  intent: Extract<EngineIntent, { type: "submit/requested" }>,
  strategy: PromptQueueSendNowStrategy,
  targetTurnId: string | null | undefined
): PromptQueuePendingSendNow | null {
  const target = targetTurnId?.trim() ?? "";
  return strategy === "await_capabilities" && target
    ? {
        awaitingTurnExpiresAtUnixMs:
          intent.requestedAtUnixMs + TURN_CANCEL_TIMEOUT_MS,
        cancelCommandId: `submit:cancel:${intent.clientSubmitId}`,
        promptId: intent.clientSubmitId,
        targetTurnId: target,
        timeoutMs: TURN_CANCEL_TIMEOUT_MS
      }
    : null;
}

export function setPendingSendNowForPrompt(
  current: PromptQueueRecord["pendingSendNowByPromptId"],
  promptId: string,
  pending: PromptQueuePendingSendNow | null
): PromptQueueRecord["pendingSendNowByPromptId"] {
  const next = { ...(current ?? {}) };
  if (pending) {
    next[promptId] = pending;
  } else {
    delete next[promptId];
  }
  return Object.keys(next).length > 0 ? next : undefined;
}

export type PendingSendNowResolution =
  | { kind: "continue"; record: PromptQueueRecord }
  | {
      kind: "request";
      intent: PromptQueueSendNowRequestedIntent;
      record: PromptQueueRecord;
    }
  | { kind: "waiting" };

export function resolvePendingSendNow(input: {
  activeTurnId: string | null | undefined;
  availability: CanonicalSubmitAvailability;
  capabilities: AgentActivitySessionCapabilities | null | undefined;
  record: PromptQueueRecord;
}): PendingSendNowResolution {
  if (
    input.record.inFlight ||
    input.record.uncertainDelivery ||
    input.record.sendNextPromptId ||
    input.record.prompts[0]?.guidance === true
  ) {
    return { kind: "continue", record: input.record };
  }
  const pending = input.record.prompts
    .map((prompt) => input.record.pendingSendNowByPromptId?.[prompt.id])
    .find((candidate) => candidate != null);
  if (!pending) {
    return { kind: "continue", record: input.record };
  }
  const activeTurnId = input.activeTurnId?.trim() ?? "";
  const targetsSameTurn =
    activeTurnId.length > 0 && activeTurnId === pending.targetTurnId;
  if (targetsSameTurn && input.availability.state === "blocked") {
    if (
      input.availability.reason !== "active_turn" ||
      input.capabilities == null
    ) {
      return { kind: "waiting" };
    }
  }

  const strategy = targetsSameTurn
    ? resolvePromptSendNowStrategy(input.availability, input.capabilities)
    : null;
  const record = {
    ...input.record,
    pendingSendNowByPromptId: setPendingSendNowForPrompt(
      input.record.pendingSendNowByPromptId,
      pending.promptId,
      null
    )
  };
  if (strategy !== "native_guidance" && strategy !== "cancel_then_send") {
    return { kind: "continue", record };
  }
  return {
    intent: {
      agentSessionId: input.record.agentSessionId,
      awaitingTurnExpiresAtUnixMs: pending.awaitingTurnExpiresAtUnixMs,
      cancelCommandId: pending.cancelCommandId,
      promptId: pending.promptId,
      targetTurnId: pending.targetTurnId,
      timeoutMs: pending.timeoutMs,
      type: "queue/sendNowRequested"
    },
    kind: "request",
    record
  };
}
