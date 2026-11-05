import { useMemo } from "react";
import type {
  AgentGUITranscriptReader,
  AgentGUIWorkspaceLoadReader
} from "../../../../../conversationServices";
import type { AgentConversationServices } from "../../../../../state/conversation-services";
import {
  ConversationListPresentationState,
  type AgentGUIConversationListQuery
} from "../../../../../state/ConversationListPresentationState";
import type { AgentGUIAgentTarget } from "../../../../../types";
export type { AgentGUIConversationListQuery };
const EMPTY_TARGETS: readonly AgentGUIAgentTarget[] = [];
export function useAgentGuiConversationList(
  engine: AgentConversationServices,
  transcript: AgentGUITranscriptReader,
  workspaceLoad: AgentGUIWorkspaceLoadReader,
  query: AgentGUIConversationListQuery | null,
  agentTargets: readonly AgentGUIAgentTarget[] = EMPTY_TARGETS
) {
  return useMemo(
    () =>
      query
        ? new ConversationListPresentationState(
            engine,
            transcript,
            workspaceLoad,
            query,
            agentTargets
          )
        : null,
    [engine, transcript, workspaceLoad, query, agentTargets]
  );
}
