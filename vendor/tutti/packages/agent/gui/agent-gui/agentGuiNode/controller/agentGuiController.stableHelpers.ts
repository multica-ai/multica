import type { AgentActivityComposerOptions } from "@tutti-os/agent-activity-core";
import { useCallback, useRef } from "react";
import { AGENT_PROVIDER_LABEL } from "../../../contexts/settings/domain/agentSettings";
import type {
  AgentSessionComposerSettings,
  AgentSessionPermissionConfig,
  AgentSessionPermissionModeOption
} from "../../../shared/agentSessionTypes";
import type { AgentProviderId } from "../../../shared/contracts/dto";
import type { WorkspaceAgentActivityCard } from "../../../shared/workspaceAgentActivityListViewModel";
import { isWorkspaceAgentUntitledConversation } from "../../../shared/workspaceAgentLatestActivitySummary";
import type { WorkspaceAgentSessionDetailViewModel } from "../../../shared/workspaceAgentSessionDetailViewModel";
import { type AgentGUIConversationSummary } from "../model/agentGuiConversationModel";
import type {
  AgentGUIComposerSettingOption,
  AgentGUIComposerSettingsVM,
  AgentGUIProviderSkillOption
} from "../model/agentGuiNodeTypes";
import {
  areProviderSkillOptionListsEqual,
  normalizeConfigOptionValue,
  sameComposerSettings,
  slashCommandPoliciesEqual as sameSlashPolicy
} from "./agentGuiController.composerHelpers";
import { hasPromptConversationTitle } from "./agentGuiController.conversationHelpers";
export {
  normalizePermissionModeSemantic,
  permissionConfigFromComposerOptions,
  permissionModeDescription,
  permissionModeLabel,
  permissionModeOptions
} from "./agentGuiController.composerHelpers";
export {
  agentGUIConversationDiagnosticDetails,
  agentGUIRuntimeSessionDiagnosticDetails,
  agentGUISessionStateDiagnosticDetails,
  agentGUIToolCallStatusIsWaiting,
  promptRequestId
} from "./agentGuiController.diagnostics";
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
export {
  messageFromMessageUpdate,
  normalizeTimelineStatus,
  normalizedPositiveNumber,
  timelineItemTime
} from "./agentGuiController.sessionHelpers";
export {
  filterMessagesForDetailWindowOverlay,
  maxFiniteMessageVersion,
  minFiniteMessageVersion,
  sessionHasRenderableMessages,
  windowHasTurnMissingUserPrompt
} from "./useAgentConversationMessagePaging";
export function stableConversationSummaryList(
  previous: readonly AgentGUIConversationSummary[] | null,
  next: AgentGUIConversationSummary[]
): AgentGUIConversationSummary[] {
  if (previous?.length !== next.length) {
    const previousById = new Map(
      (previous ?? []).map((conversation) => [conversation.id, conversation])
    );
    return next.map((conversation) => {
      const previousConversation = previousById.get(conversation.id);
      return previousConversation &&
        conversationSummariesRenderEqual(previousConversation, conversation)
        ? previousConversation
        : conversation;
    });
  }
  let hasRenderChange = false;
  const stable = next.map((conversation, index) => {
    const previousConversation = previous[index];
    if (
      previousConversation &&
      conversationSummariesRenderEqual(previousConversation, conversation)
    ) {
      return previousConversation;
    }
    hasRenderChange = true;
    return conversation;
  });
  return hasRenderChange ? stable : (previous as AgentGUIConversationSummary[]);
}

export function useStableConversationDetail(
  detail: WorkspaceAgentSessionDetailViewModel | null
): WorkspaceAgentSessionDetailViewModel | null {
  const detailRef = useRef<WorkspaceAgentSessionDetailViewModel | null>(null);
  detailRef.current = stabilizeConversationDetail(detailRef.current, detail);
  return detailRef.current;
}

export function stabilizeConversationDetail(
  previous: WorkspaceAgentSessionDetailViewModel | null,
  next: WorkspaceAgentSessionDetailViewModel | null
): WorkspaceAgentSessionDetailViewModel | null {
  if (!previous || !next) {
    return next;
  }
  const session = conversationDetailSessionsEqual(
    previous.session,
    next.session
  )
    ? previous.session
    : next.session;
  const activity = stabilizeConversationDetailActivity(
    previous.activity,
    next.activity
  );
  if (
    previous.cwd === next.cwd &&
    previous.workspaceRoot === next.workspaceRoot &&
    previous.showProcessingIndicator === next.showProcessingIndicator &&
    previous.turns === next.turns &&
    previous.sessionTurns === next.sessionTurns &&
    previous.session === session &&
    previous.activity === activity
  ) {
    return previous;
  }
  return {
    ...next,
    activity,
    session
  };
}

export function stabilizeConversationDetailActivity(
  previous: WorkspaceAgentActivityCard,
  next: WorkspaceAgentActivityCard
): WorkspaceAgentActivityCard {
  const changedFiles = conversationDetailChangedFilesEqual(
    previous.changedFiles,
    next.changedFiles
  )
    ? previous.changedFiles
    : next.changedFiles;
  if (
    previous.id === next.id &&
    previous.sessionId === next.sessionId &&
    previous.userId === next.userId &&
    previous.userName === next.userName &&
    previous.userAvatarUrl === next.userAvatarUrl &&
    previous.agentProvider === next.agentProvider &&
    previous.agentName === next.agentName &&
    previous.title === next.title &&
    previous.status === next.status &&
    previous.latestActivitySummary === next.latestActivitySummary &&
    previous.conversationPreview === next.conversationPreview &&
    previous.latestActivityActorName === next.latestActivityActorName &&
    previous.toolCalls === next.toolCalls &&
    previous.changedFiles === changedFiles &&
    previous.sortTimeUnixMs === next.sortTimeUnixMs &&
    previous.readTimeUnixMs === next.readTimeUnixMs
  ) {
    return previous;
  }
  return {
    ...next,
    changedFiles
  };
}

export function conversationDetailChangedFilesEqual(
  left: WorkspaceAgentActivityCard["changedFiles"],
  right: WorkspaceAgentActivityCard["changedFiles"]
): boolean {
  return (
    left.length === right.length &&
    left.every(
      (file, index) =>
        file.path === right[index]?.path && file.label === right[index]?.label
    )
  );
}

export function conversationDetailSessionsEqual(
  left: WorkspaceAgentSessionDetailViewModel["session"],
  right: WorkspaceAgentSessionDetailViewModel["session"]
): boolean {
  return (
    left.workspaceId === right.workspaceId &&
    left.agentSessionId === right.agentSessionId &&
    left.userId === right.userId &&
    left.kind === right.kind &&
    left.agentTargetId === right.agentTargetId &&
    left.provider === right.provider &&
    left.providerSessionId === right.providerSessionId &&
    left.model === right.model &&
    left.resumable === right.resumable &&
    left.endedAtUnixMs === right.endedAtUnixMs &&
    left.title === right.title &&
    left.pinnedAtUnixMs === right.pinnedAtUnixMs &&
    left.createdAtUnixMs === right.createdAtUnixMs &&
    left.updatedAtUnixMs === right.updatedAtUnixMs &&
    left.cwd === right.cwd &&
    left.isolation?.mode === right.isolation?.mode &&
    left.activeTurnId === right.activeTurnId &&
    left.activeTurn?.updatedAtUnixMs === right.activeTurn?.updatedAtUnixMs &&
    left.latestTurn?.updatedAtUnixMs === right.latestTurn?.updatedAtUnixMs &&
    left.lifecycleCapabilities.fork === right.lifecycleCapabilities.fork &&
    left.lifecycleCapabilities.forkThroughTurn ===
      right.lifecycleCapabilities.forkThroughTurn &&
    left.forkedFrom?.sourceAgentSessionId ===
      right.forkedFrom?.sourceAgentSessionId &&
    left.forkedFrom?.sourceTurnId === right.forkedFrom?.sourceTurnId &&
    left.forkedFrom?.targetTurnId === right.forkedFrom?.targetTurnId &&
    left.forkedFrom?.operationId === right.forkedFrom?.operationId &&
    left.forkedFrom?.forkedAtUnixMs === right.forkedFrom?.forkedAtUnixMs &&
    left.pendingInteractions === right.pendingInteractions &&
    left.latestTurnInteractions === right.latestTurnInteractions
  );
}

export function conversationSummariesRenderEqual(
  left: AgentGUIConversationSummary,
  right: AgentGUIConversationSummary
): boolean {
  return (
    left.id === right.id &&
    left.userId === right.userId &&
    left.provider === right.provider &&
    left.title === right.title &&
    left.titleLeadingMentionKind === right.titleLeadingMentionKind &&
    conversationTitleFallbacksRenderEqual(
      left.titleFallback,
      right.titleFallback
    ) &&
    left.status === right.status &&
    left.cwd === right.cwd &&
    left.railSectionKey === right.railSectionKey &&
    left.pinnedAtUnixMs === right.pinnedAtUnixMs &&
    left.sortTimeUnixMs === right.sortTimeUnixMs &&
    left.updatedAtUnixMs === right.updatedAtUnixMs &&
    left.isTransient === right.isTransient &&
    left.projectionSource === right.projectionSource &&
    left.isImported === right.isImported &&
    left.hasUnreadCompletion === right.hasUnreadCompletion &&
    left.unreadCompletionKey === right.unreadCompletionKey &&
    left.needsUserAction === right.needsUserAction &&
    conversationProjectsRenderEqual(left.project, right.project)
  );
}

export function conversationTitleFallbacksRenderEqual(
  left: AgentGUIConversationSummary["titleFallback"],
  right: AgentGUIConversationSummary["titleFallback"]
): boolean {
  return (
    left === right ||
    JSON.stringify(left ?? null) === JSON.stringify(right ?? null)
  );
}

export function conversationProjectsRenderEqual(
  left: AgentGUIConversationSummary["project"],
  right: AgentGUIConversationSummary["project"]
): boolean {
  return (
    left === right ||
    (!left || !right
      ? !left && !right
      : left.id === right.id &&
        left.path === right.path &&
        left.sectionKey === right.sectionKey &&
        left.label === right.label &&
        left.createdAtUnixMs === right.createdAtUnixMs &&
        left.updatedAtUnixMs === right.updatedAtUnixMs &&
        left.lastUsedAtUnixMs === right.lastUsedAtUnixMs)
  );
}

export function mergeConversationTitleUpdateFields(
  current: AgentGUIConversationSummary,
  incomingTitle: string,
  provider?: AgentProviderId
): Pick<AgentGUIConversationSummary, "title" | "titleFallback"> {
  const title = incomingTitle.trim();
  if (!title) {
    return {
      title: current.title,
      titleFallback: current.titleFallback
    };
  }
  const currentHasPromptTitle = hasPromptConversationTitle(current);
  if (currentHasPromptTitle) {
    return {
      title: current.title,
      titleFallback: current.titleFallback
    };
  }

  if (
    provider &&
    shouldPreserveExistingConversationTitle(current, title, provider)
  ) {
    return {
      title: current.title,
      titleFallback: current.titleFallback
    };
  }
  return {
    title,
    titleFallback: null
  };
}

export function shouldPreserveExistingConversationTitle(
  current: AgentGUIConversationSummary,
  incomingTitle: string,
  provider: AgentProviderId
): boolean {
  const normalizedIncoming = incomingTitle.trim();
  if (!normalizedIncoming || !current.title.trim()) {
    return false;
  }
  if (isWorkspaceAgentUntitledConversation(normalizedIncoming)) {
    return true;
  }
  const providerLabel =
    AGENT_PROVIDER_LABEL[provider as keyof typeof AGENT_PROVIDER_LABEL] ??
    provider;
  return (
    normalizedIncoming === provider || normalizedIncoming === providerLabel
  );
}

export function draftAgentSessionIdFromComposerOptions(
  options: AgentActivityComposerOptions | null | undefined
): string | null {
  return normalizeConfigOptionValue(options?.draftAgentSessionId);
}

export function areComposerSettingOptionsEqual(
  left: AgentGUIComposerSettingOption,
  right: AgentGUIComposerSettingOption
): boolean {
  return (
    left.value === right.value &&
    left.label === right.label &&
    left.description === right.description
  );
}

export function areComposerSettingOptionListsEqual(
  left: readonly AgentGUIComposerSettingOption[] | null | undefined,
  right: readonly AgentGUIComposerSettingOption[] | null | undefined
): boolean {
  const leftOptions = left ?? [];
  const rightOptions = right ?? [];
  return (
    leftOptions.length === rightOptions.length &&
    leftOptions.every((option, index) =>
      areComposerSettingOptionsEqual(option, rightOptions[index]!)
    )
  );
}

function areComposerModelChoiceHistoriesEqual(
  left: AgentGUIComposerSettingsVM["modelChoiceHistory"],
  right: AgentGUIComposerSettingsVM["modelChoiceHistory"]
): boolean {
  if (!left || !right) {
    return left === right;
  }
  if (left.targetId !== right.targetId) {
    return false;
  }
  const leftCatalog = left.catalog;
  const rightCatalog = right.catalog;
  if (!leftCatalog || !rightCatalog) {
    return leftCatalog === rightCatalog;
  }
  return (
    leftCatalog.authoritative === rightCatalog.authoritative &&
    leftCatalog.loading === rightCatalog.loading &&
    leftCatalog.effectiveModel === rightCatalog.effectiveModel &&
    leftCatalog.models.length === rightCatalog.models.length &&
    leftCatalog.models.every(
      (model, index) =>
        model.value === rightCatalog.models[index]!.value &&
        Boolean(model.requested) ===
          Boolean(rightCatalog.models[index]!.requested)
    )
  );
}

export function useStableComposerSettings(
  settings: AgentSessionComposerSettings
): AgentSessionComposerSettings;
export function useStableComposerSettings(
  settings: AgentSessionComposerSettings | null
): AgentSessionComposerSettings | null;
export function useStableComposerSettings(
  settings: AgentSessionComposerSettings | null
): AgentSessionComposerSettings | null {
  const settingsRef = useRef<{
    value: AgentSessionComposerSettings | null;
  } | null>(null);
  if (
    settingsRef.current === null ||
    !sameComposerSettings(settingsRef.current.value, settings)
  ) {
    settingsRef.current = { value: settings };
  }
  return settingsRef.current.value;
}

export function useStableProviderSkillOptions(
  skills: AgentGUIProviderSkillOption[]
): AgentGUIProviderSkillOption[] {
  const skillsRef = useRef<AgentGUIProviderSkillOption[] | null>(null);
  if (
    skillsRef.current === null ||
    !areProviderSkillOptionListsEqual(skillsRef.current, skills)
  ) {
    skillsRef.current = skills;
  }
  return skillsRef.current;
}

export function areComposerSettingsDraftsEqual(
  left: AgentGUIComposerSettingsVM["draftSettings"],
  right: AgentGUIComposerSettingsVM["draftSettings"]
): boolean {
  return (
    left.codexSaverMode === right.codexSaverMode &&
    left.rtkSaverMode === right.rtkSaverMode &&
    left.model === right.model &&
    left.reasoningEffort === right.reasoningEffort &&
    left.speed === right.speed &&
    left.planMode === right.planMode &&
    (left.browserUse ?? true) === (right.browserUse ?? true) &&
    (left.computerUse ?? true) === (right.computerUse ?? true) &&
    (left.permissionModeId ?? null) === (right.permissionModeId ?? null)
  );
}

export function arePermissionModeOptionsEqual(
  left: AgentSessionPermissionModeOption,
  right: AgentSessionPermissionModeOption
): boolean {
  return (
    left.id === right.id &&
    left.label === right.label &&
    left.description === right.description &&
    left.semantic === right.semantic
  );
}

export function arePermissionConfigsEqual(
  left: AgentSessionPermissionConfig | null | undefined,
  right: AgentSessionPermissionConfig | null | undefined
): boolean {
  if (!left || !right) {
    return left === right;
  }
  return (
    left.configurable === right.configurable &&
    (left.defaultValue ?? null) === (right.defaultValue ?? null) &&
    left.modes.length === right.modes.length &&
    left.modes.every((mode, index) =>
      arePermissionModeOptionsEqual(mode, right.modes[index]!)
    )
  );
}

export function areComposerSettingsVMsEqual(
  left: AgentGUIComposerSettingsVM,
  right: AgentGUIComposerSettingsVM
): boolean {
  return (
    sameComposerSettings(left.sessionSettings, right.sessionSettings) &&
    areComposerSettingsDraftsEqual(left.draftSettings, right.draftSettings) &&
    left.supportsModel === right.supportsModel &&
    (left.supportsCodexSaverMode ?? false) ===
      (right.supportsCodexSaverMode ?? false) &&
    left.supportsReasoningEffort === right.supportsReasoningEffort &&
    left.supportsSpeed === right.supportsSpeed &&
    (left.supportsPermissionMode ?? false) ===
      (right.supportsPermissionMode ?? false) &&
    left.supportsPlanMode === right.supportsPlanMode &&
    (left.supportsBrowser ?? false) === (right.supportsBrowser ?? false) &&
    (left.supportsComputerUse ?? false) ===
      (right.supportsComputerUse ?? false) &&
    (left.permissionModeChangeDuringTurn ?? false) ===
      (right.permissionModeChangeDuringTurn ?? false) &&
    sameSlashPolicy(left.slashCommandPolicy, right.slashCommandPolicy) &&
    left.isSettingsLoading === right.isSettingsLoading &&
    Boolean(left.composerOptionsError) ===
      Boolean(right.composerOptionsError) &&
    (left.composerOptionsLoadStatus ?? null) ===
      (right.composerOptionsLoadStatus ?? null) &&
    !!left.isCapabilityOptionsLoading === !!right.isCapabilityOptionsLoading &&
    !!left.isConnectorOptionsLoading === !!right.isConnectorOptionsLoading &&
    !!left.isModelOptionsLoading === !!right.isModelOptionsLoading &&
    areComposerModelChoiceHistoriesEqual(
      left.modelChoiceHistory,
      right.modelChoiceHistory
    ) &&
    left.modelUnavailable === right.modelUnavailable &&
    left.reasoningUnavailable === right.reasoningUnavailable &&
    left.speedUnavailable === right.speedUnavailable &&
    (left.permissionModeUnavailable ?? false) ===
      (right.permissionModeUnavailable ?? false) &&
    (left.planExclusiveWithPermissionMode ?? false) ===
      (right.planExclusiveWithPermissionMode ?? false) &&
    (left.selectedModelValue ?? null) === (right.selectedModelValue ?? null) &&
    (left.effectiveModelValue ?? null) ===
      (right.effectiveModelValue ?? null) &&
    (left.selectedReasoningEffortValue ?? null) ===
      (right.selectedReasoningEffortValue ?? null) &&
    (left.selectedSpeedValue ?? null) === (right.selectedSpeedValue ?? null) &&
    (left.selectedPermissionModeValue ?? null) ===
      (right.selectedPermissionModeValue ?? null) &&
    arePermissionConfigsEqual(left.permissionConfig, right.permissionConfig) &&
    (left.selectedProjectPath ?? null) ===
      (right.selectedProjectPath ?? null) &&
    Boolean(left.shouldApplyPreparedProjectSelection) ===
      Boolean(right.shouldApplyPreparedProjectSelection) &&
    Boolean(left.projectLocked) === Boolean(right.projectLocked) &&
    Boolean(left.projectPathIsRemote) === Boolean(right.projectPathIsRemote) &&
    Boolean(left.collapseModelOptionsToLatest) ===
      Boolean(right.collapseModelOptionsToLatest) &&
    (left.modelPlan?.id ?? null) === (right.modelPlan?.id ?? null) &&
    (left.modelPlan?.name ?? null) === (right.modelPlan?.name ?? null) &&
    (left.modelPlan?.protocol ?? null) ===
      (right.modelPlan?.protocol ?? null) &&
    Boolean(left.modelSwitchTakesEffectNextTurn) ===
      Boolean(right.modelSwitchTakesEffectNextTurn) &&
    areComposerSettingOptionListsEqual(
      left.availableModels,
      right.availableModels
    ) &&
    areComposerSettingOptionListsEqual(
      left.availableReasoningEfforts,
      right.availableReasoningEfforts
    ) &&
    areComposerSettingOptionListsEqual(
      left.availableSpeeds,
      right.availableSpeeds
    ) &&
    areComposerSettingOptionListsEqual(
      left.availablePermissionModes,
      right.availablePermissionModes
    )
  );
}

export function useStableComposerSettingsVM(
  settings: AgentGUIComposerSettingsVM
): AgentGUIComposerSettingsVM {
  const settingsRef = useRef<AgentGUIComposerSettingsVM | null>(null);
  settingsRef.current = stabilizeComposerSettingsVM(
    settingsRef.current,
    settings
  );
  return settingsRef.current;
}

export function stabilizeComposerSettingsVM(
  previous: AgentGUIComposerSettingsVM | null,
  next: AgentGUIComposerSettingsVM
): AgentGUIComposerSettingsVM {
  if (!previous) {
    return next;
  }
  if (areComposerSettingsVMsEqual(previous, next)) {
    return previous;
  }

  const sessionSettings = sameComposerSettings(
    previous.sessionSettings,
    next.sessionSettings
  )
    ? previous.sessionSettings
    : next.sessionSettings;
  const draftSettings = areComposerSettingsDraftsEqual(
    previous.draftSettings,
    next.draftSettings
  )
    ? previous.draftSettings
    : next.draftSettings;
  const permissionConfig = arePermissionConfigsEqual(
    previous.permissionConfig,
    next.permissionConfig
  )
    ? previous.permissionConfig
    : next.permissionConfig;
  const modelChoiceHistory = areComposerModelChoiceHistoriesEqual(
    previous.modelChoiceHistory,
    next.modelChoiceHistory
  )
    ? previous.modelChoiceHistory
    : next.modelChoiceHistory;
  const availableModels = areComposerSettingOptionListsEqual(
    previous.availableModels,
    next.availableModels
  )
    ? previous.availableModels
    : next.availableModels;
  const availableReasoningEfforts = areComposerSettingOptionListsEqual(
    previous.availableReasoningEfforts,
    next.availableReasoningEfforts
  )
    ? previous.availableReasoningEfforts
    : next.availableReasoningEfforts;
  const availableSpeeds = areComposerSettingOptionListsEqual(
    previous.availableSpeeds,
    next.availableSpeeds
  )
    ? previous.availableSpeeds
    : next.availableSpeeds;
  const availablePermissionModes = areComposerSettingOptionListsEqual(
    previous.availablePermissionModes ?? [],
    next.availablePermissionModes ?? []
  )
    ? previous.availablePermissionModes
    : next.availablePermissionModes;

  return {
    ...next,
    sessionSettings,
    draftSettings,
    modelChoiceHistory,
    permissionConfig,
    availableModels,
    availableReasoningEfforts,
    availableSpeeds,
    availablePermissionModes
  };
}

export function useStableControllerEventCallback<
  Args extends unknown[],
  Result
>(callback: (...args: Args) => Result): (...args: Args) => Result {
  const callbackRef = useRef(callback);
  callbackRef.current = callback;
  return useCallback((...args: Args) => callbackRef.current(...args), []);
}

export function stringArraysEqual(
  first: string[] | null | undefined,
  second: string[] | null | undefined
): boolean {
  if (!first || !second) {
    return first === second;
  }
  return (
    first.length === second.length &&
    first.every((value, index) => value === second[index])
  );
}

export function useStableStringArrayByValue(values: string[]): string[] {
  const valuesRef = useRef<string[] | null>(null);
  const currentValues = valuesRef.current;
  if (!stringArraysEqual(currentValues, values)) {
    valuesRef.current = values;
    return values;
  }
  return currentValues ?? values;
}
