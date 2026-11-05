type AgentGUIView = {
  shell: AgentGUIShellViewModel;
  rail: AgentGUIRailViewModel;
  detail: AgentGUIDetailViewModel;
  composer: AgentGUIComposerViewModel;
  interaction: AgentGUIInteractionViewModel;
  readiness: AgentGUIReadinessViewModel;
  operations: AgentGUIOperationsViewModel;
};
import type {
  AgentGUIComposerViewModel,
  AgentGUIDetailViewModel,
  AgentGUIInteractionViewModel,
  AgentGUIOperationsViewModel,
  AgentGUIRailViewModel,
  AgentGUIReadinessViewModel,
  AgentGUIShellViewModel
} from "./agentGuiNodeTypes";

export type FlatAgentGUIViewFixture = AgentGUIShellViewModel &
  AgentGUIRailViewModel &
  AgentGUIDetailViewModel &
  Omit<AgentGUIComposerViewModel, "state"> &
  Pick<
    AgentGUIComposerViewModel["state"],
    | "handoffAgentTargets"
    | "availableCommands"
    | "availableSkills"
    | "promptImagesSupported"
    | "compactSupported"
    | "gate"
    | "composerSettings"
  > &
  AgentGUIInteractionViewModel &
  AgentGUIReadinessViewModel &
  AgentGUIOperationsViewModel & {
    forkThroughTurnPendingTurnIds?: readonly string[];
    isCreatingConversation?: boolean;
    isSubmitting?: boolean;
    isInterrupting?: boolean;
    isCancelPending?: boolean;
    hasPendingSubmitStopTarget?: boolean;
  };

export type AgentGUIViewFixtureOverrides = Partial<FlatAgentGUIViewFixture>;

export function groupAgentGUIViewFixture(
  flat: FlatAgentGUIViewFixture
): AgentGUIView {
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
      avoidGroupingEdits: flat.avoidGroupingEdits
    },
    composer: {
      draftPrompt: flat.draftPrompt,
      draftContent: flat.draftContent,
      state: {
        usage: flat.usage,
        hasCompactableContext: flat.hasSentUserMessage,
        isSendingTurn:
          flat.gate.conversationBusy || flat.gate.isAwaitingTurnStart === true,
        isSubmittingPrompt: flat.pendingInteractivePrompt
          ? flat.isRespondingInteractivePrompt
          : flat.isRespondingApproval,
        activeTurnId: null,
        handoffAgentTargets: flat.handoffAgentTargets,
        availableCommands: flat.availableCommands,
        availableSkills: flat.availableSkills,
        promptImagesSupported: flat.promptImagesSupported,
        compactSupported: flat.compactSupported,
        gate: flat.gate,
        composerSettings: flat.composerSettings
      }
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
      isDeletingConversation: flat.isDeletingConversation,
      isUserProjectMutationPending: flat.isUserProjectMutationPending ?? false,
      pendingDeleteConversation: flat.pendingDeleteConversation,
      pendingDeleteProjectConversations: flat.pendingDeleteProjectConversations
    }
  };
}

export function flattenAgentGUIViewFixture({
  shell,
  rail,
  detail,
  composer,
  interaction,
  readiness,
  operations
}: AgentGUIView): FlatAgentGUIViewFixture {
  return {
    ...shell,
    ...rail,
    ...detail,
    ...composer,
    ...composer.state,
    ...interaction,
    ...readiness,
    ...operations
  };
}
