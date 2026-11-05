import { useIsSidePane } from "@multica/tutti-agent-gui-host/side-entry";
import { observer } from "mobx-react-lite";
import { useCallback, useMemo, useRef } from "react";
import { useConversationServices } from "../../../conversationServices";
import type { AgentTranscriptVirtualScrollController } from "../../../shared/agentConversation/components/AgentTranscriptView";
import { latestAssistantMessageText } from "../../../shared/agentConversation/projection/agentConversationProjection";
import { useActiveConversationState } from "../../../state/useActiveConversationState";
import type { AgentComposerProps } from "../AgentComposer";
import styles from "../AgentGUINode.styles";
import {
  emptyAgentComposerDraft,
  updateAgentComposerDraft
} from "../model/agentComposerDraft";
import { resolveAgentComposerDraftScopeKey } from "../model/agentComposerDraftScope";
import type { AgentHomeSuggestionAction } from "../model/agentGuiNodeTypes";
import { AgentGUIBottomDockPane } from "./AgentGUIBottomDockPane";
import { EMPTY_WORKSPACE_APP_ICONS } from "./agentGUIDetailConstants";
import {
  agentGUIDetailBottomDockStoreRevision,
  buildAgentConversationHandoffPrompt,
  handoffProjectPathForConversation,
  resolveAgentGUIComposerInteractionDisabledReason
} from "./agentGUIDetailModelHelpers";
import type { AgentGUIDetailPaneProps } from "./AgentGUIDetailPane.types";
import type { AgentGUIPendingPrependScrollAnchor } from "./agentGUIDetailScrollTypes";
import { AgentGUIDetailTimeline } from "./AgentGUIDetailTimeline";
import {
  AgentGUIEmptyHomePane,
  EMPTY_HOME_SUGGESTIONS
} from "./AgentGUIEmptyState";
import type { TimelineScrollAnchor } from "./agentGUIScrollMemory";
import {
  useOptionalStableEventCallback,
  useStableEventCallback
} from "./agentGUIViewUtils";
import { useAgentGUIComposerFooter } from "./useAgentGUIComposerFooter";
import { useAgentGUIComposerInputHistoryProps } from "./useAgentGUIComposerInputHistoryProps";
import { useAgentGUIDetailEditRetry } from "./useAgentGUIDetailEditRetry";
import { useAgentGUIDetailModel } from "./useAgentGUIDetailModel";
import { useAgentGUIDetailScroll } from "./useAgentGUIDetailScroll";
import { useBottomDockInteractionSubmission } from "./useBottomDockInteractionSubmission";

const AGENT_GUI_WORKBENCH_OPEN_EXTERNAL_IMPORT_EVENT =
  "tutti:agent-gui-workbench-open-external-import";
export const AgentGUIDetailPane = observer(function AgentGUIDetailPane({
  shell,
  rail,
  detail,
  composer,
  interaction,
  readiness,
  operations,
  homeTargetProjection,
  referenceProvenanceFilters = null,
  hiddenMentionFilterIds = [],
  sessionInputHistoryEnabled = false,
  sessionWorktreeEnabled = false,
  sessionLaunchModesByProjectSectionKey,
  onSessionLaunchModePreferenceChange,
  composerEngagement,
  actions,
  labels,
  workspaceUserProjectI18n,
  uiLanguage,
  isActive,
  isVisible,
  workspaceReferencePickerOpen,
  composerFocusRequestSequence,
  slashStatusLimits,
  slashStatusLimitsLoading,
  slashStatusLimitsUnavailable,
  slashStatusOverride,
  onSlashStatusOpen,
  onSlashStatusClose,
  onSlashStatusRefresh,
  onLinkAction,
  onHandoffConversation,
  showHandoffTargetOwnershipLabels = false,
  capabilityMenuState,
  capabilityControlsReadOnly = false,
  onCapabilitySettingsRequest,
  onAgentProviderLogin,
  onRequestWorkspaceReferences,
  resolveExternalPromptEntries = null,
  prepareExternalPromptFiles = null,
  resolvePastedPath = null,
  promptAssetLimit = null,
  selectProjectDirectory,
  projectSelectOptions,
  onRequestGitBranches,
  onRequestComposerFocus,
  workspaceAppIcons = EMPTY_WORKSPACE_APP_ICONS,
  renderComposerFooterAccessory,
  composerEmptySubmit = null
}: AgentGUIDetailPaneProps): React.JSX.Element {
  const isHostSidePane = useIsSidePane();
  const services = useConversationServices();
  const nativeState = useActiveConversationState(rail.activeConversationId);
  const rows = rail.activeConversationId
    ? services.conversationRows(rail.activeConversationId, {
        workspaceId: shell.workspaceId,
        userId: shell.currentUserId,
        workspaceRoot: shell.workspacePath ?? "",
        avoidGroupingEdits: detail.avoidGroupingEdits
      })
    : null;
  const currentConversation = rows?.structure ?? null;
  const conversationDetail = currentConversation?.sourceDetail ?? null;

  const bottomDockRef = useRef<HTMLDivElement | null>(null);
  const pendingPrependScrollAnchorRef =
    useRef<AgentGUIPendingPrependScrollAnchor | null>(null);
  const submittedPromptScrollConversationRef = useRef<string | null>(null);
  const timelineContentRef = useRef<HTMLDivElement | null>(null);
  const timelineRef = useRef<HTMLDivElement | null>(null);
  const timelineScrollAnchorRef = useRef<TimelineScrollAnchor | null>(null);
  const virtualScrollControllerRef =
    useRef<AgentTranscriptVirtualScrollController | null>(null);
  const {
    activePromptResponsePending,
    bottomDockLiftedPrompt,
    bottomDockReplacementPrompt,
    chromeLabels,
    chromeState,
    composerActivePrompt,
    composerDisabledReason,
    composerGate,
    composerLabels,
    conversation,
    conversationFlowEmpty,
    conversationFlowLabels,
    dismissBottomDockPrompt,
    emptyProviderReadinessGate,
    hasActiveConversation,
    homeNoticeChrome,
    inlineNoticeChrome,
    interactivePromptLabels,
    isComposerSending,
    sessionChrome,
    showStopButton,
    stopDisabled,
    showTimelineSkeleton,
    showUnavailableChatEmpty,
    slashStatus: derivedSlashStatus,
    timelineConversationId,
    timelineInteractionLocked
  } = useAgentGUIDetailModel({
    conversation: currentConversation,
    labels,
    slashStatusLimits,
    slashStatusLimitsLoading,
    slashStatusLimitsUnavailable,
    ...{ shell, rail, detail, composer, interaction, readiness, operations }
  });
  const slashStatus = slashStatusOverride ?? derivedSlashStatus;
  const handleForkThroughTurn = useStableEventCallback((turnId: string) => {
    const agentSessionId =
      conversation?.sourceDetail.session.agentSessionId.trim() ?? "";
    if (agentSessionId) {
      void actions.forkConversationThroughTurn(agentSessionId, turnId);
    }
  });
  const openForkSourceSession = useStableEventCallback(
    actions.openForkSourceConversation
  );
  const editRetry = useAgentGUIDetailEditRetry({
    agentSessionId: rail.activeConversationId,
    workspaceId: shell.workspaceId
  });
  const submitApproval = useStableEventCallback(actions.submitApprovalOption);
  const retryActivation = useStableEventCallback(actions.retryActivation);
  const continueInNewConversation = useStableEventCallback(
    actions.continueInNewConversation
  );
  const updateDraftContent = useStableEventCallback(actions.updateDraftContent);
  const clearMainDraft = useStableEventCallback(() =>
    updateDraftContent(emptyAgentComposerDraft())
  );
  const updateSelectedProjectPath = useOptionalStableEventCallback(
    actions.updateSelectedProjectPath
  );
  const updateComposerSettings = useStableEventCallback(
    actions.updateComposerSettings
  );
  const retryComposerOptions = useStableEventCallback(
    actions.retryComposerOptions
  );
  const updatePlanIssueBudgetPreset = useStableEventCallback(
    actions.updatePlanIssueBudgetPreset
  );
  const selectHomeComposerAgentTarget = useStableEventCallback(
    actions.selectHomeComposerAgentTarget
  );
  const selectHomeComposerAgentTargetAndFocus = useCallback(
    (input: Parameters<typeof selectHomeComposerAgentTarget>[0]) => {
      selectHomeComposerAgentTarget(input);
      onRequestComposerFocus();
    },
    [onRequestComposerFocus, selectHomeComposerAgentTarget]
  );
  const handleSelectHomeSuggestion = useCallback(
    (prompt: string) => {
      updateDraftContent(
        updateAgentComposerDraft(composer.draftContent, { prompt })
      );
    },
    [updateDraftContent, composer.draftContent]
  );
  const handleHomeSuggestionAction = useCallback(
    (action: AgentHomeSuggestionAction) => {
      if (action === "import-session") {
        window.dispatchEvent(
          new CustomEvent(AGENT_GUI_WORKBENCH_OPEN_EXTERNAL_IMPORT_EVENT)
        );
      }
    },
    []
  );
  const submitPrompt = useStableEventCallback(actions.submitPrompt);
  const submitGuidancePrompt = useStableEventCallback(
    actions.submitGuidancePrompt
  );
  const requestSubmittedPromptScrollToBottom = useStableEventCallback(() => {
    const activeConversationId = rail.activeConversationId;
    if (!activeConversationId) {
      return;
    }
    submittedPromptScrollConversationRef.current = activeConversationId;
    pendingPrependScrollAnchorRef.current = null;
  });
  const submitPromptAndScrollToBottom = useStableEventCallback(
    (...args: Parameters<typeof submitPrompt>): void => {
      requestSubmittedPromptScrollToBottom();
      submitPrompt(...args);
    }
  );
  const submitGuidancePromptAndScrollToBottom = useStableEventCallback(
    (...args: Parameters<typeof submitGuidancePrompt>): void => {
      requestSubmittedPromptScrollToBottom();
      submitGuidancePrompt(...args);
    }
  );
  const showPromptImagesUnsupported = useStableEventCallback(
    actions.showPromptImagesUnsupported
  );
  const sendQueuedPromptNext = useStableEventCallback(
    actions.sendQueuedPromptNext
  );
  const removeQueuedPrompt = useStableEventCallback(actions.removeQueuedPrompt);
  const editQueuedPrompt = useStableEventCallback(actions.editQueuedPrompt);
  const submitInteractivePrompt = useStableEventCallback(
    actions.submitInteractivePrompt
  );
  const stableLinkAction = useOptionalStableEventCallback(onLinkAction);
  const sourceActiveTurn = conversationDetail?.session.activeTurn ?? null;
  const handleInterruptCurrentTurn = useStableEventCallback(() => {
    actions.interruptCurrentTurn(labels.noRunningResponse);
  });
  const stableRequestWorkspaceReferences = useOptionalStableEventCallback(
    onRequestWorkspaceReferences
  );
  const stableSelectProjectDirectory = useOptionalStableEventCallback(
    selectProjectDirectory
  );
  const stableRequestGitBranches =
    useOptionalStableEventCallback(onRequestGitBranches);
  const authLogin = useOptionalStableEventCallback(onAgentProviderLogin);
  const submitBottomDockInteractivePrompt = useBottomDockInteractionSubmission(
    submitInteractivePrompt,
    dismissBottomDockPrompt
  );
  const composerActivePromptDisabledReason =
    resolveAgentGUIComposerInteractionDisabledReason(
      composerActivePrompt?.kind,
      interaction
    );
  const homeComposerProviderTargets = homeTargetProjection.agentTargets;
  const selectedHomeComposerTarget = homeTargetProjection.selectedAgentTarget;
  const composerProviderTargets =
    rail.activeConversationId === null
      ? homeComposerProviderTargets
      : rail.agentTargets;
  const composerHandoffProviderTargets = composer.state.handoffAgentTargets;
  const composerProvider =
    rail.activeConversationId === null
      ? (selectedHomeComposerTarget?.provider ?? shell.data.provider)
      : shell.data.provider;
  const composerSelectedProviderTarget =
    rail.activeConversationId === null
      ? selectedHomeComposerTarget
      : (rail.agentTargets.find((target) => {
          if (target.provider !== shell.data.provider) {
            return false;
          }
          const agentTargetId = shell.data.agentTargetId;
          return (
            !agentTargetId ||
            target.targetId === agentTargetId ||
            target.agentTargetId === agentTargetId
          );
        }) ?? rail.selectedAgentTarget);
  const handoffSourceSessionId = rail.activeConversationId;
  const stableHandoffConversation = useOptionalStableEventCallback(
    onHandoffConversation && handoffSourceSessionId !== null
      ? (target: (typeof composerHandoffProviderTargets)[number]) =>
          onHandoffConversation({
            agentTargetId: target.agentTargetId ?? target.targetId,
            draftPrompt: buildAgentConversationHandoffPrompt({
              activeConversation: rail.activeConversation,
              currentUserId: shell.currentUserId,
              labels,
              selectedAgentTarget: composerSelectedProviderTarget,
              uiLanguage,
              workspaceId: shell.workspaceId
            }),
            provider: target.provider,
            sourceAgentSessionId: handoffSourceSessionId,
            userProjectPath: handoffProjectPathForConversation(
              rail.activeConversation
            )
          })
      : undefined
  );
  const composerInputHistoryProps = useAgentGUIComposerInputHistoryProps({
    actions,
    conversation,
    enabled: sessionInputHistoryEnabled,
    pendingPrependScrollAnchorRef,
    timelineRef,
    ...{ shell, rail, detail, composer, interaction, readiness, operations }
  });
  const baseComposerProps = useMemo<AgentComposerProps>(
    () => ({
      state: composer.state,
      workspaceId: shell.workspaceId,
      agentSessionId: rail.activeConversationId,
      workspacePath: shell.workspacePath,
      currentUserId: shell.currentUserId,
      provider: composerProvider,
      slashStatus,
      onSlashStatusOpen,
      onSlashStatusClose,
      onSlashStatusRefresh,
      draftContent: composer.draftContent,
      engagement: composerEngagement,
      draftScopeKey: resolveAgentComposerDraftScopeKey({
        agentSessionId: rail.activeConversationId
      }),
      ...{ ...composerInputHistoryProps, hiddenMentionFilterIds },
      selectedAgentTarget: composerSelectedProviderTarget,
      sessionWorktreeEnabled:
        sessionWorktreeEnabled &&
        sessionLaunchModesByProjectSectionKey !== undefined,
      sessionLaunchMode:
        sessionLaunchModesByProjectSectionKey?.[
          composer.state.composerSettings.selectedProjectSectionKey?.trim() ??
            ""
        ] ?? "local",
      onSessionLaunchModeChange:
        onSessionLaunchModePreferenceChange &&
        composer.state.composerSettings.selectedProjectSectionKey?.trim()
          ? (mode) =>
              onSessionLaunchModePreferenceChange({
                mode,
                projectSectionKey:
                  composer.state.composerSettings.selectedProjectSectionKey!.trim()
              })
          : undefined,
      agentTargets: composerProviderTargets,
      handoffAgentTargets: composerHandoffProviderTargets,
      showHandoffTargetOwnershipLabels,
      providerSelectReadonly: rail.activeConversationId !== null,
      onProviderSelect:
        rail.activeConversationId === null
          ? selectHomeComposerAgentTargetAndFocus
          : undefined,
      presentationEditorDisabled: timelineInteractionLocked,
      disabledReason: composerDisabledReason,
      presentationSubmitDisabled: timelineInteractionLocked,
      workspaceAppIcons,
      placeholder: detail.hasSentUserMessage
        ? labels.followupPlaceholder
        : labels.initialPlaceholder,
      showStopButton,
      stopDisabled: stopDisabled || timelineInteractionLocked,
      workspaceReferencePickerOpen,
      ...{ referenceProvenanceFilters, activePrompt: composerActivePrompt },
      activePromptKeyboardShortcutsEnabled: isActive,
      activePromptDisabledReason: composerActivePromptDisabledReason,
      promptTips: labels.promptTips,
      composerFocusRequestSequence,
      isActive: isActive,
      providerSelectLabel: labels.providerSwitchLabel,
      handoffLabel: labels.handoffConversation,
      handoffMenuLabel: labels.handoffConversationMenu,
      modelConsult:
        rail.activeConversationId !== null
          ? {
              agentSessionId: rail.activeConversationId,
              lastAssistantMessageText: latestAssistantMessageText(conversation)
            }
          : null,
      uiLanguage,
      labels: composerLabels,
      workspaceUserProjectI18n,
      capabilityMenuState,
      capabilityControlsReadOnly,
      onDraftContentChange: updateDraftContent,
      onProjectPathChange: updateSelectedProjectPath,
      onSettingsChange: updateComposerSettings,
      onRetryComposerOptions: retryComposerOptions,
      onPlanIssueBudgetPresetChange: updatePlanIssueBudgetPreset,
      onSubmit: submitPromptAndScrollToBottom,
      onSubmitEmpty: composerEmptySubmit?.onSubmit,
      emptySubmitLabel: composerEmptySubmit?.label,
      onSubmitGuidance: submitGuidancePromptAndScrollToBottom,
      onPromptImagesUnsupported: showPromptImagesUnsupported,
      onSendQueuedPromptNext: sendQueuedPromptNext,
      onRemoveQueuedPrompt: removeQueuedPrompt,
      onEditQueuedPrompt: editQueuedPrompt,
      onInterruptCurrentTurn: handleInterruptCurrentTurn,
      onSubmitInteractivePrompt: submitInteractivePrompt,
      onCapabilitySettingsRequest,
      onLinkAction: stableLinkAction,
      onHandoffConversation: stableHandoffConversation,
      onRequestWorkspaceReferences: stableRequestWorkspaceReferences,
      resolveExternalPromptEntries,
      prepareExternalPromptFiles,
      resolvePastedPath,
      promptAssetLimit,
      selectProjectDirectory: stableSelectProjectDirectory,
      projectSelectOptions,
      onRequestGitBranches: stableRequestGitBranches
    }),
    [
      capabilityMenuState,
      capabilityControlsReadOnly,
      composerDisabledReason,
      composerGate,
      composerFocusRequestSequence,
      composerEngagement,
      composerInputHistoryProps,
      composerHandoffProviderTargets,
      composerLabels,
      conversation,
      composerProviderTargets,
      composerSelectedProviderTarget,
      timelineInteractionLocked,
      handleInterruptCurrentTurn,
      isActive,
      isComposerSending,
      labels,
      stableHandoffConversation,
      onSlashStatusOpen,
      onSlashStatusClose,
      onSlashStatusRefresh,
      workspaceReferencePickerOpen,
      composerActivePrompt,
      composerActivePromptDisabledReason,
      editQueuedPrompt,
      onCapabilitySettingsRequest,
      removeQueuedPrompt,
      resolveExternalPromptEntries,
      prepareExternalPromptFiles,
      resolvePastedPath,
      promptAssetLimit,
      projectSelectOptions,
      sendQueuedPromptNext,
      showPromptImagesUnsupported,
      showStopButton,
      sourceActiveTurn?.turnId,
      showHandoffTargetOwnershipLabels,
      sessionWorktreeEnabled,
      sessionLaunchModesByProjectSectionKey,
      onSessionLaunchModePreferenceChange,
      stopDisabled,
      slashStatus,
      submitInteractivePrompt,
      submitPromptAndScrollToBottom,
      composer.state.composerSettings.slashCommandPolicy,
      submitGuidancePromptAndScrollToBottom,
      uiLanguage,
      stableLinkAction,
      stableRequestGitBranches,
      stableSelectProjectDirectory,
      stableRequestWorkspaceReferences,
      updateComposerSettings,
      retryComposerOptions,
      updatePlanIssueBudgetPreset,
      updateDraftContent,
      updateSelectedProjectPath,
      rail.activeConversationId,
      composer.state.availableCommands,
      composer.state.availableSkills,
      composer.state.compactSupported,
      composer.state.composerSettings,
      shell.currentUserId,
      rail.activeConversation,
      composerProvider,
      composer.draftContent,
      composer.draftPrompt,
      detail.hasSentUserMessage,
      activePromptResponsePending,
      composer.state.promptImagesSupported,
      detail.usage,
      shell.workspaceId,
      shell.workspacePath,
      referenceProvenanceFilters,
      workspaceUserProjectI18n,
      workspaceAppIcons,
      selectHomeComposerAgentTargetAndFocus
    ]
  );
  const { bottomDockComposerProps, addSelectionToConversation } =
    useAgentGUIComposerFooter({
      baseComposerProps,
      onRequestComposerFocus,
      renderComposerFooterAccessory
    });
  const emptyHeroComposerProps = useMemo<AgentComposerProps>(
    () => ({
      ...bottomDockComposerProps,
      layoutMode: "hero"
    }),
    [bottomDockComposerProps]
  );
  const emptyHeroProvider =
    composerSelectedProviderTarget?.provider ?? shell.data.provider;
  const bottomDockStoreRevision = agentGUIDetailBottomDockStoreRevision({
    activePromptResponsePending,
    bottomDockLiftedPrompt,
    bottomDockReplacementPrompt,
    inlineNoticeChrome,
    sessionChrome,
    ...{ shell, rail, detail, composer, interaction, readiness, operations }
  });
  const {
    followEndMode,
    isTimelineScrolledToBottom,
    isTimelineScrolledToTop,
    setVirtualScrollController,
    scrollTimelineToBottom
  } = useAgentGUIDetailScroll({
    actions,
    bottomDockRef,
    bottomDockStoreRevision,
    conversation,
    isVisible,
    pendingPrependScrollAnchorRef,
    showTimelineSkeleton,
    submittedPromptScrollConversationRef,
    timelineConversationId,
    timelineContentRef,
    timelineRef,
    timelineScrollAnchorRef,
    virtualScrollControllerRef,
    activeConversationId: rail.activeConversationId,
    hasOlderMessages: detail.hasOlderMessages,
    isLoadingOlderMessages: detail.isLoadingOlderMessages
  });
  const showDraftWelcome =
    hasActiveConversation &&
    detail.availability === "ready" &&
    conversation?.rows.length === 0 &&
    conversationDetail?.turns.length === 0 &&
    !isComposerSending;
  const homeContent =
    !hasActiveConversation || showDraftWelcome ? (
      <AgentGUIEmptyHomePane
        isActive={isActive}
        isVisible={isVisible}
        provider={emptyHeroProvider}
        providerReadinessGate={emptyProviderReadinessGate}
        showAllProviders={rail.conversationFilter.kind === "all"}
        agentTargets={composerProviderTargets}
        selectedAgentTarget={composerSelectedProviderTarget}
        onProviderSelect={
          rail.activeConversationId === null
            ? selectHomeComposerAgentTargetAndFocus
            : undefined
        }
        noticeSource={chromeState.homeSource}
        onSubmitApprovalOption={submitApproval}
        onRetryActivation={retryActivation}
        onAuthLogin={authLogin}
        onContinueInNewConversation={continueInNewConversation}
        chromeLabels={chromeLabels}
        composerProps={hasActiveConversation ? null : emptyHeroComposerProps}
        labels={labels}
        suggestions={
          hasActiveConversation
            ? EMPTY_HOME_SUGGESTIONS
            : (labels.homeSuggestions ?? EMPTY_HOME_SUGGESTIONS)
        }
        suggestionsCloseLabel={labels.homeSuggestionsClose}
        onSelectSuggestion={handleSelectHomeSuggestion}
        onSelectSuggestionAction={handleHomeSuggestionAction}
      />
    ) : null;
  const forkedFrom = conversationDetail?.session.forkedFrom ?? null;
  return (
    <main
      className={styles.detail}
      aria-busy={timelineInteractionLocked || undefined}
      data-agent-session-id={rail.activeConversationId ?? undefined}
    >
      <div className={styles.detailWorkbench}>
        <section
          className={styles.detailPrimary}
          inert={timelineInteractionLocked}
        >
          <AgentGUIDetailTimeline
            availableSkills={composer.state.availableSkills}
            agentSessionId={timelineConversationId}
            editRetry={editRetry}
            conversationFlowEmpty={homeContent ?? conversationFlowEmpty}
            conversationFlowLabels={conversationFlowLabels}
            followEndMode={followEndMode}
            forkedFrom={forkedFrom}
            hasActiveConversation={hasActiveConversation}
            homeContent={homeContent}
            isLoadingOlderMessages={detail.isLoadingOlderMessages}
            isVisible={isVisible}
            isTimelineScrolledToTop={isTimelineScrolledToTop}
            labels={labels}
            onAuthLogin={authLogin}
            onForkThroughTurn={handleForkThroughTurn}
            onOpenForkSourceSession={openForkSourceSession}
            onLinkAction={stableLinkAction}
            onAddSelectionToConversation={addSelectionToConversation}
            textSelectionActionsEnabled={false}
            showTimelineSkeleton={showTimelineSkeleton}
            showUnavailableChatEmpty={showUnavailableChatEmpty}
            timelineContentRef={timelineContentRef}
            timelineRef={timelineRef}
            virtualScrollControllerRef={setVirtualScrollController}
            workspaceAppIcons={workspaceAppIcons}
          />
          {hasActiveConversation ? (
            <AgentGUIBottomDockPane
              bottomDockRef={bottomDockRef}
              showScrollToBottom={!isTimelineScrolledToBottom}
              scrollToBottomLabel={labels.scrollToBottom}
              onScrollToBottom={scrollTimelineToBottom}
              bottomDockLiftedPrompt={bottomDockLiftedPrompt}
              bottomDockReplacementPrompt={bottomDockReplacementPrompt}
              composerProps={
                showDraftWelcome && !isHostSidePane
                  ? emptyHeroComposerProps
                  : bottomDockComposerProps
              }
              interaction={interaction}
              chrome={chromeState}
              keyboardShortcutsEnabled={isActive}
              chromeLabels={chromeLabels}
              promptLabels={interactivePromptLabels}
              onSubmitApprovalOption={submitApproval}
              onRetryActivation={retryActivation}
              onRetryInlineNotice={retryActivation}
              onAuthLogin={authLogin}
              onContinueInNewConversation={continueInNewConversation}
              onSubmitBottomDockInteractivePrompt={
                submitBottomDockInteractivePrompt
              }
            />
          ) : null}
        </section>
      </div>
    </main>
  );
});
