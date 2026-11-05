import { type AgentActivityDurableMessage } from "@tutti-os/agent-activity-core";
import type { AgentConversationServices } from "./state/conversation-services";

export type AgentConversationMessagePagePhase = "error" | "idle" | "loading";

export interface AgentConversationMessageControllerSnapshot {
  agentSessionId: string | null;
  error: unknown | null;
  olderPagePhase: AgentConversationMessagePagePhase;
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
  /** See ConversationServices.loadOlderSessionMessages. */
  loadOlderSessionMessages(input: {
    workspaceId: string;
    agentSessionId: string;
  }): Promise<void>;
  /** The active conversation's position in its history. */
  olderMessages(agentSessionId: string): {
    hasOlderMessages: boolean;
    oldestLoadedVersion: number | null;
  };
  diagnostics?: AgentConversationMessageControllerDiagnostics;
  engine: AgentConversationServices;
  ensureSessionSynchronized?(input: {
    agentSessionId: string;
    onError(error: unknown): void;
    workspaceId: string;
  }): () => void;
  isAvailable(agentSessionId?: string | null): boolean;
  onOlderPageApplied?(agentSessionId: string): void;
  onOlderPageFailed?(error: unknown): void;
  onOlderPageSettled?(): void;
  onSnapshotChanged?(
    snapshot: AgentConversationMessageControllerSnapshot
  ): void;
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
 * Explicit older-page reads are fenced to the active Session; the runtime applies them.
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
    input.engine.sessionLoads.request(normalized, {
      state: needsState,
      messages: true
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
      const window = input.olderMessages(normalized);
      const beforeVersion = window.oldestLoadedVersion;
      if (!window.hasOlderMessages || beforeVersion === null) {
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
        details: { beforeVersion },
        event: "agent.gui.messages.older.requested"
      });

      const request = {
        abortController,
        agentSessionId: normalized,
        beforeVersion,
        generation: requestGeneration,
        promise: Promise.resolve()
      };
      const promise = input
        .loadOlderSessionMessages({
          agentSessionId: normalized,
          workspaceId: input.workspaceId
        })
        .then(() => {
          if (!isCurrentRequest(request)) return;
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
          if (!disposed && generation === requestGeneration)
            input.onOlderPageSettled?.();
        });
      request.promise = promise;
      olderRequest = request;
      return promise;
    },
    requestInitial(agentSessionId, options) {
      const normalized = normalizeSessionId(agentSessionId);
      if (!normalized) return;
      setActiveSession(normalized);
      const state = input.engine;
      if (
        options?.force !== true &&
        (state.sessionLoads.isDetailHydrated(normalized) ||
          state.sessionLoads.isDetailLoading(normalized))
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
