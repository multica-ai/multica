import type { AgentActivityInteraction, AgentActivityTurn } from "../types.ts";
import { areJsonLikeValuesEqual } from "../merge.ts";
import { normalizeAgentActivitySession } from "../sessionNormalization.ts";
import type { AgentActivitySessionInput } from "../sessionNormalization.ts";
import type {
  CanonicalAgentSession,
  SessionLifecycleState
} from "./sessionLifecycle.types.ts";
import { initialOperation } from "./sessionLifecycle.state.ts";
import {
  canonicalInteractionKey,
  canonicalTurnKey
} from "./sessionEntityKeys.ts";

// A conversation has one source, which already orders what it reports, so the
// last write wins. Records that did not change keep their objects, because what
// is built from them is kept by identity.

function same<T>(current: T | undefined, next: T): T {
  return current !== undefined && areJsonLikeValuesEqual(current, next)
    ? current
    : next;
}

function withEntry<T>(
  record: Readonly<Record<string, T>>,
  key: string,
  value: T
): Readonly<Record<string, T>> {
  const next = same(record[key], value);
  return next === record[key] ? record : { ...record, [key]: next };
}

/** Writes a list of conversations. One missing from a list is not deleted: a list can be older than what is known. */
export function upsertCanonicalSessions(
  state: SessionLifecycleState,
  sessions: readonly AgentActivitySessionInput[]
): SessionLifecycleState {
  return sessions.reduce(upsertCanonicalSession, state);
}

export function upsertCanonicalSession(
  state: SessionLifecycleState,
  source: AgentActivitySessionInput
): SessionLifecycleState {
  const id = source.agentSessionId.trim();
  if (!id || state.deletedSessionIds[id]) return state;
  let turnsById = state.turnsById;
  for (const turn of [source.activeTurn, source.latestTurn]) {
    if (turn?.agentSessionId !== id) continue;
    turnsById = withEntry(
      turnsById,
      canonicalTurnKey(id, turn.turnId),
      turn
    );
  }
  const sessionsById = withEntry(state.sessionsById, id, canonicalSession(source));
  const operationBySessionId = state.operationBySessionId[id]
    ? state.operationBySessionId
    : { ...state.operationBySessionId, [id]: initialOperation() };
  return withInteractions(
    sessionsById === state.sessionsById &&
      turnsById === state.turnsById &&
      operationBySessionId === state.operationBySessionId
      ? state
      : { ...state, operationBySessionId, sessionsById, turnsById },
    id,
    source
  );
}

function withInteractions(
  state: SessionLifecycleState,
  id: string,
  source: AgentActivitySessionInput
): SessionLifecycleState {
  const incoming = new Map<string, AgentActivityInteraction>();
  for (const interaction of [
    ...source.latestTurnInteractions,
    ...source.pendingInteractions
  ]) {
    if (interaction.agentSessionId !== id) continue;
    incoming.set(
      canonicalInteractionKey(id, interaction.turnId, interaction.requestId),
      interaction
    );
  }
  let interactionsById = state.interactionsById;
  for (const [key, interaction] of Object.entries(state.interactionsById)) {
    // What is no longer pending and no longer reported has been dealt with.
    if (
      interaction.agentSessionId === id &&
      interaction.status === "pending" &&
      !incoming.has(key)
    ) {
      const { [key]: _removed, ...rest } = interactionsById;
      interactionsById = rest;
    }
  }
  for (const [key, interaction] of incoming) {
    interactionsById = withEntry(interactionsById, key, interaction);
  }
  return interactionsById === state.interactionsById
    ? state
    : { ...state, interactionsById };
}

/** A conversation's whole history: its turns are exactly these. */
export function replaceCanonicalSessionHistory(
  state: SessionLifecycleState,
  session: AgentActivitySessionInput,
  turns: readonly AgentActivityTurn[]
): SessionLifecycleState {
  const id = session.agentSessionId.trim();
  const upserted = upsertCanonicalSession(state, session);
  if (!id || upserted.deletedSessionIds[id]) return upserted;
  const incoming = new Map(
    turns
      .filter((turn) => turn.agentSessionId.trim() === id && turn.turnId.trim())
      .map((turn) => [canonicalTurnKey(id, turn.turnId), turn])
  );
  const turnsById: Record<string, AgentActivityTurn> = {};
  let changed = false;
  for (const [key, turn] of Object.entries(upserted.turnsById)) {
    if (turn.agentSessionId !== id) turnsById[key] = turn;
    else if (!incoming.has(key)) changed = true;
  }
  for (const [key, turn] of incoming) {
    const current = upserted.turnsById[key];
    turnsById[key] = same(current, turn);
    if (turnsById[key] !== current) changed = true;
  }
  const validTurnIds = new Set([...incoming.values()].map((turn) => turn.turnId));
  const interactionsById = Object.fromEntries(
    Object.entries(upserted.interactionsById).filter(
      ([, interaction]) =>
        interaction.agentSessionId !== id ||
        validTurnIds.has(interaction.turnId)
    )
  );
  const interactionsChanged =
    Object.keys(interactionsById).length !==
    Object.keys(upserted.interactionsById).length;
  if (!changed && !interactionsChanged) return upserted;
  return {
    ...upserted,
    ...(changed ? { turnsById } : {}),
    ...(interactionsChanged ? { interactionsById } : {})
  };
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
    deletedSessionIds: { ...state.deletedSessionIds, [agentSessionId]: true },
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
