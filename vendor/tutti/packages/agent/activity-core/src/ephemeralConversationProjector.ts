import {
  cloneAgentActivityMessage,
  compareAgentActivityMessages
} from "./merge.ts";
import type {
  AgentActivityInteraction,
  AgentActivityMessage,
  AgentActivityMessageDeltaEvent,
  AgentActivityMessageSemantics,
  AgentActivitySession,
  AgentActivitySnapshot,
  AgentActivityTurn,
  AgentActivityTurnOrigin,
  AgentActivityTurnOutcome,
  AgentActivityTurnPhase
} from "./types.ts";

export interface AgentActivityEphemeralConversationIdentity {
  workspaceId: string;
  agentSessionId: string;
  sourceAgentSessionId: string;
}

export interface AgentActivityEphemeralConversationSeed extends AgentActivityEphemeralConversationIdentity {
  provider: string;
  cwd?: string | null;
  title?: string | null;
  occurredAtUnixMs?: number;
}

export interface AgentActivityEphemeralTurnPatch {
  turnId: string;
  activeTurnId?: string | null;
  phase?: string | null;
  outcome?: string | null;
  origin?: string | null;
  error?: AgentActivityTurn["error"];
  fileChanges?: Record<string, unknown> | null;
  startedAtUnixMs?: number | null;
  completedAtUnixMs?: number | null;
  updatedAtUnixMs?: number | null;
}

export interface AgentActivityEphemeralInteractionPatch {
  requestId: string;
  turnId: string;
  kind: "approval" | "plan" | "question";
  status: string;
  toolName?: string | null;
  input?: Record<string, unknown> | null;
  metadata?: Record<string, unknown> | null;
  output?: Record<string, unknown> | null;
  occurredAtUnixMs?: number | null;
}

export interface AgentActivityEphemeralStatePatch {
  provider?: string | null;
  cwd?: string | null;
  title?: string | null;
  lifecycleStatus?: string | null;
  currentPhase?: string | null;
  activeTurnId?: string | null;
  turn?: AgentActivityEphemeralTurnPatch | null;
  interaction?: AgentActivityEphemeralInteractionPatch | null;
  occurredAtUnixMs?: number | null;
}

export type AgentActivityEphemeralConversationChange =
  | {
      kind: "message_delta";
      data: AgentActivityMessageDeltaEvent["data"];
    }
  | {
      kind: "message_update";
      message: AgentActivityMessage;
    }
  | {
      kind: "state_patch";
      patch: AgentActivityEphemeralStatePatch;
    }
  | {
      kind: "noop";
    };

export interface AgentActivityEphemeralConversationEvent extends AgentActivityEphemeralConversationIdentity {
  sequence: number;
  change: AgentActivityEphemeralConversationChange;
}

export type AgentActivityEphemeralConversationExpiryReason =
  | "identity_mismatch"
  | "sequence_gap"
  | "terminal_session";

export interface AgentActivityEphemeralConversationProjection {
  identity: AgentActivityEphemeralConversationIdentity;
  sequence: number;
  expired: boolean;
  expiryReason: AgentActivityEphemeralConversationExpiryReason | null;
  activitySnapshot: AgentActivitySnapshot;
  sessionTurns: readonly AgentActivityTurn[];
  interactions: readonly AgentActivityInteraction[];
}

export interface AgentActivityEphemeralConversationApplyResult {
  applied: boolean;
  expired: boolean;
  reason?: AgentActivityEphemeralConversationExpiryReason;
}

export interface AgentActivityEphemeralConversationProjector {
  apply(
    event: AgentActivityEphemeralConversationEvent
  ): AgentActivityEphemeralConversationApplyResult;
  beginTurn(input: {
    turnId: string;
    content: string;
    displayPrompt?: string | null;
    occurredAtUnixMs?: number;
  }): void;
  failTurn(input: {
    turnId: string;
    occurredAtUnixMs?: number;
    message?: string | null;
  }): void;
  getSnapshot(): AgentActivityEphemeralConversationProjection;
}

export function createAgentActivityEphemeralConversationProjector(
  seed: AgentActivityEphemeralConversationSeed
): AgentActivityEphemeralConversationProjector {
  const identity = normalizedIdentity(seed);
  const startedAtUnixMs = positiveTimestamp(seed.occurredAtUnixMs);
  const messages = new Map<string, AgentActivityMessage>();
  const turns = new Map<string, AgentActivityTurn>();
  const interactions = new Map<string, AgentActivityInteraction>();
  let sequence = 0;
  let expired = false;
  let expiryReason: AgentActivityEphemeralConversationExpiryReason | null =
    null;
  let provider = normalizedText(seed.provider) || "unknown";
  let cwd = normalizedText(seed.cwd);
  let title = normalizedText(seed.title);
  let activeTurnId: string | null = null;
  let lastEventUnixMs = startedAtUnixMs;
  let projection = project();

  function expire(
    reason: AgentActivityEphemeralConversationExpiryReason
  ): AgentActivityEphemeralConversationApplyResult {
    expired = true;
    expiryReason = reason;
    activeTurnId = null;
    projection = project();
    return { applied: false, expired: true, reason };
  }

  function apply(
    event: AgentActivityEphemeralConversationEvent
  ): AgentActivityEphemeralConversationApplyResult {
    if (expired) {
      return {
        applied: false,
        expired: true,
        ...(expiryReason ? { reason: expiryReason } : {})
      };
    }
    if (!sameIdentity(identity, event)) {
      return expire("identity_mismatch");
    }
    if (
      !Number.isSafeInteger(event.sequence) ||
      event.sequence !== sequence + 1
    ) {
      if (event.sequence <= sequence) {
        return { applied: false, expired: false };
      }
      return expire("sequence_gap");
    }
    sequence = event.sequence;
    switch (event.change.kind) {
      case "message_delta":
        applyMessageDelta(event.change.data);
        break;
      case "message_update":
        applyMessageUpdate(event.change.message);
        break;
      case "state_patch":
        if (applyStatePatch(event.change.patch)) {
          return expire("terminal_session");
        }
        break;
      case "noop":
        break;
    }
    projection = project();
    return { applied: true, expired: false };
  }

  function beginTurn(input: {
    turnId: string;
    content: string;
    displayPrompt?: string | null;
    occurredAtUnixMs?: number;
  }): void {
    if (expired) return;
    const turnId = normalizedText(input.turnId);
    if (!turnId) return;
    const occurredAtUnixMs = positiveTimestamp(input.occurredAtUnixMs);
    lastEventUnixMs = Math.max(lastEventUnixMs, occurredAtUnixMs);
    activeTurnId = turnId;
    turns.set(turnId, {
      agentSessionId: identity.agentSessionId,
      turnId,
      origin: "user_prompt",
      phase: "running",
      outcome: null,
      startedAtUnixMs: occurredAtUnixMs,
      updatedAtUnixMs: occurredAtUnixMs
    });
    const payload: Record<string, unknown> = {
      source: "side",
      content: input.content,
      text: input.content
    };
    const displayPrompt = normalizedText(input.displayPrompt);
    if (displayPrompt) payload.displayPrompt = displayPrompt;
    messages.set(`side-user:${turnId}`, {
      workspaceId: identity.workspaceId,
      agentSessionId: identity.agentSessionId,
      messageId: `side-user:${turnId}`,
      version: 0,
      turnId,
      role: "user",
      kind: "text",
      status: "completed",
      payload,
      occurredAtUnixMs
    });
    projection = project();
  }

  function failTurn(input: {
    turnId: string;
    occurredAtUnixMs?: number;
    message?: string | null;
  }): void {
    if (expired) return;
    const turnId = normalizedText(input.turnId);
    const existing = turns.get(turnId);
    if (!turnId || !existing) return;
    const occurredAtUnixMs = positiveTimestamp(input.occurredAtUnixMs);
    lastEventUnixMs = Math.max(lastEventUnixMs, occurredAtUnixMs);
    turns.set(turnId, {
      ...existing,
      phase: "settled",
      outcome: "failed",
      settledAtUnixMs: occurredAtUnixMs,
      updatedAtUnixMs: occurredAtUnixMs,
      error: normalizedText(input.message)
        ? { message: normalizedText(input.message) }
        : existing.error
    });
    if (activeTurnId === turnId) activeTurnId = null;
    projection = project();
  }

  function applyMessageUpdate(incoming: AgentActivityMessage): void {
    const normalized = normalizeMessage(identity, incoming);
    if (!normalized) return;
    const optimisticUser = [...messages.values()].find(
      (candidate) =>
        candidate.role === "user" &&
        normalized.role === "user" &&
        candidate.turnId === normalized.turnId &&
        candidate.messageId !== normalized.messageId
    );
    if (optimisticUser) messages.delete(optimisticUser.messageId);
    const existing = messages.get(normalized.messageId);
    messages.set(
      normalized.messageId,
      existing ? mergeMessage(existing, normalized) : normalized
    );
    lastEventUnixMs = Math.max(lastEventUnixMs, normalized.occurredAtUnixMs);
  }

  function applyMessageDelta(
    data: AgentActivityMessageDeltaEvent["data"]
  ): void {
    if (
      normalizedText(data.workspaceId) !== identity.workspaceId ||
      normalizedText(data.agentSessionId) !== identity.agentSessionId
    ) {
      return;
    }
    const messageId = normalizedText(data.messageId);
    if (!messageId) return;
    const existing = messages.get(messageId);
    const occurredAtUnixMs = positiveTimestamp(data.occurredAtUnixMs);
    const next: AgentActivityMessage = existing
      ? cloneAgentActivityMessage(existing)
      : {
          workspaceId: identity.workspaceId,
          agentSessionId: identity.agentSessionId,
          messageId,
          version: 0,
          turnId: normalizedText(data.turnId) || null,
          role: normalizedText(data.role) || "assistant",
          kind: normalizedText(data.kind) || "text",
          payload: {},
          occurredAtUnixMs
        };
    next.turnId = normalizedText(data.turnId) || next.turnId;
    next.role = normalizedText(data.role) || next.role;
    next.kind = normalizedText(data.kind) || next.kind;
    next.occurredAtUnixMs = Math.max(next.occurredAtUnixMs, occurredAtUnixMs);
    if (data.content?.operation === "append_text") {
      const current = stringValue(next.payload.text);
      const text = current + data.content.text;
      next.payload.text = text;
      next.payload.content = text;
    } else if (data.content?.operation === "set") {
      next.payload.content = cloneValue(data.content.value);
      if (typeof data.content.value === "string") {
        next.payload.text = data.content.value;
      } else {
        delete next.payload.text;
      }
    }
    if (data.toolOutput) {
      const output = recordValue(next.payload.output) ?? {};
      const current = stringValue(output.text);
      output.text =
        data.toolOutput.operation === "set"
          ? data.toolOutput.text
          : current + data.toolOutput.text;
      next.payload.output = output;
    }
    for (const [key, value] of Object.entries(data.payloadSet ?? {})) {
      next.payload[key] = cloneValue(value);
    }
    for (const key of data.payloadUnset ?? []) delete next.payload[key];
    if (data.status !== undefined) next.status = data.status;
    if (data.semantics !== undefined) {
      next.semantics = cloneValue(
        data.semantics
      ) as AgentActivityMessageSemantics;
    }
    if (data.startedAtUnixMs !== undefined) {
      next.startedAtUnixMs = data.startedAtUnixMs;
    }
    if (data.completedAtUnixMs !== undefined) {
      next.completedAtUnixMs = data.completedAtUnixMs;
    }
    messages.set(messageId, next);
    lastEventUnixMs = Math.max(lastEventUnixMs, occurredAtUnixMs);
  }

  function applyStatePatch(patch: AgentActivityEphemeralStatePatch): boolean {
    const occurredAtUnixMs = positiveTimestamp(patch.occurredAtUnixMs);
    lastEventUnixMs = Math.max(lastEventUnixMs, occurredAtUnixMs);
    provider = normalizedText(patch.provider) || provider;
    cwd = normalizedText(patch.cwd) || cwd;
    title = normalizedText(patch.title) || title;
    if (patch.turn)
      applyTurnPatch(patch.turn, patch.currentPhase, occurredAtUnixMs);
    if (patch.activeTurnId !== undefined) {
      const nextActiveTurnId = normalizedText(patch.activeTurnId) || null;
      if (nextActiveTurnId && !turns.has(nextActiveTurnId)) {
        applyTurnPatch(
          {
            turnId: nextActiveTurnId,
            activeTurnId: nextActiveTurnId,
            phase: patch.currentPhase
          },
          patch.currentPhase,
          occurredAtUnixMs
        );
      } else if (!nextActiveTurnId && activeTurnId) {
        const current = turns.get(activeTurnId);
        if (current) {
          applyTurnPatch(
            {
              turnId: current.turnId,
              activeTurnId: null,
              phase: patch.currentPhase,
              completedAtUnixMs: occurredAtUnixMs
            },
            patch.currentPhase,
            occurredAtUnixMs
          );
        }
      }
      activeTurnId = nextActiveTurnId;
    }
    if (patch.interaction)
      applyInteractionPatch(patch.interaction, occurredAtUnixMs);
    const lifecycle = normalizedToken(patch.lifecycleStatus);
    return (
      lifecycle === "completed" ||
      lifecycle === "failed" ||
      lifecycle === "ended"
    );
  }

  function applyTurnPatch(
    patch: AgentActivityEphemeralTurnPatch,
    currentPhase: string | null | undefined,
    occurredAtUnixMs: number
  ): void {
    const turnId = normalizedText(patch.turnId);
    if (!turnId) return;
    const existing = turns.get(turnId);
    const phase = normalizeTurnPhase(patch.phase ?? currentPhase);
    const outcome = normalizeTurnOutcome(patch.outcome);
    const updatedAtUnixMs =
      positiveOptionalTimestamp(patch.updatedAtUnixMs) ?? occurredAtUnixMs;
    const startedAtUnixMs =
      positiveOptionalTimestamp(patch.startedAtUnixMs) ??
      existing?.startedAtUnixMs ??
      occurredAtUnixMs;
    const settledAtUnixMs =
      phase === "settled"
        ? (positiveOptionalTimestamp(patch.completedAtUnixMs) ??
          existing?.settledAtUnixMs ??
          updatedAtUnixMs)
        : null;
    turns.set(turnId, {
      ...existing,
      agentSessionId: identity.agentSessionId,
      turnId,
      origin: normalizeTurnOrigin(patch.origin ?? existing?.origin),
      phase,
      outcome,
      error: patch.error ?? existing?.error ?? null,
      fileChanges: patch.fileChanges ?? existing?.fileChanges ?? null,
      startedAtUnixMs,
      settledAtUnixMs,
      updatedAtUnixMs
    });
    if (patch.activeTurnId !== undefined) {
      activeTurnId = normalizedText(patch.activeTurnId) || null;
    } else if (phase === "settled" && activeTurnId === turnId) {
      activeTurnId = null;
    } else if (phase !== "settled") {
      activeTurnId = turnId;
    }
  }

  function applyInteractionPatch(
    patch: AgentActivityEphemeralInteractionPatch,
    fallbackOccurredAtUnixMs: number
  ): void {
    const requestId = normalizedText(patch.requestId);
    const turnId = normalizedText(patch.turnId);
    if (!requestId || !turnId) return;
    const existing = interactions.get(requestId);
    const occurredAtUnixMs =
      positiveOptionalTimestamp(patch.occurredAtUnixMs) ??
      fallbackOccurredAtUnixMs;
    const statusToken = normalizedToken(patch.status);
    const status: AgentActivityInteraction["status"] =
      statusToken === "answered"
        ? "answered"
        : statusToken === "superseded" ||
            statusToken === "interrupted" ||
            statusToken === "canceled"
          ? "superseded"
          : "pending";
    interactions.set(requestId, {
      agentSessionId: identity.agentSessionId,
      requestId,
      turnId,
      kind: patch.kind,
      status,
      toolName: patch.toolName ?? existing?.toolName ?? null,
      input: patch.input ?? existing?.input ?? null,
      metadata: patch.metadata ?? existing?.metadata ?? null,
      output: patch.output ?? existing?.output ?? null,
      createdAtUnixMs: existing?.createdAtUnixMs ?? occurredAtUnixMs,
      updatedAtUnixMs: occurredAtUnixMs
    });
  }

  function project(): AgentActivityEphemeralConversationProjection {
    const sessionTurns = [...turns.values()].sort(
      (left, right) =>
        left.startedAtUnixMs - right.startedAtUnixMs ||
        left.turnId.localeCompare(right.turnId)
    );
    const projectedInteractions = [...interactions.values()].sort(
      (left, right) =>
        left.createdAtUnixMs - right.createdAtUnixMs ||
        left.requestId.localeCompare(right.requestId)
    );
    const latestTurn = sessionTurns.at(-1) ?? null;
    const activeTurn =
      sessionTurns.find((turn) => turn.turnId === activeTurnId) ?? null;
    const latestTurnInteractions = latestTurn
      ? projectedInteractions.filter(
          (interaction) => interaction.turnId === latestTurn.turnId
        )
      : [];
    const pendingInteractions = latestTurnInteractions.filter(
      (interaction) => interaction.status === "pending"
    );
    const session: AgentActivitySession = {
      workspaceId: identity.workspaceId,
      agentSessionId: identity.agentSessionId,
      kind: "root",
      rootAgentSessionId: null,
      rootTurnId: null,
      parentAgentSessionId: null,
      parentTurnId: null,
      parentToolCallId: null,
      agentTargetId: null,
      provider,
      providerSessionId: null,
      cwd,
      title,
      activeTurnId: expired ? null : activeTurnId,
      activeTurn: expired ? null : activeTurn,
      latestTurn,
      latestTurnInteractions,
      pendingInteractions,
      settings: {},
      permissionConfig: { configurable: false, modes: [] },
      capabilities: null,
      lifecycleCapabilities: { fork: false, forkThroughTurn: false },
      forkedFrom: null,
      usage: null,
      imported: false,
      visible: true,
      resumable: false,
      messageVersion: 0,
      lastEventUnixMs,
      startedAtUnixMs,
      endedAtUnixMs: expired ? lastEventUnixMs : null,
      pinnedAtUnixMs: null,
      createdAtUnixMs: startedAtUnixMs,
      updatedAtUnixMs: lastEventUnixMs
    };
    return {
      identity,
      sequence,
      expired,
      expiryReason,
      activitySnapshot: {
        workspaceId: identity.workspaceId,
        sessions: [session],
        presences: [],
        sessionMessagesById: {
          [identity.agentSessionId]: [...messages.values()]
            .map((message) => cloneAgentActivityMessage(message))
            .sort(compareAgentActivityMessages)
        }
      },
      sessionTurns,
      interactions: projectedInteractions
    };
  }

  return {
    apply,
    beginTurn,
    failTurn,
    getSnapshot: () => projection
  };
}

function normalizedIdentity(
  identity: AgentActivityEphemeralConversationIdentity
): AgentActivityEphemeralConversationIdentity {
  return {
    workspaceId: normalizedText(identity.workspaceId),
    agentSessionId: normalizedText(identity.agentSessionId),
    sourceAgentSessionId: normalizedText(identity.sourceAgentSessionId)
  };
}

function sameIdentity(
  expected: AgentActivityEphemeralConversationIdentity,
  actual: AgentActivityEphemeralConversationIdentity
): boolean {
  const normalized = normalizedIdentity(actual);
  return (
    normalized.workspaceId === expected.workspaceId &&
    normalized.agentSessionId === expected.agentSessionId &&
    normalized.sourceAgentSessionId === expected.sourceAgentSessionId
  );
}

function normalizeMessage(
  identity: AgentActivityEphemeralConversationIdentity,
  message: AgentActivityMessage
): AgentActivityMessage | null {
  if (
    normalizedText(message.workspaceId) !== identity.workspaceId ||
    normalizedText(message.agentSessionId) !== identity.agentSessionId ||
    !normalizedText(message.messageId)
  ) {
    return null;
  }
  return {
    ...cloneAgentActivityMessage(message),
    workspaceId: identity.workspaceId,
    agentSessionId: identity.agentSessionId,
    messageId: normalizedText(message.messageId),
    turnId: normalizedText(message.turnId) || null,
    role: normalizedText(message.role) || "assistant",
    kind: normalizedText(message.kind) || "text",
    occurredAtUnixMs: positiveTimestamp(message.occurredAtUnixMs)
  };
}

function mergeMessage(
  current: AgentActivityMessage,
  incoming: AgentActivityMessage
): AgentActivityMessage {
  return {
    ...current,
    ...incoming,
    sequence: undefined,
    semantics: incoming.semantics
      ? { ...current.semantics, ...incoming.semantics }
      : current.semantics,
    payload: { ...current.payload, ...incoming.payload }
  };
}

function normalizeTurnPhase(
  value: string | null | undefined
): AgentActivityTurnPhase {
  switch (normalizedToken(value)) {
    case "submitted":
      return "submitted";
    case "waiting":
    case "blocked":
    case "approval":
      return "waiting";
    case "settling":
      return "settling";
    case "settled":
    case "idle":
    case "completed":
    case "failed":
    case "canceled":
    case "cancelled":
    case "interrupted":
      return "settled";
    default:
      return "running";
  }
}

function normalizeTurnOutcome(
  value: string | null | undefined
): AgentActivityTurnOutcome | null {
  switch (normalizedToken(value)) {
    case "completed":
      return "completed";
    case "failed":
      return "failed";
    case "canceled":
    case "cancelled":
      return "canceled";
    case "interrupted":
      return "interrupted";
    default:
      return null;
  }
}

function normalizeTurnOrigin(
  value: string | null | undefined
): AgentActivityTurnOrigin {
  switch (normalizedToken(value)) {
    case "user_prompt":
      return "user_prompt";
    case "goal_arm":
      return "goal_arm";
    case "goal_continuation":
      return "goal_continuation";
    case "provider_initiated":
      return "provider_initiated";
    default:
      return "legacy_unknown";
  }
}

function positiveTimestamp(value: number | null | undefined): number {
  return positiveOptionalTimestamp(value) ?? Date.now();
}

function positiveOptionalTimestamp(
  value: number | null | undefined
): number | null {
  return typeof value === "number" && Number.isFinite(value) && value > 0
    ? value
    : null;
}

function normalizedText(value: unknown): string {
  return typeof value === "string" ? value.trim() : "";
}

function normalizedToken(value: unknown): string {
  return normalizedText(value).toLowerCase();
}

function stringValue(value: unknown): string {
  return typeof value === "string" ? value : "";
}

function recordValue(value: unknown): Record<string, unknown> | null {
  return value && typeof value === "object"
    ? { ...(value as Record<string, unknown>) }
    : null;
}

function cloneValue<T>(value: T): T {
  if (typeof structuredClone === "function") return structuredClone(value);
  return JSON.parse(JSON.stringify(value)) as T;
}
