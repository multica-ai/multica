import type { AgentActivityInteraction, AgentActivityTurn } from "../types.ts";
import { shouldUseIncomingInteraction } from "../interactionMonotonicity.ts";
import { areJsonLikeValuesEqual } from "../merge.ts";
import { normalizeAgentActivitySession } from "../sessionNormalization.ts";
import type { AgentActivitySessionInput } from "../sessionNormalization.ts";
import type {
  CanonicalAgentSession,
  CanonicalSessionMetadataPatch,
  SessionLifecycleState,
  SessionOperationState
} from "./sessionLifecycle.types.ts";
import {
  canonicalInteractionKey,
  canonicalTurnKey
} from "./sessionEntityKeys.ts";

export function replaceCanonicalSessionSnapshot(
  state: SessionLifecycleState,
  incoming: readonly AgentActivitySessionInput[],
  createOperation: () => SessionOperationState
): SessionLifecycleState {
  const sessionsById: Record<string, CanonicalAgentSession> = {};
  const turnsById: Record<string, AgentActivityTurn> = {};
  const interactionsById: Record<string, AgentActivityInteraction> = {};
  const operationBySessionId: Record<string, SessionOperationState> = {};
  const incomingSessionIds = new Set<string>();
  const authoritativePendingVersionBySessionId: Record<string, number> = {};
  for (const source of incoming) {
    const id = source.agentSessionId.trim();
    if (!id || state.deletedSessionIds[id]) continue;
    incomingSessionIds.add(id);
    const current = state.sessionsById[id];
    const incomingSession = canonicalSession(source);
    const useIncoming = shouldUseIncomingSession(current, incomingSession);
    const candidateSession = useIncoming
      ? preserveProjectedSessionState(
          current,
          preserveCanonicalActiveTurnProjection(
            state,
            current,
            incomingSession,
            source
          )
        )
      : current!;
    sessionsById[id] =
      current && areJsonLikeValuesEqual(current, candidateSession)
        ? current
        : candidateSession;
    operationBySessionId[id] =
      state.operationBySessionId[id] ?? createOperation();
    if (useIncoming) {
      authoritativePendingVersionBySessionId[id] =
        sessionVersion(incomingSession);
    }
    if (useIncoming && source.activeTurn?.agentSessionId === id) {
      mergeTurnInto(
        turnsById,
        state.turnsById,
        source.activeTurn,
        source.lifecycleCapabilitiesProjected !== true
      );
    }
    if (useIncoming && source.latestTurn?.agentSessionId === id) {
      mergeTurnInto(
        turnsById,
        state.turnsById,
        source.latestTurn,
        source.lifecycleCapabilitiesProjected !== true
      );
    }
    for (const interaction of [
      ...source.latestTurnInteractions,
      ...source.pendingInteractions
    ]) {
      if (interaction.agentSessionId === id) {
        mergeInteractionInto(
          interactionsById,
          state.interactionsById,
          interaction
        );
      }
    }
  }
  // Session removal is an explicit protocol-v2 event. A list response can be
  // stale or paginated, so omission must not erase a newer locally observed
  // session (for example one created while the list request was in flight).
  for (const [id, session] of Object.entries(state.sessionsById)) {
    if (!sessionsById[id] && !state.deletedSessionIds[id]) {
      sessionsById[id] = session;
      operationBySessionId[id] =
        state.operationBySessionId[id] ?? createOperation();
    }
  }
  for (const turn of Object.values(state.turnsById)) {
    const key = canonicalTurnKey(turn.agentSessionId, turn.turnId);
    if (sessionsById[turn.agentSessionId] && !turnsById[key]) {
      turnsById[key] = turn;
    }
  }
  for (const interaction of Object.values(state.interactionsById)) {
    if (!sessionsById[interaction.agentSessionId]) continue;
    const key = canonicalInteractionKey(
      interaction.agentSessionId,
      interaction.turnId,
      interaction.requestId
    );
    const projected = interactionsById[key];
    const authoritativePendingVersion =
      authoritativePendingVersionBySessionId[interaction.agentSessionId];
    const authoritativelyOmitted =
      !projected &&
      incomingSessionIds.has(interaction.agentSessionId) &&
      interaction.status === "pending" &&
      authoritativePendingVersion !== undefined &&
      authoritativePendingVersion >= interaction.updatedAtUnixMs;
    if (
      !authoritativelyOmitted &&
      (!projected || shouldUseIncomingInteraction(projected, interaction))
    ) {
      interactionsById[key] = interaction;
    }
  }
  const nextInteractionsById = reuseRecordIfShallowEqual(
    state.interactionsById,
    interactionsById
  );
  const nextOperationBySessionId = reuseRecordIfShallowEqual(
    state.operationBySessionId,
    operationBySessionId
  );
  const nextSessionsById = reuseRecordIfShallowEqual(
    state.sessionsById,
    sessionsById
  );
  const nextTurnsById = reuseRecordIfShallowEqual(state.turnsById, turnsById);
  if (
    nextInteractionsById === state.interactionsById &&
    nextOperationBySessionId === state.operationBySessionId &&
    nextSessionsById === state.sessionsById &&
    nextTurnsById === state.turnsById
  ) {
    return state;
  }
  return {
    ...state,
    interactionsById: nextInteractionsById,
    operationBySessionId: nextOperationBySessionId,
    sessionsById: nextSessionsById,
    turnsById: nextTurnsById
  };
}

export function upsertCanonicalSession(
  state: SessionLifecycleState,
  source: AgentActivitySessionInput,
  createOperation: () => SessionOperationState
): SessionLifecycleState {
  const id = source.agentSessionId.trim();
  if (!id || state.deletedSessionIds[id]) return state;
  const current = state.sessionsById[id];
  const incoming = canonicalSession(source);
  const useIncoming = shouldUseIncomingSession(current, incoming);
  const candidateSession = useIncoming
    ? preserveProjectedSessionState(
        current,
        preserveCanonicalActiveTurnProjection(state, current, incoming, source)
      )
    : current!;
  const nextSession =
    current && areJsonLikeValuesEqual(current, candidateSession)
      ? current
      : candidateSession;
  const operationBySessionId = state.operationBySessionId[id]
    ? state.operationBySessionId
    : { ...state.operationBySessionId, [id]: createOperation() };
  let next: SessionLifecycleState =
    operationBySessionId === state.operationBySessionId &&
    nextSession === current
      ? state
      : {
          ...state,
          operationBySessionId,
          sessionsById:
            nextSession === current
              ? state.sessionsById
              : { ...state.sessionsById, [id]: nextSession }
        };
  if (useIncoming && source.activeTurn?.agentSessionId === id) {
    next = upsertCanonicalTurn(next, source.activeTurn);
  }
  if (useIncoming && source.latestTurn?.agentSessionId === id) {
    next = upsertCanonicalTurn(next, source.latestTurn);
  }
  for (const interaction of source.latestTurnInteractions) {
    next = upsertCanonicalInteraction(next, interaction);
  }
  if (useIncoming) {
    next = removeMissingPendingInteractions(
      next,
      id,
      source.pendingInteractions,
      sessionVersion(incoming)
    );
  }
  for (const interaction of source.pendingInteractions) {
    next = upsertCanonicalInteraction(next, interaction);
  }
  return next;
}

export function patchCanonicalSessionMetadata(
  state: SessionLifecycleState,
  rawAgentSessionId: string,
  patch: CanonicalSessionMetadataPatch
): SessionLifecycleState {
  const agentSessionId = rawAgentSessionId.trim();
  const session = state.sessionsById[agentSessionId];
  if (
    !session ||
    Object.entries(patch).every(
      ([key, value]) => session[key as keyof typeof session] === value
    )
  ) {
    return state;
  }
  return {
    ...state,
    sessionsById: {
      ...state.sessionsById,
      [agentSessionId]: { ...session, ...patch }
    }
  };
}

function removeMissingPendingInteractions(
  state: SessionLifecycleState,
  agentSessionId: string,
  incoming: readonly AgentActivityInteraction[],
  authoritativeVersion: number
): SessionLifecycleState {
  const incomingKeys = new Set(
    incoming.map((item) =>
      canonicalInteractionKey(item.agentSessionId, item.turnId, item.requestId)
    )
  );
  const interactionsById = { ...state.interactionsById };
  let changed = false;
  for (const [key, interaction] of Object.entries(state.interactionsById)) {
    if (
      interaction.agentSessionId === agentSessionId &&
      interaction.status === "pending" &&
      !incomingKeys.has(
        canonicalInteractionKey(
          interaction.agentSessionId,
          interaction.turnId,
          interaction.requestId
        )
      ) &&
      authoritativeVersion >= interaction.updatedAtUnixMs
    ) {
      delete interactionsById[key];
      changed = true;
    }
  }
  return changed ? { ...state, interactionsById } : state;
}

export function upsertCanonicalTurn(
  state: SessionLifecycleState,
  turn: AgentActivityTurn
): SessionLifecycleState {
  if (
    !turn.agentSessionId.trim() ||
    !turn.turnId.trim() ||
    state.deletedSessionIds[turn.agentSessionId]
  )
    return state;
  const key = canonicalTurnKey(turn.agentSessionId, turn.turnId);
  const current = state.turnsById[key];
  if (current && !shouldUseIncomingTurn(current, turn)) return state;
  return writeCanonicalTurn(state, turn, current);
}

function writeCanonicalTurn(
  state: SessionLifecycleState,
  turn: AgentActivityTurn,
  current: AgentActivityTurn | undefined
): SessionLifecycleState {
  const key = canonicalTurnKey(turn.agentSessionId, turn.turnId);
  const nextTurn = current ? preserveTurnProvenance(current, turn) : turn;
  if (current && areJsonLikeValuesEqual(current, nextTurn)) return state;
  return {
    ...state,
    turnsById: { ...state.turnsById, [key]: { ...nextTurn } }
  };
}

export function upsertCanonicalTurnProjection(
  state: SessionLifecycleState,
  input: CanonicalTurnProjection
): SessionLifecycleState {
  const agentSessionId = input.turn.agentSessionId.trim();
  const turnId = input.turn.turnId.trim();
  if (
    !turnProjectionIsConsistent(input) ||
    state.deletedSessionIds[agentSessionId]
  ) {
    return state;
  }

  const key = canonicalTurnKey(agentSessionId, turnId);
  const currentTurn = state.turnsById[key];
  if (
    currentTurn &&
    !shouldUseIncomingTurnProjection(
      currentTurn,
      input.turn,
      input.hostFencedSameTurnSettlement === true
    )
  ) {
    return state;
  }

  const withTurn = writeCanonicalTurn(state, input.turn, currentTurn);
  const session = withTurn.sessionsById[agentSessionId];
  if (!session) return withTurn;

  const activeTurnId = projectedActiveTurnId(withTurn, session, input);
  if (activeTurnId === undefined || session.activeTurnId === activeTurnId) {
    return withTurn;
  }
  return {
    ...withTurn,
    sessionsById: {
      ...withTurn.sessionsById,
      [agentSessionId]: {
        ...session,
        activeTurnId
      }
    }
  };
}

export function replaceCanonicalTurnSnapshot(
  state: SessionLifecycleState,
  agentSessionId: string,
  turns: readonly AgentActivityTurn[]
): SessionLifecycleState {
  const id = agentSessionId.trim();
  if (!id || state.deletedSessionIds[id]) return state;
  const turnsById = Object.fromEntries(
    Object.entries(state.turnsById).filter(
      ([, turn]) => turn.agentSessionId !== id
    )
  );
  for (const turn of turns) {
    if (turn.agentSessionId.trim() !== id || !turn.turnId.trim()) continue;
    turnsById[canonicalTurnKey(id, turn.turnId)] = { ...turn };
  }
  const validTurnIds = new Set(
    turns
      .filter((turn) => turn.agentSessionId.trim() === id)
      .map((turn) => turn.turnId.trim())
      .filter(Boolean)
  );
  const interactionsById = Object.fromEntries(
    Object.entries(state.interactionsById).filter(
      ([, interaction]) =>
        interaction.agentSessionId !== id ||
        validTurnIds.has(interaction.turnId.trim())
    )
  );
  return {
    ...state,
    interactionsById,
    turnsById
  };
}

export function upsertCanonicalInteraction(
  state: SessionLifecycleState,
  interaction: AgentActivityInteraction
): SessionLifecycleState {
  if (
    !interaction.agentSessionId.trim() ||
    !interaction.requestId.trim() ||
    !interaction.turnId.trim() ||
    state.deletedSessionIds[interaction.agentSessionId]
  ) {
    return state;
  }
  const key = canonicalInteractionKey(
    interaction.agentSessionId,
    interaction.turnId,
    interaction.requestId
  );
  const current = state.interactionsById[key];
  if (!shouldUseIncomingInteraction(current, interaction)) return state;
  if (current && areJsonLikeValuesEqual(current, interaction)) return state;
  return {
    ...state,
    interactionsById: {
      ...state.interactionsById,
      [key]: { ...interaction }
    }
  };
}

function mergeInteractionInto(
  target: Record<string, AgentActivityInteraction>,
  existing: Readonly<Record<string, AgentActivityInteraction>>,
  interaction: AgentActivityInteraction
): void {
  const key = canonicalInteractionKey(
    interaction.agentSessionId,
    interaction.turnId,
    interaction.requestId
  );
  const current = target[key] ?? existing[key];
  if (shouldUseIncomingInteraction(current, interaction)) {
    target[key] =
      current && areJsonLikeValuesEqual(current, interaction)
        ? current
        : { ...interaction };
  }
}

function shouldUseIncomingTurn(
  current: AgentActivityTurn,
  incoming: AgentActivityTurn
): boolean {
  if (incoming.updatedAtUnixMs < current.updatedAtUnixMs) return false;
  if (current.phase === "settled") {
    return incoming.phase === "settled" && incoming.outcome === current.outcome;
  }
  if (!allowedTurnTransition(current.phase, incoming.phase)) return false;
  return true;
}

function shouldUseIncomingTurnProjection(
  current: AgentActivityTurn,
  incoming: AgentActivityTurn,
  hostFencedSameTurnSettlement: boolean
): boolean {
  if (
    hostFencedSameTurnSettlement &&
    current.phase !== "settled" &&
    incoming.phase === "settled" &&
    allowedTurnTransition(current.phase, incoming.phase)
  ) {
    return true;
  }
  return shouldUseIncomingTurn(current, incoming);
}

interface CanonicalTurnProjection {
  activeTurnId: string | null;
  hostFencedSameTurnSettlement?: true;
  turn: AgentActivityTurn;
}

function turnProjectionIsConsistent(input: CanonicalTurnProjection): boolean {
  const turnId = input.turn.turnId.trim();
  const activeTurnId = input.activeTurnId?.trim() ?? null;
  return (
    Boolean(input.turn.agentSessionId.trim()) &&
    Boolean(turnId) &&
    (input.turn.phase === "settled"
      ? activeTurnId === null
      : activeTurnId === turnId)
  );
}

function projectedActiveTurnId(
  state: SessionLifecycleState,
  session: CanonicalAgentSession,
  input: CanonicalTurnProjection
): string | null | undefined {
  const currentActiveTurnId = session.activeTurnId?.trim() ?? null;
  const turnId = input.turn.turnId.trim();
  if (input.turn.phase === "settled") {
    return currentActiveTurnId === turnId ? null : undefined;
  }
  if (currentActiveTurnId === turnId) return turnId;

  if (!currentActiveTurnId) {
    return turnId;
  }
  const currentActiveTurn =
    state.turnsById[
      canonicalTurnKey(session.agentSessionId, currentActiveTurnId)
    ];
  const currentVersion =
    currentActiveTurn?.updatedAtUnixMs ?? sessionVersion(session);
  return input.turn.updatedAtUnixMs > currentVersion ? turnId : undefined;
}

function allowedTurnTransition(current: string, incoming: string): boolean {
  if (current === incoming) return true;
  switch (current) {
    case "submitted":
      return ["running", "waiting", "settling", "settled"].includes(incoming);
    case "running":
      return ["waiting", "settling", "settled"].includes(incoming);
    case "waiting":
      return ["running", "settling", "settled"].includes(incoming);
    case "settling":
      return incoming === "settled";
    case "settled":
      return false;
    default:
      return false;
  }
}

export function removeCanonicalSession(
  state: SessionLifecycleState,
  agentSessionId: string
): SessionLifecycleState {
  const sessionsById = { ...state.sessionsById };
  const operationBySessionId = { ...state.operationBySessionId };
  delete sessionsById[agentSessionId];
  delete operationBySessionId[agentSessionId];
  return {
    ...state,
    interactionsById: Object.fromEntries(
      Object.entries(state.interactionsById).filter(
        ([, value]) => value.agentSessionId !== agentSessionId
      )
    ),
    operationBySessionId,
    sessionsById,
    turnsById: Object.fromEntries(
      Object.entries(state.turnsById).filter(
        ([, value]) => value.agentSessionId !== agentSessionId
      )
    )
  };
}

function canonicalSession(
  source: AgentActivitySessionInput
): CanonicalAgentSession {
  const normalized = normalizeAgentActivitySession(source);
  const {
    activeTurn: _activeTurn,
    latestTurn: _latestTurn,
    latestTurnInteractions: _latestTurnInteractions,
    pendingInteractions: _pendingInteractions,
    ...session
  } = normalized;
  return { ...session, activeTurnId: normalized.activeTurnId };
}

function mergeTurnInto(
  target: Record<string, AgentActivityTurn>,
  existing: Readonly<Record<string, AgentActivityTurn>>,
  turn: AgentActivityTurn,
  preserveProjectedForkBinding: boolean
): void {
  const key = canonicalTurnKey(turn.agentSessionId, turn.turnId);
  const current = target[key] ?? existing[key];
  if (!current || shouldUseIncomingTurn(current, turn)) {
    const candidate = current
      ? preserveTurnProjectionState(
          current,
          preserveTurnProvenance(current, turn),
          preserveProjectedForkBinding
        )
      : turn;
    target[key] =
      current && areJsonLikeValuesEqual(current, candidate)
        ? current
        : { ...candidate };
  }
}

function preserveTurnProjectionState(
  current: AgentActivityTurn,
  incoming: AgentActivityTurn,
  preserveProjectedForkBinding: boolean
): AgentActivityTurn {
  if (
    !preserveProjectedForkBinding ||
    current.providerForkBindingAvailable !== true ||
    incoming.providerForkBindingAvailable === true
  ) {
    return incoming;
  }
  return {
    ...incoming,
    providerForkBindingAvailable: true,
    providerForkBindingState: current.providerForkBindingState
  };
}

function reuseRecordIfShallowEqual<T>(
  current: Readonly<Record<string, T>>,
  next: Record<string, T>
): Record<string, T> {
  const currentKeys = Object.keys(current);
  const nextKeys = Object.keys(next);
  if (
    currentKeys.length === nextKeys.length &&
    nextKeys.every((key) => current[key] === next[key])
  ) {
    return current as Record<string, T>;
  }
  return next;
}

/**
 * Turn provenance is assigned once. Realtime and HTTP snapshots may arrive in
 * either order, so lifecycle refreshes must not reclassify an observed Turn or
 * erase source fields omitted by a later payload. An absent optional source
 * field may be completed once, but an explicit null/value is immutable.
 */
function preserveTurnProvenance(
  current: AgentActivityTurn,
  incoming: AgentActivityTurn
): AgentActivityTurn {
  let sourceGoalOperationId = current.sourceGoalOperationId;
  let sourceGoalRevision = current.sourceGoalRevision;
  let sourceGoalRepairEpoch = current.sourceGoalRepairEpoch;
  // Historical provenance is intentionally opaque and must never be filled
  // from a later lifecycle payload.
  const canCompleteGoalSource =
    current.origin !== "legacy_unknown" &&
    (current.origin === "goal_arm" || current.origin === "goal_continuation") &&
    incoming.origin === current.origin;
  if (canCompleteGoalSource) {
    if (sourceGoalOperationId === undefined) {
      sourceGoalOperationId = incoming.sourceGoalOperationId;
    }
    if (sourceGoalRevision === undefined) {
      sourceGoalRevision = incoming.sourceGoalRevision;
    }
    if (sourceGoalRepairEpoch === undefined) {
      sourceGoalRepairEpoch = incoming.sourceGoalRepairEpoch;
    }
  }
  const next = { ...incoming, origin: current.origin };
  if (sourceGoalOperationId === undefined) {
    delete next.sourceGoalOperationId;
  } else {
    next.sourceGoalOperationId = sourceGoalOperationId;
  }
  if (sourceGoalRevision === undefined) {
    delete next.sourceGoalRevision;
  } else {
    next.sourceGoalRevision = sourceGoalRevision;
  }
  if (sourceGoalRepairEpoch === undefined) {
    delete next.sourceGoalRepairEpoch;
  } else {
    next.sourceGoalRepairEpoch = sourceGoalRepairEpoch;
  }
  return next;
}

function shouldUseIncomingSession(
  current: CanonicalAgentSession | undefined,
  incoming: CanonicalAgentSession
): boolean {
  return !current || sessionVersion(incoming) >= sessionVersion(current);
}

function preserveCanonicalActiveTurnProjection(
  state: SessionLifecycleState,
  current: CanonicalAgentSession | undefined,
  incoming: CanonicalAgentSession,
  source: AgentActivitySessionInput
): CanonicalAgentSession {
  const currentActiveTurnId = current?.activeTurnId?.trim() ?? null;
  const incomingActiveTurnId = incoming.activeTurnId?.trim() ?? null;
  if (incomingActiveTurnId) {
    const cachedIncomingTurn =
      state.turnsById[
        canonicalTurnKey(incoming.agentSessionId, incomingActiveTurnId)
      ];
    if (cachedIncomingTurn?.phase === "settled") {
      return {
        ...incoming,
        activeTurnId:
          currentActiveTurnId === incomingActiveTurnId
            ? null
            : currentActiveTurnId
      };
    }
  }
  if (currentActiveTurnId === incomingActiveTurnId) {
    return incoming;
  }
  if (!current || !currentActiveTurnId) return incoming;

  const currentTurn =
    state.turnsById[
      canonicalTurnKey(incoming.agentSessionId, currentActiveTurnId)
    ];
  const currentVersion = Math.max(
    sessionVersion(current),
    currentTurn?.updatedAtUnixMs ?? 0
  );
  const incomingVersion = Math.max(
    sessionVersion(incoming),
    ...[source.activeTurn, source.latestTurn]
      .filter((turn): turn is AgentActivityTurn => {
        if (!turn) return false;
        return (
          turn.agentSessionId.trim() === incoming.agentSessionId &&
          (incomingActiveTurnId
            ? turn.turnId.trim() === incomingActiveTurnId &&
              turn.phase !== "settled"
            : turn.phase === "settled" &&
              turn.turnId.trim() === currentActiveTurnId)
        );
      })
      .map((turn) => turn.updatedAtUnixMs)
  );
  return incomingVersion > currentVersion
    ? incoming
    : { ...incoming, activeTurnId: currentActiveTurnId };
}

function preserveProjectedSessionState(
  current: CanonicalAgentSession | undefined,
  incoming: CanonicalAgentSession
): CanonicalAgentSession {
  const messageVersion = Math.max(
    current?.messageVersion ?? 0,
    incoming.messageVersion ?? 0
  );
  const preserved = {
    ...incoming,
    ...(messageVersion === (incoming.messageVersion ?? 0)
      ? {}
      : { messageVersion }),
  };
  if (
    current?.lifecycleCapabilitiesProjected === true &&
    incoming.lifecycleCapabilitiesProjected !== true
  ) {
    return {
      ...preserved,
      lifecycleCapabilities: current.lifecycleCapabilities,
      lifecycleCapabilitiesProjected: true
    };
  }
  return preserved;
}

function sessionVersion(session: CanonicalAgentSession): number {
  return (
    session.updatedAtUnixMs ??
    session.lastEventUnixMs ??
    session.messageVersion ??
    session.createdAtUnixMs ??
    session.startedAtUnixMs ??
    0
  );
}
