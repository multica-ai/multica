import type {
  AgentActivityDurableMessage,
  AgentActivityInteraction,
  AgentActivitySession,
  AgentActivityTurn
} from "../types.ts";
import type { AgentActivitySessionInput } from "../sessionNormalization.ts";

export interface SessionOperationState {
  runtimeAvailability: SessionRuntimeAvailability;
  runtimeActivity: SessionRuntimeActivity;
  runtimeActivityOccurredAtUnixMs: number;
  operationError: string | null;
}

export type SessionRuntimeActivity = "idle" | "running";

/**
 * Host-projected, session-scoped availability for commands that must reach the
 * session runtime. This is intentionally separate from the canonical Session:
 * transport reachability, exact-target Agent capabilities, and shared access
 * are ephemeral and may differ between Sessions sharing one workspace engine.
 */
export type SessionRuntimeAvailability =
  | { state: "available" }
  | {
      state: "blocked";
      reason:
        | "agent_capability_checking"
        | "agent_capability_unavailable"
        | "transport_reconnecting"
        | "transport_unavailable";
    }
  | {
      state: "blocked";
      reason: "agent_sharing_revoked";
      ownerLabel: string;
    };

export type CanonicalAgentSession = Omit<
  AgentActivitySession,
  "activeTurn" | "latestTurn" | "latestTurnInteractions" | "pendingInteractions"
> & {
  activeTurnId: string | null;
};

export interface SessionLifecycleState {
  deletedSessionIds: Readonly<Record<string, true>>;
  interactionsById: Readonly<Record<string, AgentActivityInteraction>>;
  operationBySessionId: Readonly<Record<string, SessionOperationState>>;
  sessionsById: Readonly<Record<string, CanonicalAgentSession>>;
  turnsById: Readonly<Record<string, AgentActivityTurn>>;
}

export interface SessionSnapshotReceivedIntent {
  type: "session/snapshotReceived";
  sessions: readonly AgentActivitySessionInput[];
  observedAtUnixMs?: number;
  /** Session ids filtered at the Engine identity boundary for wrong scope. */
  workspaceMismatchSessionIds?: readonly string[];
}

export interface SessionUpsertedIntent {
  type: "session/upserted";
  session: AgentActivitySessionInput;
  observedAtUnixMs?: number;
}

export type CanonicalSessionMetadataPatch = Partial<
  Pick<
    CanonicalAgentSession,
    | "cwd"
    | "pinnedAtUnixMs"
    | "resumable"
    | "title"
    | "updatedAtUnixMs"
  >
>;

export interface SessionMetadataPatchedIntent {
  type: "session/metadataPatched";
  agentSessionId: string;
  patch: CanonicalSessionMetadataPatch;
}

export interface TurnUpsertedIntent {
  type: "turn/upserted";
  /**
   * Whether this upsert is a live observation capable of creating attention.
   * Historical detail hydration uses false while preserving the same
   * canonical lifecycle write. Omission remains compatible with older hosts
   * and is treated as a live observation.
   */
  live?: boolean;
  turn: AgentActivityTurn;
}

/**
 * One authoritative realtime Turn projection. The Turn and the Session's
 * active-turn reference are one wire fact and must enter the Engine atomically.
 */
export interface TurnProjectionReceivedIntent {
  type: "turn/projectionReceived";
  activeTurnId: string | null;
  /**
   * The host has already fenced transport identity and ordering, so settlement
   * of this immutable Turn may absorb a temporary projection from another
   * version domain even when its wall-clock timestamp is lower.
   */
  hostFencedSameTurnSettlement?: true;
  turn: AgentActivityTurn;
  workspaceId: string;
}

export interface SessionHistoryAuthoritativeSnapshotReceivedIntent {
  type: "session/historyAuthoritativeSnapshotReceived";
  agentSessionId: string;
  childSessions: readonly AgentActivitySessionInput[];
  historyRevision: number;
  session: AgentActivitySessionInput;
  liveTurnId?: string;
  turns: readonly AgentActivityTurn[];
  workspaceId: string;
}

export interface InteractionUpsertedIntent {
  type: "interaction/upserted";
  interaction: AgentActivityInteraction;
}

export interface SessionRemovedIntent {
  type: "session/removed";
  agentSessionId: string;
}

export interface SessionRestoredIntent {
  type: "session/restored";
  agentSessionId: string;
}

export interface SessionErrorRecordedIntent {
  type: "session/errorRecorded";
  agentSessionId: string;
  errorMessage: string;
}

export interface SessionErrorClearedIntent {
  type: "session/errorCleared";
  agentSessionId: string;
}

/** Holds what the engine's queue and activations would still start. */
export interface SessionStopRequestedIntent {
  type: "session/stopRequested";
  agentSessionId: string;
  workspaceId: string;
}

export interface SessionRuntimeAvailabilityChangedIntent {
  type: "session/runtimeAvailabilityChanged";
  agentSessionId: string;
  availability: SessionRuntimeAvailability;
}

export interface SessionRuntimeActivityChangedIntent {
  type: "session/runtimeActivityChanged";
  agentSessionId: string;
  state: SessionRuntimeActivity;
  /** Zero clears the disconnected transport's transient observation and fence. */
  occurredAtUnixMs: number;
}

export type SessionLifecycleIntent =
  | InteractionUpsertedIntent
  | SessionErrorClearedIntent
  | SessionErrorRecordedIntent
  | SessionHistoryAuthoritativeSnapshotReceivedIntent
  | SessionMetadataPatchedIntent
  | SessionRemovedIntent
  | SessionRestoredIntent
  | SessionRuntimeActivityChangedIntent
  | SessionRuntimeAvailabilityChangedIntent
  | SessionSnapshotReceivedIntent
  | SessionStopRequestedIntent
  | SessionUpsertedIntent
  | TurnProjectionReceivedIntent
  | TurnUpsertedIntent;
