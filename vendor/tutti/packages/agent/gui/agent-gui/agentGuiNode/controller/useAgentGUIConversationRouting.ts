import type { AgentConversationServices } from "../../../state/conversation-services";

import type { Dispatch, RefObject, SetStateAction } from "react";
import { useCallback, useEffect } from "react";
import type { AgentGUIConversationSummary } from "../model/agentGuiConversationModel";
import {
  normalizeAgentGUIOpenSessionRequest,
  type AgentGUIOpenSessionRequest
} from "./agentGuiController.draftMessageHelpers";
import {
  resolveConversationSummaryById,
  type AgentGUIConversationSelectionOptions,
  type ConversationIntent
} from "./useAgentConversationSelection";

interface UseAgentGUIConversationRoutingInput {
  activeConversationIdRef: RefObject<string | null>;
  conversationListQuery: unknown | null;
  conversations: readonly AgentGUIConversationSummary[];
  conversationsRef: RefObject<AgentGUIConversationSummary[]>;
  handledOpenSessionSequenceRef: RefObject<number | null>;
  hasLoadedConversations: boolean;
  intent: ConversationIntent;
  openSessionRequest: AgentGUIOpenSessionRequest | null | undefined;
  pendingOpenSessionRequestRef: RefObject<AgentGUIOpenSessionRequest | null>;
  selectConversation(
    agentSessionId: string,
    options?: AgentGUIConversationSelectionOptions
  ): void;
  conversationServices: AgentConversationServices;
  setIntent: Dispatch<SetStateAction<ConversationIntent>>;
  transientConversation: AgentGUIConversationSummary | null;
  workspaceId: string;
}

export function useAgentGUIConversationRouting(
  input: UseAgentGUIConversationRoutingInput
): void {
  const {
    activeConversationIdRef,
    conversationListQuery,
    conversations,
    conversationsRef,
    handledOpenSessionSequenceRef,
    hasLoadedConversations,
    intent,
    openSessionRequest,
    pendingOpenSessionRequestRef,
    selectConversation,
    conversationServices,
    setIntent,
    transientConversation,
    workspaceId
  } = input;

  const ensureTransientOpenSessionConversation = useCallback(
    (agentSessionId: string) => {
      const normalizedAgentSessionId = agentSessionId.trim();
      if (!normalizedAgentSessionId) return;
      if (
        resolveConversationSummaryById(
          conversationsRef.current,
          normalizedAgentSessionId,
          transientConversation
        )
      ) {
        return;
      }
      const consumerSession = conversationServices.sessions.consumerSession(
        normalizedAgentSessionId
      );
      if (consumerSession && consumerSession.session.visible !== false) return;
      const reconcile = conversationServices.sessionLoads.record(
        normalizedAgentSessionId
      );
      if (
        reconcile?.inFlight ||
        reconcile?.pendingMessages ||
        reconcile?.pendingState
      ) {
        return;
      }
      conversationServices.sessionLoads.request(normalizedAgentSessionId, {
        state: true,
        messages: true
      });
    },
    [conversationServices, transientConversation, workspaceId]
  );

  useEffect(() => {
    const normalizedOpenSessionRequest =
      normalizeAgentGUIOpenSessionRequest(openSessionRequest);
    if (
      normalizedOpenSessionRequest &&
      handledOpenSessionSequenceRef.current !==
        normalizedOpenSessionRequest.sequence
    ) {
      handledOpenSessionSequenceRef.current =
        normalizedOpenSessionRequest.sequence;
      pendingOpenSessionRequestRef.current = normalizedOpenSessionRequest;
    }
    const pendingOpenSessionRequest = pendingOpenSessionRequestRef.current;
    const hasExplicitOpenSessionRequest = Boolean(
      pendingOpenSessionRequest?.agentSessionId.trim()
    );
    const resolveId = (id: string) =>
      resolveConversationSummaryById(
        conversations,
        id,
        transientConversation
      ) !== null;
    const resolveCanonicalId = (id: string) =>
      resolveConversationSummaryById(conversations, id, null) !== null;
    if (hasExplicitOpenSessionRequest) {
      const requestedId = pendingOpenSessionRequest!.agentSessionId.trim();
      if (!hasLoadedConversations) return;
      pendingOpenSessionRequestRef.current = null;
      selectConversation(requestedId, {
        reloadConversations: false,
        reveal: "external-open"
      });
      ensureTransientOpenSessionConversation(requestedId);
      return;
    }

    if (intent.tag !== "home") {
      if (
        conversationServices.activations.failedResolution(
          intent.id,
          Boolean(conversationServices.sessions.session(intent.id)),
          intent.tag === "active"
            ? { selectionSource: intent.source }
            : undefined
        ) === "rollback"
      ) {
        // Terminal activation settlement clears the optimistic selection in an
        // earlier effect. Do not let this effect's stale active/requested intent
        // select the provisional session again during the same effect flush.
        setIntent({ tag: "home" });
        return;
      }
    }

    switch (intent.tag) {
      case "home":
        return;
      case "active":
        if (resolveCanonicalId(intent.id)) return;
        if (resolveId(intent.id)) return;
        // An active intent is produced by an explicit user/session selection.
        // Rail pages are bounded and may not contain that selected session yet;
        // list absence must not demote it into the requested/fallback flow.
        if (activeConversationIdRef.current === intent.id) return;
        if (!hasLoadedConversations) return;
        setIntent({ tag: "requested", id: intent.id });
        return;
      case "requested":
        if (!hasLoadedConversations) return;
        // Persisted/external selection is authoritative. The bounded list may
        // not contain it after restart, so activate it and reconcile detail
        // instead of replacing it with the first visible rail row.
        selectConversation(intent.id, { reloadConversations: false });
        ensureTransientOpenSessionConversation(intent.id);
        return;
    }
  }, [
    conversationListQuery,
    conversations,
    ensureTransientOpenSessionConversation,
    hasLoadedConversations,
    intent,
    openSessionRequest,
    selectConversation,
    transientConversation
  ]);
}
