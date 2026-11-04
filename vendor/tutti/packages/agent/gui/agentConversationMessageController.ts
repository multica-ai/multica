import {
  agentActivitySessionMessageWindowFromDescendingPage,
  selectEngineSessionDetailHydrated,
  selectEngineSessionDetailLoading,
  selectSessionMessageWindow,
  type AgentActivityDurableMessage,
  type AgentActivityMessagePage,
  type AgentSessionEngine
} from "@tutti-os/agent-activity-core";

const DEFAULT_PAGE_SIZE = 100;

export type AgentConversationMessagePagePhase = "error" | "idle" | "loading";

export interface AgentConversationMessageControllerSnapshot {
  agentSessionId: string | null;
  error: unknown | null;
  olderPagePhase: AgentConversationMessagePagePhase;
}

export interface AgentConversationMessagePageInput {
  agentSessionId: string;
  beforeVersion: number;
  limit: number;
  order: "desc";
  signal: AbortSignal;
  workspaceId: string;
}

export interface AgentConversationMessageControllerDiagnostics {
  error?(input: {
    agentSessionId: string;
    beforeVersion: number;
    error: unknown;
  }): void;
  page?(input: {
    agentSessionId: string;
    details: Readonly<Record<string, unknown>>;
    event: string;
    level?: "debug" | "warn";
    messages?: readonly AgentActivityDurableMessage[];
  }): void;
  synchronizationError?(input: {
    agentSessionId: string;
    error: unknown;
  }): void;
}

export interface CreateAgentConversationMessageControllerInput {
  /** See AgentGUIRuntime.loadOlderSessionMessages. */
  loadOlderSessionMessages?(input: { workspaceId: string; agentSessionId: string }): Promise<void>;
  diagnostics?: AgentConversationMessageControllerDiagnostics;
  engine: Pick<AgentSessionEngine, "dispatch" | "getSnapshot">;
  ensureSessionSynchronized?(input: {
    agentSessionId: string;
    onError(error: unknown): void;
    workspaceId: string;
  }): () => void;
  isAvailable(agentSessionId?: string | null): boolean;
  listSessionMessages(
    input: AgentConversationMessagePageInput
  ): Promise<AgentActivityMessagePage>;
  onOlderPageApplied?(agentSessionId: string): void;
  onOlderPageFailed?(error: unknown): void;
  onOlderPageSettled?(): void;
  onSnapshotChanged?(
    snapshot: AgentConversationMessageControllerSnapshot
  ): void;
  pageSize?: number;
  workspaceId: string;
}

export interface AgentConversationMessageController {
  cancel(): void;
  dispose(): void;
  getSnapshot(): AgentConversationMessageControllerSnapshot;
  loadOlder(agentSessionId?: string | null): Promise<void>;
  requestInitial(agentSessionId: string, options?: { force?: boolean }): void;
  requestLatest(agentSessionId?: string | null): void;
  setActiveSession(agentSessionId: string | null): void;
}

const EMPTY_SNAPSHOT: AgentConversationMessageControllerSnapshot = {
  agentSessionId: null,
  error: null,
  olderPagePhase: "idle"
};

/**
 * Owns the renderer-neutral message-query state for one focused conversation.
 *
 * Canonical detail/latest hydration remains an Engine reconcile command.
 * Explicit older-page reads are fenced to the active Session and applied to
 * the same Engine, so Desktop and Mobile cannot drift into separate paging
 * stores or retry/concurrency rules.
 */
export function createAgentConversationMessageController(
  input: CreateAgentConversationMessageControllerInput
): AgentConversationMessageController {
  let activeSessionId: string | null = null;
  let disposed = false;
  let generation = 0;
  let snapshot = EMPTY_SNAPSHOT;
  let releaseSessionSynchronization: (() => void) | null = null;
  let olderRequest: {
    abortController: AbortController;
    agentSessionId: string;
    beforeVersion: number;
    generation: number;
    promise: Promise<void>;
  } | null = null;
  const pageSize = input.pageSize ?? DEFAULT_PAGE_SIZE;

  const publish = (next: AgentConversationMessageControllerSnapshot): void => {
    if (
      snapshot.agentSessionId === next.agentSessionId &&
      snapshot.error === next.error &&
      snapshot.olderPagePhase === next.olderPagePhase
    ) {
      return;
    }
    snapshot = next;
    input.onSnapshotChanged?.(snapshot);
  };
  const normalizeSessionId = (
    agentSessionId: string | null | undefined
  ): string | null => {
    const normalized = agentSessionId?.trim() ?? "";
    return normalized || null;
  };
  const abortOlderRequest = (): void => {
    const current = olderRequest;
    olderRequest = null;
    current?.abortController.abort();
  };
  const isCurrentRequest = (request: {
    agentSessionId: string;
    beforeVersion: number;
    generation: number;
  }): boolean =>
    !disposed &&
    input.isAvailable(request.agentSessionId) &&
    generation === request.generation &&
    activeSessionId === request.agentSessionId &&
    olderRequest?.agentSessionId === request.agentSessionId &&
    olderRequest.beforeVersion === request.beforeVersion &&
    olderRequest.generation === request.generation;

  const setActiveSession = (agentSessionId: string | null): void => {
    if (disposed) return;
    const normalized = normalizeSessionId(agentSessionId);
    if (normalized === activeSessionId) return;
    generation += 1;
    abortOlderRequest();
    releaseSessionSynchronization?.();
    releaseSessionSynchronization = null;
    activeSessionId = normalized;
    publish({
      agentSessionId: normalized,
      error: null,
      olderPagePhase: "idle"
    });
    if (!normalized || !input.ensureSessionSynchronized) return;
    const reportSynchronizationError = (error: unknown): void => {
      input.diagnostics?.synchronizationError?.({
        agentSessionId: normalized,
        error
      });
    };
    try {
      releaseSessionSynchronization = input.ensureSessionSynchronized({
        agentSessionId: normalized,
        onError: reportSynchronizationError,
        workspaceId: input.workspaceId
      });
    } catch (error) {
      reportSynchronizationError(error);
    }
  };

  const requestReconcile = (
    agentSessionId: string | null | undefined,
    needsState: boolean
  ): void => {
    const normalized = normalizeSessionId(agentSessionId);
    if (
      disposed ||
      !normalized ||
      !input.isAvailable(normalized) ||
      activeSessionId !== normalized
    )
      return;
    input.engine.dispatch({
      agentSessionId: normalized,
      needsMessages: true,
      needsState,
      type: "session/reconcileRequested",
      workspaceId: input.workspaceId
    });
  };

  return {
    cancel() {
      if (disposed) return;
      generation += 1;
      abortOlderRequest();
      publish({
        agentSessionId: activeSessionId,
        error: null,
        olderPagePhase: "idle"
      });
    },
    dispose() {
      if (disposed) return;
      disposed = true;
      generation += 1;
      abortOlderRequest();
      releaseSessionSynchronization?.();
      releaseSessionSynchronization = null;
    },
    getSnapshot: () => snapshot,
    async loadOlder(agentSessionId) {
      const normalized = normalizeSessionId(agentSessionId ?? activeSessionId);
      if (disposed || !input.isAvailable(normalized)) return;
      if (!normalized || activeSessionId !== normalized) {
        input.diagnostics?.page?.({
          agentSessionId: normalized ?? "",
          details: {
            activeConversationId: activeSessionId,
            reason: "inactive_session"
          },
          event: "agent.gui.messages.older.skipped",
          level: "debug"
        });
        return;
      }
      const window = selectSessionMessageWindow(
        input.engine.getSnapshot(),
        normalized
      );
      const beforeVersion = window?.oldestLoadedVersion ?? null;
      if (!window?.hasOlderMessages || beforeVersion === null) {
        input.diagnostics?.page?.({
          agentSessionId: normalized,
          details: {
            beforeVersion,
            reason: "exhausted_cursor"
          },
          event: "agent.gui.messages.older.suppressed_exhausted_cursor"
        });
        return;
      }
      if (olderRequest) {
        input.diagnostics?.page?.({
          agentSessionId: normalized,
          details: {
            beforeVersion,
            inFlightBeforeVersion: olderRequest.beforeVersion,
            reason: "in_flight_request"
          },
          event: "agent.gui.messages.older.suppressed_in_flight"
        });
        return olderRequest.promise;
      }

      const requestGeneration = generation;
      const abortController = new AbortController();
      publish({
        agentSessionId: normalized,
        error: null,
        olderPagePhase: "loading"
      });
      input.diagnostics?.page?.({
        agentSessionId: normalized,
        details: {
          beforeVersion,
          limit: pageSize,
          order: "desc"
        },
        event: "agent.gui.messages.older.requested"
      });

      const request = {
        abortController,
        agentSessionId: normalized,
        beforeVersion,
        generation: requestGeneration,
        promise: Promise.resolve()
      };
      const ownedPaging = input.loadOlderSessionMessages?.({ agentSessionId: normalized, workspaceId: input.workspaceId });
      const promise = ownedPaging ? ownedPaging.then(() => {
        if (!isCurrentRequest(request)) return;
        input.onOlderPageApplied?.(normalized);
        publish({ agentSessionId: normalized, error: null, olderPagePhase: "idle" });
      }).catch((error: unknown) => {
        if (!isCurrentRequest(request)) return;
        input.onOlderPageFailed?.(error);
        publish({ agentSessionId: normalized, error, olderPagePhase: "error" });
      }).finally(() => {
        if (olderRequest?.promise !== promise) return;
        olderRequest = null;
        if (!disposed && generation === requestGeneration) input.onOlderPageSettled?.();
      }) : input
        .listSessionMessages({
          agentSessionId: normalized,
          beforeVersion,
          limit: pageSize,
          order: "desc",
          signal: abortController.signal,
          workspaceId: input.workspaceId
        })
        .then((page) => {
          if (!isCurrentRequest(request)) return;
          assertPageIdentity(normalized, page);
          const authoritativePage =
            page.messages.length === 0 && page.hasMore
              ? { ...page, hasMore: false }
              : page;
          input.diagnostics?.page?.({
            agentSessionId: normalized,
            details: {
              beforeVersion,
              hasMore: authoritativePage.hasMore,
              latestVersion: authoritativePage.latestVersion
            },
            event: "agent.gui.messages.older.resolved",
            messages: authoritativePage.messages
          });
          input.engine.dispatch({
            messages: authoritativePage.messages,
            sessionMessageWindows: [
              {
                agentSessionId: normalized,
                ...agentActivitySessionMessageWindowFromDescendingPage(
                  authoritativePage
                )
              }
            ],
            type: "message/snapshotReceived",
            workspaceId: input.workspaceId
          });
          input.onOlderPageApplied?.(normalized);
          publish({
            agentSessionId: normalized,
            error: null,
            olderPagePhase: "idle"
          });
        })
        .catch((error: unknown) => {
          if (!isCurrentRequest(request)) return;
          input.diagnostics?.error?.({
            agentSessionId: normalized,
            beforeVersion,
            error
          });
          input.onOlderPageFailed?.(error);
          publish({
            agentSessionId: normalized,
            error,
            olderPagePhase: "error"
          });
        })
        .finally(() => {
          if (olderRequest?.promise !== promise) return;
          olderRequest = null;
          if (!disposed && generation === requestGeneration) {
            input.onOlderPageSettled?.();
          }
        });
      request.promise = promise;
      olderRequest = request;
      return promise;
    },
    requestInitial(agentSessionId, options) {
      const normalized = normalizeSessionId(agentSessionId);
      if (!normalized) return;
      setActiveSession(normalized);
      const state = input.engine.getSnapshot();
      if (
        options?.force !== true &&
        (selectEngineSessionDetailHydrated(state, normalized) ||
          selectEngineSessionDetailLoading(state, normalized))
      ) {
        return;
      }
      generation += 1;
      abortOlderRequest();
      publish({
        agentSessionId: normalized,
        error: null,
        olderPagePhase: "idle"
      });
      requestReconcile(normalized, true);
    },
    requestLatest(agentSessionId) {
      requestReconcile(agentSessionId ?? activeSessionId, false);
    },
    setActiveSession
  };
}

function assertPageIdentity(
  expectedAgentSessionId: string,
  page: AgentActivityMessagePage
): void {
  for (const message of page.messages) {
    if (message.agentSessionId !== expectedAgentSessionId) {
      throw new Error(
        `conversation message page identity mismatch: expected ${expectedAgentSessionId}, received ${message.agentSessionId}`
      );
    }
  }
}
