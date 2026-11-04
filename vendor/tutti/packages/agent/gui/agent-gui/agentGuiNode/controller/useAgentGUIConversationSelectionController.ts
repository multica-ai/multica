import {
  selectFailedNewActivationResolution,
  selectEngineSession,
  selectEngineSessionCanReload,
  selectEngineSessionDetailHydrated,
  selectEngineSessionStateHydrated,
  selectLatestActivationForSession,
  type AgentSessionEngine,
  type PendingActivationIntentRecord
} from "@tutti-os/agent-activity-core";
import type { Dispatch, RefObject, SetStateAction } from "react";
import { useCallback, useEffect, useRef } from "react";
import type { AgentGUIRuntime } from "../../../agentActivityRuntime";
import { translate } from "../../../i18n/index";
import type { AgentGUINodeData } from "../../../types";
import type { AgentGUIConversationSummary } from "../model/agentGuiConversationModel";
import type { AgentGUIConversationRailRevealReason } from "../model/agentGuiConversationRailViewState";
import {
  forgetAgentGUISessionMemories,
  rememberAgentGUIActiveConversation
} from "../model/agentGuiSessionNavigationMemory";
import { AGENT_SESSION_NOT_FOUND_ERROR } from "./agentGuiController.errors";
import {
  reportAgentGUIActiveConversationCleared,
  reportAgentGUIConversationListProjectionSkipped
} from "./agentGuiController.reporting";
import {
  isPendingNewConversationActivation,
  isPendingNewConversationActivationForSession,
  type useAgentGUIActivation
} from "./useAgentGUIActivation";
import {
  resolveConversationSummaryById,
  useAgentConversationSelection,
  type ConversationIntent
} from "./useAgentConversationSelection";

type ActivationRecord = Pick<
  PendingActivationIntentRecord,
  | "agentSessionId"
  | "agentTargetId"
  | "errorMessage"
  | "mode"
  | "requestId"
  | "status"
>;

interface UseAgentGUIConversationSelectionControllerInput {
  activation: ReturnType<typeof useAgentGUIActivation>;
  activeConversationId: string | null;
  activeConversationIdRef: RefObject<string | null>;
  activePendingActivation: ActivationRecord | null;
  activeSessionReconcileErrorCode: string | null;
  agentActivityRuntime: AgentGUIRuntime;
  conversationIdsRef: RefObject<Set<string>>;
  conversationsRef: RefObject<AgentGUIConversationSummary[]>;
  conversationListQuery: unknown | null;
  currentUserId: string | null | undefined;
  data: AgentGUINodeData;
  dataRef: RefObject<AgentGUINodeData>;
  intent: ConversationIntent;
  isComposerHomeRef: RefObject<boolean>;
  isMountedRef: RefObject<boolean>;
  loadDraftComposerOptions(): void;
  loadSelectedConversationMessages(
    agentSessionId: string,
    options?: { force?: boolean }
  ): Promise<void>;
  loadSessionState(agentSessionId: string): void;
  markSelectedConversationDetailPending(agentSessionId: string): string | null;
  onDataChangeRef: RefObject<
    (updater: (current: AgentGUINodeData) => AgentGUINodeData) => void
  >;
  sessionEngine: AgentSessionEngine;
  setActiveConversationId: Dispatch<SetStateAction<string | null>>;
  setDetailError: Dispatch<SetStateAction<string | null>>;
  setIntent: Dispatch<SetStateAction<ConversationIntent>>;
  setIsComposerHome: Dispatch<SetStateAction<boolean>>;
  setIsLoadingMessages: Dispatch<SetStateAction<boolean>>;
  setActiveMessageSession(agentSessionId: string | null): void;
  clearRailRevealRequest(): void;
  requestRailReveal(
    agentSessionId: string,
    reason: AgentGUIConversationRailRevealReason
  ): void;
  transientConversation: AgentGUIConversationSummary | null;
  workspaceId: string;
}

export function clearRolledBackAgentGUISelection(
  current: AgentGUINodeData,
  rolledBackAgentSessionId: string
): AgentGUINodeData {
  const normalized = rolledBackAgentSessionId.trim();
  return normalized
    ? forgetAgentGUISessionMemories(current, new Set([normalized]))
    : current;
}

export function shouldClearMissingAgentGUISelection(input: {
  activeConversationId: string | null;
  currentActiveConversationId: string | null;
  reconcileErrorCode: string | null;
}): boolean {
  const activeConversationId = input.activeConversationId?.trim() ?? "";
  return (
    activeConversationId !== "" &&
    activeConversationId ===
      (input.currentActiveConversationId?.trim() ?? "") &&
    input.reconcileErrorCode?.trim() === AGENT_SESSION_NOT_FOUND_ERROR
  );
}

export function useAgentGUIConversationSelectionController(
  input: UseAgentGUIConversationSelectionControllerInput
) {
  const {
    activation,
    activeConversationId,
    activeConversationIdRef,
    activePendingActivation,
    activeSessionReconcileErrorCode,
    agentActivityRuntime,
    conversationIdsRef,
    conversationsRef,
    conversationListQuery,
    currentUserId,
    data,
    dataRef,
    intent,
    isComposerHomeRef,
    isMountedRef,
    loadDraftComposerOptions,
    loadSelectedConversationMessages,
    loadSessionState,
    markSelectedConversationDetailPending,
    onDataChangeRef,
    sessionEngine,
    setActiveConversationId,
    setDetailError,
    setIntent,
    setIsComposerHome,
    setIsLoadingMessages,
    setActiveMessageSession,
    clearRailRevealRequest,
    requestRailReveal,
    transientConversation,
    workspaceId
  } = input;
  const rolledBackSelectionSessionIdRef = useRef<string | null>(null);
  const persistedConfirmedActivationRequestIdRef = useRef<string | null>(null);

  useEffect(() => {
    setActiveMessageSession(activeConversationId);
    const rollbackSelection = (
      agentSessionId: string,
      errorMessage: string | null
    ) => {
      rolledBackSelectionSessionIdRef.current = agentSessionId;
      setActiveMessageSession(null);
      activeConversationIdRef.current = null;
      setActiveConversationId(null);
      isComposerHomeRef.current = true;
      setIsComposerHome(true);
      setIntent({ tag: "home" });
      clearRailRevealRequest();
      onDataChangeRef.current((current) =>
        clearRolledBackAgentGUISelection(current, agentSessionId)
      );
      if (errorMessage) setDetailError(errorMessage);
    };

    if (
      activeConversationIdRef.current ===
        activePendingActivation?.agentSessionId &&
      selectFailedNewActivationResolution(
        sessionEngine.getSnapshot(),
        activePendingActivation.agentSessionId,
        intent.tag === "active" ? { selectionSource: intent.source } : undefined
      ) === "rollback"
    ) {
      rollbackSelection(
        activePendingActivation.agentSessionId,
        activePendingActivation.status === "failed"
          ? activePendingActivation.errorMessage ||
              translate("agentHost.agentGui.sessionActivationFailed")
          : null
      );
      return;
    }
    if (
      shouldClearMissingAgentGUISelection({
        activeConversationId,
        currentActiveConversationId: activeConversationIdRef.current,
        reconcileErrorCode: activeSessionReconcileErrorCode
      })
    ) {
      rollbackSelection(
        activeConversationId!.trim(),
        translate("agentHost.agentGui.sessionNoLongerAvailable")
      );
      setIsLoadingMessages(false);
      return;
    }
    if (
      activePendingActivation?.mode === "new" &&
      activePendingActivation.status === "confirmed" &&
      activeConversationIdRef.current ===
        activePendingActivation.agentSessionId &&
      persistedConfirmedActivationRequestIdRef.current !==
        activePendingActivation.requestId
    ) {
      persistedConfirmedActivationRequestIdRef.current =
        activePendingActivation.requestId;
      onDataChangeRef.current((current) =>
        rememberAgentGUIActiveConversation(
          current,
          activePendingActivation.agentSessionId,
          activePendingActivation.agentTargetId
        )
      );
    }
  }, [
    activeConversationId,
    activePendingActivation,
    activeSessionReconcileErrorCode,
    clearRailRevealRequest,
    sessionEngine,
    setActiveMessageSession
  ]);

  useEffect(() => {
    const externalId = data.lastActiveAgentSessionId?.trim() ?? "";
    const rolledBackSelectionSessionId =
      rolledBackSelectionSessionIdRef.current;
    if (rolledBackSelectionSessionId) {
      if (externalId === rolledBackSelectionSessionId) {
        // The Workbench state update that clears a failed or missing session
        // can reach this component after the local rollback. Until that echo
        // arrives, the old persisted id is not a new external selection.
        return;
      }
      rolledBackSelectionSessionIdRef.current = null;
    }
    if (externalId === (activeConversationIdRef.current ?? "")) return;
    if (!externalId) {
      const previous = activeConversationIdRef.current;
      if (!previous && isComposerHomeRef.current && intent.tag === "home") {
        return;
      }
      reportAgentGUIActiveConversationCleared({
        details: {
          dataLastActiveAgentSessionId: data.lastActiveAgentSessionId ?? null,
          intent: intent.tag,
          isComposerHome: isComposerHomeRef.current
        },
        previousAgentSessionId: previous,
        reason: "external_last_active_empty",
        runtime: agentActivityRuntime,
        workspaceId
      });
      if (
        previous &&
        !isPendingNewConversationActivationForSession(
          activePendingActivation,
          previous
        )
      ) {
        void activation.unactivate(previous);
      }
      setIntent({ tag: "home" });
      isComposerHomeRef.current = true;
      setIsComposerHome(true);
      setActiveMessageSession(null);
      activeConversationIdRef.current = null;
      setActiveConversationId(null);
      setIsLoadingMessages(false);
      setDetailError(null);
      clearRailRevealRequest();
      loadDraftComposerOptions();
      return;
    }
    setIntent((current) => {
      if (
        (current.tag === "active" || current.tag === "requested") &&
        current.id === externalId
      ) {
        return current;
      }
      if (current.tag === "requested") {
        return current;
      }
      return { tag: "requested", id: externalId };
    });
    // External persisted selection is the trigger; routing dependencies stay in
    // refs or stable controller callbacks to avoid replaying a local selection.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [data.lastActiveAgentSessionId]);
  const selection = useAgentConversationSelection({
    activation: {
      canReload: (agentSessionId) => {
        return selectEngineSessionCanReload(
          sessionEngine.getSnapshot(),
          agentSessionId
        );
      },
      forget: activation.clearFailure,
      isPending: (agentSessionId) =>
        isPendingNewConversationActivation(
          selectLatestActivationForSession(
            sessionEngine.getSnapshot(),
            agentSessionId
          )
        )
    },
    conversations: {
      agentTargetIdFor: (agentSessionId) => {
        const state = sessionEngine.getSnapshot();
        const sessionAgentTargetId =
          selectEngineSession(state, agentSessionId)?.agentTargetId?.trim() ??
          "";
        if (sessionAgentTargetId) return sessionAgentTargetId;
        const activationAgentTargetId =
          selectLatestActivationForSession(
            state,
            agentSessionId
          )?.agentTargetId?.trim() ?? "";
        if (activationAgentTargetId) return activationAgentTargetId;
        return (
          resolveConversationSummaryById(
            conversationsRef.current,
            agentSessionId,
            transientConversation
          )?.agentTargetId?.trim() || null
        );
      },
      contains: (agentSessionId) =>
        conversationIdsRef.current.has(agentSessionId)
    },
    detail: {
      ensureHydrated: (agentSessionId) => {
        void loadSelectedConversationMessages(agentSessionId);
      },
      ensureStateHydrated: loadSessionState,
      isHydrated: (agentSessionId) =>
        selectEngineSessionDetailHydrated(
          sessionEngine.getSnapshot(),
          agentSessionId
        ),
      isStateHydrated: (agentSessionId) =>
        selectEngineSessionStateHydrated(
          sessionEngine.getSnapshot(),
          agentSessionId
        ),
      markPending: markSelectedConversationDetailPending,
      setLoading: setIsLoadingMessages
    },
    hasConversationListQuery: () => Boolean(conversationListQuery),
    isMounted: () => isMountedRef.current,
    onMissingConversationListQuery: (previous) => {
      const workspaceIdPresent = Boolean(workspaceId.trim());
      const currentUserIdPresent = Boolean(currentUserId?.trim());
      const diagnosticInput = {
        currentUserIdPresent,
        dataLastActiveAgentSessionId:
          dataRef.current.lastActiveAgentSessionId ?? null,
        isComposerHome: isComposerHomeRef.current,
        provider: dataRef.current.provider,
        runtime: agentActivityRuntime,
        workspaceId,
        workspaceIdPresent
      };
      reportAgentGUIConversationListProjectionSkipped({
        ...diagnosticInput,
        activeConversationId: previous,
        reason: "conversation_list_query_missing"
      });
      reportAgentGUIActiveConversationCleared({
        details: {
          currentUserIdPresent,
          dataLastActiveAgentSessionId:
            diagnosticInput.dataLastActiveAgentSessionId,
          isComposerHome: diagnosticInput.isComposerHome,
          provider: diagnosticInput.provider,
          workspaceIdPresent
        },
        previousAgentSessionId: previous,
        reason: "conversation_list_query_missing",
        runtime: agentActivityRuntime,
        workspaceId
      });
    },
    persistence: { update: (updater) => onDataChangeRef.current(updater) },
    rail: {
      clearRevealRequest: clearRailRevealRequest,
      requestReveal: requestRailReveal
    },
    selection: {
      clearDetailError: () => setDetailError(null),
      getActiveSessionId: () => activeConversationIdRef.current,
      setActiveSessionId: (agentSessionId) => {
        setActiveMessageSession(agentSessionId);
        activeConversationIdRef.current = agentSessionId;
        setActiveConversationId(agentSessionId);
      },
      setComposerHome: (home) => {
        isComposerHomeRef.current = home;
        setIsComposerHome(home);
      },
      setIntent
    }
  });

  const removeConversations = useCallback(
    (conversationIds: readonly string[]) => {
      const removedIds = new Set(
        conversationIds
          .map((agentSessionId) => agentSessionId.trim())
          .filter(Boolean)
      );
      for (const agentSessionId of conversationIds) {
        sessionEngine.dispatch({
          type: "session/removed",
          agentSessionId
        });
      }
      if (removedIds.size === 0) return;
      onDataChangeRef.current((current) => {
        const next = forgetAgentGUISessionMemories(current, removedIds);
        dataRef.current = next;
        return next;
      });
    },
    [dataRef, onDataChangeRef, sessionEngine]
  );

  return { ...selection, removeConversations };
}
