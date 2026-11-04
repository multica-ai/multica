import type {
  AgentGUIComposerViewModel,
  AgentGUIDetailViewModel,
  AgentGUIInteractionViewModel,
  AgentGUINodeViewModel,
  AgentGUIOperationsViewModel,
  AgentGUIRailViewModel,
  AgentGUIReadinessViewModel,
  AgentGUIShellViewModel
} from "./agentGuiNodeTypes";

export type FlatAgentGUINodeViewModelFixture = AgentGUIShellViewModel &
  AgentGUIRailViewModel &
  AgentGUIDetailViewModel &
  AgentGUIComposerViewModel &
  AgentGUIInteractionViewModel &
  AgentGUIReadinessViewModel &
  AgentGUIOperationsViewModel;

export type AgentGUINodeViewModelFixtureOverrides =
  Partial<FlatAgentGUINodeViewModelFixture>;

export function groupAgentGUINodeViewModelFixture(
  flat: FlatAgentGUINodeViewModelFixture
): AgentGUINodeViewModel {
  return {
    shell: {
      workspaceId: flat.workspaceId,
      workspacePath: flat.workspacePath,
      currentUserId: flat.currentUserId,
      data: flat.data
    },
    rail: {
      selectedAgentTarget: flat.selectedAgentTarget,
      agentTargets: flat.agentTargets,
      agentTargetsLoading: flat.agentTargetsLoading,
      providerRailMode: flat.providerRailMode,
      comingSoonProviders: flat.comingSoonProviders,
      conversationFilter: flat.conversationFilter,
      conversations: flat.conversations,
      userProjects: flat.userProjects,
      activeConversation: flat.activeConversation,
      activeConversationId: flat.activeConversationId,
      revealRequest: flat.revealRequest,
      isLoadingConversations: flat.isLoadingConversations,
      listError: flat.listError
    },
    detail: {
      availability: flat.availability,
      isLoadingMessages: flat.isLoadingMessages,
      isLoadingOlderMessages: flat.isLoadingOlderMessages,
      hasOlderMessages: flat.hasOlderMessages,
      usage: flat.usage,
      hasSentUserMessage: flat.hasSentUserMessage,
      avoidGroupingEdits: flat.avoidGroupingEdits,
      conversation: flat.conversation,
      conversationDetail: flat.conversationDetail
    },
    composer: {
      handoffAgentTargets: flat.handoffAgentTargets,
      availableCommands: flat.availableCommands,
      availableSkills: flat.availableSkills,
      draftPrompt: flat.draftPrompt,
      draftContent: flat.draftContent,
      isCreatingConversation: flat.isCreatingConversation,
      isSubmitting: flat.isSubmitting,
      isInterrupting: flat.isInterrupting,
      isCancelPending: flat.isCancelPending,
      hasPendingSubmitStopTarget: flat.hasPendingSubmitStopTarget,
      promptImagesSupported: flat.promptImagesSupported,
      compactSupported: flat.compactSupported,
      gate: flat.gate,
      composerSettings: flat.composerSettings,
      queueStatus: flat.queueStatus,
      queuedPrompts: flat.queuedPrompts,
      drainingQueuedPromptId: flat.drainingQueuedPromptId
    },
    interaction: {
      approvalDisabledReason: flat.approvalDisabledReason ?? null,
      interactivePromptDisabledReason:
        flat.interactivePromptDisabledReason ?? null,
      isRespondingApproval: flat.isRespondingApproval,
      isRespondingInteractivePrompt:
        flat.isRespondingInteractivePrompt ?? flat.isRespondingApproval,
      pendingApproval: flat.pendingApproval,
      pendingInteractivePrompt: flat.pendingInteractivePrompt,
      sessionChrome: flat.sessionChrome,
      inlineNotice: flat.inlineNotice
    },
    readiness: {
      activeLiveState: flat.activeLiveState,
      activationError: flat.activationError,
      providerReadinessGate: flat.providerReadinessGate
    },
    operations: {
      forkThroughTurnPendingTurnIds: flat.forkThroughTurnPendingTurnIds ?? [],
      isDeletingConversation: flat.isDeletingConversation,
      isUserProjectMutationPending: flat.isUserProjectMutationPending ?? false,
      pendingDeleteConversation: flat.pendingDeleteConversation,
      pendingDeleteProjectConversations: flat.pendingDeleteProjectConversations
    }
  };
}

export function flattenAgentGUINodeViewModelFixture(
  viewModel: AgentGUINodeViewModel
): FlatAgentGUINodeViewModelFixture {
  return {
    ...viewModel.shell,
    ...viewModel.rail,
    ...viewModel.detail,
    ...viewModel.composer,
    ...viewModel.interaction,
    ...viewModel.readiness,
    ...viewModel.operations
  };
}
