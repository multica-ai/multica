import {
  type AgentActivityMessage,
  type AgentSessionFamilySnapshot
} from "@tutti-os/agent-activity-core";
import type { RefObject } from "react";
import { useCallback } from "react";
import { useAgentSessionControllerState } from "../../../contexts/workspace/presentation/renderer/agentSessions/useAgentSessionControllerState";
import { type ConversationServices } from "../../../conversationServices";
import type { AgentConversationServices } from "../../../state/conversation-services";
import { conversationTranscriptStateFor } from "../../../state/ConversationTranscriptState";
import type { AgentGUINodeData } from "../../../types";
import {
  reportAgentGUIMessagePageDiagnostic,
  reportConversationServicesError
} from "./agentGuiController.reporting";
import { useAgentConversationMessagePaging } from "./useAgentConversationMessagePaging";

const NO_MESSAGES: readonly AgentActivityMessage[] = [];

export function useAgentGUISessionDetailTransport(input: {
  activeConversationId: string | null;
  activeConversationIdRef: RefObject<string | null>;
  agentActivityRuntime: ConversationServices;
  agentActivityRuntimeOrigin: string;
  dataRef: RefObject<AgentGUINodeData>;
  isMountedRef: RefObject<boolean>;
  sessionFamily: AgentSessionFamilySnapshot;
  conversationServices: AgentConversationServices;
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
    conversationServices,
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
  const { transcript } = agentActivityRuntime;
  const transcriptState = activeConversationId
    ? conversationTranscriptStateFor(transcript, activeConversationId)
    : null;
  const state = useAgentSessionControllerState(
    sessionViewRef(activeConversationId),
    transcriptState?.oldestLoadedVersion ?? null,
    transcriptState?.hasOlderMessages ?? false
  );
  const resolveSessionMessages = useCallback(
    (agentSessionId: string | null | undefined) => {
      const normalized = agentSessionId?.trim() ?? "";
      if (!normalized) return [];
      return transcript.messages(normalized);
    },
    [transcript]
  );
  const loadSessionState = useCallback(
    (agentSessionId: string) => {
      const normalized = agentSessionId.trim();
      if (!normalized) return;
      conversationServices.sessionLoads.request(normalized, {
        state: true,
        messages: false
      });
    },
    [conversationServices, workspaceId]
  );
  const paging = useAgentConversationMessagePaging({
    diagnostics: {
      error: ({ agentSessionId, context, error, phase }) =>
        reportConversationServicesError({
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
    conversationServices,
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

    resolveSessionMessages,
    setActiveMessageSession: paging.setActiveSession,
    sessionViewRef
  };
}
