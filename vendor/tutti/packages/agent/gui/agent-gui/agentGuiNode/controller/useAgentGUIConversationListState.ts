import { useAgentGUIRuntime } from "../../../agentActivityRuntime";
import {
  type AgentSessionEngine
} from "@tutti-os/agent-activity-core";
import { useMemo, useRef, useState } from "react";
import {
  useAgentGuiConversationList,
  type AgentGUIConversationListQuery
} from "../../../contexts/workspace/presentation/renderer/agentGuiConversationList/useAgentGuiConversationList";
import type { AgentGUINodeData, AgentGUIAgentTarget } from "../../../types";
import {
  createAgentGUIConversationFilterState,
  type AgentGUIConversationFilter
} from "../model/agentGuiConversationFilter";

interface UseAgentGUIConversationListStateInput {
  agentActivityRuntimeOrigin: string;
  currentUserId?: string | null;
  data: AgentGUINodeData;
  normalizedProviderTargets: readonly AgentGUIAgentTarget[];
  sessionEngine: AgentSessionEngine;
  workspaceId: string;
}

export function useAgentGUIConversationListState({
  agentActivityRuntimeOrigin,
  currentUserId,
  data,
  normalizedProviderTargets,
  sessionEngine,
  workspaceId
}: UseAgentGUIConversationListStateInput) {
  const { transcript } = useAgentGUIRuntime();
  const [conversationFilter, setConversationFilter] =
    useState<AgentGUIConversationFilter>(
      () => createAgentGUIConversationFilterState().filter
    );
  const conversationFilterRef = useRef(conversationFilter);
  conversationFilterRef.current = conversationFilter;
  const conversationListQuery =
    useMemo<AgentGUIConversationListQuery | null>(() => {
      const userId = currentUserId?.trim() ?? "";
      const provider = data.provider?.trim() ?? "";
      if (!workspaceId.trim() || !userId || !provider) {
        return null;
      }
      return {
        conversationFilter,
        workspaceId,
        userId,
        provider: data.provider,
        sessionOrigin: agentActivityRuntimeOrigin
      };
    }, [
      agentActivityRuntimeOrigin,
      conversationFilter,
      currentUserId,
      data.provider,
      workspaceId
    ]);
  const conversationListState = useAgentGuiConversationList(
    sessionEngine,
    transcript,
    conversationListQuery,
    normalizedProviderTargets
  );
  const conversations = conversationListState?.conversations ?? [];

  return {
    conversationFilter,
    conversationFilterRef,
    conversationListQuery,
    conversationListState,
    conversations,
    setConversationFilter
  };
}
