import { useMemo } from "react";
import { useAgentGUIViewModel } from "../model/useAgentGUIViewModel";
import type { AgentGUIProviderRailMode } from "../../../types";
import type { AgentGUIDetailViewModel } from "../model/agentGuiNodeTypes";
import { useAgentGUIComposerPresentation } from "./useAgentGUIComposerPresentation";
import { useAgentGUIControllerActions } from "./useAgentGUIControllerActions";
import { useAgentGUIConversationDetail } from "./useAgentGUIConversationDetail";
import { useAgentGUIConversationPresentation } from "./useAgentGUIConversationPresentation";
import { useAgentGUIProviderHome } from "./useAgentGUIProviderHome";
import { useAgentGUISessionPresentation } from "./useAgentGUISessionPresentation";
import type { useAgentGUIOperationActions } from "./useAgentGUIOperationActions";
import type { useAgentGUIProviderCatalogSelection } from "./useAgentGUIProviderCatalogSelection";
import type { useAgentGUILocalState } from "./useAgentGUILocalState";
import type { useAgentGUIComposerCapabilities } from "./useAgentGUIComposerCapabilities";
import type { useAgentGUISessionDetailTransport } from "./useAgentGUISessionDetailTransport";
import { resolveAgentGUIProviderReadinessGateForView } from "../model/agentGuiProviderReadiness";
import { targetConnectionForAgentGUIView } from "./agentGuiController.providerHelpers";
import { isAgentGUIAgentTargetComingSoon } from "../../../agentTargets";
import { useModelValue } from "../../../shared/engine/useEngineSelector";
import type { AgentGUIRuntime } from "../../../agentActivityRuntime";

type ConversationPresentationInput = Parameters<
  typeof useAgentGUIConversationPresentation
>[0];
type ConversationDetailInput = Omit<
  Parameters<typeof useAgentGUIConversationDetail>[0],
  "activeConversation" | "activeSessionView"
>;
type ActiveSessionViewProjection = Parameters<
  typeof useAgentGUIConversationDetail
>[0]["activeSessionView"];
type ComposerPresentationInput = Omit<
  Parameters<typeof useAgentGUIComposerPresentation>[0],
  "activeConversation"
>;
type SessionPresentationInput = Omit<
  Parameters<typeof useAgentGUISessionPresentation>[0],
  | "activeConversation"
  | "activeLiveState"
  | "activationError"
  | "activationErrorCode"
  | "conversation"
  | "isInterrupting"
  | "pendingApproval"
  | "providerReadinessGate"
  | "selectedAgentTargetUnavailable"
  | "selectedAgentTargetUnavailableReason"
  | "selectedAgentTargetOwnerLabel"
  | "serverInteractivePrompt"
>;
type ProviderHomeInput = Parameters<typeof useAgentGUIProviderHome>[0];
type OperationActions = ReturnType<typeof useAgentGUIOperationActions>;
type ProviderCatalog = ReturnType<typeof useAgentGUIProviderCatalogSelection>;
type LocalState = ReturnType<typeof useAgentGUILocalState>;
type ComposerCapabilities = ReturnType<typeof useAgentGUIComposerCapabilities>;
type SessionDetailTransport = ReturnType<
  typeof useAgentGUISessionDetailTransport
>;

type UseAgentGUIViewAssemblyInput = ConversationPresentationInput &
  ConversationDetailInput &
  ComposerPresentationInput &
  SessionPresentationInput &
  ProviderHomeInput &
  ProviderCatalog &
  LocalState &
  ComposerCapabilities &
  SessionDetailTransport &
  OperationActions & {
    agentActivityRuntime: AgentGUIRuntime;
    nodeId?: string | null;
    operationActions: OperationActions;
    detailAvailability: AgentGUIDetailViewModel["availability"];
    updateSelectedProjectPath: Parameters<
      typeof useAgentGUIControllerActions
    >[0]["updateSelectedProjectPath"];
    selectConversation: Parameters<
      typeof useAgentGUIControllerActions
    >[0]["selectConversation"];
    providerRailMode: AgentGUIProviderRailMode | undefined;
  };

export function useAgentGUIViewAssembly(input: UseAgentGUIViewAssemblyInput) {
  const { activeConversation, visibleConversations } =
    useAgentGUIConversationPresentation(input);
  const { mutations } = input.agentActivityRuntime;
  const forkThroughTurnPendingTurnIds = useModelValue(
    () => mutations.pendingForkTurnIds(input.activeConversationId),
    [mutations, input.activeConversationId]
  );
  const targetConnection = useMemo(
    () =>
      targetConnectionForAgentGUIView({
        activeConversation,
        selectedTarget: input.effectiveSelectedProviderTarget,
        targets: input.normalizedProviderTargets
      }),
    [
      activeConversation,
      input.effectiveSelectedProviderTarget,
      input.normalizedProviderTargets
    ]
  );
  const stableActiveSessionViewProjection =
    useMemo<ActiveSessionViewProjection>(
      () =>
        input.activeSessionView
          ? {
              hasOlderMessages: input.activeSessionView.hasOlderMessages,
              isLoadingOlderMessages:
                input.activeSessionView.isLoadingOlderMessages,
              olderMessageCount: input.activeMessages.length,
              oldestLoadedVersion: input.activeSessionView.oldestLoadedVersion
            }
          : null,
      [
        input.activeSessionView?.hasOlderMessages,
        input.activeSessionView?.isLoadingOlderMessages,
        input.activeSessionView?.oldestLoadedVersion,
        input.activeMessages.length
      ]
    );
  const detail = useAgentGUIConversationDetail({
    ...input,
    activeConversation,
    activeSessionView: stableActiveSessionViewProjection
  });
  const { stableComposerSettings } = useAgentGUIComposerPresentation({
    ...input,
    activeConversation
  });
  const providerReadinessGate = useMemo(
    () =>
      input.activeConversationId === null &&
      isAgentGUIAgentTargetComingSoon(
        input.effectiveSelectedProviderTarget,
        input.normalizedComingSoonProviders
      )
        ? ({ status: "coming_soon" } as const)
        : resolveAgentGUIProviderReadinessGateForView({
            activeConversationId: input.activeConversationId,
            providerReadinessGates: input.providerReadinessGates,
            selectedProvider: input.effectiveSelectedProviderTarget.provider
          }),
    [
      input.activeConversationId,
      input.effectiveSelectedProviderTarget,
      input.normalizedComingSoonProviders,
      input.providerReadinessGates
    ]
  );
  const selectedAgentTargetUnavailable =
    input.activeConversationId === null &&
    input.effectiveSelectedProviderTarget.disabled === true &&
    !isAgentGUIAgentTargetComingSoon(
      input.effectiveSelectedProviderTarget,
      input.normalizedComingSoonProviders
    );
  const selectedAgentTargetUnavailableReason = selectedAgentTargetUnavailable
    ? input.effectiveSelectedProviderTarget.availability?.reason?.trim() ||
      input.effectiveSelectedProviderTarget.unavailableReason?.trim() ||
      null
    : null;
  const selectedAgentTargetOwnerLabel =
    input.effectiveSelectedProviderTarget.ownerLabel?.trim() || null;
  const session = useAgentGUISessionPresentation({
    ...input,
    activeConversation,
    currentUserId: input.currentUserId,
    ownerDeviceLabel: targetConnection.ownerDeviceLabel,
    providerReadinessGate,
    selectedAgentTargetUnavailable,
    selectedAgentTargetUnavailableReason,
    selectedAgentTargetOwnerLabel,
    targetConnectionAgentTargetId: targetConnection.agentTargetId,
    activeLiveState: detail.activeLiveState,
    activationError: detail.activationError,
    activationErrorCode: detail.activationErrorCode,
    conversation: detail.conversation,
    isInterrupting: detail.isInterrupting,
    pendingApproval: detail.pendingApproval,
    serverInteractivePrompt: detail.serverInteractivePrompt
  });
  const pendingQuestionComposerTarget = detail.pendingQuestionComposerTarget;
  const pendingInteractivePrompt = session.pendingInteractivePrompt;
  const canAnswerPendingInteractivePromptFromComposer =
    pendingInteractivePrompt?.kind === "ask-user" &&
    pendingQuestionComposerTarget !== null &&
    pendingInteractivePrompt.agentSessionId?.trim() ===
      pendingQuestionComposerTarget.agentSessionId &&
    pendingInteractivePrompt.turnId?.trim() ===
      pendingQuestionComposerTarget.turnId &&
    pendingInteractivePrompt.requestId ===
      pendingQuestionComposerTarget.requestId;
  const railConversations = useMemo(() => {
    const prompt = session.pendingInteractivePrompt;
    if (prompt?.kind !== "plan-implementation" || !input.activeConversationId) {
      return visibleConversations;
    }
    let changed = false;
    const next = visibleConversations.map((conversation) => {
      if (
        conversation.id !== input.activeConversationId ||
        conversation.needsUserAction
      ) {
        return conversation;
      }
      changed = true;
      return { ...conversation, needsUserAction: true };
    });
    return changed ? next : visibleConversations;
  }, [
    input.activeConversationId,
    session.pendingInteractivePrompt,
    visibleConversations
  ]);
  const railActiveConversation = useMemo(() => {
    if (
      !activeConversation ||
      session.pendingInteractivePrompt?.kind !== "plan-implementation" ||
      activeConversation.needsUserAction
    ) {
      return activeConversation;
    }
    return { ...activeConversation, needsUserAction: true };
  }, [activeConversation, session.pendingInteractivePrompt]);
  const providerHome = useAgentGUIProviderHome(input);
  const controllerActions = useAgentGUIControllerActions({
    ...input.operationActions,
    ...providerHome,
    loadOlderConversationMessages: input.loadOlderConversationMessages,
    selectConversation: input.selectConversation,
    updateSelectedProjectPath: input.updateSelectedProjectPath
  });
  const viewData =
    input.activeConversationId === null
      ? input.selectedComposerTargetData.data
      : input.data;
  const viewModel = useAgentGUIViewModel({
    shell: {
      nodeId: input.nodeId?.trim() || null,
      workspaceId: input.workspaceId,
      workspacePath: input.workspacePath,
      currentUserId: input.currentUserId,
      data: viewData
    },
    rail: {
      selectedAgentTarget: input.effectiveSelectedProviderTarget,
      agentTargets: input.normalizedProviderTargets,
      agentTargetsLoading: input.agentTargetsLoading,
      providerRailMode: input.providerRailMode ?? "catalog",
      comingSoonProviders: input.normalizedComingSoonProviders,
      conversationFilter: input.conversationFilter,
      conversations: railConversations,
      userProjects: input.userProjects,
      activeConversation: railActiveConversation,
      activeConversationId: input.activeConversationId,
      revealRequest: input.railRevealRequest,
      isLoadingConversations: input.isLoadingConversations,
      listError: input.listError
    },
    detail: {
      availability: input.detailAvailability,
      isLoadingMessages: input.isLoadingMessages,
      isLoadingOlderMessages:
        input.activeSessionView?.isLoadingOlderMessages ?? false,
      hasOlderMessages: input.activeSessionView?.hasOlderMessages ?? false,
      usage: input.usage,
      hasSentUserMessage: session.hasSentUserMessage,
      avoidGroupingEdits: input.avoidGroupingEdits,
      conversation: detail.conversation,
      conversationDetail: detail.conversationDetail
    },
    composer: {
      handoffAgentTargets: input.handoffAgentTargets,
      availableCommands: detail.availableCommands,
      availableSkills: detail.availableSkills,
      draftPrompt: detail.draftPrompt,
      draftContent: detail.draftContent,
      isCreatingConversation: input.isCreatingConversation,
      isSubmitting: input.isSubmitting,
      isInterrupting: detail.isInterrupting,
      isCancelPending: detail.isCancelPending,
      hasPendingSubmitStopTarget: session.hasPendingSubmitStopTarget,
      promptImagesSupported: input.promptImagesSupported,
      compactSupported: input.compactSupported,
      gate: session.composerGate,
      composerSettings: stableComposerSettings,
      queueStatus: detail.queueStatus,
      queuedPrompts: detail.queuedPrompts,
      drainingQueuedPromptId: detail.drainingQueuedPromptId
    },
    interaction: {
      approvalDisabledReason: session.approvalDisabledReason,
      interactivePromptDisabledReason: session.interactivePromptDisabledReason,
      isRespondingApproval: session.isRespondingApproval,
      isRespondingInteractivePrompt: session.isRespondingInteractivePrompt,
      canAnswerPendingInteractivePromptFromComposer,
      pendingApproval: session.pendingApproval,
      pendingInteractivePrompt: session.pendingInteractivePrompt,
      sessionChrome: session.sessionChrome,
      inlineNotice: detail.effectiveDetailError
        ? {
            id: `agent-gui-detail-error:${input.activeConversationId ?? "current"}`,
            message: detail.effectiveDetailError,
            tone: "error" as const,
            autoDismissMs: null
          }
        : null
    },
    readiness: {
      activeLiveState: detail.activeLiveState,
      activationError: detail.activationError,
      providerReadinessGate
    },
    operations: {
      forkThroughTurnPendingTurnIds,
      isDeletingConversation: input.isDeletingConversation,
      isUserProjectMutationPending: input.isUserProjectMutationPending,
      pendingDeleteConversation: input.pendingDeleteConversation,
      pendingDeleteProjectConversations: input.pendingDeleteProjectConversations
    }
  });
  return useMemo(
    () => ({ viewModel, actions: controllerActions }),
    [controllerActions, viewModel]
  );
}
