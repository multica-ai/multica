import { useRef } from "react";
import { useAgentHostApi } from "../../../agentActivityHost";
import { useConversationServices } from "../../../conversationServices";
import { useAccountStore } from "../../../host/agentHostAccountStore";
import type { AgentSessionComposerSettings } from "../../../shared/agentSessionTypes";
import { AGENT_GUI_RUNTIME_SESSION_ORIGIN } from "../model/agentGuiConversationModel";
import type { AgentGUIPageInput } from "./agentGuiController.types";
import { useAgentGUIComposerCapabilities } from "./useAgentGUIComposerCapabilities";
import { useAgentGUIControllerRefs } from "./useAgentGUIControllerRefs";
import { useAgentGUIConversationListState } from "./useAgentGUIConversationListState";
import { useAgentGUILocalState } from "./useAgentGUILocalState";
import { useAgentGUIProviderCatalogSelection } from "./useAgentGUIProviderCatalogSelection";
import { useAgentGUISessionDetailTransport } from "./useAgentGUISessionDetailTransport";
import { useAgentGUISessionState } from "./useAgentGUISessionState";

export function useAgentGUISelectionState({
  workspaceId,
  currentUserId,
  data,
  agentTargets,
  agentTargetsLoading = false,
  handoffAgentTargets,
  handoffAgentTargetsLoading = false,
  providerRailMode = "catalog",
  comingSoonProviders,
  providerReadinessGates = null,
  defaultAgentTargetId = null,
  onDataChange,
  onRememberComposerDefaults,
  onShowMessage
}: AgentGUIPageInput) {
  const agentActivityRuntime = useConversationServices();
  const agentActivityRuntimeOrigin =
    agentActivityRuntime.environment?.origin?.trim() ||
    AGENT_GUI_RUNTIME_SESSION_ORIGIN;
  const conversationServices = agentActivityRuntime;
  // Stable runtime identity isolates conversation queries and session-view refs.
  const agentHostApi = useAgentHostApi();
  const providerCatalogSelection = useAgentGUIProviderCatalogSelection({
    comingSoonProviders,
    data,
    defaultAgentTargetId,
    providerRailMode,
    providerReadinessGates,
    agentTargets,
    agentTargetsLoading,
    handoffAgentTargets,
    handoffAgentTargetsLoading
  });
  const {
    effectiveSelectedProviderTarget,
    homeComposerTargetOverride,
    normalizedComingSoonProviders,
    normalizedExplicitProviderTargets,
    normalizedProviderTargets,
    selectedComposerTargetData,
    selectedAgentTargetIsExplicit,
    setHomeComposerTargetOverride
  } = providerCatalogSelection;
  const localState = useAgentGUILocalState({
    data,
    userProjectsApi: agentHostApi.userProjects
  });
  const {
    activeConversationId,
    clearRailRevealRequest,
    draftByScopeKey,
    draftSettingsBySessionId,
    intent,
    isComposerHome,
    selectedProjectPath,
    requestRailReveal,
    setActiveConversationId,
    setDetailError,
    setIntent,
    setIsComposerHome,
    setIsLoadingMessages,
    setIsUserProjectMutationPending,
    setSelectedProjectPath,
    setUserProjects,
    userProjects
  } = localState;
  const conversationList = useAgentGUIConversationListState({
    agentActivityRuntimeOrigin,
    currentUserId,
    data,
    normalizedProviderTargets,
    conversationServices,
    workspaceId
  });
  const {
    conversationFilter,
    conversationListQuery,
    conversationListState,
    conversations
  } = conversationList;
  const hasLoadedConversations = conversationListState?.initialized ?? false;
  const isLoadingConversations = conversationListState?.isLoading ?? false;
  const sessionState = useAgentGUISessionState({
    activeConversationId,
    composer: agentActivityRuntime.composer,
    prompts: agentActivityRuntime.prompts,
    conversationServices
  });
  const {
    activeCanonicalSession,
    activePendingActivation,
    activeSessionState,
    isCreatingConversation
  } = sessionState;
  const activeSessionFamily = sessionState.activeSessionFamily;
  const activeRelatedPendingInteractions = sessionState.pendingInteractions;
  const activeRelatedIsRespondingToInteraction =
    sessionState.relatedIsRespondingToInteraction;
  // Bridges submitInteractivePrompt
  // updateComposerSettings (defined later); assigned right after the
  // callback's definition.
  const updateComposerSettingsRef = useRef<
    (nextSettings: Partial<AgentSessionComposerSettings>) => void
  >(() => {});
  const composerCapabilities = useAgentGUIComposerCapabilities({
    native: sessionState,
    data,
    draftSettingsBySessionId,
    selectedComposerTargetData,
    composer: agentActivityRuntime.composer
  });
  const {
    composerSupport,
    composerTargetData,
    defaultReasoningEffort,
    providerComposerOptions
  } = composerCapabilities;
  const optimisticComposerTarget =
    composerCapabilities.optimisticComposerTarget;
  const accountProfilesByUserId = useAccountStore(
    (state) => state.profilesByUserId
  );
  const controllerRefs = useAgentGUIControllerRefs({
    activeConversationId,
    conversations,
    data,
    draftByScopeKey,
    draftSettingsBySessionId,
    effectiveSelectedProviderTarget,
    homeComposerTargetOverride,
    isComposerHome,
    isCreatingConversation,
    onDataChange,
    onRememberComposerDefaults,
    onShowMessage,
    agentTargetsProvided: agentTargets !== undefined,
    selectedComposerTargetData,
    selectedProjectPath,
    selectedAgentTargetIsExplicit,
    userProjects
  });
  const {
    activeConversationIdRef,
    conversationIdsRef,
    conversationsRef,
    dataRef,
    draftSettingsBySessionIdRef,
    handledOpenSessionSequenceRef,
    isComposerHomeRef,
    isMountedRef,
    loadDraftComposerOptionsRef,
    onDataChangeRef,
    onComposerDefaultsAuthorityReloadedRef,
    pendingOpenSessionRequestRef,
    selectedComposerTargetDataRef,
    selectedProjectPathRef,
    userProjectsLoadSeqRef,
    userProjectsRef
  } = controllerRefs;
  const sessionDetailTransport = useAgentGUISessionDetailTransport({
    activeConversationId,
    activeConversationIdRef,
    agentActivityRuntime,
    agentActivityRuntimeOrigin,
    dataRef,
    isMountedRef,
    sessionFamily: activeSessionFamily,
    conversationServices,
    workspaceId
  });
  const {
    loadSelectedConversationMessages,
    loadSessionState,
    markSelectedConversationDetailPending,
    resolveSessionMessages,
    setActiveMessageSession
  } = sessionDetailTransport;
  return {
    agentActivityRuntime,
    agentActivityRuntimeOrigin,
    conversationServices,
    agentHostApi,
    providerCatalogSelection,
    effectiveSelectedProviderTarget,
    homeComposerTargetOverride,
    normalizedComingSoonProviders,
    normalizedExplicitProviderTargets,
    normalizedProviderTargets,
    selectedComposerTargetData,
    selectedAgentTargetIsExplicit,
    setHomeComposerTargetOverride,
    localState,
    activeConversationId,
    clearRailRevealRequest,
    draftByScopeKey,
    draftSettingsBySessionId,
    intent,
    isComposerHome,
    selectedProjectPath,
    requestRailReveal,
    setActiveConversationId,
    setDetailError,
    setIntent,
    setIsComposerHome,
    setIsLoadingMessages,
    setIsUserProjectMutationPending,
    setSelectedProjectPath,
    setUserProjects,
    userProjects,
    conversationList,
    conversationFilter,
    conversationListQuery,
    conversationListState,
    conversations,
    hasLoadedConversations,
    isLoadingConversations,
    sessionState,
    activeCanonicalSession,
    activePendingActivation,
    activeSessionState,
    isCreatingConversation,
    activeSessionFamily,
    activeRelatedPendingInteractions,
    activeRelatedIsRespondingToInteraction,
    optimisticComposerTarget,
    updateComposerSettingsRef,
    composerCapabilities,
    composerSupport,
    composerTargetData,
    defaultReasoningEffort,
    providerComposerOptions,
    accountProfilesByUserId,
    controllerRefs,
    activeConversationIdRef,
    conversationIdsRef,
    conversationsRef,
    dataRef,
    draftSettingsBySessionIdRef,
    handledOpenSessionSequenceRef,
    isComposerHomeRef,
    isMountedRef,
    loadDraftComposerOptionsRef,
    onDataChangeRef,
    onComposerDefaultsAuthorityReloadedRef,
    pendingOpenSessionRequestRef,
    selectedComposerTargetDataRef,
    selectedProjectPathRef,
    userProjectsLoadSeqRef,
    userProjectsRef,
    sessionDetailTransport,
    loadSelectedConversationMessages,
    loadSessionState,
    markSelectedConversationDetailPending,
    resolveSessionMessages,
    setActiveMessageSession
  };
}
