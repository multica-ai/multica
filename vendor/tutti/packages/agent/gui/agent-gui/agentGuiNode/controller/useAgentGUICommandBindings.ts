import { comparer, computed } from "mobx";
import { useCallback, useEffect, useMemo } from "react";
import type { AgentHostUserProject } from "../../../host/agentHostApi";
import {
  areAgentComposerProjectPathsEqual,
  normalizeAgentComposerDraftProjectPath
} from "../model/agentComposerDraftScope";
import { conversationSummaryFromAgentSession } from "../model/agentGuiConversationModel";
import { mergeVisibleConversations } from "./agentGuiController.conversationHelpers";
import {
  getAgentGUIErrorCode,
  getAgentGUIErrorMessage
} from "./agentGuiController.errors";
import {
  areAgentGUIUserProjectsEqual,
  readAgentGUIUserProjectMutationPending,
  readAgentGUIUserProjectSnapshot,
  upsertAgentGUIUserProject
} from "./agentGuiController.interactiveHelpers";
import {
  composerTargetDataFromProviderTarget,
  isExplicitAgentGUIAgentTarget
} from "./agentGuiController.providerHelpers";
import { reportAgentGUIActiveConversationCleared } from "./agentGuiController.reporting";
import type { AgentGUIPageInput } from "./agentGuiController.types";
import { trackAgentGUISettingsProjectChange } from "./agentGuiProjectAnalytics";
import { useAgentGUIActivation } from "./useAgentGUIActivation";
import { useAgentGUIComposerOptionsSync } from "./useAgentGUIComposerOptionsSync";
import { useAgentGUIConversationRouting } from "./useAgentGUIConversationRouting";
import { useAgentGUIConversationSelectionController } from "./useAgentGUIConversationSelectionController";
import { useAgentGUIOperationActions } from "./useAgentGUIOperationActions";
import { useAgentGUISelectionState } from "./useAgentGUISelectionState";
import { useAgentGUIViewAssembly } from "./useAgentGUIViewAssembly";

export function useAgentGUICommandBindings(
  {
    nodeId,
    workspaceId,
    currentUserId,
    workspacePath,
    avoidGroupingEdits,
    data,
    agentTargetsLoading = false,
    providerRailMode = "catalog",
    providerReadinessGates = null,
    targetConnectionSource = null,
    interactionReadinessSource = null,
    observationGapSource = null,
    defaultAgentTargetId = null,
    composerAppendRequest = null,
    openSessionRequest = null,
    prefillPromptRequest = null,
    codexSaverModeEntryEnabled = false,
    rtkSaverModeEntryEnabled = false,
    onComposerAppendHandled,
    onRememberComposerDefaults
  }: AgentGUIPageInput,
  selection: ReturnType<typeof useAgentGUISelectionState>
) {
  const {
    agentActivityRuntime,
    conversationServices,
    agentHostApi,
    providerCatalogSelection,
    normalizedComingSoonProviders,
    normalizedExplicitProviderTargets,
    normalizedProviderTargets,
    selectedComposerTargetData,
    setHomeComposerTargetOverride,
    localState,
    activeConversationId,
    clearRailRevealRequest,
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
    conversations,
    hasLoadedConversations,
    isLoadingConversations,
    sessionState,
    activeCanonicalSession,
    activePendingActivation,
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
    setActiveMessageSession
  } = selection;

  const transientConversationState = useMemo(
    () =>
      computed(
        () => {
          const session = sessionState.activeCanonicalSession;
          if (
            !session ||
            conversations.some(
              (conversation) => conversation.id === session.agentSessionId
            )
          ) {
            return null;
          }
          return {
            ...conversationSummaryFromAgentSession(session, {
              needsUserAction: sessionState.pendingInteractions.length > 0,
              userProjects
            }),
            isTransient: true
          };
        },
        { equals: comparer.structural }
      ),
    [sessionState, conversations, userProjects]
  );
  const transientConversation = transientConversationState.get();
  // Stashes the error message from a failed first-message create so the
  // activeConversationId-null effect (which otherwise clears detailError on
  // every home transition) can surface it on the home composer instead of
  // wiping it out during the optimistic-entry revert.
  const activation = useAgentGUIActivation({
    engine: conversationServices,
    workspaceId,
    getErrorMessage: getAgentGUIErrorMessage,
    getErrorCode: getAgentGUIErrorCode
  });
  const setUserProjectsSnapshot = useCallback(
    (projects: readonly AgentHostUserProject[]) => {
      setUserProjects((current) =>
        areAgentGUIUserProjectsEqual(current, projects)
          ? current
          : [...projects]
      );
    },
    []
  );

  useEffect(() => {
    const api = agentHostApi.userProjects;
    let disposed = false;
    setUserProjectsSnapshot(readAgentGUIUserProjectSnapshot(api));
    setIsUserProjectMutationPending(
      readAgentGUIUserProjectMutationPending(api)
    );
    const loadUserProjects = async () => {
      const requestSeq = ++userProjectsLoadSeqRef.current;
      if (!api) {
        if (!disposed && requestSeq === userProjectsLoadSeqRef.current) {
          setUserProjectsSnapshot([]);
        }
        return;
      }
      try {
        const result = await api.list();
        if (!disposed && requestSeq === userProjectsLoadSeqRef.current) {
          setUserProjectsSnapshot(result.projects);
        }
      } catch {
        if (!disposed && requestSeq === userProjectsLoadSeqRef.current) {
          setUserProjectsSnapshot([]);
        }
      }
    };
    void loadUserProjects();
    const unsubscribe = api?.subscribe?.(() => {
      setIsUserProjectMutationPending(
        readAgentGUIUserProjectMutationPending(api)
      );
      void loadUserProjects();
    });
    return () => {
      disposed = true;
      unsubscribe?.();
    };
  }, [
    agentHostApi.userProjects,
    setIsUserProjectMutationPending,
    setUserProjectsSnapshot
  ]);

  // NOTE: project metadata is intentionally NOT written back into the shared
  // conversation store. `conversation.project` is a per-window exact JOIN of
  // railSectionKey × userProjects.sectionKey; deriving it here and persisting it
  // caused cross-window update storms. Rail membership comes from the same
  // backend railSectionKey contract.

  useEffect(() => {
    if (activeConversationId === null && isComposerHome) {
      return;
    }
    setHomeComposerTargetOverride(null);
  }, [activeConversationId, isComposerHome]);

  const conversationSelection = useAgentGUIConversationSelectionController({
    activation,
    activeConversationId,
    activeConversationIdRef,
    activePendingActivation,
    activeSessionReconcileErrorCode:
      sessionState.activeSessionReconcileErrorCode,
    agentActivityRuntime,
    conversationIdsRef,
    conversationsRef,
    conversationListQuery,
    clearRailRevealRequest,
    currentUserId,
    data,
    dataRef,
    intent,
    isComposerHomeRef,
    isMountedRef,
    loadDraftComposerOptions: () => loadDraftComposerOptionsRef.current(),
    loadSelectedConversationMessages,
    loadSessionState,
    markSelectedConversationDetailPending,
    onDataChangeRef,
    conversationServices,
    requestRailReveal,
    setActiveConversationId,
    setDetailError,
    setIntent,
    setIsComposerHome,
    setIsLoadingMessages,
    setActiveMessageSession,
    transientConversation,
    workspaceId
  });
  const persistActiveConversation =
    conversationSelection.persistActiveConversation;
  const selectConversation = conversationSelection.selectConversation;
  const syncConversationListProjection =
    conversationSelection.syncConversationListProjection;

  const updateSelectedProjectPath = useCallback(
    (
      path: string | null,
      metadata?: {
        action: "clear" | "create_new" | "import_directory" | "select_existing";
        project?: {
          id: string;
          path: string;
          label: string;
          sectionKey?: string;
          createdAtUnixMs?: number;
          updatedAtUnixMs?: number;
          lastUsedAtUnixMs?: number | null;
          pinnedAtUnixMs: number;
        };
      }
    ) => {
      const normalizedPath = normalizeAgentComposerDraftProjectPath(path);
      const project = metadata?.project;
      if (
        project &&
        normalizedPath &&
        areAgentComposerProjectPathsEqual(project.path, normalizedPath)
      ) {
        const nextProjects = upsertAgentGUIUserProject(
          userProjectsRef.current,
          project
        );
        userProjectsRef.current = nextProjects;
        setUserProjectsSnapshot(nextProjects);
      }
      selectedProjectPathRef.current = normalizedPath;
      setSelectedProjectPath(normalizedPath);
      trackAgentGUISettingsProjectChange({
        agentActivityRuntime,
        agentSessionId: activeConversationIdRef.current,
        metadata,
        provider: dataRef.current.provider,
        workspaceId
      });
    },
    [agentActivityRuntime, workspaceId]
  );

  useEffect(() => {
    if (!hasLoadedConversations) {
      return;
    }
    const nextConversationCount = mergeVisibleConversations(
      conversations,
      transientConversation
    ).filter((conversation) => !conversation.hiddenFromRail).length;
    onDataChangeRef.current((current) =>
      current.conversationCount === nextConversationCount
        ? current
        : { ...current, conversationCount: nextConversationCount }
    );
  }, [conversations.length, hasLoadedConversations, transientConversation]);

  useAgentGUIConversationRouting({
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
  });

  const { loadDraftComposerOptions, reloadComposerOptionsForTarget } =
    useAgentGUIComposerOptionsSync({
      activeConversationId,
      activeConversationIdRef,
      activeSessionTarget: activeCanonicalSession,
      agentActivityRuntime,
      composerTargetData,
      conversationFilter,
      currentUserId,
      data,
      dataRef,
      defaultReasoningEffort,
      draftSettingsBySessionIdRef,
      isComposerHome,
      isComposerHomeRef,
      isCreatingConversation,
      loadDraftComposerOptionsRef,
      loadSessionState,
      onComposerDefaultsAuthorityReloadedRef,
      optimisticComposerTarget,
      providerComposerOptions,
      selectedComposerTargetDataRef,
      selectedProjectPath,
      selectedProjectPathRef,
      syncConversationListProjection,
      workspaceId,
      workspacePath
    });
  const operationActions = useAgentGUIOperationActions({
    ...providerCatalogSelection,
    ...localState,
    ...conversationList,

    activeCanonicalComposerSettings:
      sessionState.activeCanonicalComposerSettings,
    activeEngineActiveTurn: sessionState.activeEngineActiveTurn,

    activePendingActivation: sessionState.activePendingActivation,
    activeSessionState: sessionState.activeSessionState,

    providerComposerOptions: composerCapabilities.providerComposerOptions,
    defaultReasoningEffort: composerCapabilities.defaultReasoningEffort,
    promptImagesSupported: composerCapabilities.promptImagesSupported,
    ...controllerRefs,
    ...sessionDetailTransport,
    ...conversationSelection,
    activeEnginePendingInteractions: activeRelatedPendingInteractions,
    accountProfilesByUserId,
    activation,
    agentActivityRuntime,
    agentHostApi,
    composerTargetDataFromProviderTarget,
    composerAppendRequest,
    codexSaverModeEntryEnabled,
    rtkSaverModeEntryEnabled,
    onComposerAppendHandled,
    composerSupportPermissionModeChangeDeferred:
      composerSupport.permissionModeChangeDeferred,
    currentProvider: data.provider,
    currentUserId,
    data,
    defaultAgentTargetId,
    isExplicitAgentGUIAgentTarget,
    isRespondingToInteraction: activeRelatedIsRespondingToInteraction,
    loadDraftComposerOptions,
    reloadComposerOptionsForTarget,
    normalizedExplicitProviderTargets,
    normalizedProviderTargets,
    interactionReadinessSource,
    prefillPromptRequest,
    reportActiveConversationCleared: reportAgentGUIActiveConversationCleared,
    conversationServices,
    setUserProjectsSnapshot,
    transientConversation,
    unactivate: activation.unactivate,
    updateComposerSettingsRef,
    workspaceId
  });
  const viewAssembly = useAgentGUIViewAssembly({
    native: sessionState,
    list: conversationList.conversationListState,
    transient: transientConversationState,
    capabilities: composerCapabilities,
    ...providerCatalogSelection,
    ...localState,
    ...conversationList,

    activePendingActivation: sessionState.activePendingActivation,

    ...controllerRefs,
    ...sessionDetailTransport,
    ...conversationSelection,
    ...operationActions,

    agentActivityRuntime,
    avoidGroupingEdits,
    currentUserId,

    codexSaverModeEntryEnabled,
    rtkSaverModeEntryEnabled,
    data,
    defaultAgentTargetId,

    isLoadingConversations,
    isLoadingMessages: localState.isLoadingMessages,
    normalizedComingSoonProviders,
    nodeId,
    operationActions,
    persistActiveConversation,
    providerRailMode,
    providerReadinessGates,
    targetConnectionSource,
    interactionReadinessSource,
    observationGapSource,
    agentTargetsLoading,
    selectedComposerTargetData,
    conversationServices,
    transientConversation,
    unactivate: activation.unactivate,
    updateSelectedProjectPath,
    userProjects,
    workspaceId,
    workspacePath
  });
  return viewAssembly;
}
