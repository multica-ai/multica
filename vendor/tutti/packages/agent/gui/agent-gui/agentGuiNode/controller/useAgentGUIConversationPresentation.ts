import type { IComputedValue } from "mobx";
import { useEffect, useMemo } from "react";
import type { ConversationListPresentationState } from "../../../state/ConversationListPresentationState";
import {
  ConversationOverviewState,
  type ConversationOverviewInput
} from "../../../state/ConversationOverviewState";
import type { AgentConversationServices } from "../../../state/conversation-services";
type Input = Omit<
  ConversationOverviewInput,
  "activityDisplayStatuses" | "conversations" | "transientConversation"
> & {
  conversationServices: AgentConversationServices;
  list: ConversationListPresentationState | null;
  transient: IComputedValue<ConversationOverviewInput["transientConversation"]>;
};
export function useAgentGUIConversationPresentation(input: Input) {
  const {
    activeConversationId,
    list,
    currentUserId,
    data,
    dataRef,
    defaultAgentTargetId,
    normalizedExplicitProviderTargets,
    normalizedProviderTargets,
    onDataChangeRef,
    agentTargetsLoading,
    shouldUseStaticProviderTargets,
    transient,
    userProjects,
    workspacePath,
    conversationServices
  } = input;
  const state = useMemo(
    () =>
      new ConversationOverviewState({
        activeConversationId,
        currentUserId,
        data,
        dataRef,
        defaultAgentTargetId,
        normalizedExplicitProviderTargets,
        normalizedProviderTargets,
        onDataChangeRef,
        agentTargetsLoading,
        shouldUseStaticProviderTargets,
        userProjects,
        workspacePath,
        get conversations() {
          return list?.conversations ?? [];
        },
        get transientConversation() {
          return transient.get();
        },
        get activityDisplayStatuses() {
          return new Map(
            conversationServices.sessions.allConsumerSessions.map((item) => [
              item.session.agentSessionId,
              item.displayStatus
            ])
          );
        }
      }),
    [
      activeConversationId,
      list,
      currentUserId,
      data,
      dataRef,
      defaultAgentTargetId,
      normalizedExplicitProviderTargets,
      normalizedProviderTargets,
      onDataChangeRef,
      agentTargetsLoading,
      shouldUseStaticProviderTargets,
      transient,
      userProjects,
      workspacePath,
      conversationServices
    ]
  );
  useEffect(() => state.startRemembering(), [state]);
  return state;
}
