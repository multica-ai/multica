import { useMemo } from "react";
import { isAgentGUIAgentTargetComingSoon } from "../../../agentTargets";
import { UnavailableChatIcon } from "../../../app/renderer/components/icons/UnavailableChatIcon";
import { useProjectedAgentConversation } from "../../../shared/agentConversation/projection/useProjectedAgentConversation";
import type { AgentComposerSlashStatusLimit } from "../AgentComposer";
import type {
  AgentGUIInlineNotice,
  AgentGUINodeViewModel,
  AgentGUISessionChrome
} from "../model/agentGuiNodeTypes";
import type { AgentGUIViewLabels } from "../AgentGUINodeView";
import {
  isContextCanceledMessage,
  isAgentGUIHomeStatusNoticeVisible,
  resolveAgentGUIHomeNoticeChrome,
  resolveAgentGUIStopControl,
  resolveSlashStatus,
  useStableSlashStatus
} from "./agentGUIDetailModelHelpers";
import { useAgentGUIDetailLabels } from "./useAgentGUIDetailLabels";
import { useAgentGUITimelineTransition } from "./useAgentGUITimelineTransition";
import { useBottomDockPromptDismissal } from "./useBottomDockPromptDismissal";
import styles from "../AgentGUINode.styles";
interface Input {
  labels: AgentGUIViewLabels;
  slashStatusLimits: readonly AgentComposerSlashStatusLimit[];
  slashStatusLimitsLoading: boolean;
  slashStatusLimitsUnavailable: boolean;
  viewModel: AgentGUINodeViewModel;
}
export function useAgentGUIDetailModel(input: Input) {
  const {
    labels,
    slashStatusLimits,
    slashStatusLimitsLoading,
    slashStatusLimitsUnavailable,
    viewModel
  } = input;
  const projectedConversation = useProjectedAgentConversation({
    conversation: viewModel.detail.conversation,
    detail: viewModel.detail.conversationDetail,
    avoidGroupingEdits: viewModel.detail.avoidGroupingEdits
  });
  const targetConversation =
    viewModel.detail.availability === "not_found"
      ? null
      : projectedConversation;
  const {
    conversation,
    showTimelineSkeleton,
    timelineConversationId,
    transitionPending: timelineTransitionPending
  } = useAgentGUITimelineTransition({
    activeConversationId: viewModel.rail.activeConversationId,
    availability: viewModel.detail.availability,
    conversation: targetConversation
  });
  const hasActiveConversation = viewModel.rail.activeConversationId !== null;
  const selectedAgentTargetComingSoon = isAgentGUIAgentTargetComingSoon(
    viewModel.rail.selectedAgentTarget,
    viewModel.rail.comingSoonProviders
  );
  const emptyProviderReadinessGate = !hasActiveConversation
    ? viewModel.readiness.providerReadinessGate
    : null;
  const activePrompt =
    viewModel.interaction.pendingInteractivePrompt ??
    viewModel.interaction.pendingApproval;
  const activePromptResponsePending =
    activePrompt?.kind === "approval"
      ? viewModel.interaction.isRespondingApproval
      : viewModel.interaction.isRespondingInteractivePrompt;
  const activePromptRequestId = activePrompt?.requestId ?? null;
  const { dismissPrompt: dismissBottomDockPrompt, promptVisible } =
    useBottomDockPromptDismissal(
      viewModel.rail.activeConversationId,
      activePromptRequestId
    );
  const sessionChrome = useMemo<AgentGUISessionChrome>(
    () => ({ ...viewModel.interaction.sessionChrome, approval: null }),
    [viewModel.interaction.sessionChrome]
  );
  const homeStatusNoticeVisible = isAgentGUIHomeStatusNoticeVisible(
    sessionChrome.recovery
  );
  const rawSlashStatus = useMemo(
    () =>
      resolveSlashStatus({
        rawState: viewModel.interaction.sessionChrome.rawState,
        limits: slashStatusLimits,
        limitsLoading: slashStatusLimitsLoading,
        limitsUnavailable: slashStatusLimitsUnavailable,
        usage: viewModel.detail.usage
      }),
    [
      slashStatusLimits,
      slashStatusLimitsLoading,
      slashStatusLimitsUnavailable,
      viewModel.detail.usage,
      viewModel.interaction.sessionChrome.rawState
    ]
  );
  const slashStatus = useStableSlashStatus(rawSlashStatus);
  const displayedInlineNotice = useMemo(() => {
    if (homeStatusNoticeVisible) {
      return null;
    }
    const inlineNotice = viewModel.interaction.inlineNotice;
    const inlineNoticeMessage = inlineNotice?.message.trim() ?? "";
    if (!inlineNotice || inlineNoticeMessage === "") {
      return null;
    }
    if (
      isContextCanceledMessage(inlineNoticeMessage) &&
      viewModel.rail.activeConversation?.status === "completed" &&
      viewModel.readiness.activeLiveState !== "failed"
    ) {
      return null;
    }
    const chromeMessages = [
      sessionChrome.auth?.message,
      sessionChrome.recovery?.message
    ].flatMap((message) => {
      const normalizedMessage = message?.trim() ?? "";
      return normalizedMessage === "" ? [] : [normalizedMessage];
    });

    return chromeMessages.includes(inlineNoticeMessage)
      ? null
      : { ...inlineNotice, message: inlineNoticeMessage };
  }, [
    sessionChrome.auth?.message,
    sessionChrome.recovery?.message,
    viewModel.rail.activeConversation?.status,
    viewModel.readiness.activeLiveState,
    homeStatusNoticeVisible,
    viewModel.interaction.inlineNotice
  ]);
  const inlineNoticeChrome = useMemo<AgentGUISessionChrome | null>(() => {
    if (!displayedInlineNotice) {
      return null;
    }
    return {
      auth: null,
      approval: null,
      recovery: {
        kind: displayedInlineNotice.tone === "warning" ? "warning" : "failed",
        message: displayedInlineNotice.message,
        canRetry: displayedInlineNotice.id.startsWith(
          "agent-gui-tutti-mode-update-failed"
        )
      },
      rawState: null
    };
  }, [displayedInlineNotice]);
  const homeNoticeChrome = resolveAgentGUIHomeNoticeChrome({
    inlineNoticeChrome,
    sessionChrome
  });
  // Plan decisions replace the composer in the bottom dock: the card takes its slot
  // and the composer hides until it is acted on (optimistically cleared via
  // bottomDockDismissedPrompt) or otherwise resolves.
  const activePromptIsPlanDecision =
    activePrompt?.kind === "exit-plan" ||
    activePrompt?.kind === "plan-implementation";
  const activePromptIsVisible =
    activePrompt !== null && !homeStatusNoticeVisible && promptVisible;
  const bottomDockReplacementPrompt =
    activePromptIsPlanDecision && activePromptIsVisible ? activePrompt : null;
  // Approval / ask-user prompts keep the original layout: they lift above the
  // inline notice when one is present, otherwise they embed in the composer
  // (which stays visible). Only plan decisions replace the composer.
  const shouldLiftActivePromptAboveInlineNotice = inlineNoticeChrome !== null;
  const bottomDockLiftedPrompt =
    !activePromptIsPlanDecision &&
    shouldLiftActivePromptAboveInlineNotice &&
    activePromptIsVisible
      ? activePrompt
      : null;
  const composerActivePrompt =
    !activePromptIsVisible ||
    activePromptIsPlanDecision ||
    shouldLiftActivePromptAboveInlineNotice
      ? null
      : activePrompt;
  const showUnavailableChatEmpty =
    hasActiveConversation && viewModel.detail.availability === "not_found";
  const activeConversationTurnBusy = viewModel.composer.gate.conversationBusy;
  const isComposerSending =
    activeConversationTurnBusy ||
    viewModel.composer.gate.isAwaitingTurnStart === true ||
    (!hasActiveConversation &&
      viewModel.composer.gate.submission.status === "blocked" &&
      viewModel.composer.gate.submission.reason === "creating_conversation");
  const isCollaboratorConversation =
    viewModel.composer.gate.editor.status === "blocked" &&
    viewModel.composer.gate.editor.reason === "collaborator_read_only";
  const composerDisabledReason = isCollaboratorConversation
    ? labels.collaboratorSessionReadOnlyPlaceholder
    : null;
  const stopControl = resolveAgentGUIStopControl({
    hasPendingApproval: viewModel.interaction.pendingApproval !== null,
    hasPendingInteractivePrompt:
      viewModel.interaction.pendingInteractivePrompt !== null,
    isAuthBlocked: sessionChrome.auth !== null,
    isCancelPending: viewModel.composer.isCancelPending,
    isConversationBusy: activeConversationTurnBusy,
    isCreatingConversation: viewModel.composer.isCreatingConversation,
    hasPendingSubmitStopTarget:
      viewModel.composer.hasPendingSubmitStopTarget === true,
    isInterrupting: viewModel.composer.isInterrupting,
    isSubmitting: viewModel.composer.isSubmitting,
    isUnavailable: viewModel.readiness.activeLiveState === "failed"
  });
  const showStopButton = stopControl.visible;
  const stopDisabled = stopControl.disabled;
  const {
    chromeLabels,
    conversationFlowLabels,
    interactivePromptLabels
  } = useAgentGUIDetailLabels({
    canAnswerPendingInteractivePromptFromComposer:
      viewModel.interaction.canAnswerPendingInteractivePromptFromComposer,
    isCancelPending: viewModel.composer.isCancelPending,
    labels
  });
  const conversationFlowEmpty = useMemo(
    () =>
      showUnavailableChatEmpty ? (
        <div
          className={styles.unavailableChatEmpty}
          data-testid="agent-gui-unavailable-chat-empty"
        >
          <UnavailableChatIcon className={styles.unavailableChatEmptyIcon} />
          <span className={styles.unavailableChatEmptyText}>
            {labels.conversationUnavailable}
          </span>
        </div>
      ) : (
        <></>
      ),
    [labels.conversationUnavailable, showUnavailableChatEmpty]
  );
  const composerLabels = useMemo(
    () => ({
      send: labels.send,
      modelLabel: labels.modelLabel,
      modelSelectionLabel: labels.modelSelectionLabel,
      modelContextWindowSuffix: labels.modelContextWindowSuffix,
      modelTooltipVersionLabel: labels.modelTooltipVersionLabel,
      defaultModel: labels.defaultModel,
      loadingOptions: labels.loadingOptions,
      composerOptionsLoadFailed: labels.composerOptionsLoadFailed,
      retry: labels.composerOptionsRetry ?? labels.retryActivation,
      retryTooltip:
        labels.composerOptionsRetryTooltip ??
        labels.composerOptionsRetry ??
        labels.retryActivation,
      inheritedUnavailable: labels.inheritedUnavailable,
      loadingConversation: labels.loadingConversation,
      reasoningLabel: labels.reasoningLabel,
      reasoningDegreeLabel: labels.reasoningDegreeLabel,
      reasoningOptionDefault: labels.reasoningOptionDefault,
      reasoningOptionMinimal: labels.reasoningOptionMinimal,
      reasoningOptionLow: labels.reasoningOptionLow,
      reasoningOptionMedium: labels.reasoningOptionMedium,
      reasoningOptionHigh: labels.reasoningOptionHigh,
      reasoningOptionXHigh: labels.reasoningOptionXHigh,
      reasoningOptionMax: labels.reasoningOptionMax,
      reasoningOptionUltra: labels.reasoningOptionUltra,
      speedLabel: labels.speedLabel,
      speedSelectionLabel: labels.speedSelectionLabel,
      speedOptionStandard: labels.speedOptionStandard,
      speedOptionStandardDescription: labels.speedOptionStandardDescription,
      speedOptionFast: labels.speedOptionFast,
      speedOptionFastDescription: labels.speedOptionFastDescription,
      permissionLabel: labels.permissionLabel,
      permissionModeReadOnly: labels.permissionModeReadOnly,
      permissionModeAuto: labels.permissionModeAuto,
      permissionModeFullAccess: labels.permissionModeFullAccess,
      permissionModeChangeUnavailableDuringTurn:
        labels.permissionModeChangeUnavailableDuringTurn,
      modelDescriptions: labels.modelDescriptions,
      planModeLabel: labels.planModeLabel,
      codexSaverModeLabel: labels.codexSaverModeLabel,
      codexSaverModeDescription: labels.codexSaverModeDescription,
      rtkSaverModeLabel: labels.rtkSaverModeLabel,
      rtkSaverModeDescription: labels.rtkSaverModeDescription,
      normalModeLabel: labels.normalModeLabel,
      normalModeDescription: labels.normalModeDescription,
      planModeDescription: labels.planModeDescription,
      planModeOnLabel: labels.planModeOnLabel,
      planModeOffLabel: labels.planModeOffLabel,
      planUnavailable: labels.planUnavailable,
      queuedLabel: labels.queuedLabel,
      queuePausedByUserLabel: labels.queuePausedByUserLabel,
      sendQueuedPromptNext: labels.sendQueuedPromptNext,
      editQueuedPrompt: labels.editQueuedPrompt,
      deleteQueuedPrompt: labels.deleteQueuedPrompt,
      queuedPromptMoreActions: labels.queuedPromptMoreActions,
      stop: labels.stop,
      stopping: labels.stopping,
      slashCommandPalette: labels.slashCommandPalette,
      skillPickerPalette: labels.skillPickerPalette,
      slashPaletteCommandsGroup: labels.slashPaletteCommandsGroup,
      slashPaletteCapabilitiesGroup: labels.slashPaletteCapabilitiesGroup,
      slashPaletteCapabilitiesLoading: labels.slashPaletteCapabilitiesLoading,
      slashPaletteSkillsGroup: labels.slashPaletteSkillsGroup,
      slashPalettePluginsGroup: labels.slashPalettePluginsGroup,
      slashPaletteConnectorsGroup: labels.slashPaletteConnectorsGroup,
      slashPaletteConnectorConnected: labels.slashPaletteConnectorConnected,
      slashPaletteConnectorNotConnected:
        labels.slashPaletteConnectorNotConnected,
      slashPaletteConnectorUnsupported: labels.slashPaletteConnectorUnsupported,
      slashPaletteMcpGroup: labels.slashPaletteMcpGroup,
      slashCommandPresentation: labels.slashCommandPresentation,
      slashCommandCompactLabel: labels.slashCommandCompactLabel,
      slashCommandContextLabel: labels.slashCommandContextLabel,
      slashCommandFastLabel: labels.slashCommandFastLabel,
      slashCommandGoalLabel: labels.slashCommandGoalLabel,
      slashCommandInitLabel: labels.slashCommandInitLabel,
      slashCommandPlanLabel: labels.slashCommandPlanLabel,
      slashCommandReviewLabel: labels.slashCommandReviewLabel,
      slashCommandStatusLabel: labels.slashCommandStatusLabel,
      slashCommandUsageLabel: labels.slashCommandUsageLabel,
      slashCommandCompactDescription: labels.slashCommandCompactDescription,
      slashCommandContextDescription: labels.slashCommandContextDescription,
      slashCommandFastDescription: labels.slashCommandFastDescription,
      slashCommandGoalDescription: labels.slashCommandGoalDescription,
      slashCommandInitDescription: labels.slashCommandInitDescription,
      slashCommandPlanDescription: labels.slashCommandPlanDescription,
      slashCommandReviewDescription: labels.slashCommandReviewDescription,
      slashCommandStatusDescription: labels.slashCommandStatusDescription,
      slashCommandUsageDescription: labels.slashCommandUsageDescription,
      browserUseCapabilityLabel: labels.browserUseCapabilityLabel,
      browserUseCapabilityDescription: labels.browserUseCapabilityDescription,
      browserUseCapabilityDescriptionAutoConnect:
        labels.browserUseCapabilityDescriptionAutoConnect,
      browserUseCapabilityDescriptionIsolated:
        labels.browserUseCapabilityDescriptionIsolated,
      browserUseCapabilitySettingsLabel:
        labels.browserUseCapabilitySettingsLabel,
      browserUseCapabilitySettingsDescription:
        labels.browserUseCapabilitySettingsDescription,
      capabilityInlineSettingsLabel: labels.capabilityInlineSettingsLabel,
      computerUseCapabilityLabel: labels.computerUseCapabilityLabel,
      computerUseCapabilityDescription: labels.computerUseCapabilityDescription,
      computerUseCapabilitySetupRequiredDescription:
        labels.computerUseCapabilitySetupRequiredDescription,
      computerUseCapabilityAuthorizationRequiredDescription:
        labels.computerUseCapabilityAuthorizationRequiredDescription,
      computerUseCapabilityAuthorizationUnknownDescription:
        labels.computerUseCapabilityAuthorizationUnknownDescription,
      computerUseCapabilitySettingsLabel:
        labels.computerUseCapabilitySettingsLabel,
      computerUseCapabilitySettingsDescription:
        labels.computerUseCapabilitySettingsDescription,
      slashStatusTitle: labels.slashStatusTitle,
      slashStatusSession: labels.slashStatusSession,
      slashStatusBaseUrl: labels.slashStatusBaseUrl,
      slashStatusContext: labels.slashStatusContext,
      slashStatusLimits: labels.slashStatusLimits,
      slashStatusAccount: labels.slashStatusAccount,
      slashStatusProviderAccount: labels.slashStatusProviderAccount,
      slashStatusClose: labels.slashStatusClose,
      slashStatusContextValue: labels.slashStatusContextValue,
      slashStatusContextUnavailable: labels.slashStatusContextUnavailable,
      slashStatusLimitsUnavailable: labels.slashStatusLimitsUnavailable,
      slashStatusEmptyValue: labels.slashStatusEmptyValue,
      slashStatusUsageJustUpdated: labels.slashStatusUsageJustUpdated,
      slashStatusUsageMinutesAgo: labels.slashStatusUsageMinutesAgo,
      slashStatusUsageHoursAgo: labels.slashStatusUsageHoursAgo,
      slashStatusUsageUpdating: labels.slashStatusUsageUpdating,
      slashStatusUsageRefreshFailed: labels.slashStatusUsageRefreshFailed,
      slashStatusUsageRefreshAria: labels.slashStatusUsageRefreshAria,
      slashStatusUsageAuthRequired: labels.slashStatusUsageAuthRequired,
      slashStatusUsageSessionExpired: labels.slashStatusUsageSessionExpired,
      slashStatusUsageSubscriptionRequired:
        labels.slashStatusUsageSubscriptionRequired,
      slashStatusUsageQuotaExhausted: labels.slashStatusUsageQuotaExhausted,
      slashStatusUsageParseFailed: labels.slashStatusUsageParseFailed,
      slashStatusUsageError: labels.slashStatusUsageError,
      usageChipLabel: labels.usageChipLabel,
      usageTooltipLabel: labels.usageTooltipLabel,
      usagePopoverTitle: labels.usagePopoverTitle,
      usageContextWindowLabel: labels.usageContextWindowLabel,
      usageTokensLabel: labels.usageTokensLabel,
      usageLimitsLabel: labels.usageLimitsLabel,
      usageCompactAction: labels.usageCompactAction,
      fileMentionPalette: labels.fileMentionPalette,
      fileMentionLoading: labels.fileMentionLoading,
      fileMentionEmpty: labels.fileMentionEmpty,
      fileMentionError: labels.fileMentionError,
      fileMentionTabHint: labels.fileMentionTabHint,
      fileDropHint: labels.fileDropHint,
      mentionPalette: labels.mentionPalette,
      removeMention: labels.removeMention,
      addReference: labels.addReference,
      addContent: labels.addContent,
      addContentResourcePanel: labels.addContentResourcePanel,
      addContentConnectors: labels.addContentConnectors,
      addContentConnectorConnected: labels.addContentConnectorConnected,
      addContentConnectorConnect: labels.addContentConnectorConnect,
      addContentConnectorAuthorize: labels.addContentConnectorAuthorize,
      addContentConnectorEmpty: labels.addContentConnectorEmpty,
      addContentConnectorLoading: labels.addContentConnectorLoading,
      addContentConnectorMore: labels.addContentConnectorMore,
      addContentConnectorSelected: labels.addContentConnectorSelected,
      referenceWorkspaceFiles: labels.referenceWorkspaceFiles,
      handoffConversation: labels.handoffConversation,
      handoffConversationTooltip: labels.handoffConversationTooltip,
      handoffConversationMenu: labels.handoffConversationMenu,
      handoffTargetDeviceSource: labels.handoffTargetDeviceSource,
      handoffTargetSelf: labels.handoffTargetSelf,
      handoffTargetShared: labels.handoffTargetShared,
      providerSwitchLabel: labels.providerSwitchLabel,
      projectLocked: labels.projectLocked,
      sessionLaunchModeLabel: labels.sessionLaunchModeLabel,
      sessionLaunchModeLocal: labels.sessionLaunchModeLocal,
      sessionLaunchModeWorktree: labels.sessionLaunchModeWorktree,
      projectMissingDescription: labels.projectMissingDescription,
      promptTipsPrefix: labels.promptTipsPrefix,
      reviewPicker: labels.reviewPicker,
      quickPrompts: labels.quickPrompts,
      ...interactivePromptLabels
    }),
    [
      interactivePromptLabels,
      labels.composerOptionsLoadFailed,
      labels.composerOptionsRetry,
      labels.composerOptionsRetryTooltip,
      labels.defaultModel,
      labels.addReference,
      labels.addContent,
      labels.addContentResourcePanel,
      labels.addContentConnectors,
      labels.addContentConnectorConnected,
      labels.addContentConnectorConnect,
      labels.addContentConnectorAuthorize,
      labels.addContentConnectorEmpty,
      labels.addContentConnectorLoading,
      labels.addContentConnectorMore,
      labels.addContentConnectorSelected,
      labels.deleteQueuedPrompt,
      labels.editQueuedPrompt,
      labels.fileMentionEmpty,
      labels.fileMentionError,
      labels.fileMentionLoading,
      labels.fileMentionPalette,
      labels.fileMentionTabHint,
      labels.fileDropHint,
      labels.handoffConversation,
      labels.handoffConversationTooltip,
      labels.handoffConversationMenu,
      labels.handoffTargetDeviceSource,
      labels.handoffTargetSelf,
      labels.handoffTargetShared,
      labels.inheritedUnavailable,
      labels.loadingConversation,
      labels.modelLabel,
      labels.modelContextWindowSuffix,
      labels.modelDescriptions,
      labels.modelSelectionLabel,
      labels.modelTooltipVersionLabel,
      labels.permissionLabel,
      labels.permissionModeAuto,
      labels.permissionModeChangeUnavailableDuringTurn,
      labels.permissionModeFullAccess,
      labels.permissionModeReadOnly,
      labels.planModeLabel,
      labels.codexSaverModeLabel,
      labels.codexSaverModeDescription,
      labels.rtkSaverModeLabel,
      labels.rtkSaverModeDescription,
      labels.normalModeLabel,
      labels.normalModeDescription,
      labels.planModeDescription,
      labels.planModeOffLabel,
      labels.planModeOnLabel,
      labels.planUnavailable,
      labels.projectLocked,
      labels.sessionLaunchModeLabel,
      labels.sessionLaunchModeLocal,
      labels.sessionLaunchModeWorktree,
      labels.projectMissingDescription,
      labels.promptTipsPrefix,
      labels.reviewPicker,
      labels.quickPrompts,
      labels.queuedLabel,
      labels.queuePausedByUserLabel,
      labels.queuedPromptMoreActions,
      labels.referenceWorkspaceFiles,
      labels.providerSwitchLabel,
      labels.removeMention,
      labels.reasoningDegreeLabel,
      labels.reasoningLabel,
      labels.reasoningOptionDefault,
      labels.reasoningOptionHigh,
      labels.reasoningOptionLow,
      labels.reasoningOptionMax,
      labels.reasoningOptionUltra,
      labels.reasoningOptionMedium,
      labels.reasoningOptionMinimal,
      labels.reasoningOptionXHigh,
      labels.speedLabel,
      labels.speedSelectionLabel,
      labels.speedOptionStandard,
      labels.speedOptionStandardDescription,
      labels.speedOptionFast,
      labels.speedOptionFastDescription,
      labels.send,
      labels.sendQueuedPromptNext,
      labels.slashCommandPalette,
      labels.browserUseCapabilityDescription,
      labels.browserUseCapabilityDescriptionAutoConnect,
      labels.browserUseCapabilityDescriptionIsolated,
      labels.browserUseCapabilityLabel,
      labels.browserUseCapabilitySettingsDescription,
      labels.browserUseCapabilitySettingsLabel,
      labels.capabilityInlineSettingsLabel,
      labels.computerUseCapabilityDescription,
      labels.computerUseCapabilityAuthorizationRequiredDescription,
      labels.computerUseCapabilityAuthorizationUnknownDescription,
      labels.computerUseCapabilitySetupRequiredDescription,
      labels.computerUseCapabilityLabel,
      labels.computerUseCapabilitySettingsDescription,
      labels.computerUseCapabilitySettingsLabel,
      labels.slashPaletteCapabilitiesGroup,
      labels.slashPaletteCapabilitiesLoading,
      labels.slashPaletteCommandsGroup,
      labels.slashPaletteConnectorConnected,
      labels.slashPaletteConnectorNotConnected,
      labels.slashPaletteConnectorUnsupported,
      labels.slashPaletteConnectorsGroup,
      labels.slashCommandCompactLabel,
      labels.slashCommandContextLabel,
      labels.slashCommandFastLabel,
      labels.slashCommandGoalLabel,
      labels.slashCommandInitLabel,
      labels.slashCommandPlanLabel,
      labels.slashCommandReviewLabel,
      labels.slashCommandStatusLabel,
      labels.slashCommandUsageLabel,
      labels.slashCommandCompactDescription,
      labels.slashCommandContextDescription,
      labels.slashCommandFastDescription,
      labels.slashCommandGoalDescription,
      labels.slashCommandInitDescription,
      labels.slashCommandPlanDescription,
      labels.slashCommandReviewDescription,
      labels.slashCommandStatusDescription,
      labels.slashCommandUsageDescription,
      labels.slashPaletteMcpGroup,
      labels.slashPalettePluginsGroup,
      labels.slashPaletteSkillsGroup,
      labels.slashStatusClose,
      labels.slashStatusContext,
      labels.slashStatusContextUnavailable,
      labels.slashStatusContextValue,
      labels.slashStatusBaseUrl,
      labels.slashStatusLimits,
      labels.slashStatusLimitsUnavailable,
      labels.slashStatusEmptyValue,
      labels.slashStatusUsageHoursAgo,
      labels.slashStatusUsageJustUpdated,
      labels.slashStatusUsageMinutesAgo,
      labels.slashStatusUsageRefreshAria,
      labels.slashStatusUsageRefreshFailed,
      labels.slashStatusUsageUpdating,
      labels.slashStatusUsageAuthRequired,
      labels.slashStatusUsageSessionExpired,
      labels.slashStatusUsageSubscriptionRequired,
      labels.slashStatusUsageQuotaExhausted,
      labels.slashStatusUsageParseFailed,
      labels.slashStatusUsageError,
      labels.slashStatusSession,
      labels.slashStatusTitle,
      labels.slashCommandPresentation,
      labels.usageChipLabel,
      labels.usageContextWindowLabel,
      labels.usageLimitsLabel,
      labels.usageCompactAction,
      labels.usagePopoverTitle,
      labels.usageTokensLabel,
      labels.stop,
      labels.stopping
    ]
  );

  return {
    activePrompt,
    activeConversationTurnBusy,
    activePromptRequestId,
    activePromptResponsePending,
    bottomDockLiftedPrompt,
    bottomDockReplacementPrompt,
    chromeLabels,
    composerActivePrompt,
    composerDisabledReason,
    composerGate: viewModel.composer.gate,
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
    isCollaboratorConversation,
    isComposerSending,
    selectedAgentTargetComingSoon,
    sessionChrome,
    showStopButton,
    stopDisabled,
    showTimelineSkeleton,
    showUnavailableChatEmpty,
    slashStatus,
    timelineConversationId,
    timelineInteractionLocked: timelineTransitionPending
  };
}
