import { useCallback, useMemo } from "react";
import { isAgentGUIAgentTargetComingSoon } from "../../../agentTargets";
import { ComposerFeatureState } from "../../../state/ComposerFeatureState";
import { ConversationDetailState } from "../../../state/ConversationDetailState";
import { RailPresentationState } from "../../../state/RailPresentationState";
import type { AgentGUIProviderRailMode } from "../../../types";
import { agentComposerDraftPrompt } from "../model/agentComposerDraft";
import { resolveAgentComposerDraftScopeKey } from "../model/agentComposerDraftScope";
import { resolveAgentGUIProviderReadinessGateForView } from "../model/agentGuiProviderReadiness";
import {
  EMPTY_AGENT_COMPOSER_DRAFT,
  readAgentComposerDraftContent
} from "./agentGuiController.draftMessageHelpers";
import { useAgentGUIComposerPresentation } from "./useAgentGUIComposerPresentation";
import { useAgentGUIControllerActions } from "./useAgentGUIControllerActions";
import { useAgentGUIConversationPresentation } from "./useAgentGUIConversationPresentation";
import type { useAgentGUILocalState } from "./useAgentGUILocalState";
import type { useAgentGUIOperationActions } from "./useAgentGUIOperationActions";
import type { useAgentGUIProviderCatalogSelection } from "./useAgentGUIProviderCatalogSelection";
import { useAgentGUIProviderHome } from "./useAgentGUIProviderHome";
import type { useAgentGUISessionDetailTransport } from "./useAgentGUISessionDetailTransport";
import { useAgentGUISessionPresentation } from "./useAgentGUISessionPresentation";

type OperationActions = ReturnType<typeof useAgentGUIOperationActions>;
type Input = Parameters<typeof useAgentGUIConversationPresentation>[0] &
  Omit<Parameters<typeof useAgentGUIComposerPresentation>[0], "conversation"> &
  Omit<
    Parameters<typeof useAgentGUISessionPresentation>[0],
    | "conversation"
    | "selectedTarget"
    | "targets"
    | "providerReadinessGate"
    | "selectedAgentTargetUnavailable"
    | "selectedAgentTargetUnavailableReason"
    | "selectedAgentTargetOwnerLabel"
  > &
  Parameters<typeof useAgentGUIProviderHome>[0] &
  ReturnType<typeof useAgentGUIProviderCatalogSelection> &
  ReturnType<typeof useAgentGUILocalState> &
  ReturnType<typeof useAgentGUISessionDetailTransport> &
  OperationActions & {
    nodeId?: string | null;
    operationActions: OperationActions;
    updateSelectedProjectPath: Parameters<
      typeof useAgentGUIControllerActions
    >[0]["updateSelectedProjectPath"];
    selectConversation: Parameters<
      typeof useAgentGUIControllerActions
    >[0]["selectConversation"];
    providerRailMode: AgentGUIProviderRailMode | undefined;
  };

export function useAgentGUIViewAssembly(input: Input) {
  const overview = useAgentGUIConversationPresentation(input);
  const conversation = useCallback(
    () => overview.activeConversation,
    [overview]
  );
  const settings = useAgentGUIComposerPresentation({ ...input, conversation });
  const selectedTarget = input.effectiveSelectedProviderTarget;
  const selectedTargetComingSoon = isAgentGUIAgentTargetComingSoon(
    selectedTarget,
    input.normalizedComingSoonProviders
  );
  const providerReadinessGate =
    input.activeConversationId === null && selectedTargetComingSoon
      ? ({ status: "coming_soon" } as const)
      : resolveAgentGUIProviderReadinessGateForView({
          activeConversationId: input.activeConversationId,
          providerReadinessGates: input.providerReadinessGates,
          selectedProvider: selectedTarget.provider
        });
  const selectedAgentTargetUnavailable =
    input.activeConversationId === null &&
    selectedTarget.disabled === true &&
    !selectedTargetComingSoon;
  const session = useAgentGUISessionPresentation({
    ...input,
    conversation,
    selectedTarget,
    targets: input.normalizedProviderTargets,
    providerReadinessGate,
    selectedAgentTargetUnavailable,
    selectedAgentTargetUnavailableReason: selectedAgentTargetUnavailable
      ? selectedTarget.availability?.reason?.trim() ||
        selectedTarget.unavailableReason?.trim() ||
        null
      : null,
    selectedAgentTargetOwnerLabel: selectedTarget.ownerLabel?.trim() || null
  });
  const composerState = useMemo(
    () =>
      new ComposerFeatureState({
        native: input.native,
        capabilities: input.capabilities,
        settings,
        session,
        handoffAgentTargets: input.handoffAgentTargets
      }),
    [
      input.native,
      input.capabilities,
      settings,
      session,
      input.handoffAgentTargets
    ]
  );
  const draftContent = input.activeConversationId
    ? (input.draftByScopeKey[
        resolveAgentComposerDraftScopeKey({
          agentSessionId: input.activeConversationId
        })
      ] ?? EMPTY_AGENT_COMPOSER_DRAFT)
    : readAgentComposerDraftContent({ drafts: input.draftByScopeKey });
  const composer = useMemo(
    () => ({
      state: composerState,
      draftContent,
      draftPrompt: agentComposerDraftPrompt(draftContent)
    }),
    [composerState, draftContent]
  );
  const detail = useMemo(
    () =>
      new ConversationDetailState({
        native: input.native,
        services: input.agentActivityRuntime,
        capabilities: input.capabilities,
        session,
        isLoadingMessages: input.isLoadingMessages,
        isLoadingOlderMessages:
          input.activeSessionView?.isLoadingOlderMessages ?? false,
        detailError: input.detailError,
        avoidGroupingEdits: input.avoidGroupingEdits
      }),
    [
      input.native,
      input.agentActivityRuntime,
      input.capabilities,
      session,
      input.isLoadingMessages,
      input.activeSessionView?.isLoadingOlderMessages,
      input.detailError,
      input.avoidGroupingEdits
    ]
  );
  const providerHome = useAgentGUIProviderHome(input);
  const actions = useAgentGUIControllerActions({
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
  const shell = useMemo(
    () => ({
      nodeId: input.nodeId?.trim() || null,
      workspaceId: input.workspaceId,
      workspacePath: input.workspacePath,
      currentUserId: input.currentUserId,
      data: viewData
    }),
    [
      input.nodeId,
      input.workspaceId,
      input.workspacePath,
      input.currentUserId,
      viewData
    ]
  );
  const rail = useMemo(
    () =>
      new RailPresentationState({
        selectedAgentTarget: selectedTarget,
        agentTargets: input.normalizedProviderTargets,
        agentTargetsLoading: input.agentTargetsLoading,
        providerRailMode: input.providerRailMode ?? "catalog",
        comingSoonProviders: input.normalizedComingSoonProviders,
        conversationFilter: input.conversationFilter,
        userProjects: input.userProjects,
        activeConversationId: input.activeConversationId,
        revealRequest: input.railRevealRequest,
        isLoadingConversations: input.isLoadingConversations,
        listError: input.listError,
        overview,
        session
      }),
    [
      selectedTarget,
      input.normalizedProviderTargets,
      input.agentTargetsLoading,
      input.providerRailMode,
      input.normalizedComingSoonProviders,
      input.conversationFilter,
      input.userProjects,
      input.activeConversationId,
      input.railRevealRequest,
      input.isLoadingConversations,
      input.listError,
      overview,
      session
    ]
  );
  const operations = useMemo(
    () => ({
      isDeletingConversation: input.isDeletingConversation,
      isUserProjectMutationPending: input.isUserProjectMutationPending,
      pendingDeleteConversation: input.pendingDeleteConversation,
      pendingDeleteProjectConversations: input.pendingDeleteProjectConversations
    }),
    [
      input.isDeletingConversation,
      input.isUserProjectMutationPending,
      input.pendingDeleteConversation,
      input.pendingDeleteProjectConversations
    ]
  );
  return {
    shell,
    rail,
    detail,
    composer,
    interaction: session,
    readiness: session,
    operations,
    actions
  };
}
