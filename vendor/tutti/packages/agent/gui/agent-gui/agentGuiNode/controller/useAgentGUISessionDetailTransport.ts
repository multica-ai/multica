import {
  selectSessionMessages,
  selectSessionMessageWindow,
  type AgentSessionFamilySnapshot,
  type AgentSessionEngine
} from "@tutti-os/agent-activity-core";
import type { RefObject } from "react";
import { useCallback } from "react";
import {
  useAgentActivitySessionMessages,
  type AgentGUIRuntime
} from "../../../agentActivityRuntime";
import { useAgentSessionControllerState } from "../../../contexts/workspace/presentation/renderer/agentSessions/useAgentSessionControllerState";
import type { AgentGUINodeData } from "../../../types";
import {
  reportAgentGUIMessagePageDiagnostic,
  reportAgentGUIRuntimeError
} from "./agentGuiController.reporting";
import { useAgentConversationMessagePaging } from "./useAgentConversationMessagePaging";
import { useEngineSelector } from "../../../shared/engine/useEngineSelector";

export function useAgentGUISessionDetailTransport(input: {
  activeConversationId: string | null;
  activeConversationIdRef: RefObject<string | null>;
  agentActivityRuntime: AgentGUIRuntime;
  agentActivityRuntimeOrigin: string;
  dataRef: RefObject<AgentGUINodeData>;
  isMountedRef: RefObject<boolean>;
  sessionFamily: AgentSessionFamilySnapshot;
  sessionEngine: AgentSessionEngine;
  workspaceId: string;
}) {
  const {
    activeConversationId,
    activeConversationIdRef,
    agentActivityRuntime,
    agentActivityRuntimeOrigin,
    dataRef,
    isMountedRef,
    sessionFamily,
    sessionEngine,
    workspaceId
  } = input;
  const sessionViewRef = useCallback(
    (agentSessionId: string | null | undefined) => ({
      workspaceId,
      agentSessionId,
      origin: agentActivityRuntimeOrigin
    }),
    [agentActivityRuntimeOrigin, workspaceId]
  );
  // Only the fallback for history coverage without a window: the user's messages suffice.
  const activeCanonicalMessages = useEngineSelector(
    sessionEngine,
    (engineState) => selectSessionMessages(engineState, activeConversationId)
  );
  const projectedSessionMessagesById = useAgentActivitySessionMessages([
    activeConversationId,
    sessionFamily.rootSession?.agentSessionId,
    ...sessionFamily.childSessions.map((session) => session.agentSessionId)
  ]);
  const activeCanonicalWindow = useEngineSelector(
    sessionEngine,
    (engineState) =>
      selectSessionMessageWindow(engineState, activeConversationId)
  );
  const state = useAgentSessionControllerState(
    sessionViewRef(activeConversationId),
    activeCanonicalMessages,
    activeCanonicalWindow
  );
  const resolveSessionMessages = useCallback(
    (agentSessionId: string | null | undefined) => {
      const normalized = agentSessionId?.trim() ?? "";
      if (!normalized) return [];
      return (
        projectedSessionMessagesById[normalized] ??
        agentActivityRuntime.transcript.messages(normalized)
      );
    },
    [agentActivityRuntime, projectedSessionMessagesById]
  );
  const loadSessionState = useCallback(
    (agentSessionId: string) => {
      const normalized = agentSessionId.trim();
      if (!normalized) return;
      sessionEngine.dispatch({
        agentSessionId: normalized,
        needsMessages: false,
        needsState: true,
        type: "session/reconcileRequested",
        workspaceId
      });
    },
    [sessionEngine, workspaceId]
  );
  const paging = useAgentConversationMessagePaging({
    diagnostics: {
      error: ({ agentSessionId, context, error, phase }) =>
        reportAgentGUIRuntimeError({
          agentSessionId,
          context,
          error,
          phase,
          provider: dataRef.current.provider,
          runtime: agentActivityRuntime,
          workspaceId
        }),
      page: ({ agentSessionId, details, event, level, messages }) =>
        reportAgentGUIMessagePageDiagnostic({
          agentSessionId,
          details,
          event,
          level,
          messages,
          runtime: agentActivityRuntime,
          workspaceId
        })
    },
    getActiveSessionId: () => activeConversationIdRef.current,
    isMounted: () => isMountedRef.current,
    onOlderPageLoadingChanged: (loading) =>
      state.setAgentSessionViewOlderMessagesLoading(
        sessionViewRef(activeConversationIdRef.current),
        loading
      ),
    runtime: agentActivityRuntime,
    sessionEngine,
    workspaceId
  });
  const markSelectedConversationDetailPending = useCallback(
    (agentSessionId: string) => {
      const normalized = agentSessionId.trim();
      if (!normalized) return null;
      const ref = sessionViewRef(normalized);
      state.setAgentSessionViewError(ref, null);
      return normalized;
    },
    [sessionViewRef, state]
  );

  return {
    ...state,
    loadOlderConversationMessages: paging.loadOlderMessages,
    loadSelectedConversationMessages: paging.loadInitialMessages,
    loadSessionState,
    markSelectedConversationDetailPending,
    projectedSessionMessagesById,
    resolveSessionMessages,
    setActiveMessageSession: paging.setActiveSession,
    sessionViewRef
  };
}
