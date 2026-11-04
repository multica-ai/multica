import type {
  EngineCommand,
  EngineCommandResultIntent,
  EngineIntent,
  EngineReducerResult
} from "./types.ts";
import type {
  SessionReconcileRecord,
  SessionReconcileState
} from "./sessionReconcile.types.ts";
import type { CanonicalAgentSession } from "./sessionLifecycle.types.ts";

const NO_COMMANDS: readonly EngineCommand[] = [];
// Keep the renderer recovery window aligned with the daemon's fixed 50ms
// streaming-report coalescer. They run in separate processes, so this is a
// shared timing contract rather than one shared timer.
const STREAMING_MESSAGE_COALESCE_WINDOW_MS = 50;

export function createInitialSessionReconcileState(): SessionReconcileState {
  return { nextCommandSequence: 1, recordsBySessionId: {} };
}

export function sessionReconcileReducer(
  state: SessionReconcileState,
  intent: EngineIntent,
  context: {
    deletedSessionIds: Readonly<Record<string, true>>;
    pendingNewSessionIds?: ReadonlySet<string>;
    sessionsById: Readonly<Record<string, CanonicalAgentSession>>;
    workspaceReconcileCommandId: string | null;
  } = {
    deletedSessionIds: {},
    pendingNewSessionIds: new Set<string>(),
    sessionsById: {},
    workspaceReconcileCommandId: null
  }
): EngineReducerResult<SessionReconcileState> {
  switch (intent.type) {
    case "session/detailSnapshotReceived":
      return receiveDetailSnapshot(state, intent);
    case "session/historyAuthoritativeSnapshotReceived":
      if (context.deletedSessionIds[intent.agentSessionId.trim()]) {
        return unchanged(state);
      }
      return receiveAuthoritativeHistorySnapshot(state, intent);
    case "session/historyRevisionObserved":
      if (context.deletedSessionIds[intent.agentSessionId.trim()]) {
        return unchanged(state);
      }
      return applyHistoryCheckpoint(state, intent);
    case "session/activityObserved":
      if (context.deletedSessionIds[intent.agentSessionId.trim()]) {
        return unchanged(state);
      }
      if (intent.inlineApplied && intent.terminalTurn !== true) {
        return unchanged(state);
      }
      return requestReconcile(state, {
        agentSessionId: intent.agentSessionId,
        deferMessages:
          intent.eventType === "message_update" &&
          intent.hasCachedSession &&
          intent.hasInlineMessages &&
          intent.terminalTurn !== true,
        needsMessages:
          intent.eventType === "message_update" ||
          intent.eventType === "session_audit" ||
          intent.eventType === "session_reconcile_required" ||
          intent.terminalTurn === true,
        needsState:
          !intent.hasCachedSession ||
          (intent.eventType !== "message_update" &&
            intent.eventType !== "session_audit") ||
          !intent.hasInlineMessages,
        live: intent.eventType === "turn_update",
        waitForSessionCommit: shouldWaitForSessionCommit(
          context,
          intent.agentSessionId
        ),
        workspaceId: intent.workspaceId
      });
    case "turn/projectionReceived":
      if (context.deletedSessionIds[intent.turn.agentSessionId.trim()]) {
        return unchanged(state);
      }
      return requestReconcile(state, {
        agentSessionId: intent.turn.agentSessionId,
        needsMessages: intent.turn.phase === "settled",
        needsState: true,
        live: true,
        waitForSessionCommit: shouldWaitForSessionCommit(
          context,
          intent.turn.agentSessionId
        ),
        workspaceId: intent.workspaceId
      });
    case "session/reconcileRequested":
      if (context.deletedSessionIds[intent.agentSessionId.trim()]) {
        return unchanged(state);
      }
      return requestReconcile(state, {
        ...intent,
        waitForSessionCommit: shouldWaitForSessionCommit(
          context,
          intent.agentSessionId
        )
      });
    case "session/upserted":
      return releaseReconcileAfterSessionCommit(
        state,
        intent.session.agentSessionId
      );
    case "session/removed":
      return removeRecord(state, intent.agentSessionId);
    case "engine/intentExpired":
      return expireStreamingMessageReconcile(state, intent.expiryId);
    case "engine/commandResult":
      if (
        intent.commandType === "engine/reconcileWorkspace" &&
        intent.outcome === "succeeded" &&
        intent.commandId === context.workspaceReconcileCommandId
      ) {
        return hydrateActiveRootSessions(state, context.sessionsById);
      }
      return intent.commandType === "session/reconcile"
        ? settleReconcile(state, intent)
        : unchanged(state);
    default:
      return unchanged(state);
  }
}

function receiveDetailSnapshot(
  state: SessionReconcileState,
  intent: Extract<EngineIntent, { type: "session/detailSnapshotReceived" }>,
  liveTurnId?: string
): EngineReducerResult<SessionReconcileState> {
  const followUpIntents: EngineIntent[] = [
    {
      session: intent.session,
      ...(intent.observedAtUnixMs === undefined
        ? {}
        : { observedAtUnixMs: intent.observedAtUnixMs }),
      type: "session/upserted"
    }
  ];
  if (intent.live && intent.session.latestTurn) {
    followUpIntents.push({
      live: true,
      turn: intent.session.latestTurn,
      type: "turn/upserted"
    });
  }
  followUpIntents.push(
    ...intent.turns.map(
      (turn): EngineIntent => ({ live: false, turn, type: "turn/upserted" })
    ),
    ...intent.childSessions.map(
      (session): EngineIntent => ({ session, type: "session/upserted" })
    )
  );
  if (intent.messages || intent.sessionMessageWindows) {
    followUpIntents.push({
      messages: intent.messages ?? [],
      ...(intent.sessionMessageWindows
        ? { sessionMessageWindows: intent.sessionMessageWindows }
        : {}),
      type: "message/snapshotReceived",
      workspaceId: intent.workspaceId
    });
  }
  return { commands: NO_COMMANDS, followUpIntents, state };
}

function receiveAuthoritativeHistorySnapshot(
  state: SessionReconcileState,
  intent: Extract<
    EngineIntent,
    { type: "session/historyAuthoritativeSnapshotReceived" }
  >
): EngineReducerResult<SessionReconcileState> {
  const detail = receiveDetailSnapshot(
    state,
    {
      childSessions: intent.childSessions,
      messages: intent.messages,
      session: intent.session,
      sessionMessageWindows: intent.sessionMessageWindows,
      turns: intent.turns,
      type: "session/detailSnapshotReceived",
      workspaceId: intent.workspaceId
    },
    intent.liveTurnId
  );
  const checkpoint = applyHistoryCheckpoint(
    state,
    {
      agentSessionId: intent.agentSessionId,
      historyRevision: intent.historyRevision,
      workspaceId: intent.workspaceId
    },
    true
  );
  return {
    commands: checkpoint.commands,
    followUpIntents: detail.followUpIntents,
    state: checkpoint.state
  };
}

function hydrateActiveRootSessions(
  state: SessionReconcileState,
  sessionsById: Readonly<Record<string, CanonicalAgentSession>>
): EngineReducerResult<SessionReconcileState> {
  let next = state;
  const commands: EngineCommand[] = [];
  const activeRoots = Object.values(sessionsById)
    .filter(
      (session) =>
        session.kind === "root" && Boolean(session.activeTurnId?.trim())
    )
    .sort((left, right) =>
      left.agentSessionId.localeCompare(right.agentSessionId)
    );

  for (const session of activeRoots) {
    const requested = requestReconcile(next, {
      agentSessionId: session.agentSessionId,
      needsMessages: false,
      needsState: true,
      live: false,
      workspaceId: session.workspaceId
    });
    next = requested.state;
    commands.push(...requested.commands);
  }

  return { commands, state: next };
}

function requestReconcile(
  state: SessionReconcileState,
  input: {
    agentSessionId: string;
    live?: boolean;
    authoritativeMessages?: boolean;
    deferMessages?: boolean;
    needsMessages: boolean;
    needsState: boolean;
    requiredHistoryRevision?: number;
    waitForSessionCommit?: boolean;
    workspaceId: string;
  }
): EngineReducerResult<SessionReconcileState> {
  const agentSessionId = input.agentSessionId.trim();
  const workspaceId = input.workspaceId.trim();
  const requiredHistoryRevision = normalizeHistoryRevision(
    input.requiredHistoryRevision
  );
  if (
    !agentSessionId ||
    !workspaceId ||
    (!input.needsMessages &&
      !input.needsState &&
      input.authoritativeMessages !== true &&
      requiredHistoryRevision === null)
  ) {
    return unchanged(state);
  }
  const current = state.recordsBySessionId[agentSessionId] ?? {
    agentSessionId,
    appliedHistoryRevision: null,
    authoritativeMessagesRequired: false,
    errorCode: null,
    errorMessage: null,
    inFlightCommandId: null,
    inFlightLive: false,
    inFlightScope: null,
    messageRefreshScheduled: false,
    messagesHydrated: false,
    pendingLive: false,
    pendingMessages: false,
    pendingMessagesImmediate: false,
    pendingState: false,
    requiredHistoryRevision: null,
    workspaceId
  };
  const authoritativeDemandArrivedWhileInFlight =
    current.inFlightCommandId !== null &&
    (input.authoritativeMessages === true ||
      (requiredHistoryRevision !== null &&
        requiredHistoryRevision >
          (current.requiredHistoryRevision ??
            current.appliedHistoryRevision ??
            -1)));
  const deferMessages =
    input.deferMessages === true &&
    input.needsMessages &&
    !input.needsState &&
    input.live !== true &&
    input.authoritativeMessages !== true &&
    requiredHistoryRevision === null &&
    !current.authoritativeMessagesRequired &&
    !historyRevisionIsPending(current);
  const pendingMessages =
    current.pendingMessages ||
    input.needsMessages ||
    authoritativeDemandArrivedWhileInFlight;
  const record = {
    ...current,
    authoritativeMessagesRequired:
      current.authoritativeMessagesRequired ||
      input.authoritativeMessages === true,
    errorCode: null,
    errorMessage: null,
    pendingLive: current.pendingLive || input.live === true,
    pendingMessages,
    pendingMessagesImmediate:
      pendingMessages &&
      (current.pendingMessagesImmediate ||
        (input.needsMessages && !deferMessages)),
    pendingState: current.pendingState || input.needsState,
    requiredHistoryRevision:
      requiredHistoryRevision === null
        ? current.requiredHistoryRevision
        : Math.max(
            current.requiredHistoryRevision ?? 0,
            requiredHistoryRevision
          )
  };
  const next = replaceRecord(state, record);
  if (input.waitForSessionCommit === true) {
    return { commands: NO_COMMANDS, state: next };
  }
  if (record.inFlightCommandId || !hasReconcileDemand(record)) {
    return { commands: NO_COMMANDS, state: next };
  }
  if (record.messageRefreshScheduled) {
    if (deferMessages) {
      return { commands: NO_COMMANDS, state: next };
    }
    const unscheduled = { ...record, messageRefreshScheduled: false };
    const started = startReconcile(
      replaceRecord(state, unscheduled),
      unscheduled
    );
    return {
      commands: [
        {
          expiryId: streamingMessageReconcileExpiryId(record.agentSessionId),
          type: "engine/cancelExpiry"
        },
        ...started.commands
      ],
      state: started.state
    };
  }
  return deferMessages
    ? scheduleStreamingMessageReconcile(next, record)
    : startReconcile(next, record);
}

function shouldWaitForSessionCommit(
  context: {
    pendingNewSessionIds?: ReadonlySet<string>;
    sessionsById: Readonly<Record<string, CanonicalAgentSession>>;
  },
  rawAgentSessionId: string
): boolean {
  const agentSessionId = rawAgentSessionId.trim();
  return (
    agentSessionId !== "" &&
    context.sessionsById[agentSessionId] === undefined &&
    context.pendingNewSessionIds?.has(agentSessionId) === true
  );
}

function releaseReconcileAfterSessionCommit(
  state: SessionReconcileState,
  rawAgentSessionId: string
): EngineReducerResult<SessionReconcileState> {
  const agentSessionId = rawAgentSessionId.trim();
  const record = state.recordsBySessionId[agentSessionId];
  if (
    !record ||
    record.inFlightCommandId !== null ||
    !hasReconcileDemand(record)
  ) {
    return unchanged(state);
  }
  const ready = record.messageRefreshScheduled
    ? { ...record, messageRefreshScheduled: false }
    : record;
  const started = startReconcile(replaceRecord(state, ready), ready);
  return {
    commands: [
      ...(record.messageRefreshScheduled
        ? [
            {
              expiryId: streamingMessageReconcileExpiryId(agentSessionId),
              type: "engine/cancelExpiry" as const
            }
          ]
        : []),
      ...started.commands
    ],
    state: started.state
  };
}

function settleReconcile(
  state: SessionReconcileState,
  intent: EngineCommandResultIntent
): EngineReducerResult<SessionReconcileState> {
  const record = Object.values(state.recordsBySessionId).find(
    (candidate) => candidate.inFlightCommandId === intent.commandId
  );
  if (!record) {
    return unchanged(state);
  }
  const settled = {
    ...record,
    errorCode:
      intent.outcome === "succeeded"
        ? null
        : intent.outcome === "timedOut"
          ? "timeout"
          : intent.errorCode?.trim() || null,
    errorMessage:
      intent.outcome === "succeeded"
        ? null
        : intent.errorMessage?.trim() ||
          (intent.outcome === "timedOut"
            ? "Session synchronization timed out."
            : null),
    inFlightCommandId: null,
    inFlightLive: false,
    inFlightScope: null,
    messagesHydrated:
      record.messagesHydrated ||
      (intent.outcome === "succeeded" &&
        (record.inFlightScope === "messages" ||
          record.inFlightScope === "state_and_messages")),
    pendingLive:
      record.pendingLive ||
      (intent.outcome !== "succeeded" && record.inFlightLive)
  };
  const next = replaceRecord(state, settled);
  const shouldContinue =
    settled.pendingMessages ||
    settled.pendingState ||
    (intent.outcome === "succeeded" &&
      (settled.authoritativeMessagesRequired ||
        historyRevisionIsPending(settled)));
  if (!shouldContinue) {
    return { commands: NO_COMMANDS, state: next };
  }
  return settled.pendingMessages &&
    !settled.pendingMessagesImmediate &&
    !settled.pendingState &&
    !settled.pendingLive &&
    !settled.authoritativeMessagesRequired &&
    !historyRevisionIsPending(settled)
    ? scheduleStreamingMessageReconcile(next, settled)
    : startReconcile(next, settled);
}

function expireStreamingMessageReconcile(
  state: SessionReconcileState,
  expiryId: string
): EngineReducerResult<SessionReconcileState> {
  const record = Object.values(state.recordsBySessionId).find(
    (candidate) =>
      candidate.messageRefreshScheduled &&
      streamingMessageReconcileExpiryId(candidate.agentSessionId) === expiryId
  );
  if (!record) return unchanged(state);
  const ready = { ...record, messageRefreshScheduled: false };
  const next = replaceRecord(state, ready);
  if (ready.inFlightCommandId || !hasReconcileDemand(ready)) {
    return { commands: NO_COMMANDS, state: next };
  }
  return startReconcile(next, ready);
}

function scheduleStreamingMessageReconcile(
  state: SessionReconcileState,
  record: SessionReconcileRecord
): EngineReducerResult<SessionReconcileState> {
  if (record.messageRefreshScheduled || record.inFlightCommandId) {
    return { commands: NO_COMMANDS, state };
  }
  const scheduled = { ...record, messageRefreshScheduled: true };
  return {
    commands: [
      {
        delayMs: STREAMING_MESSAGE_COALESCE_WINDOW_MS,
        expiryId: streamingMessageReconcileExpiryId(record.agentSessionId),
        type: "engine/scheduleExpiryAfter"
      }
    ],
    state: replaceRecord(state, scheduled)
  };
}

function streamingMessageReconcileExpiryId(agentSessionId: string): string {
  return `session:streaming-message-reconcile:${agentSessionId}`;
}

function hasReconcileDemand(record: SessionReconcileRecord): boolean {
  return (
    record.pendingMessages ||
    record.pendingState ||
    record.authoritativeMessagesRequired ||
    historyRevisionIsPending(record)
  );
}

function startReconcile(
  state: SessionReconcileState,
  record: SessionReconcileRecord
): EngineReducerResult<SessionReconcileState> {
  const needsState = record.pendingState || record.pendingLive;
  const requiresAuthoritativeMessages =
    record.authoritativeMessagesRequired || historyRevisionIsPending(record);
  const scope = requiresAuthoritativeMessages
    ? "state_and_messages"
    : needsState
      ? record.pendingMessages
        ? "state_and_messages"
        : "state"
      : "messages";
  const commandId = `session:reconcile:${record.agentSessionId}:${state.nextCommandSequence}`;
  const live = record.pendingLive;
  return {
    commands: [
      {
        agentSessionId: record.agentSessionId,
        ...(requiresAuthoritativeMessages
          ? { authoritativeMessages: true }
          : {}),
        commandId,
        live,
        ...(record.requiredHistoryRevision === null
          ? {}
          : { requiredHistoryRevision: record.requiredHistoryRevision }),
        scope,
        timeoutMs: 30_000,
        type: "session/reconcile",
        workspaceId: record.workspaceId
      }
    ],
    state: replaceRecord(
      { ...state, nextCommandSequence: state.nextCommandSequence + 1 },
      {
        ...record,
        errorCode: null,
        errorMessage: null,
        inFlightCommandId: commandId,
        inFlightLive: live,
        inFlightScope: scope,
        messageRefreshScheduled: false,
        pendingLive: false,
        pendingMessages: false,
        pendingMessagesImmediate: false,
        pendingState: false
      }
    )
  };
}

function applyHistoryCheckpoint(
  state: SessionReconcileState,
  input: {
    agentSessionId: string;
    historyRevision: number | undefined;
    workspaceId: string;
  },
  authoritative = false
): EngineReducerResult<SessionReconcileState> {
  const agentSessionId = input.agentSessionId.trim();
  const workspaceId = input.workspaceId.trim();
  const historyRevision = normalizeHistoryRevision(input.historyRevision);
  if (!agentSessionId || !workspaceId || historyRevision === null) {
    return unchanged(state);
  }
  const current = state.recordsBySessionId[agentSessionId] ?? {
    agentSessionId,
    appliedHistoryRevision: null,
    authoritativeMessagesRequired: false,
    errorCode: null,
    errorMessage: null,
    inFlightCommandId: null,
    inFlightLive: false,
    inFlightScope: null,
    messageRefreshScheduled: false,
    messagesHydrated: false,
    pendingLive: false,
    pendingMessages: false,
    pendingMessagesImmediate: false,
    pendingState: false,
    requiredHistoryRevision: null,
    workspaceId
  };
  if (current.workspaceId !== workspaceId) {
    return unchanged(state);
  }
  const appliedHistoryRevision = Math.max(
    current.appliedHistoryRevision ?? 0,
    historyRevision
  );
  const requiredHistoryRevisionRemainsPending =
    current.requiredHistoryRevision !== null &&
    appliedHistoryRevision < current.requiredHistoryRevision;
  const next = {
    ...current,
    appliedHistoryRevision,
    authoritativeMessagesRequired: authoritative
      ? requiredHistoryRevisionRemainsPending
      : current.authoritativeMessagesRequired,
    errorCode: null,
    errorMessage: null,
    pendingMessages:
      authoritative && !requiredHistoryRevisionRemainsPending
        ? false
        : current.pendingMessages,
    pendingMessagesImmediate:
      authoritative && !requiredHistoryRevisionRemainsPending
        ? false
        : current.pendingMessagesImmediate
  };
  return {
    commands: NO_COMMANDS,
    state: replaceRecord(state, next)
  };
}

function historyRevisionIsPending(record: SessionReconcileRecord): boolean {
  return (
    record.requiredHistoryRevision !== null &&
    (record.appliedHistoryRevision === null ||
      record.appliedHistoryRevision < record.requiredHistoryRevision)
  );
}

function normalizeHistoryRevision(value: number | undefined): number | null {
  return typeof value === "number" && Number.isSafeInteger(value) && value >= 0
    ? value
    : null;
}

function replaceRecord(
  state: SessionReconcileState,
  record: SessionReconcileRecord
): SessionReconcileState {
  return {
    ...state,
    recordsBySessionId: {
      ...state.recordsBySessionId,
      [record.agentSessionId]: record
    }
  };
}

function removeRecord(
  state: SessionReconcileState,
  rawAgentSessionId: string
): EngineReducerResult<SessionReconcileState> {
  const records = { ...state.recordsBySessionId };
  const agentSessionId = rawAgentSessionId.trim();
  const record = records[agentSessionId];
  if (!record) {
    return unchanged(state);
  }
  delete records[agentSessionId];
  return {
    commands: record.messageRefreshScheduled
      ? [
          {
            expiryId: streamingMessageReconcileExpiryId(agentSessionId),
            type: "engine/cancelExpiry"
          }
        ]
      : NO_COMMANDS,
    state: { ...state, recordsBySessionId: records }
  };
}

function unchanged(
  state: SessionReconcileState
): EngineReducerResult<SessionReconcileState> {
  return { commands: NO_COMMANDS, state };
}
