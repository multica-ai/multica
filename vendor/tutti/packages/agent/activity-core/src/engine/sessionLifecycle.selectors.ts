import type { AgentActivityInteraction, AgentActivityTurn } from "../types.ts";
import type { AgentActivityDisplayStatus } from "../displayStatus.types.ts";
import type {
  AgentSessionEngineState,
  AgentSessionEngineStateBase
} from "./types.ts";
import type {
  CanonicalAgentSession,
  SessionOperationState
} from "./sessionLifecycle.types.ts";
import {
  canonicalInteractionKey,
  canonicalTurnKey
} from "./sessionEntityKeys.ts";
import {
  compareTurnsByOccurrence,
  latestTurnForSession
} from "./sessionTurnOrdering.ts";
import { selectLatestActivationForSession } from "./pendingIntents.selectors.ts";
import { isPendingActivationViable } from "./pendingIntents.types.ts";
import {
  deriveCanonicalSubmitAvailability,
  type CanonicalSubmitAvailability
} from "./sessionLifecycle.availability.ts";

export interface WorkspaceAgentConsumerSession {
  activeTurn: AgentActivityTurn | null;
  displayStatus: AgentActivityDisplayStatus;
  latestTurn: AgentActivityTurn | null;
  pendingInteractions: readonly AgentActivityInteraction[];
  session: CanonicalAgentSession;
}

export interface WorkspaceAgentConsumerCounts {
  canceled: number;
  completed: number;
  failed: number;
  idle: number;
  waiting: number;
  working: number;
}

export interface EngineSubmitAvailability {
  reason?: Exclude<CanonicalSubmitAvailability["reason"], undefined>;
  state: "available" | "blocked";
}

/**
 * A failed new-session activation is only a missing session when the
 * canonical session entity is absent. The runtime may be unavailable while
 * the session remains a durable, selectable conversation.
 */
export type FailedNewActivationResolution =
  | "not-applicable"
  | "preserve"
  | "rollback";

export function selectFailedNewActivationResolution(
  state: AgentSessionEngineStateBase,
  agentSessionId: string | null | undefined,
  options?: { selectionSource?: "activation" | "user-selection" }
): FailedNewActivationResolution {
  if (options?.selectionSource === "user-selection") {
    return "not-applicable";
  }
  const activation = selectLatestActivationForSession(state, agentSessionId);
  if (activation?.mode !== "new" || isPendingActivationViable(activation)) {
    return "not-applicable";
  }
  return state.sessionLifecycle?.sessionsById &&
    selectEngineSession(state, agentSessionId)
    ? "preserve"
    : "rollback";
}

/**
 * Session selection may reload after a failed or canceled activation. Only an
 * activation whose delivery is still pending/uncertain must wait for an
 * authoritative result before starting detail hydration.
 */
export function selectEngineSessionCanReload(
  state: AgentSessionEngineStateBase,
  agentSessionId: string | null | undefined
): boolean {
  const activation = selectLatestActivationForSession(state, agentSessionId);
  return (
    activation?.status !== "requested" && activation?.status !== "uncertain"
  );
}

export function selectEngineSessionRuntimeAvailability(
  state: AgentSessionEngineStateBase,
  agentSessionId: string | null | undefined
) {
  const id = agentSessionId?.trim() ?? "";
  return (
    state.sessionLifecycle.operationBySessionId[id]?.runtimeAvailability ?? null
  );
}

export function selectEngineSessionRuntimeActivity(
  state: AgentSessionEngineStateBase,
  agentSessionId: string | null | undefined
) {
  const id = agentSessionId?.trim() ?? "";
  return (
    state.sessionLifecycle.operationBySessionId[id]?.runtimeActivity ?? "idle"
  );
}

const EMPTY_CONSUMER_COUNTS: WorkspaceAgentConsumerCounts = {
  canceled: 0,
  completed: 0,
  failed: 0,
  idle: 0,
  waiting: 0,
  working: 0
};

export function selectEngineSession(
  state: AgentSessionEngineStateBase,
  agentSessionId: string | null | undefined
): CanonicalAgentSession | null {
  const id = agentSessionId?.trim() ?? "";
  if (!state.sessionLifecycle.sessionsById[id]) return null;
  return state.sessionLifecycle.sessionsById[id] ?? null;
}

export function selectEngineSessionDeleted(
  state: AgentSessionEngineStateBase,
  agentSessionId: string | null | undefined
): boolean {
  const id = agentSessionId?.trim() ?? "";
  return Boolean(id && state.sessionLifecycle.deletedSessionIds[id]);
}

export function selectEngineTurnsForSession(
  state: AgentSessionEngineStateBase,
  agentSessionId: string | null | undefined
): readonly AgentActivityTurn[] {
  const id = agentSessionId?.trim() ?? "";
  if (!state.sessionLifecycle.sessionsById[id]) return [];
  return Object.values(state.sessionLifecycle.turnsById)
    .filter((turn) => turn.agentSessionId === id)
    .sort(compareTurnsByOccurrence);
}

export function selectEngineActiveTurn(
  state: AgentSessionEngineStateBase,
  agentSessionId: string | null | undefined
): AgentActivityTurn | null {
  const session = selectEngineSession(state, agentSessionId);
  return session?.activeTurnId
    ? (state.sessionLifecycle.turnsById[
        canonicalTurnKey(session.agentSessionId, session.activeTurnId)
      ] ?? null)
    : null;
}

export function selectEngineTurn(
  state: AgentSessionEngineStateBase,
  agentSessionId: string | null | undefined,
  turnId: string | null | undefined
): AgentActivityTurn | null {
  const sessionId = agentSessionId?.trim() ?? "";
  const id = turnId?.trim() ?? "";
  return sessionId && id && state.sessionLifecycle.sessionsById[sessionId]
    ? (state.sessionLifecycle.turnsById[canonicalTurnKey(sessionId, id)] ??
        null)
    : null;
}

export function selectEngineInteraction(
  state: AgentSessionEngineStateBase,
  agentSessionId: string | null | undefined,
  turnId: string | null | undefined,
  requestId: string | null | undefined
): AgentActivityInteraction | null {
  const sessionId = agentSessionId?.trim() ?? "";
  const turn = turnId?.trim() ?? "";
  const id = requestId?.trim() ?? "";
  if (
    !sessionId ||
    !turn ||
    !id ||
    !state.sessionLifecycle.sessionsById[sessionId]
  ) {
    return null;
  }
  const interaction =
    state.sessionLifecycle.interactionsById[
      canonicalInteractionKey(sessionId, turn, id)
    ];
  return interaction &&
    state.sessionLifecycle.turnsById[
      canonicalTurnKey(sessionId, interaction.turnId)
    ]
    ? interaction
    : null;
}


export function selectEngineLatestTurn(
  state: AgentSessionEngineStateBase,
  agentSessionId: string | null | undefined
): AgentActivityTurn | null {
  const id = agentSessionId?.trim() ?? "";
  if (!state.sessionLifecycle.sessionsById[id]) return null;
  return latestTurnForSession(state.sessionLifecycle.turnsById, id);
}

export function selectEngineInteractionsForSession(
  state: AgentSessionEngineStateBase,
  agentSessionId: string | null | undefined
): readonly AgentActivityInteraction[] {
  const id = agentSessionId?.trim() ?? "";
  if (!state.sessionLifecycle.sessionsById[id]) return [];
  return Object.values(state.sessionLifecycle.interactionsById)
    .filter(
      (interaction) =>
        interaction.agentSessionId === id &&
        Boolean(
          state.sessionLifecycle.turnsById[
            canonicalTurnKey(id, interaction.turnId)
          ]
        )
    )
    .sort(
      (left, right) =>
        left.createdAtUnixMs - right.createdAtUnixMs ||
        left.requestId.localeCompare(right.requestId)
    );
}

export function selectEnginePendingInteractions(
  state: AgentSessionEngineStateBase,
  agentSessionId: string | null | undefined
): readonly AgentActivityInteraction[] {
  return selectEngineInteractionsForSession(state, agentSessionId).filter(
    (interaction) => interaction.status === "pending"
  );
}

export function selectEngineSubmitAvailability(
  state: AgentSessionEngineStateBase,
  agentSessionId: string | null | undefined
): EngineSubmitAvailability | null {
  const availability = deriveCanonicalSubmitAvailability(
    state.sessionLifecycle,
    agentSessionId
  );
  if (availability.state === "missing") return null;
  return availability.reason
    ? { state: availability.state, reason: availability.reason }
    : { state: availability.state };
}

export function selectEngineSessionOperation(
  state: AgentSessionEngineStateBase,
  agentSessionId: string | null | undefined
): SessionOperationState | null {
  const id = agentSessionId?.trim() ?? "";
  return state.sessionLifecycle.operationBySessionId[id] ?? null;
}

export function selectEngineHasPendingInteractions(
  state: AgentSessionEngineStateBase,
  agentSessionId: string | null | undefined
): boolean {
  return selectEnginePendingInteractions(state, agentSessionId).length > 0;
}

/**
 * Returns only failures from session-scoped commands. Canonical Turn failures
 * belong to the owning Turn and must not be promoted into session chrome.
 */
export function selectEngineSessionOperationError(
  state: AgentSessionEngineStateBase,
  agentSessionId: string | null | undefined
): string | null {
  const id = agentSessionId?.trim() ?? "";
  return (
    state.sessionLifecycle.operationBySessionId[id]?.operationError ?? null
  );
}

export function selectWorkspaceAgentConsumerSessions(
  state: AgentSessionEngineStateBase
): readonly WorkspaceAgentConsumerSession[] {
  return selectAllWorkspaceAgentConsumerSessions(state).filter(
    (item) => item.session.kind === "root" && item.session.visible !== false
  );
}

export function selectRootAgentSessionIdsWithPendingInteractions(
  state: AgentSessionEngineStateBase
): readonly string[] {
  const sessionIdsWithPendingInteractions = new Set<string>();
  for (const interaction of Object.values(
    state.sessionLifecycle.interactionsById
  )) {
    if (
      interaction.status === "pending" &&
      state.sessionLifecycle.turnsById[
        canonicalTurnKey(interaction.agentSessionId, interaction.turnId)
      ]
    ) {
      sessionIdsWithPendingInteractions.add(interaction.agentSessionId);
    }
  }
  const rootAgentSessionIds = new Set<string>();
  for (const session of Object.values(state.sessionLifecycle.sessionsById)) {
    if (!sessionIdsWithPendingInteractions.has(session.agentSessionId)) {
      continue;
    }
    const rootAgentSessionId =
      session.kind === "child"
        ? session.rootAgentSessionId
        : session.agentSessionId;
    if (rootAgentSessionId) rootAgentSessionIds.add(rootAgentSessionId);
  }
  return [...rootAgentSessionIds];
}

export function selectWorkspaceAgentRootConversationSessions(
  state: AgentSessionEngineStateBase
): readonly WorkspaceAgentConsumerSession[] {
  const consumers = selectAllWorkspaceAgentConsumerSessions(state);
  const rootSessionIds = new Set(
    consumers
      .filter((consumer) => consumer.session.kind === "root")
      .map((consumer) => consumer.session.agentSessionId)
  );
  const consumersByRootSessionId = new Map<
    string,
    WorkspaceAgentConsumerSession[]
  >();

  for (const consumer of consumers) {
    const rootSessionId =
      consumer.session.kind === "child"
        ? (consumer.session.rootAgentSessionId?.trim() ?? "")
        : consumer.session.agentSessionId;
    if (!rootSessionIds.has(rootSessionId)) continue;
    const conversationConsumers =
      consumersByRootSessionId.get(rootSessionId) ?? [];
    conversationConsumers.push(consumer);
    consumersByRootSessionId.set(rootSessionId, conversationConsumers);
  }

  return consumers
    .filter((consumer) => consumer.session.kind === "root")
    .map((consumer) => {
      const conversationConsumers =
        consumersByRootSessionId.get(consumer.session.agentSessionId) ?? [];
      const pendingInteractions = conversationConsumers
        .flatMap((item) => item.pendingInteractions)
        .sort(compareInteractionsByOccurrence);
      return {
        ...consumer,
        displayStatus: rootConversationDisplayStatus(
          consumer,
          conversationConsumers,
          pendingInteractions
        ),
        pendingInteractions
      };
    });
}

export function selectAllWorkspaceAgentConsumerSessions(
  state: AgentSessionEngineStateBase
): readonly WorkspaceAgentConsumerSession[] {
  const latestTurnsBySessionId = new Map<string, AgentActivityTurn>();
  for (const turn of Object.values(state.sessionLifecycle.turnsById)) {
    if (!state.sessionLifecycle.sessionsById[turn.agentSessionId]) continue;
    const latestTurn = latestTurnsBySessionId.get(turn.agentSessionId);
    if (!latestTurn || compareTurnsByOccurrence(latestTurn, turn) < 0) {
      latestTurnsBySessionId.set(turn.agentSessionId, turn);
    }
  }

  const pendingInteractionsBySessionId = new Map<
    string,
    AgentActivityInteraction[]
  >();
  for (const interaction of Object.values(
    state.sessionLifecycle.interactionsById
  )) {
    if (
      interaction.status !== "pending" ||
      !state.sessionLifecycle.sessionsById[interaction.agentSessionId] ||
      !state.sessionLifecycle.turnsById[
        canonicalTurnKey(interaction.agentSessionId, interaction.turnId)
      ]
    ) {
      continue;
    }
    const pendingInteractions =
      pendingInteractionsBySessionId.get(interaction.agentSessionId) ?? [];
    pendingInteractions.push(interaction);
    pendingInteractionsBySessionId.set(
      interaction.agentSessionId,
      pendingInteractions
    );
  }
  for (const pendingInteractions of pendingInteractionsBySessionId.values()) {
    pendingInteractions.sort(compareSessionInteractionsByOccurrence);
  }

  return Object.values(state.sessionLifecycle.sessionsById).map((session) => {
    const activeTurn = session.activeTurnId
      ? (state.sessionLifecycle.turnsById[
          canonicalTurnKey(session.agentSessionId, session.activeTurnId)
        ] ?? null)
      : null;
    const latestTurn =
      latestTurnsBySessionId.get(session.agentSessionId) ?? null;
    const pendingInteractions =
      pendingInteractionsBySessionId.get(session.agentSessionId) ?? [];
    return {
      activeTurn,
      displayStatus: displayStatusFromCanonicalState({
        activeTurn,
        latestTurn,
        pendingInteractions,
        runtimeActivity:
          state.sessionLifecycle.operationBySessionId[session.agentSessionId] ??
          null
      }),
      latestTurn,
      pendingInteractions,
      session
    };
  });
}

export function selectWorkspaceAgentConsumerSession(
  state: AgentSessionEngineStateBase,
  agentSessionId: string | null | undefined
): WorkspaceAgentConsumerSession | null {
  const id = agentSessionId?.trim() ?? "";
  const session = state.sessionLifecycle.sessionsById[id];
  if (!session) return null;
  const activeTurn = selectEngineActiveTurn(state, id);
  const latestTurn = selectEngineLatestTurn(state, id);
  const pendingInteractions = selectEnginePendingInteractions(state, id);
  return {
    activeTurn,
    displayStatus: displayStatusFromCanonicalState({
      activeTurn,
      latestTurn,
      pendingInteractions,
      runtimeActivity: state.sessionLifecycle.operationBySessionId[id] ?? null
    }),
    latestTurn,
    pendingInteractions,
    session
  };
}

export function selectWorkspaceAgentConsumerCounts(
  state: AgentSessionEngineStateBase
): WorkspaceAgentConsumerCounts {
  return selectWorkspaceAgentConsumerSessions(state).reduce(
    (counts, item) => {
      counts[item.displayStatus] += 1;
      return counts;
    },
    { ...EMPTY_CONSUMER_COUNTS }
  );
}

function displayStatusFromCanonicalState(state: {
  activeTurn: AgentActivityTurn | null;
  latestTurn: AgentActivityTurn | null;
  pendingInteractions: readonly AgentActivityInteraction[];
  runtimeActivity: SessionOperationState | null;
}): AgentActivityDisplayStatus {
  if (state.pendingInteractions.length > 0) return "waiting";
  if (state.activeTurn && state.activeTurn.phase !== "settled") {
    return state.activeTurn.phase === "waiting" ? "waiting" : "working";
  }
  if (runtimeActivityCanOverrideCanonicalTurn(state)) return "working";
  if (!state.latestTurn) return "idle";
  if (state.latestTurn.phase !== "settled") return "idle";
  switch (state.latestTurn.outcome) {
    case "failed":
      return "failed";
    case "canceled":
    case "interrupted":
      return "canceled";
    case "completed":
      return "completed";
    default:
      return "idle";
  }
}

function runtimeActivityCanOverrideCanonicalTurn(state: {
  latestTurn: AgentActivityTurn | null;
  runtimeActivity: SessionOperationState | null;
}): boolean {
  if (state.runtimeActivity?.runtimeActivity !== "running") return false;
  if (!state.latestTurn || state.latestTurn.phase !== "settled") return true;
  const canonicalTerminalAtUnixMs = Math.max(
    state.latestTurn.updatedAtUnixMs,
    state.latestTurn.settledAtUnixMs ?? 0
  );
  return (
    state.runtimeActivity.runtimeActivityOccurredAtUnixMs >
    canonicalTerminalAtUnixMs
  );
}

function rootConversationDisplayStatus(
  root: WorkspaceAgentConsumerSession,
  consumers: readonly WorkspaceAgentConsumerSession[],
  pendingInteractions: readonly AgentActivityInteraction[]
): AgentActivityDisplayStatus {
  if (pendingInteractions.length > 0) return "waiting";
  if (consumers.some((consumer) => consumer.displayStatus === "working")) {
    return "working";
  }
  if (consumers.some((consumer) => consumer.displayStatus === "waiting")) {
    return "waiting";
  }
  return root.displayStatus;
}

function compareInteractionsByOccurrence(
  left: AgentActivityInteraction,
  right: AgentActivityInteraction
): number {
  return (
    left.createdAtUnixMs - right.createdAtUnixMs ||
    left.updatedAtUnixMs - right.updatedAtUnixMs ||
    left.agentSessionId.localeCompare(right.agentSessionId) ||
    left.turnId.localeCompare(right.turnId) ||
    left.requestId.localeCompare(right.requestId)
  );
}

function compareSessionInteractionsByOccurrence(
  left: AgentActivityInteraction,
  right: AgentActivityInteraction
): number {
  return (
    left.createdAtUnixMs - right.createdAtUnixMs ||
    left.requestId.localeCompare(right.requestId)
  );
}
