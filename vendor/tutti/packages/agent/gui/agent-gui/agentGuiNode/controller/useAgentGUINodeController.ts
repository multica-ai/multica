import {
  createAgentSessionFamilySnapshotSelector,
  selectWorkspaceAgentConsumerSession,
  selectWorkspaceAgentConsumerSessions
} from "@tutti-os/agent-activity-core";
import { useCallback, useEffect, useMemo, useRef } from "react";
import { useAgentHostApi } from "../../../agentActivityHost";
import { useAgentGUIRuntime } from "../../../agentActivityRuntime";
import { useAccountStore } from "../../../host/agentHostAccountStore";
import type { AgentHostUserProject } from "../../../host/agentHostApi";
import type { AgentSessionComposerSettings } from "../../../shared/agentSessionTypes";
import {
  useEngineSelector,
  useModelValue
} from "../../../shared/engine/useEngineSelector";
import type { AgentGUIDetailViewModel } from "../model/agentGuiNodeTypes";
import {
  AGENT_GUI_RUNTIME_SESSION_ORIGIN,
  conversationSummaryFromAgentSession,
  type AgentGUIConversationSummary
} from "../model/agentGuiConversationModel";
import {
  areAgentComposerProjectPathsEqual,
  normalizeAgentComposerDraftProjectPath
} from "../model/agentComposerDraftScope";
import { mergeVisibleConversations } from "./agentGuiController.conversationHelpers";
import { reuseAgentActivityDisplayStatusesIfUnchanged } from "./agentGuiController.draftMessageHelpers";
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
  EMPTY_AGENT_GUI_MESSAGES,
  composerTargetDataFromProviderTarget,
  isExplicitAgentGUIAgentTarget
} from "./agentGuiController.providerHelpers";
import type { UseAgentGUINodeControllerInput } from "./agentGuiController.types";
import { reportAgentGUIActiveConversationCleared } from "./agentGuiController.reporting";
import { useAgentGUIActivation } from "./useAgentGUIActivation";
import { useAgentGUIActiveMessages } from "./useAgentGUIActiveMessages";
import { useAgentGUIConversationRouting } from "./useAgentGUIConversationRouting";
import { useAgentGUIConversationSelectionController } from "./useAgentGUIConversationSelectionController";
import { useAgentGUIConversationListState } from "./useAgentGUIConversationListState";
import { useAgentGUIComposerCapabilities } from "./useAgentGUIComposerCapabilities";
import { useAgentGUIComposerOptionsSync } from "./useAgentGUIComposerOptionsSync";
import { useAgentGUIControllerRefs } from "./useAgentGUIControllerRefs";
import { useAgentGUIOperationActions } from "./useAgentGUIOperationActions";
import { useAgentGUIViewAssembly } from "./useAgentGUIViewAssembly";
import { useAgentGUIProviderCatalogSelection } from "./useAgentGUIProviderCatalogSelection";
import { useAgentGUISessionEngineState } from "./useAgentGUISessionEngineState";
import { useAgentGUISessionDetailTransport } from "./useAgentGUISessionDetailTransport";
import { useAgentGUILocalState } from "./useAgentGUILocalState";
export {
  normalizePermissionModeSemantic,
  permissionConfigFromComposerOptions,
  permissionModeDescription,
  permissionModeLabel,
  permissionModeOptions
} from "./agentGuiController.composerHelpers";
import { trackAgentGUISettingsProjectChange } from "./agentGuiProjectAnalytics";
import type { OptimisticComposerTarget } from "./agentGuiController.composerPresentation";
export * from "./agentGuiController.conversationHelpers";
export {
  agentGUIConversationDiagnosticDetails,
  agentGUIRuntimeSessionDiagnosticDetails,
  agentGUISessionStateDiagnosticDetails,
  agentGUIToolCallStatusIsWaiting,
  promptRequestId
} from "./agentGuiController.diagnostics";
export * from "./agentGuiController.draftMessageHelpers";
export * from "./agentGuiController.errors";
export {
  createAgentGUIConversationId,
  normalizeOptionalPrompt,
  normalizeOptionalText,
  projectAgentGUIMessagesToTimelineItems,
  recordValue,
  stringPayloadValue
} from "./agentGuiController.promptHelpers";
export * from "./agentGuiController.providerHelpers";
export * from "./agentGuiController.reporting";
export {
  messageFromMessageUpdate,
  normalizeTimelineStatus,
  normalizedPositiveNumber,
  timelineItemTime
} from "./agentGuiController.sessionHelpers";
export * from "./agentGuiController.stableHelpers";
export {
  filterMessagesForDetailWindowOverlay,
  maxFiniteMessageVersion,
  minFiniteMessageVersion,
  sessionHasRenderableMessages,
  windowHasTurnMissingUserPrompt
} from "./useAgentConversationMessagePaging";
export { resolveConversationSummaryById } from "./useAgentConversationSelection";
export type { ConversationIntent } from "./useAgentConversationSelection";

export type { AgentGUIOpenSessionRequest } from "./agentGuiController.draftMessageHelpers";
export type { AgentGUIPrefillPromptRequest } from "./useAgentGUIConversationHome";
export type { AgentGUIComposerAppendRequest } from "./useAgentGUIComposerAppendRequest";
export type {
  AgentGUIRememberComposerDefaultsInput,
  AgentGUIRememberComposerDefaultsResult
} from "./agentGuiController.providerHelpers";

export function useAgentGUINodeController({
  nodeId,
  workspaceId,
  currentUserId,
  workspacePath,
  avoidGroupingEdits,
  data,
  agentTargets,
  agentTargetsLoading = false,
  handoffAgentTargets,
  handoffAgentTargetsLoading = false,
  providerRailMode = "catalog",
  comingSoonProviders,
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
  onDataChange,
  onComposerAppendHandled,
  onRememberComposerDefaults,
  onShowMessage
}: UseAgentGUINodeControllerInput) {
  const agentActivityRuntime = useAgentGUIRuntime();
  const agentActivityRuntimeOrigin =
    agentActivityRuntime.origin?.trim() || AGENT_GUI_RUNTIME_SESSION_ORIGIN;
  const sessionEngine = useMemo(() => {
    const engine = agentActivityRuntime.getSessionEngine(workspaceId);
    if (
      engine.identity.workspaceId !== workspaceId ||
      engine.identity.origin !== agentActivityRuntimeOrigin
    ) {
      throw new Error(
        "Agent activity runtime returned a session engine for a different identity."
      );
    }
    return engine;
  }, [agentActivityRuntime, agentActivityRuntimeOrigin, workspaceId]);
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
  const agentActivityDisplayStatuses = useEngineSelector(
    sessionEngine,
    (state) => {
      const statuses = new Map(
        selectWorkspaceAgentConsumerSessions(state).map((item) => [
          item.session.agentSessionId,
          item.displayStatus
        ])
      );
      if (activeConversationId && !statuses.has(activeConversationId)) {
        const activeConsumer = selectWorkspaceAgentConsumerSession(
          state,
          activeConversationId
        );
        if (activeConsumer) {
          statuses.set(activeConversationId, activeConsumer.displayStatus);
        }
      }
      return statuses;
    },
    (left, right) =>
      reuseAgentActivityDisplayStatusesIfUnchanged(left, right) === left
  );
  const activeAgentActivityDisplayStatus = activeConversationId
    ? (agentActivityDisplayStatuses.get(activeConversationId) ?? null)
    : null;
  const conversationList = useAgentGUIConversationListState({
    agentActivityRuntimeOrigin,
    currentUserId,
    data,
    normalizedProviderTargets,
    sessionEngine,
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
  const sessionEngineState = useAgentGUISessionEngineState({
    activeConversationId,
    composer: agentActivityRuntime.composer,
    prompts: agentActivityRuntime.prompts,
    sessionEngine
  });
  const {
    activeEngineSession,
    activePendingActivation,
    activePendingSubmits,
    activeQueuedPrompts,
    activeSessionState,
    isCreatingConversation
  } = sessionEngineState;
  const selectActiveSessionFamily = useMemo(
    () => createAgentSessionFamilySnapshotSelector(activeConversationId),
    [activeConversationId]
  );
  const activeSessionFamily = useEngineSelector(
    sessionEngine,
    selectActiveSessionFamily
  );
  const activeRelatedPendingInteractions =
    activeSessionFamily.pendingInteractions;
  const activeRelatedIsRespondingToInteraction = useModelValue(
    () =>
      activeRelatedPendingInteractions.some((interaction) =>
        agentActivityRuntime.prompts.isRespondingToInteraction(
          interaction.agentSessionId
        )
      ),
    [agentActivityRuntime.prompts, activeRelatedPendingInteractions]
  );
  const optimisticComposerTarget =
    useMemo<OptimisticComposerTarget | null>(() => {
      if (
        !isCreatingConversation ||
        activePendingActivation?.mode !== "new" ||
        activePendingActivation.agentSessionId !== activeConversationId ||
        activePendingActivation.agentTargetId !==
          selectedComposerTargetData.agentTargetId
      ) {
        return null;
      }
      return {
        agentSessionId: activePendingActivation.agentSessionId,
        target: selectedComposerTargetData
      };
    }, [
      activeConversationId,
      activePendingActivation,
      isCreatingConversation,
      selectedComposerTargetData
    ]);
  // Bridges submitInteractivePrompt
  // updateComposerSettings (defined later); assigned right after the
  // callback's definition.
  const updateComposerSettingsRef = useRef<
    (nextSettings: Partial<AgentSessionComposerSettings>) => void
  >(() => {});
  const composerCapabilities = useAgentGUIComposerCapabilities({
    activeConversationId,
    activeEngineSession,
    activeSessionState,
    data,
    draftSettingsBySessionId,
    optimisticComposerTarget,
    selectedComposerTargetData,
    composer: agentActivityRuntime.composer
  });
  const {
    composerSupport,
    composerTargetData,
    defaultReasoningEffort,
    providerComposerOptions
  } = composerCapabilities;
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
    sessionEngine,
    workspaceId
  });
  const {
    loadSelectedConversationMessages,
    loadSessionState,
    markSelectedConversationDetailPending,
    resolveSessionMessages,
    setActiveMessageSession
  } = sessionDetailTransport;
  const storedActiveMessages = activeConversationId
    ? resolveSessionMessages(activeConversationId)
    : EMPTY_AGENT_GUI_MESSAGES;
  const { activeMessages, activeTimelineItems } = useAgentGUIActiveMessages({
    activeConversationId,
    activePendingActivation,
    activePendingSubmits,
    activeQueuedPrompts,
    currentUserId,
    storedMessages: storedActiveMessages,
    workspaceId
  });
  const transientConversation =
    useMemo<AgentGUIConversationSummary | null>(() => {
      const session = activeEngineSession;
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
          needsUserAction: activeRelatedPendingInteractions.length > 0,
          userProjects
        }),
        isTransient: true
      };
    }, [
      activeEngineSession,
      activeRelatedPendingInteractions.length,
      agentActivityDisplayStatuses,
      conversations,
      userProjects
    ]);
  // Stashes the error message from a failed first-message create so the
  // activeConversationId-null effect (which otherwise clears detailError on
  // every home transition) can surface it on the home composer instead of
  // wiping it out during the optimistic-entry revert.
  const activation = useAgentGUIActivation({
    engine: sessionEngine,
    workspaceId,
    getErrorMessage: getAgentGUIErrorMessage,
    getErrorCode: getAgentGUIErrorCode
  });
  const activeConversationLiveState = activation.stateFor(activeConversationId);
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
      sessionEngineState.activeSessionReconcileErrorCode,
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
    sessionEngine,
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
    sessionEngine,
    setIntent,
    transientConversation,
    workspaceId
  });

  const { loadDraftComposerOptions, reloadComposerOptionsForTarget } =
    useAgentGUIComposerOptionsSync({
      activeConversationId,
      activeConversationIdRef,
      activeSessionTarget: activeEngineSession,
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
    ...sessionEngineState,
    ...composerCapabilities,
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
    sessionEngine,
    setUserProjectsSnapshot,
    transientConversation,
    unactivate: activation.unactivate,
    updateComposerSettingsRef,
    workspaceId
  });
  const isLoadingMessages =
    localState.isLoadingMessages ||
    sessionEngineState.activeSessionDetailLoading;
  const detailAvailability: AgentGUIDetailViewModel["availability"] =
    activeConversationId === null
      ? "ready"
      : sessionEngineState.activeEngineSessionDeleted
        ? "not_found"
        : isLoadingMessages
          ? "loading"
          : sessionEngineState.activeSessionReconcileError ||
              localState.detailError
            ? "error"
            : "ready";
  const viewAssembly = useAgentGUIViewAssembly({
    ...providerCatalogSelection,
    ...localState,
    ...conversationList,
    ...sessionEngineState,
    ...composerCapabilities,
    ...controllerRefs,
    ...sessionDetailTransport,
    ...conversationSelection,
    ...operationActions,
    activeCancelStatus: sessionEngineState.activeCancelStatus,
    activePendingInteractions: activeRelatedPendingInteractions,
    activeTurn: sessionEngineState.activeEngineActiveTurn,
    activeLatestPendingSubmitTurnId:
      sessionEngineState.activeLatestPendingSubmit?.turnId ?? null,
    activeMessages,
    activeSessionFamily,
    activeTimelineItems,
    activeEngineHasPendingInteractions:
      activeRelatedPendingInteractions.length > 0,
    isRespondingToInteraction: activeRelatedIsRespondingToInteraction,
    activeConversationLiveState,
    activationState: activeConversationLiveState,
    activityDisplayStatus: activeAgentActivityDisplayStatus,
    activityDisplayStatuses: agentActivityDisplayStatuses,
    agentActivityRuntime,
    avoidGroupingEdits,
    currentUserId,
    codeFor: activation.codeFor,
    composerTargetProvider: composerTargetData.provider,
    codexSaverModeEntryEnabled,
    rtkSaverModeEntryEnabled,
    data,
    defaultAgentTargetId,
    errorFor: activation.errorFor,
    detailAvailability,
    isCreatingConversation,
    isLoadingConversations,
    isLoadingMessages,
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
    sessionEngine,
    transientConversation,
    unactivate: activation.unactivate,
    updateSelectedProjectPath,
    userProjects,
    workspaceId,
    workspacePath
  });
  return viewAssembly;
}
