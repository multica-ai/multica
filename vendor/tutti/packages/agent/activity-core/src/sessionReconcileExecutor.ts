import type { AgentActivityAdapter } from "./adapter.ts";
import { createAgentActivitySnapshotProjector } from "./engine/agentActivitySnapshot.projector.ts";
import type {
  AgentActivitySessionDetailSnapshot,
  SessionReconcileCommand
} from "./engine/sessionReconcile.types.ts";
import type { AgentSessionEngine } from "./engine/types.ts";
import { mergeAgentActivityMessages } from "./merge.ts";
import {
  agentActivitySessionMessageWindowFromDescendingPage,
  loadAllAgentSessionMessages
} from "./pagination.ts";
import {
  assertMessagePageIdentity,
  conversationReconcileAfterVersion,
  detailSessionIds,
  fallbackSession,
  latestDurableMessageVersion,
  latestMessageVersion,
  normalizeRequiredIdentity,
  normalizeVersion,
  shouldRetrySessionDetailRead,
  withoutDeletedChildren,
  type ReconcileSessionIdentity
} from "./sessionReconcileExecutor.helpers.ts";
import type {
  AgentActivityDurableMessage,
  AgentActivityMessage,
  AgentActivityMessagePage,
  AgentActivitySession,
  AgentActivityTurn
} from "./types.ts";

const MESSAGE_PAGE_SIZE = 100;
const MAX_MESSAGE_PAGES = 1_000;
const MAX_SESSION_DETAIL_READ_ATTEMPTS = 2;

export type AgentActivityChildMessageHydration =
  | "requested_session"
  | "session_hierarchy";

export interface AgentActivitySessionReconcilePort {
  getSessionDetail(input: {
    agentSessionId: string;
    projection: "authoritative" | "message_hydration";
    signal?: AbortSignal;
    workspaceId: string;
  }): Promise<AgentActivitySessionDetailSnapshot>;
  listSessionMessages: AgentActivityAdapter["listSessionMessages"];
}

export type AgentActivitySessionReconcileResult =
  | {
      affectedSessionIds: readonly string[];
      appliedMessages: readonly AgentActivityDurableMessage[];
      session: AgentActivitySession | null;
      status: "applied";
    }
  | {
      reason: "session_deleted";
      status: "skipped";
    };

export interface AgentActivitySessionReconcileExecutor {
  execute(
    command: SessionReconcileCommand,
    options?: { signal?: AbortSignal }
  ): Promise<AgentActivitySessionReconcileResult>;
}

export type AgentActivitySessionReconcileTrace =
  | {
      agentSessionId: string;
      detail?: AgentActivitySessionDetailSnapshot;
      phase: "discovery" | "final" | "state";
      status: "requested" | "resolved";
      type: "detail";
    }
  | {
      afterVersion: number;
      agentSessionId: string;
      latestVersion?: number;
      messageCount?: number;
      messageVersion: number;
      requestedAgentSessionId: string;
      scope: "combined" | "messages";
      status: "requested" | "resolved" | "skipped";
      type: "messages";
    }
  | {
      detail: AgentActivitySessionDetailSnapshot;
      scope: "state" | "state_and_messages";
      status: "applied" | "applying";
      type: "detailApply";
    };

export interface CreateAgentActivitySessionReconcileExecutorInput {
  childMessageHydration: AgentActivityChildMessageHydration;
  engine: Pick<AgentSessionEngine, "dispatch" | "getSnapshot">;
  isAvailable?(): boolean;
  isSessionDeleted(agentSessionId: string): boolean;
  onTrace?(event: AgentActivitySessionReconcileTrace): void;
  port: AgentActivitySessionReconcilePort;
  reconcileAuthoritativeHistory(
    agentSessionId: string,
    canonicalMessages: readonly AgentActivityDurableMessage[],
    effectiveTurns: readonly AgentActivityTurn[]
  ): void;
  reconcileOptimisticMessages(agentSessionId: string): void;
  workspaceId: string;
}

interface ReconciledMessagePage {
  agentSessionId: string;
  page: AgentActivityMessagePage;
  startsAtNewestBoundary: boolean;
}

export function createAgentActivitySessionReconcileExecutor(
  input: CreateAgentActivitySessionReconcileExecutorInput
): AgentActivitySessionReconcileExecutor {
  const workspaceId = normalizeRequiredIdentity(
    input.workspaceId,
    "workspaceId"
  );
  const projectSnapshot = createAgentActivitySnapshotProjector(workspaceId);

  const readSnapshot = () => projectSnapshot(input.engine.getSnapshot());
  const shouldSkip = (agentSessionId: string, signal?: AbortSignal) =>
    input.isSessionDeleted(agentSessionId) || signal?.aborted === true;
  const skippedResult = (): Extract<
    AgentActivitySessionReconcileResult,
    { status: "skipped" }
  > => ({
    reason: "session_deleted",
    status: "skipped"
  });
  const throwIfAborted = (signal?: AbortSignal) => {
    if (signal?.aborted) {
      throw signal.reason ?? new Error("session reconcile aborted");
    }
  };
  const throwIfUnavailable = () => {
    if (input.isAvailable?.() === false) {
      throw new Error("session reconcile host is unavailable");
    }
  };
  const assertExecutionActive = (signal?: AbortSignal) => {
    throwIfAborted(signal);
    throwIfUnavailable();
  };

  const getDetail = async (
    agentSessionId: string,
    phase: "discovery" | "final" | "state",
    signal?: AbortSignal
  ): Promise<AgentActivitySessionDetailSnapshot> => {
    for (
      let attempt = 0;
      attempt < MAX_SESSION_DETAIL_READ_ATTEMPTS;
      attempt += 1
    ) {
      assertExecutionActive(signal);
      input.onTrace?.({
        agentSessionId,
        phase,
        status: "requested",
        type: "detail"
      });
      let detail: AgentActivitySessionDetailSnapshot;
      try {
        detail = await input.port.getSessionDetail({
          agentSessionId,
          projection:
            phase === "discovery" ? "message_hydration" : "authoritative",
          signal,
          workspaceId
        });
      } catch (error) {
        if (
          signal?.aborted ||
          !shouldRetrySessionDetailRead(error) ||
          attempt === MAX_SESSION_DETAIL_READ_ATTEMPTS - 1
        ) {
          throw error;
        }
        continue;
      }
      assertExecutionActive(signal);
      const expectedProjection =
        phase === "discovery" ? "message_hydration" : "authoritative";
      if (detail.projection !== expectedProjection) {
        throw new Error(
          `session reconcile detail projection mismatch: expected ${expectedProjection}, received ${detail.projection}`
        );
      }
      if (
        detail.lifecycleCapabilitiesProjected !==
        (expectedProjection === "authoritative")
      ) {
        throw new Error(
          `session reconcile lifecycle capability projection mismatch for ${expectedProjection} detail`
        );
      }
      if (detail.session.agentSessionId !== agentSessionId) {
        throw new Error(
          `session reconcile detail identity mismatch: expected ${agentSessionId}, received ${detail.session.agentSessionId}`
        );
      }
      input.onTrace?.({
        agentSessionId,
        detail,
        phase,
        status: "resolved",
        type: "detail"
      });
      return detail;
    }
    throw new Error("session reconcile detail retry exhausted");
  };

  const reconcilePage = async (
    session: ReconcileSessionIdentity,
    cached: readonly AgentActivityMessage[],
    messageWindowKnown: boolean,
    requestedAgentSessionId: string,
    scope: "combined" | "messages",
    signal?: AbortSignal
  ): Promise<ReconciledMessagePage | null> => {
    const agentSessionId = session.agentSessionId;
    assertExecutionActive(signal);
    if (shouldSkip(agentSessionId, signal)) return null;
    const childSession = session.kind === "child";
    const afterVersion = childSession
      ? latestDurableMessageVersion(cached)
      : conversationReconcileAfterVersion(cached);
    if (
      scope === "combined" &&
      childSession &&
      messageWindowKnown &&
      afterVersion >= session.messageVersion
    ) {
      input.onTrace?.({
        afterVersion,
        agentSessionId,
        messageVersion: session.messageVersion,
        requestedAgentSessionId,
        scope,
        status: "skipped",
        type: "messages"
      });
      return null;
    }
    input.onTrace?.({
      afterVersion,
      agentSessionId,
      messageVersion: session.messageVersion,
      requestedAgentSessionId,
      scope,
      status: "requested",
      type: "messages"
    });
    const startsAtNewestBoundary = !messageWindowKnown;
    const shouldLoadNewestPage =
      startsAtNewestBoundary || (!childSession && cached.length === 0);
    if (shouldLoadNewestPage) {
      const page = await input.port.listSessionMessages({
        agentSessionId,
        limit: MESSAGE_PAGE_SIZE,
        order: "desc",
        signal,
        workspaceId
      });
      assertExecutionActive(signal);
      assertMessagePageIdentity(page, workspaceId, agentSessionId);
      if (shouldSkip(agentSessionId, signal)) return null;
      input.onTrace?.({
        afterVersion,
        agentSessionId,
        latestVersion: page.latestVersion,
        messageCount: page.messages.length,
        messageVersion: session.messageVersion,
        requestedAgentSessionId,
        scope,
        status: "resolved",
        type: "messages"
      });
      return { agentSessionId, page, startsAtNewestBoundary };
    }

    let cursor = afterVersion;
    const messages: AgentActivityDurableMessage[] = [];
    for (let pageIndex = 0; pageIndex < MAX_MESSAGE_PAGES; pageIndex += 1) {
      const page = await input.port.listSessionMessages({
        afterVersion: cursor,
        agentSessionId,
        order: "asc",
        signal,
        workspaceId
      });
      assertExecutionActive(signal);
      assertMessagePageIdentity(page, workspaceId, agentSessionId);
      if (shouldSkip(agentSessionId, signal)) return null;
      messages.push(...page.messages);
      const nextCursor = Math.max(
        cursor,
        normalizeVersion(page.latestVersion),
        latestMessageVersion(page.messages)
      );
      if (!page.hasMore) {
        input.onTrace?.({
          afterVersion,
          agentSessionId,
          latestVersion: nextCursor,
          messageCount: messages.length,
          messageVersion: session.messageVersion,
          requestedAgentSessionId,
          scope,
          status: "resolved",
          type: "messages"
        });
        return {
          agentSessionId,
          page: {
            hasMore: false,
            latestVersion: nextCursor,
            messages
          },
          startsAtNewestBoundary
        };
      }
      if (nextCursor <= cursor) {
        throw new Error(
          `session reconcile pagination did not advance for ${agentSessionId}`
        );
      }
      cursor = nextCursor;
    }
    throw new Error(
      `session reconcile exceeded ${MAX_MESSAGE_PAGES} message pages for ${agentSessionId}`
    );
  };

  const reconcileAuthoritativePage = async (
    agentSessionId: string,
    signal?: AbortSignal
  ): Promise<AgentActivityMessagePage> => {
    const messages: AgentActivityDurableMessage[] = [];
    let anchorLatestVersion = 0;
    let beforeVersion: number | undefined;
    let latestVersion = 0;
    for (let pageIndex = 0; pageIndex < MAX_MESSAGE_PAGES; pageIndex += 1) {
      const page = await input.port.listSessionMessages({
        agentSessionId,
        beforeVersion,
        limit: MESSAGE_PAGE_SIZE,
        order: "desc",
        signal,
        workspaceId
      });
      assertExecutionActive(signal);
      assertMessagePageIdentity(page, workspaceId, agentSessionId);
      if (shouldSkip(agentSessionId, signal)) {
        return { hasMore: false, latestVersion, messages: [] };
      }
      if (pageIndex === 0) {
        anchorLatestVersion = normalizeVersion(page.latestVersion);
      }
      latestVersion = Math.max(
        latestVersion,
        normalizeVersion(page.latestVersion)
      );
      messages.push(...page.messages);
      if (!page.hasMore || page.messages.length === 0) break;
      if (pageIndex === MAX_MESSAGE_PAGES - 1) {
        throw new Error(
          `session reconcile exceeded ${MAX_MESSAGE_PAGES} authoritative message pages for ${agentSessionId}`
        );
      }
      const nextBeforeVersion = page.messages.reduce(
        (earliest, message) => Math.min(earliest, message.version),
        Number.POSITIVE_INFINITY
      );
      if (
        !Number.isSafeInteger(nextBeforeVersion) ||
        nextBeforeVersion <= 0 ||
        nextBeforeVersion === beforeVersion
      ) {
        throw new Error(
          `session reconcile authoritative pagination did not advance for ${agentSessionId}`
        );
      }
      beforeVersion = nextBeforeVersion;
    }
    const tail = await loadAllAgentSessionMessages({
      afterVersion: anchorLatestVersion,
      listPage: (cursor) =>
        input.port.listSessionMessages({
          afterVersion: cursor,
          agentSessionId,
          order: "asc",
          signal,
          workspaceId
        }),
      shouldAbort: () => shouldSkip(agentSessionId, signal)
    });
    for (const message of tail.messages) {
      if (message.agentSessionId.trim() !== agentSessionId) {
        throw new Error(
          `session reconcile message identity mismatch: expected ${agentSessionId}, received ${message.agentSessionId}`
        );
      }
    }
    latestVersion = Math.max(
      latestVersion,
      latestMessageVersion(tail.messages)
    );
    return {
      hasMore: false,
      latestVersion,
      messages: mergeAgentActivityMessages(messages, tail.messages)
    };
  };

  return {
    async execute(command, options) {
      const agentSessionId = normalizeRequiredIdentity(
        command.agentSessionId,
        "agentSessionId"
      );
      if (
        normalizeRequiredIdentity(command.workspaceId, "workspaceId") !==
        workspaceId
      ) {
        throw new Error(
          `session reconcile workspace mismatch: expected ${workspaceId}, received ${command.workspaceId}`
        );
      }
      const signal = options?.signal;
      assertExecutionActive(signal);
      if (shouldSkip(agentSessionId, signal)) {
        assertExecutionActive(signal);
        return skippedResult();
      }
      if (command.authoritativeMessages === true) {
        if (command.scope !== "state_and_messages") {
          throw new Error(
            "session reconcile authoritative messages require state_and_messages scope"
          );
        }
        const requiredHistoryRevision = command.requiredHistoryRevision ?? 0;
        for (let attempt = 0; attempt < 3; attempt += 1) {
          const before = await getDetail(agentSessionId, "state", signal);
          const page = await reconcileAuthoritativePage(agentSessionId, signal);
          if (shouldSkip(agentSessionId, signal)) {
            assertExecutionActive(signal);
            return skippedResult();
          }
          const after = await getDetail(agentSessionId, "state", signal);
          const beforeHistoryRevision = before.historyRevision ?? 0;
          const afterHistoryRevision = after.historyRevision ?? 0;
          if (
            beforeHistoryRevision !== afterHistoryRevision ||
            before.session.messageVersion !== after.session.messageVersion ||
            afterHistoryRevision < requiredHistoryRevision
          ) {
            continue;
          }
          const filtered = withoutDeletedChildren(
            after,
            input.isSessionDeleted
          );
          const latestTurnId = filtered.session.latestTurn?.turnId.trim() ?? "";
          const liveTurn = filtered.turns.find(
            (turn) => turn.turnId.trim() === latestTurnId
          );
          if (
            command.live &&
            filtered.session.latestTurn?.phase === "settled" &&
            !liveTurn
          ) {
            continue;
          }
          input.onTrace?.({
            detail: filtered,
            scope: command.scope,
            status: "applying",
            type: "detailApply"
          });
          input.engine.dispatch({
            agentSessionId,
            childSessions: filtered.childSessions,
            historyRevision: afterHistoryRevision,
            ...(command.live && liveTurn
              ? { liveTurnId: liveTurn.turnId }
              : {}),
            messages: page.messages,
            session: filtered.session,
            sessionMessageWindows: [
              {
                agentSessionId,
                ...agentActivitySessionMessageWindowFromDescendingPage(page)
              }
            ],
            turns: filtered.turns,
            type: "session/historyAuthoritativeSnapshotReceived",
            workspaceId
          });
          input.onTrace?.({
            detail: filtered,
            scope: command.scope,
            status: "applied",
            type: "detailApply"
          });
          const scopedTurns = filtered.turns.filter(
            (turn) => turn.agentSessionId === agentSessionId
          );
          input.reconcileAuthoritativeHistory(
            agentSessionId,
            page.messages,
            scopedTurns
          );
          return {
            affectedSessionIds: detailSessionIds(filtered),
            appliedMessages: page.messages,
            session: filtered.session,
            status: "applied"
          };
        }
        throw new Error(
          `session reconcile authoritative history changed during read for ${agentSessionId}`
        );
      }
      if (command.scope === "state") {
        const detail = await getDetail(agentSessionId, "state", signal);
        if (shouldSkip(agentSessionId, signal)) {
          assertExecutionActive(signal);
          return skippedResult();
        }
        const filtered = withoutDeletedChildren(detail, input.isSessionDeleted);
        input.onTrace?.({
          detail: filtered,
          scope: command.scope,
          status: "applying",
          type: "detailApply"
        });
        input.engine.dispatch({
          ...filtered,
          ...(command.live ? { live: true } : {}),
          type: "session/detailSnapshotReceived",
          workspaceId
        });
        input.onTrace?.({
          detail: filtered,
          scope: command.scope,
          status: "applied",
          type: "detailApply"
        });
        return {
          affectedSessionIds: detailSessionIds(filtered),
          appliedMessages: [],
          session: filtered.session,
          status: "applied"
        };
      }

      if (command.scope === "messages") {
        const snapshot = readSnapshot();
        const session =
          snapshot.sessions.find(
            (candidate) => candidate.agentSessionId === agentSessionId
          ) ?? fallbackSession(agentSessionId);
        const page = await reconcilePage(
          session,
          snapshot.sessionMessagesById[agentSessionId] ?? [],
          snapshot.sessionMessageWindowsById?.[agentSessionId] !== undefined,
          agentSessionId,
          "messages",
          signal
        );
        if (!page) {
          assertExecutionActive(signal);
          return shouldSkip(agentSessionId, signal)
            ? skippedResult()
            : {
                affectedSessionIds: [],
                appliedMessages: [],
                session: null,
                status: "applied"
              };
        }
        input.engine.dispatch({
          messages: page.page.messages,
          ...(page.startsAtNewestBoundary
            ? {
                sessionMessageWindows: [
                  {
                    agentSessionId,
                    ...agentActivitySessionMessageWindowFromDescendingPage(
                      page.page
                    )
                  }
                ]
              }
            : {}),
          type: "message/snapshotReceived",
          workspaceId
        });
        input.reconcileOptimisticMessages(agentSessionId);
        return {
          affectedSessionIds: [agentSessionId],
          appliedMessages: page.page.messages,
          session: null,
          status: "applied"
        };
      }

      const discoveryDetail = await getDetail(
        agentSessionId,
        "discovery",
        signal
      );
      if (shouldSkip(agentSessionId, signal)) {
        assertExecutionActive(signal);
        return skippedResult();
      }
      const initialSnapshot = readSnapshot();
      const cachedMessagesBySessionId = new Map(
        Object.entries(initialSnapshot.sessionMessagesById)
      );
      const knownMessageWindows = new Set(
        Object.keys(initialSnapshot.sessionMessageWindowsById ?? {})
      );
      const pages: ReconciledMessagePage[] = [];

      const reconcileSessions = async (
        sessions: readonly AgentActivitySession[]
      ) => {
        const results = await Promise.all(
          sessions.map((session) =>
            reconcilePage(
              session,
              cachedMessagesBySessionId.get(session.agentSessionId) ?? [],
              knownMessageWindows.has(session.agentSessionId),
              agentSessionId,
              "combined",
              signal
            )
          )
        );
        for (const page of results) {
          if (!page) continue;
          pages.push(page);
          cachedMessagesBySessionId.set(
            page.agentSessionId,
            mergeAgentActivityMessages(
              cachedMessagesBySessionId.get(page.agentSessionId) ?? [],
              page.page.messages
            )
          );
          if (page.startsAtNewestBoundary) {
            knownMessageWindows.add(page.agentSessionId);
          }
        }
      };

      const filteredDiscoveryDetail = withoutDeletedChildren(
        discoveryDetail,
        input.isSessionDeleted
      );
      const discoveredSessions = sessionsToHydrate(
        filteredDiscoveryDetail,
        input.childMessageHydration
      );
      await reconcileSessions(discoveredSessions);
      if (shouldSkip(agentSessionId, signal)) {
        assertExecutionActive(signal);
        return skippedResult();
      }

      const finalDetail = await getDetail(agentSessionId, "final", signal);
      if (shouldSkip(agentSessionId, signal)) {
        assertExecutionActive(signal);
        return skippedResult();
      }
      const discoveredIds = new Set(
        discoveredSessions.map((session) => session.agentSessionId)
      );
      const filteredDetail = withoutDeletedChildren(
        finalDetail,
        input.isSessionDeleted
      );
      const finalSessions = sessionsToHydrate(
        filteredDetail,
        input.childMessageHydration
      );
      await reconcileSessions(
        finalSessions.filter((session) => {
          if (input.isSessionDeleted(session.agentSessionId)) return false;
          if (!discoveredIds.has(session.agentSessionId)) return true;
          return (
            latestDurableMessageVersion(
              cachedMessagesBySessionId.get(session.agentSessionId) ?? []
            ) < session.messageVersion
          );
        })
      );
      if (shouldSkip(agentSessionId, signal)) {
        assertExecutionActive(signal);
        return skippedResult();
      }

      const retainedSessionIds = new Set(detailSessionIds(filteredDetail));
      const appliedPages = pages.filter((page) =>
        retainedSessionIds.has(page.agentSessionId)
      );
      const appliedMessages = mergeReconciledPageMessages(appliedPages);
      input.onTrace?.({
        detail: filteredDetail,
        scope: command.scope,
        status: "applying",
        type: "detailApply"
      });
      input.engine.dispatch({
        ...filteredDetail,
        ...(command.live ? { live: true } : {}),
        messages: appliedMessages,
        sessionMessageWindows: appliedPages.flatMap((page) =>
          page.startsAtNewestBoundary
            ? [
                {
                  agentSessionId: page.agentSessionId,
                  ...agentActivitySessionMessageWindowFromDescendingPage(
                    page.page
                  )
                }
              ]
            : []
        ),
        type: "session/detailSnapshotReceived",
        workspaceId
      });
      input.onTrace?.({
        detail: filteredDetail,
        scope: command.scope,
        status: "applied",
        type: "detailApply"
      });
      const reconciledMessageSessionIds = [
        ...new Set(appliedPages.map((page) => page.agentSessionId))
      ];
      for (const reconciledSessionId of reconciledMessageSessionIds) {
        input.reconcileOptimisticMessages(reconciledSessionId);
      }
      return {
        affectedSessionIds: detailSessionIds(filteredDetail),
        appliedMessages,
        session: filteredDetail.session,
        status: "applied"
      };
    }
  };
}

function sessionsToHydrate(
  detail: AgentActivitySessionDetailSnapshot,
  childMessageHydration: AgentActivityChildMessageHydration
): AgentActivitySession[] {
  return childMessageHydration === "session_hierarchy"
    ? [detail.session, ...detail.childSessions]
    : [detail.session];
}

function mergeReconciledPageMessages(
  pages: readonly ReconciledMessagePage[]
): AgentActivityDurableMessage[] {
  const messagesBySessionId = new Map<string, AgentActivityDurableMessage[]>();
  for (const page of pages) {
    const messages = messagesBySessionId.get(page.agentSessionId) ?? [];
    messages.push(...page.page.messages);
    messagesBySessionId.set(page.agentSessionId, messages);
  }
  return [...messagesBySessionId.values()].flatMap((messages) =>
    mergeAgentActivityMessages([], messages)
  );
}
