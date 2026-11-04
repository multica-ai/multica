import { useMemo } from "react";
import type { WorkspaceFileReferenceCopy } from "@tutti-os/workspace-file-reference/contracts";
import type { TranslateFn } from "../../i18n/index";
import {
  agentGUIProviderIdentityDisplayName,
  resolveAgentGUIProviderCatalogIdentity
} from "../../providerIdentityCatalog";
import type { AgentMessageMarkdownWorkspaceAppIcon } from "../../shared/AgentMessageMarkdown";
import type { AgentGUIHomeSuggestionId } from "../../types";
import { resolveAgentGUIProviderDisplayLabel } from "./model/agentGuiProviderIdentity";
import { buildAgentHomeSuggestions } from "./model/agentHomeSuggestions";
import type { AgentGUIViewLabels } from "./AgentGUINodeView";
import { agentGUIConversationLabels } from "./view/agentGUIConversationLabels";
import { agentGUIProviderManagerLabels } from "./view/agentGUIProviderManagerLabels";
import { agentGUIUsageStatusLabels } from "./view/agentGUIUsageStatusLabels";
import { agentQuickPromptLabels } from "./composer/quickPrompts/agentQuickPromptLabels";
import { agentSlashPaletteLabels } from "./composer/agentSlashPaletteLabels";
import { agentGUIProjectLaunchLabels } from "./composer/agentGUIProjectLaunchLabels";
import { agentGUIConnectorLabels } from "./composer/agentGUIConnectorLabels";

export { buildAgentHomeSuggestions };


const workspaceFileReferenceLocaleKeyByPickerKey: Record<string, string> = {
  "actions.cancel": "common.cancel",
  "directoryPicker.confirm": "agentHost.agentGui.directoryPicker.confirm",
  "directoryPicker.emptySearch":
    "agentHost.agentGui.directoryPicker.emptySearch",
  "directoryPicker.searchPlaceholder":
    "agentHost.agentGui.directoryPicker.searchPlaceholder",
  "directoryPicker.title": "agentHost.agentGui.directoryPicker.title",
  "referencePicker.confirm": "agentHost.agentGui.referencePicker.confirm",
  "referencePicker.clearFilter":
    "agentHost.agentGui.referencePicker.clearFilter",
  "referencePicker.emptyDirectory":
    "agentHost.agentGui.referencePicker.emptyDirectory",
  "referencePicker.emptyPreview":
    "agentHost.agentGui.referencePicker.emptyPreview",
  "referencePicker.emptySearch":
    "agentHost.agentGui.referencePicker.emptySearch",
  "referencePicker.fileTypeAll":
    "agentHost.agentGui.referencePicker.fileTypeAll",
  "referencePicker.fileTypeDocument":
    "agentHost.agentGui.referencePicker.fileTypeDocument",
  "referencePicker.fileTypeImage":
    "agentHost.agentGui.referencePicker.fileTypeImage",
  "referencePicker.fileTypeOther":
    "agentHost.agentGui.referencePicker.fileTypeOther",
  "referencePicker.fileTypeSeparator":
    "agentHost.agentGui.referencePicker.fileTypeSeparator",
  "referencePicker.fileTypeVideo":
    "agentHost.agentGui.referencePicker.fileTypeVideo",
  "referencePicker.fileTypeWebpage":
    "agentHost.agentGui.referencePicker.fileTypeWebpage",
  "referencePicker.loadMore": "agentHost.agentGui.referencePicker.loadMore",
  "referencePicker.loadMoreGroups":
    "agentHost.agentGui.referencePicker.loadMoreGroups",
  "referencePicker.loading": "agentHost.agentGui.referencePicker.loading",
  "referencePicker.loadError": "agentHost.agentGui.referencePicker.loadError",
  "referencePicker.previewBinary":
    "agentHost.agentGui.referencePicker.previewBinary",
  "referencePicker.previewDecodeFailed":
    "agentHost.agentGui.referencePicker.previewDecodeFailed",
  "referencePicker.previewError":
    "agentHost.agentGui.referencePicker.previewError",
  "referencePicker.previewFileTooLarge":
    "agentHost.agentGui.referencePicker.previewFileTooLarge",
  "referencePicker.previewFolder":
    "agentHost.agentGui.referencePicker.previewFolder",
  "referencePicker.previewHierarchy":
    "agentHost.agentGui.referencePicker.previewHierarchy",
  "referencePicker.previewLoading":
    "agentHost.agentGui.referencePicker.previewLoading",
  "referencePicker.previewModified":
    "agentHost.agentGui.referencePicker.previewModified",
  "referencePicker.previewSize":
    "agentHost.agentGui.referencePicker.previewSize",
  "referencePicker.previewSource":
    "agentHost.agentGui.referencePicker.previewSource",
  "referencePicker.previewTextTooLarge":
    "agentHost.agentGui.referencePicker.previewTextTooLarge",
  "referencePicker.previewTooLarge":
    "agentHost.agentGui.referencePicker.previewTooLarge",
  "referencePicker.previewUnavailable":
    "agentHost.agentGui.referencePicker.previewUnavailable",
  "referencePicker.previewUnsupported":
    "agentHost.agentGui.referencePicker.previewUnsupported",
  "referencePicker.searchPlaceholder":
    "agentHost.agentGui.referencePicker.searchPlaceholder",
  "referencePicker.selectGroupHint":
    "agentHost.agentGui.referencePicker.selectGroupHint",
  "referencePicker.selectedCount":
    "agentHost.agentGui.referencePicker.selectedCount",
  "referencePicker.sourceColumn":
    "agentHost.agentGui.referencePicker.sourceColumn",
  "referencePicker.title": "agentHost.agentGui.referencePicker.title"
};

export function useAgentGUIViewLabels(input: {
  disabledHomeSuggestions?: readonly AgentGUIHomeSuggestionId[];
  displayProviderLabel: string;
  fallbackAgentTitle: string;
  t: TranslateFn;
  workspaceAppIcons: readonly AgentMessageMarkdownWorkspaceAppIcon[];
  workspaceId: string;
}): AgentGUIViewLabels {
  const {
    disabledHomeSuggestions,
    displayProviderLabel,
    fallbackAgentTitle,
    t,
    workspaceAppIcons,
    workspaceId
  } = input;
  return useMemo<AgentGUIViewLabels>(() => {
    const providerInterpolation = { provider: displayProviderLabel };
    return {
      initialPlaceholder: t(
        "agentHost.agentGui.initialPlaceholder",
        providerInterpolation
      ),
      followupPlaceholder: t(
        "agentHost.agentGui.followupPlaceholder",
        providerInterpolation
      ),
      installRequiredPlaceholder: t(
        "agentHost.agentGui.installRequiredPlaceholder",
        providerInterpolation
      ),
      installRequiredAction: t("agentHost.agentGui.installRequiredAction"),
      providerGateCheckingTitle: t(
        "agentHost.agentGui.providerGateCheckingTitle"
      ),
      providerGateCheckingDescription: t(
        "agentHost.agentGui.providerGateCheckingDescription",
        providerInterpolation
      ),
      providerGateCheckingAgentsDescription: t(
        "agentHost.agentGui.providerGateCheckingAgentsDescription"
      ),
      providerGateInstallTitle: t(
        "agentHost.agentGui.providerGateInstallTitle",
        providerInterpolation
      ),
      providerGateInstallDescription: t(
        "agentHost.agentGui.providerGateInstallDescription",
        providerInterpolation
      ),
      providerGateInstallAction: t(
        "agentHost.agentGui.providerGateInstallAction"
      ),
      providerGateLoginTitle: t(
        "agentHost.agentGui.providerGateLoginTitle",
        providerInterpolation
      ),
      providerGateLoginDescription: t(
        "agentHost.agentGui.providerGateLoginDescription",
        providerInterpolation
      ),
      providerGateLoginAction: t("agentHost.agentGui.providerGateLoginAction"),
      providerGateModelPlanAction: t(
        "agentHost.agentGui.providerGateModelPlanAction"
      ),
      providerGateComingSoonTitle: t(
        "agentHost.agentGui.providerGateComingSoonTitle",
        providerInterpolation
      ),
      providerGateComingSoonDescription: t(
        "agentHost.agentGui.providerGateComingSoonDescription",
        providerInterpolation
      ),
      providerGateComingSoonAction: t(
        "agentHost.agentGui.providerGateComingSoonAction"
      ),
      providerGateUnavailableTitle: t(
        "agentHost.agentGui.providerGateUnavailableTitle",
        providerInterpolation
      ),
      providerGateUnavailableDescription: t(
        "agentHost.agentGui.providerGateUnavailableDescription",
        providerInterpolation
      ),
      providerGateRetryAction: t("agentHost.agentGui.providerGateRetryAction"),
      providerGateRuntimeSelectionTitle: t(
        "agentHost.agentGui.providerGateRuntimeSelectionTitle",
        providerInterpolation
      ),
      providerGateRuntimeSelectionDescription: t(
        "agentHost.agentGui.providerGateRuntimeSelectionDescription",
        providerInterpolation
      ),
      providerGateRuntimeSelectionAction: t(
        "agentHost.agentGui.providerGateRuntimeSelectionAction"
      ),
      providerGatePendingInstall: t(
        "agentHost.agentGui.providerGatePendingInstall"
      ),
      providerGatePendingLogin: t(
        "agentHost.agentGui.providerGatePendingLogin"
      ),
      providerGatePendingRefresh: t(
        "agentHost.agentGui.providerGatePendingRefresh"
      ),
      collaboratorSessionReadOnlyPlaceholder: t(
        "agentHost.agentGui.collaboratorSessionReadOnlyPlaceholder"
      ),
      send: t("agentHost.agentGui.send"),
      modelLabel: t("agentHost.agentGui.modelLabel"),
      modelSelectionLabel: t("agentHost.agentGui.modelSelectionLabel"),
      modelContextWindowSuffix: t(
        "agentHost.agentGui.modelContextWindowSuffix"
      ),
      modelTooltipVersionLabel: t(
        "agentHost.agentGui.modelTooltipVersionLabel"
      ),
      defaultModel: t("agentHost.agentGui.defaultModel"),
      loadingOptions: t("agentHost.agentGui.loadingOptions"),
      composerOptionsLoadFailed: t(
        "agentHost.agentGui.composerOptionsLoadFailed"
      ),
      composerOptionsRetry: t("agentHost.agentGui.composerOptionsRetry"),
      composerOptionsRetryTooltip: t(
        "agentHost.agentGui.composerOptionsRetryTooltip"
      ),
      inheritedUnavailable: t("agentHost.agentGui.inheritedUnavailable"),
      reasoningLabel: t("agentHost.agentGui.reasoningLabel"),
      reasoningDegreeLabel: t("agentHost.agentGui.reasoningDegreeLabel"),
      reasoningOptionDefault: t("agentHost.agentGui.reasoningOptionDefault"),
      reasoningOptionMinimal: t("agentHost.agentGui.reasoningOptionMinimal"),
      reasoningOptionLow: t("agentHost.agentGui.reasoningOptionLow"),
      reasoningOptionMedium: t("agentHost.agentGui.reasoningOptionMedium"),
      reasoningOptionHigh: t("agentHost.agentGui.reasoningOptionHigh"),
      reasoningOptionXHigh: t("agentHost.agentGui.reasoningOptionXHigh"),
      reasoningOptionMax: t("agentHost.agentGui.reasoningOptionMax"),
      reasoningOptionUltra: t("agentHost.agentGui.reasoningOptionUltra"),
      speedLabel: t("agentHost.agentGui.speedLabel"),
      speedSelectionLabel: t("agentHost.agentGui.speedSelectionLabel"),
      speedOptionStandard: t("agentHost.agentGui.speedOptionStandard"),
      speedOptionStandardDescription: t(
        "agentHost.agentGui.speedOptionStandardDescription"
      ),
      speedOptionFast: t("agentHost.agentGui.speedOptionFast"),
      speedOptionFastDescription: t(
        "agentHost.agentGui.speedOptionFastDescription"
      ),
      permissionLabel: t("agentHost.agentGui.permissionLabel"),
      permissionModeReadOnly: t("agentHost.agentGui.permissionModeReadOnly"),
      permissionModeAuto: t("agentHost.agentGui.permissionModeAuto"),
      permissionModeFullAccess: t(
        "agentHost.agentGui.permissionModeFullAccess"
      ),
      permissionModeChangeUnavailableDuringTurn: t(
        "agentHost.agentGui.permissionModeChangeUnavailableDuringTurn"
      ),
      modelDescriptions: {
        frontierComplexCoding: t(
          "agentHost.agentGui.modelDescriptions.frontierComplexCoding"
        ),
        everydayCoding: t(
          "agentHost.agentGui.modelDescriptions.everydayCoding"
        ),
        smallFastCostEfficient: t(
          "agentHost.agentGui.modelDescriptions.smallFastCostEfficient"
        ),
        codingOptimized: t(
          "agentHost.agentGui.modelDescriptions.codingOptimized"
        ),
        ultraFastCoding: t(
          "agentHost.agentGui.modelDescriptions.ultraFastCoding"
        ),
        professionalLongRunning: t(
          "agentHost.agentGui.modelDescriptions.professionalLongRunning"
        )
      },
      planModeLabel: t("agentHost.agentGui.planModeLabel"),
      codexSaverModeLabel: t("agentHost.agentGui.codexSaverModeLabel"),
      codexSaverModeDescription: t(
        "agentHost.agentGui.codexSaverModeDescription"
      ),
      rtkSaverModeLabel: t("agentHost.agentGui.rtkSaverModeLabel"),
      rtkSaverModeDescription: t("agentHost.agentGui.rtkSaverModeDescription"),
      normalModeLabel: t("agentHost.agentGui.normalModeLabel"),
      normalModeDescription: t("agentHost.agentGui.normalModeDescription"),
      planModeDescription: t("agentHost.agentGui.planModeDescription"),
      planModeOnLabel: t("agentHost.agentGui.planModeOnLabel"),
      planModeOffLabel: t("agentHost.agentGui.planModeOffLabel"),
      planUnavailable: t("agentHost.agentGui.planUnavailable"),
      queuedLabel: t("agentHost.agentGui.queuedLabel"),
      queuePausedByUserLabel: t("agentHost.agentGui.queuePausedByUserLabel"),
      sendQueuedPromptNext: t("agentHost.agentGui.sendQueuedPromptNext"),
      editQueuedPrompt: t("agentHost.agentGui.editQueuedPrompt"),
      deleteQueuedPrompt: t("agentHost.agentGui.deleteQueuedPrompt"),
      queuedPromptMoreActions: t("agentHost.agentGui.queuedPromptMoreActions"),
      stop: t("agentHost.agentGui.stop"),
      stopping: t("agentHost.agentGui.stopping"),
      slashStatusTitle: t("agentHost.agentGui.slashStatusTitle"),
      slashStatusSession: t("agentHost.agentGui.slashStatusSession"),
      slashStatusBaseUrl: t("agentHost.agentGui.slashStatusBaseUrl"),
      slashStatusContext: t("agentHost.agentGui.slashStatusContext"),
      slashStatusLimits: t("agentHost.agentGui.slashStatusLimits"),
      slashStatusAccount: t("agentHost.agentGui.slashStatusAccount"),
      slashStatusProviderAccount: (provider: string) => {
        const identity = resolveAgentGUIProviderCatalogIdentity(provider);
        if (!identity) {
          return null;
        }
        return t("agentHost.agentGui.slashStatusProviderAccount", {
          provider: agentGUIProviderIdentityDisplayName(identity, t)
        });
      },
      slashStatusClose: t("agentHost.agentGui.slashStatusClose"),
      slashStatusContextValue: (input: {
        percentLeft: number;
        usedTokens: string;
        totalTokens: string;
      }) =>
        t("agentHost.agentGui.slashStatusContextValue", {
          percentLeft: input.percentLeft,
          usedTokens: input.usedTokens,
          totalTokens: input.totalTokens
        }),
      slashStatusContextUnavailable: t(
        "agentHost.agentGui.slashStatusContextUnavailable"
      ),
      slashStatusLimitsUnavailable: t(
        "agentHost.agentGui.slashStatusLimitsUnavailable"
      ),
      slashStatusEmptyValue: t("agentHost.agentGui.slashStatusEmptyValue"),
      slashStatusUsageJustUpdated: t(
        "agentHost.agentGui.slashStatusUsageJustUpdated"
      ),
      slashStatusUsageMinutesAgo: (count: number) =>
        t("agentHost.agentGui.slashStatusUsageMinutesAgo", { count }),
      slashStatusUsageHoursAgo: (count: number) =>
        t("agentHost.agentGui.slashStatusUsageHoursAgo", { count }),
      slashStatusUsageUpdating: t(
        "agentHost.agentGui.slashStatusUsageUpdating"
      ),
      slashStatusUsageRefreshFailed: t(
        "agentHost.agentGui.slashStatusUsageRefreshFailed"
      ),
      slashStatusUsageRefreshAria: t(
        "agentHost.agentGui.slashStatusUsageRefreshAria"
      ),
      ...agentGUIUsageStatusLabels(t),
      usageChipLabel: (input: { percent: number }) =>
        t("agentHost.agentGui.usageChipLabel", { percent: input.percent }),
      usageTooltipLabel: t("agentHost.agentGui.usageTooltipLabel"),
      usagePopoverTitle: t("agentHost.agentGui.usagePopoverTitle"),
      usageContextWindowLabel: t("agentHost.agentGui.usageContextWindowLabel"),
      usageTokensLabel: t("agentHost.agentGui.usageTokensLabel"),
      usageLimitsLabel: t("agentHost.agentGui.usageLimitsLabel"),
      usageCompactAction: t("agentHost.agentGui.usageCompactAction"),
      planImplementationLead: t("agentHost.agentGui.planImplementationLead"),
      planImplementationConfirm: t(
        "agentHost.agentGui.planImplementationConfirm"
      ),
      planImplementationFeedbackPlaceholder: t(
        "agentHost.agentGui.planImplementationFeedbackPlaceholder"
      ),
      planImplementationSend: t("agentHost.agentGui.planImplementationSend"),
      planImplementationSkip: t("agentHost.agentGui.planImplementationSkip"),
      noRunningResponse: t("agentHost.agentGui.noRunningResponse"),
      empty: t("agentHost.agentGui.empty", providerInterpolation),
      homeSuggestions: buildAgentHomeSuggestions(
        t,
        workspaceId,
        workspaceAppIcons ?? [],
        disabledHomeSuggestions
      ),
      homeSuggestionsClose: t("agentHost.agentGui.homeSuggestionsClose"),
      emptyForProvider: (provider: string) =>
        t("agentHost.agentGui.empty", {
          provider: resolveAgentGUIProviderDisplayLabel(
            provider,
            fallbackAgentTitle
          )
        }),
      emptyProvider: displayProviderLabel,
      emptyProviderForProvider: (provider: string) =>
        resolveAgentGUIProviderDisplayLabel(provider, fallbackAgentTitle),
      conversations: t("agentHost.agentGui.conversations"),
      agentConfig: t("agentHost.agentGui.agentConfig"),
      agentSettingsMenu: t("agentHost.agentGui.agentSettingsMenu"),
      agentEnvSetup: t("agentHost.agentGui.agentEnvSetup"),
      ...agentGUIProviderManagerLabels(t),
      ...agentGUIConversationLabels(t),
      conversationFilterAll: t("agentHost.agentGui.conversationFilterAll"),
      conversationFilterCodex: t("agentHost.agentGui.conversationFilterCodex"),
      conversationFilterClaudeCode: t(
        "agentHost.agentGui.conversationFilterClaudeCode"
      ),
      conversationFilterTutti: t("agentHost.agentGui.conversationFilterTutti"),
      providerSwitchLabel: t("agentHost.agentGui.providerSwitchLabel"),
      sharedAgentOwnerSeparator: t(
        "agentHost.agentGui.sharedAgentOwnerSeparator"
      ),
      loadingConversation: t("agentHost.agentGui.loadingConversation"),
      continuedFromTask: t("agentHost.agentGui.continuedFromTask"),
      selectionAddToConversation: t(
        "agentHost.agentGui.selectionAddToConversation"
      ),
      selectionAskInSide: t("agentHost.agentGui.selectionAskInSide"),
      scrollToBottom: t("agentHost.agentGui.scrollToBottom"),
      fallbackAgentTitle,
      untitledConversationTitle: t(
        "agentHost.workspaceAgentsUntitledConversation"
      ),
      sectionToday: t("agentHost.agentGui.sectionToday"),
      sectionYesterday: t("agentHost.agentGui.sectionYesterday"),
      sectionEarlier: t("agentHost.agentGui.sectionEarlier"),
      projectSectionEdit: t("agentHost.agentGui.projectSectionEdit"),
      projectSectionMoreActions: t(
        "agentHost.agentGui.projectSectionMoreActions"
      ),
      projectSectionViewFiles: t("agentHost.agentGui.projectSectionViewFiles"),
      pinProject: t("agentHost.agentGui.pinProject"),
      unpinProject: t("agentHost.agentGui.unpinProject"),
      pinnedProjectAccessibleName: (projectLabel: string) =>
        t("agentHost.agentGui.pinnedProjectAccessibleName", {
          project: projectLabel
        }),
      projectRailCreateProject: t(
        "agentHost.agentGui.projectRailCreateProject"
      ),
      projectRailLinkExistingProject: t(
        "agentHost.agentGui.projectRailLinkExistingProject"
      ),
      removeProject: t("agentHost.agentGui.removeProject"),
      removeProjectConfirmDescription: (projectLabel: string) =>
        t("agentHost.agentGui.removeProjectConfirmDescription", {
          project: projectLabel
        }),
      removeProjectConfirmTitle: t(
        "agentHost.agentGui.removeProjectConfirmTitle"
      ),
      batchDeleteProjectSessions: t(
        "agentHost.agentGui.batchDeleteProjectSessions"
      ),
      batchDeleteProjectSessionsTitle: t(
        "agentHost.agentGui.batchDeleteProjectSessionsTitle"
      ),
      batchDeleteProjectSessionsBody: (count: number, project: string) =>
        t("agentHost.agentGui.batchDeleteProjectSessionsBody", {
          count,
          project
        }),
      batchDeleteProjectSessionsConfirm: t(
        "agentHost.agentGui.batchDeleteProjectSessionsConfirm"
      ),
      conversationsSectionMoreActions: t(
        "agentHost.agentGui.conversationsSectionMoreActions"
      ),
      batchDeleteConversations: t(
        "agentHost.agentGui.batchDeleteConversations"
      ),
      batchDeleteConversationsTitle: t(
        "agentHost.agentGui.batchDeleteConversationsTitle"
      ),
      batchDeleteConversationsBody: (count: number) =>
        t("agentHost.agentGui.batchDeleteConversationsBody", {
          count
        }),
      batchDeleteConversationsConfirm: t(
        "agentHost.agentGui.batchDeleteConversationsConfirm"
      ),
      approvalRequired: t(
        "agentHost.agentGui.approvalRequired",
        providerInterpolation
      ),
      fileChangeApprovalRequired: t(
        "agentHost.agentGui.fileChangeApprovalRequired",
        providerInterpolation
      ),
      approvalUnavailable: t("agentHost.agentGui.approvalUnavailable"),
      authRequired: t("agentHost.agentGui.authRequired"),
      authLogin: t("agentHost.agentGui.authLogin"),
      activatingSession: t("agentHost.agentGui.activatingSession"),
      cancellingSession: t("agentHost.agentGui.cancellingSession"),
      retryActivation: t("agentHost.agentGui.retryActivation"),
      continueInNewConversation: t(
        "agentHost.agentGui.continueInNewConversation"
      ),
      processing: t("agentHost.agentGui.processing"),
      turnSummary: t("agentHost.agentGui.turnSummary"),
      userMessageLocator: t("agentHost.agentGui.userMessageLocator"),
      planLead: t("agentHost.agentGui.planLead"),
      planModes: [
        {
          id: "acceptEdits",
          label: t("agentHost.agentGui.planModes.acceptEdits.label"),
          description: t("agentHost.agentGui.planModes.acceptEdits.description")
        },
        {
          id: "default",
          label: t("agentHost.agentGui.planModes.askFirst.label"),
          description: t("agentHost.agentGui.planModes.askFirst.description")
        },
        {
          id: "bypassPermissions",
          label: t("agentHost.agentGui.planModes.allowAll.label"),
          description: t("agentHost.agentGui.planModes.allowAll.description")
        },
        {
          id: "auto",
          label: t("agentHost.agentGui.planModes.auto.label"),
          description: t("agentHost.agentGui.planModes.auto.description")
        }
      ],
      stayInPlan: t("agentHost.agentGui.stayInPlan"),
      sendFeedback: t("agentHost.agentGui.sendFeedback"),
      feedbackPlaceholder: t("agentHost.agentGui.feedbackPlaceholder"),
      previousQuestion: t("agentHost.agentGui.previousQuestion"),
      nextQuestion: t("agentHost.agentGui.nextQuestion"),
      submitAnswers: t("agentHost.agentGui.submitAnswers"),
      answerPlaceholder: t("agentHost.agentGui.answerPlaceholder"),
      waitingForAnswer: t("agentHost.agentGui.waitingForAnswer"),
      returnToConversation: t("agentHost.agentGui.returnToConversation"),
      continueAnswering: t("agentHost.agentGui.continueAnswering"),
      thinkingLabel: t("agentHost.workspaceAgentSessionDetailThinking"),
      toolCallsLabel: (count: number) =>
        t("agentHost.workspaceAgentSessionDetailToolCalls", { count }),
      openConversationWindow: t("agentHost.agentGui.openConversationWindow"),
      showMoreConversations: t("agentHost.agentGui.showMoreConversations"),
      showLessConversations: t("agentHost.agentGui.showLessConversations"),
      deleteSession: t("agentHost.agentGui.deleteSession"),
      pinSession: t("agentHost.agentGui.pinSession"),
      moreSessionActions: t("agentHost.agentGui.moreSessionActions"),
      copyAsMarkdown: t("agentHost.agentGui.copyAsMarkdown"),
      copyAsReference: t("agentHost.agentGui.copyAsReference"),
      markSessionUnread: t("agentHost.agentGui.markSessionUnread"),
      conversationCopyImage: t("agentHost.agentGui.conversationCopyImage"),
      conversationCopyMentionPrefix: t(
        "agentHost.agentGui.conversationCopyMentionPrefix"
      ),
      conversationCopyFile: t("agentHost.agentGui.conversationCopyFile"),
      // Template label; the serializer substitutes {{count}} itself, and the
      // i18n runtime keeps the placeholder intact when t() gets no params.
      conversationCopyPreviousMessages: t(
        "agentHost.agentGui.conversationCopyPreviousMessages"
      ),
      // Template label; the copy hook substitutes {{count}} itself.
      conversationCopyImagesOmitted: t(
        "agentHost.agentGui.conversationCopyImagesOmitted"
      ),
      copiedToClipboard: t("agentHost.agentGui.messageCopied"),
      copyFailed: t("common.copyFailed"),
      sessionActionUnavailable: t(
        "agentHost.agentGui.sessionActionUnavailable"
      ),
      renameSession: t("agentHost.agentGui.renameSession"),
      renameSessionTitle: t("agentHost.agentGui.renameSessionTitle"),
      renameSessionDescription: t(
        "agentHost.agentGui.renameSessionDescription"
      ),
      renameSessionPlaceholder: t(
        "agentHost.agentGui.renameSessionPlaceholder"
      ),
      renameSessionSave: t("agentHost.agentGui.renameSessionSave"),
      unpinSession: t("agentHost.agentGui.unpinSession"),
      deleteSessionTitle: t("agentHost.agentGui.deleteSessionTitle"),
      deleteSessionBody: t("agentHost.agentGui.deleteSessionBody"),
      deleteSessionConfirm: t("agentHost.agentGui.deleteSessionConfirm"),
      relativeTimeJustNow: t("agentHost.agentGui.relativeTimeJustNow"),
      relativeTimeMinutes: (count: number) =>
        t("agentHost.agentGui.relativeTimeMinutes", { count }),
      relativeTimeHours: (count: number) =>
        t("agentHost.agentGui.relativeTimeHours", { count }),
      relativeTimeDays: (count: number) =>
        t("agentHost.agentGui.relativeTimeDays", { count }),
      relativeTimeMonths: (count: number) =>
        t("agentHost.agentGui.relativeTimeMonths", { count }),
      relativeTimeYears: (count: number) =>
        t("agentHost.agentGui.relativeTimeYears", { count }),
      syncPending: t("agentHost.agentGui.syncPending"),
      syncSynced: t("agentHost.agentGui.syncSynced"),
      syncFailed: t("agentHost.agentGui.syncFailed"),
      ...agentGUIProjectLaunchLabels(t),
      promptTipsPrefix: t("agentHost.agentGui.promptTipsPrefix"),
      reviewPicker: {
        title: t("agentHost.agentGui.reviewPicker.title"),
        targetLabel: t("agentHost.agentGui.reviewPicker.targetLabel"),
        searchPlaceholder: t(
          "agentHost.agentGui.reviewPicker.searchPlaceholder"
        ),
        noResults: t("agentHost.agentGui.reviewPicker.noResults"),
        uncommitted: t("agentHost.agentGui.reviewPicker.uncommitted"),
        baseBranch: t("agentHost.agentGui.reviewPicker.baseBranch"),
        commit: t("agentHost.agentGui.reviewPicker.commit"),
        custom: t("agentHost.agentGui.reviewPicker.custom"),
        branchLabel: t("agentHost.agentGui.reviewPicker.branchLabel"),
        branchPlaceholder: t(
          "agentHost.agentGui.reviewPicker.branchPlaceholder"
        ),
        branchLoading: t("agentHost.agentGui.reviewPicker.branchLoading"),
        branchEmpty: t("agentHost.agentGui.reviewPicker.branchEmpty"),
        commitPlaceholder: t(
          "agentHost.agentGui.reviewPicker.commitPlaceholder"
        ),
        customPlaceholder: t(
          "agentHost.agentGui.reviewPicker.customPlaceholder"
        ),
        submit: t("agentHost.agentGui.reviewPicker.submit"),
        cancel: t("agentHost.agentGui.reviewPicker.cancel")
      },
      quickPrompts: agentQuickPromptLabels(t),
      promptTips: [
        {
          id: "set-workspace",
          label: t("agentHost.agentGui.promptTips.setWorkspace.label"),
          prompt: t("agentHost.agentGui.promptTips.setWorkspace.prompt")
        },
        {
          id: "use-issue",
          label: t("agentHost.agentGui.promptTips.useIssue.label"),
          prompt: t("agentHost.agentGui.promptTips.useIssue.prompt")
        },
        {
          id: "map-current-state",
          label: t("agentHost.agentGui.promptTips.mapCurrentState.label"),
          prompt: t("agentHost.agentGui.promptTips.mapCurrentState.prompt")
        },
        {
          id: "continue-recent-session",
          label: t("agentHost.agentGui.promptTips.continueRecentSession.label"),
          prompt: t(
            "agentHost.agentGui.promptTips.continueRecentSession.prompt"
          )
        },
        {
          id: "reference-other-agents",
          label: t("agentHost.agentGui.promptTips.referenceOtherAgents.label"),
          prompt: t("agentHost.agentGui.promptTips.referenceOtherAgents.prompt")
        },
        {
          id: "control-permissions",
          label: t("agentHost.agentGui.promptTips.controlPermissions.label"),
          prompt: t("agentHost.agentGui.promptTips.controlPermissions.prompt")
        }
      ],
      cancel: t("common.cancel"),
      ...agentSlashPaletteLabels(t),
      slashCommandCompactLabel: t(
        "agentHost.agentGui.slashCommandCompactLabel"
      ),
      slashCommandContextLabel: t(
        "agentHost.agentGui.slashCommandContextLabel"
      ),
      slashCommandFastLabel: t("agentHost.agentGui.slashCommandFastLabel"),
      slashCommandGoalLabel: t("agentHost.agentGui.slashCommandGoalLabel"),
      slashCommandInitLabel: t("agentHost.agentGui.slashCommandInitLabel"),
      slashCommandPlanLabel: t("agentHost.agentGui.slashCommandPlanLabel"),
      slashCommandReviewLabel: t("agentHost.agentGui.slashCommandReviewLabel"),
      slashCommandStatusLabel: t("agentHost.agentGui.slashCommandStatusLabel"),
      slashCommandUsageLabel: t("agentHost.agentGui.slashCommandUsageLabel"),
      slashCommandCompactDescription: t(
        "agentHost.agentGui.slashCommandCompactDescription"
      ),
      slashCommandContextDescription: t(
        "agentHost.agentGui.slashCommandContextDescription"
      ),
      slashCommandFastDescription: t(
        "agentHost.agentGui.slashCommandFastDescription"
      ),
      slashCommandGoalDescription: t(
        "agentHost.agentGui.slashCommandGoalDescription"
      ),
      slashCommandInitDescription: t(
        "agentHost.agentGui.slashCommandInitDescription"
      ),
      slashCommandPlanDescription: t(
        "agentHost.agentGui.slashCommandPlanDescription"
      ),
      slashCommandReviewDescription: t(
        "agentHost.agentGui.slashCommandReviewDescription"
      ),
      slashCommandStatusDescription: t(
        "agentHost.agentGui.slashCommandStatusDescription"
      ),
      slashCommandUsageDescription: t(
        "agentHost.agentGui.slashCommandUsageDescription"
      ),
      browserUseCapabilityLabel: t(
        "agentHost.agentGui.browserUseCapabilityLabel"
      ),
      browserUseCapabilityDescription: t(
        "agentHost.agentGui.browserUseCapabilityDescription"
      ),
      browserUseCapabilityDescriptionAutoConnect: t(
        "agentHost.agentGui.browserUseCapabilityDescriptionAutoConnect"
      ),
      browserUseCapabilityDescriptionIsolated: t(
        "agentHost.agentGui.browserUseCapabilityDescriptionIsolated"
      ),
      browserUseCapabilitySettingsLabel: t(
        "agentHost.agentGui.browserUseCapabilitySettingsLabel"
      ),
      browserUseCapabilitySettingsDescription: t(
        "agentHost.agentGui.browserUseCapabilitySettingsDescription"
      ),
      capabilityInlineSettingsLabel: t(
        "agentHost.agentGui.capabilityInlineSettingsLabel"
      ),
      computerUseCapabilityLabel: t(
        "agentHost.agentGui.computerUseCapabilityLabel"
      ),
      computerUseCapabilityDescription: t(
        "agentHost.agentGui.computerUseCapabilityDescription"
      ),
      computerUseCapabilitySetupRequiredDescription: t(
        "agentHost.agentGui.computerUseCapabilitySetupRequiredDescription"
      ),
      computerUseCapabilityAuthorizationRequiredDescription: t(
        "agentHost.agentGui.computerUseCapabilityAuthorizationRequiredDescription"
      ),
      computerUseCapabilityAuthorizationUnknownDescription: t(
        "agentHost.agentGui.computerUseCapabilityAuthorizationUnknownDescription"
      ),
      computerUseCapabilitySettingsLabel: t(
        "agentHost.agentGui.computerUseCapabilitySettingsLabel"
      ),
      computerUseCapabilitySettingsDescription: t(
        "agentHost.agentGui.computerUseCapabilitySettingsDescription"
      ),
      fileMentionPalette: t("agentHost.agentGui.fileMentionPalette"),
      fileMentionLoading: t("agentHost.agentGui.fileMentionLoading"),
      fileMentionEmpty: t("agentHost.agentGui.fileMentionEmpty"),
      fileMentionError: t("agentHost.agentGui.fileMentionError"),
      fileMentionTabHint: t("agentHost.agentGui.fileMentionTabHint"),
      fileDropHint: t("agentHost.agentGui.fileDropHint"),
      mentionPalette: t("agentHost.agentGui.mentionPalette"),
      removeMention: t("common.remove"),
      addReference: t("agentHost.agentGui.addReference"),
      addContent: t("agentHost.agentGui.addContent"),
      addContentResourcePanel: t("agentHost.agentGui.addContentResourcePanel"),
      ...agentGUIConnectorLabels(t),
      referenceWorkspaceFiles: t("agentHost.issue.referenceWorkspaceFiles"),
      handoffConversation: t("agentHost.agentGui.handoffConversation"),
      handoffConversationTooltip: t(
        "agentHost.agentGui.handoffConversationTooltip"
      ),
      handoffConversationMenu: t("agentHost.agentGui.handoffConversationMenu"),
      handoffTargetDeviceSource: (deviceLabel: string) =>
        t("agentHost.agentGui.handoffTargetDeviceSource", {
          device: deviceLabel
        }),
      handoffTargetSelf: t("agentHost.agentGui.handoffTargetSelf"),
      handoffTargetShared: t("agentHost.agentGui.handoffTargetShared")
    };
  }, [
    displayProviderLabel,
    disabledHomeSuggestions,
    fallbackAgentTitle,
    t,
    workspaceId,
    workspaceAppIcons
  ]);
}
export function useAgentGUIWorkspaceFileReferenceCopy(
  t: TranslateFn
): WorkspaceFileReferenceCopy {
  return useMemo<WorkspaceFileReferenceCopy>(
    () => ({
      t(key, values) {
        const localeKey = workspaceFileReferenceLocaleKeyByPickerKey[key];
        return localeKey ? t(localeKey, values) : key;
      }
    }),
    [t]
  );
}
