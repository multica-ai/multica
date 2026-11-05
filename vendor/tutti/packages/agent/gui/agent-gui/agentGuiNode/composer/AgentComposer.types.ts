import type { AgentActivitySubmitSettingsPatch } from "@tutti-os/agent-activity-core";
import type {
  ReferenceProvenanceFilterController,
  ReferenceProvenanceFilterSnapshot
} from "@tutti-os/workspace-file-reference/react";
import type { WorkspaceUserProjectApi } from "@tutti-os/workspace-user-project/contracts";
import type { WorkspaceUserProjectI18nRuntime } from "@tutti-os/workspace-user-project/i18n";
import type { ReactNode } from "react";
import type { WorkspaceLinkAction } from "../../../actions/workspaceLinkActions";
import type { UiLanguage } from "../../../contexts/settings/domain/agentSettings";
import type {
  AgentConversationPromptVM,
  AgentInteractionResponseInput
} from "../../../shared/agentConversation/contracts/agentConversationVM";
import type { AgentMessageMarkdownWorkspaceAppIcon } from "../../../shared/AgentMessageMarkdown";
import type { AgentSessionCommand } from "../../../shared/agentSessionTypes";
import type { AgentPromptContentBlock } from "../../../shared/contracts/dto/agentSession";
import type { ComposerFeatureState } from "../../../state/ComposerFeatureState";
import type { AgentGUIAgentTarget, AgentGUIProvider } from "../../../types";
import type { AgentProjectDropdownOptions } from "../AgentComposerProjectMenu";
import type { AgentProjectPathChangeMetadata } from "../AgentComposerSettingsMenus";
import type { AgentMentionFilterId } from "../AgentMentionSearchContracts";
import type { AgentContextMentionItem } from "../agentRichText/agentFileMentionExtension";
import type { AgentRichTextEditorProps } from "../agentRichText/AgentRichTextEditor.types";
import type { AgentGUIComposerEngagement } from "../engagement/agentGUIEngagement.types";
import type { AgentComposerInputHistoryEntry } from "../model/agentComposerInputHistory";
import type { AgentExternalPromptEntryResolver } from "../model/agentExternalPromptEntries";
import type { AgentExternalPromptFilePreparer } from "../model/agentExternalPromptFiles";
import type {
  AgentComposerDraft,
  AgentGUIComposerGate,
  AgentGUIComposerSettingsVM,
  AgentGUIProviderSkillOption
} from "../model/agentGuiNodeTypes";
import type { AgentGUISessionLaunchMode } from "../model/agentSessionLaunchMode";
import type { AgentSlashCommandCapability } from "../model/agentSlashCommandProviderPolicy";
import type { AgentQuickPromptLabels } from "./quickPrompts/agentQuickPromptLabels";
import type { WorkspaceReferencePickResult } from "./useComposerDraftAttachments";

export interface AgentComposerReferenceProvenanceFilter {
  snapshot: ReferenceProvenanceFilterSnapshot;
  controller: Pick<
    ReferenceProvenanceFilterController,
    "reset" | "toggle" | "toggleAll"
  >;
}

export interface AgentComposerReferenceProvenanceFilters {
  byFilter: Record<
    AgentMentionFilterId,
    AgentComposerReferenceProvenanceFilter
  >;
}

export interface AgentComposerSubmitOptions {
  /** Exact draft captured by the Composer for conditional post-submit clearing. */
  submittedDraft?: AgentComposerDraft;
  isolation?: "worktree";
  requiredSettingsPatch?: AgentActivitySubmitSettingsPatch;
  capabilityRefs?: readonly AgentComposerCapabilityReference[];
  /** Exact canonical active Turn captured for native guidance. */
  targetTurnId?: string;
}

export interface AgentComposerCapabilityReference {
  capability: "tutti";
  source: "slash_command";
}

export interface AgentComposerRenderProps {
  workspaceId: string;
  agentSessionId?: string | null;
  workspacePath?: string | null;
  currentUserId?: string | null;
  provider: string;
  slashStatus?: AgentComposerSlashStatus | null;
  usage?: AgentComposerUsage | null;
  draftContent: AgentComposerDraft;
  engagement?: AgentGUIComposerEngagement;
  /** Stable project/session owner for async draft attachment work. */
  draftScopeKey?: string;
  inputHistory?: readonly AgentComposerInputHistoryEntry[];
  inputHistoryHasOlderPage?: boolean;
  inputHistoryIsLoadingOlderPage?: boolean;
  onRequestOlderInputHistoryPage?: () => void;
  availableCommands: readonly AgentSessionCommand[];
  hasCompactableContext?: boolean;
  compactSupported?: boolean | null;
  availableSkills?: readonly AgentGUIProviderSkillOption[];
  gate: AgentGUIComposerGate;
  /** View-local lock that does not redefine canonical Composer readiness. */
  presentationEditorDisabled: boolean;
  disabledReason?: string | null;
  /** Draft-independent view-local submission lock. */
  presentationSubmitDisabled: boolean;
  placeholder: string;
  composerSettings: AgentGUIComposerSettingsVM;
  workspaceAppIcons?: readonly AgentMessageMarkdownWorkspaceAppIcon[];
  selectedAgentTarget?: AgentGUIAgentTarget | null;
  sessionWorktreeEnabled?: boolean;
  sessionLaunchMode?: AgentGUISessionLaunchMode;
  onSessionLaunchModeChange?: (
    mode: AgentGUISessionLaunchMode
  ) => void | Promise<void>;
  /** Content rendered immediately before the primary non-hero action. */
  composerActionAccessory?: ReactNode;
  /** Places the primary action cluster in the prompt row or Composer footer. */
  composerActionPlacement?: "input" | "footer";
  /** Shows the canonical new-Session project selector in a non-hero footer. */
  showProjectSelectorInFooter?: boolean;
  footerAccessory?: ReactNode;
  agentTargets?: readonly AgentGUIAgentTarget[];
  handoffAgentTargets?: readonly AgentGUIAgentTarget[];
  showHandoffTargetOwnershipLabels?: boolean;
  providerSelectReadonly?: boolean;
  onProviderSelect?: (input: {
    provider: AgentGUIProvider;
    agentTargetId?: string | null;
  }) => void;
  onHandoffConversation?: (target: AgentGUIAgentTarget) => void;
  showStopButton: boolean;
  /** Canonical active Turn; distinct from a cancellable session activation. */
  activeTurnId?: string | null;
  /** Lets typed input replace an aggregate-work Stop control with Send. */
  draftOverridesStopButton?: boolean;
  stopDisabled: boolean;
  activePrompt: AgentConversationPromptVM | null;
  /** Host readiness reason for the active prompt's disabled controls. */
  activePromptDisabledReason?: string | null;
  activePromptKeyboardShortcutsEnabled?: boolean;
  promptTips?: readonly AgentComposerPromptTip[];
  isSendingTurn: boolean;
  isSubmittingPrompt: boolean;
  /** Whether the active session is authoritative enough to probe its cwd. */
  projectMissingProbeEnabled?: boolean;
  uiLanguage?: UiLanguage;
  isActive?: boolean;
  workspaceReferencePickerOpen?: boolean;
  promptImagesSupported?: boolean;
  canUploadAttachment?: boolean;
  composerFocusRequestSequence?: number | null;
  /**
   * `dock` overhangs growing drafts above a conversation timeline, `hero`
   * presents the home composer, and `embedded` keeps all draft content in
   * normal flow for compact host surfaces.
   */
  layoutMode?: "dock" | "embedded" | "hero";
  /** Lets an embedded composer consume a height explicitly owned by its host. */
  fillAvailableHeight?: boolean;
  /** Host chrome inset that portaled menus must not overlap. */
  menuViewportTopInset?: number;
  providerSelectLabel?: string;
  handoffLabel?: string;
  handoffMenuLabel?: string;
  labels: {
    /** Capability plus copy for returning to a Composer that answers the prompt. */
    conversationReturn?: {
      continueAnswering: string;
      returnToConversation: string;
    };
    send: string;
    /**
     * Plan-review send copy: with an empty-send override active the send
     * button reads sendAccept on an empty draft and sendRequestChanges once
     * feedback is typed, so the composer decision semantics stay legible.
     */
    sendAccept?: string;
    sendRequestChanges?: string;
    modelLabel: string;
    modelSelectionLabel: string;
    modelContextWindowSuffix: string;
    modelTooltipVersionLabel: string;
    defaultModel: string;
    loadingOptions: string;
    composerOptionsLoadFailed?: string;
    retry?: string;
    composerOptionsRetryTooltip?: string;
    inheritedUnavailable: string;
    loadingConversation: string;
    reasoningLabel: string;
    reasoningDegreeLabel: string;
    reasoningOptionDefault: string;
    reasoningOptionMinimal: string;
    reasoningOptionLow: string;
    reasoningOptionMedium: string;
    reasoningOptionHigh: string;
    reasoningOptionXHigh: string;
    reasoningOptionMax: string;
    reasoningOptionUltra: string;
    speedLabel: string;
    speedSelectionLabel: string;
    speedOptionStandard: string;
    speedOptionStandardDescription: string;
    speedOptionFast: string;
    speedOptionFastDescription: string;
    permissionLabel: string;
    permissionModeReadOnly: string;
    permissionModeAuto: string;
    permissionModeFullAccess: string;
    permissionModeChangeUnavailableDuringTurn: string;
    modelDescriptions: {
      frontierComplexCoding: string;
      everydayCoding: string;
      smallFastCostEfficient: string;
      codingOptimized: string;
      ultraFastCoding: string;
      professionalLongRunning: string;
    };
    planModeLabel: string;
    codexSaverModeLabel: string;
    codexSaverModeDescription: string;
    rtkSaverModeLabel: string;
    rtkSaverModeDescription: string;
    planModeDescription?: string;
    planModeOnLabel: string;
    planModeOffLabel: string;
    planUnavailable: string;
    browserUseCapabilityLabel: string;
    browserUseCapabilityDescription: string;
    browserUseCapabilityDescriptionAutoConnect: string;
    browserUseCapabilityDescriptionIsolated: string;
    browserUseCapabilitySettingsLabel: string;
    browserUseCapabilitySettingsDescription: string;
    capabilityInlineSettingsLabel: string;
    computerUseCapabilityLabel: string;
    computerUseCapabilityDescription: string;
    computerUseCapabilitySetupRequiredDescription: string;
    computerUseCapabilityAuthorizationRequiredDescription: string;
    computerUseCapabilityAuthorizationUnknownDescription: string;
    computerUseCapabilitySettingsLabel: string;
    computerUseCapabilitySettingsDescription: string;
    queuedLabel: string;
    queuePausedByUserLabel: string;
    sendQueuedPromptNext: string;
    editQueuedPrompt: string;
    deleteQueuedPrompt: string;
    queuedPromptMoreActions: string;
    stop: string;
    stopping: string;
    slashCommandPalette: string;
    skillPickerPalette: string;
    slashPaletteCommandsGroup: string;
    slashPaletteCapabilitiesGroup: string;
    slashPaletteCapabilitiesLoading: string;
    slashPaletteSkillsGroup: string;
    slashPalettePluginsGroup: string;
    slashPaletteConnectorsGroup: string;
    slashPaletteConnectorConnected: string;
    slashPaletteConnectorNotConnected: string;
    slashPaletteConnectorUnsupported: string;
    slashPaletteMcpGroup: string;
    slashCommandPresentation?: (commandName: string) => {
      description?: string;
      label?: string;
    };
    slashCommandCompactLabel: string;
    slashCommandContextLabel: string;
    slashCommandFastLabel: string;
    slashCommandInitLabel: string;
    slashCommandPlanLabel: string;
    slashCommandReviewLabel: string;
    slashCommandStatusLabel: string;
    slashCommandUsageLabel: string;
    slashCommandCompactDescription: string;
    slashCommandContextDescription: string;
    slashCommandFastDescription: string;
    slashCommandInitDescription: string;
    slashCommandPlanDescription: string;
    slashCommandReviewDescription: string;
    slashCommandStatusDescription: string;
    slashCommandUsageDescription: string;
    slashStatusTitle: string;
    slashStatusSession: string;
    slashStatusBaseUrl: string;
    slashStatusContext: string;
    slashStatusLimits: string;
    slashStatusClose: string;
    slashStatusContextValue: (input: {
      percentLeft: number;
      usedTokens: string;
      totalTokens: string;
    }) => string;
    slashStatusContextUnavailable: string;
    slashStatusLimitsUnavailable: string;
    slashStatusEmptyValue: string;
    slashStatusUsageJustUpdated: string;
    slashStatusUsageMinutesAgo: (count: number) => string;
    slashStatusUsageHoursAgo: (count: number) => string;
    slashStatusUsageUpdating: string;
    slashStatusUsageRefreshFailed: string;
    slashStatusUsageRefreshAria: string;
    slashStatusUsageAuthRequired: string;
    slashStatusUsageSessionExpired: string;
    slashStatusUsageSubscriptionRequired: string;
    slashStatusUsageQuotaExhausted: string;
    slashStatusUsageParseFailed: string;
    slashStatusUsageError: string;
    usageChipLabel: (input: { percent: number }) => string;
    usageTooltipLabel: string;
    usagePopoverTitle: string;
    usageContextWindowLabel: string;
    usageTokensLabel: string;
    usageLimitsLabel: string;
    usageCompactAction: string;
    approvalLead: string;
    fileChangeApprovalLead: string;
    planLead: string;
    planModes: Array<{ id: string; label: string; description: string }>;
    stayInPlan: string;
    sendFeedback: string;
    feedbackPlaceholder: string;
    previousQuestion: string;
    nextQuestion: string;
    submitAnswers: string;
    answerPlaceholder: string;
    waitingForAnswer: string;
    planImplementationLead: string;
    planImplementationConfirm: string;
    planImplementationFeedbackPlaceholder: string;
    planImplementationSend: string;
    planImplementationSkip: string;
    fileMentionPalette: string;
    fileMentionLoading: string;
    fileMentionEmpty: string;
    fileMentionError: string;
    fileMentionTabHint: string;
    fileDropHint: string;
    mentionPalette: string;
    removeMention: string;
    addReference: string;
    addContent: string;
    addContentResourcePanel: string;
    addContentConnectors: string;
    addContentConnectorConnected: string;
    addContentConnectorConnect: string;
    addContentConnectorAuthorize: string;
    addContentConnectorEmpty: string;
    addContentConnectorLoading: string;
    addContentConnectorMore: string;
    addContentConnectorSelected: string;
    referenceWorkspaceFiles: string;
    handoffConversation: string;
    handoffConversationTooltip: string;
    handoffConversationMenu: string;
    handoffTargetDeviceSource: (deviceLabel: string) => string;
    handoffTargetSelf: string;
    handoffTargetShared: string;
    providerSwitchLabel: string;
    projectLocked: string;
    sessionLaunchModeLabel?: string;
    sessionLaunchModeLocal?: string;
    sessionLaunchModeWorktree?: string;
    projectMissingDescription: string;
    promptTipsPrefix: string;
    reviewPicker: {
      title: string;
      targetLabel: string;
      searchPlaceholder: string;
      noResults: string;
      uncommitted: string;
      baseBranch: string;
      commit: string;
      custom: string;
      branchLabel: string;
      branchPlaceholder: string;
      branchLoading: string;
      branchEmpty: string;
      commitPlaceholder: string;
      customPlaceholder: string;
      submit: string;
      cancel: string;
    };
    quickPrompts: AgentQuickPromptLabels;
  };
  workspaceUserProjectI18n: WorkspaceUserProjectI18nRuntime;
  onDraftContentChange: (
    draftContent: AgentComposerDraft,
    sourceScopeKey?: string
  ) => void;
  onProjectPathChange?: (
    path: string | null,
    metadata?: AgentProjectPathChangeMetadata
  ) => void;
  onSettingsChange: (settings: {
    codexSaverMode?: boolean;
    rtkSaverMode?: boolean;
    model?: string | null;
    reasoningEffort?: string | null;
    speed?: string | null;
    planMode?: boolean;
    browserUse?: boolean;
    computerUse?: boolean;
    permissionModeId?: string | null;
  }) => void;
  /** Retries or explicitly refreshes the target-scoped composer options. */
  onRetryComposerOptions?: (options?: {
    force?: boolean;
    section?: "core" | "capabilities" | "connectors";
    waitForFreshModelCatalog?: boolean;
  }) => void;
  capabilityMenuState?: AgentComposerCapabilityMenuState;
  capabilityControlsReadOnly?: boolean;
  onCapabilitySettingsRequest?: (
    capability: AgentComposerCapabilitySettingsTarget
  ) => void | Promise<void>;
  onSlashStatusOpen?: () => void;
  onSlashStatusClose?: () => void;
  onSlashStatusRefresh?: () => void;
  onSubmit: (
    content: AgentPromptContentBlock[],
    displayPrompt?: string,
    options?: AgentComposerSubmitOptions
  ) => void;
  /**
   * When set, an empty-draft send is enabled and routed here instead of being
   * blocked (e.g. Tutti plan review: empty send = accept). Typed sends keep
   * flowing through onSubmit.
   */
  onSubmitEmpty?: () => void;
  /**
   * Overrides the empty-draft send button copy while the empty-send override
   * is active. Falls back to labels.sendAccept.
   */
  emptySubmitLabel?: string;
  onSubmitGuidance?: (
    content: AgentPromptContentBlock[],
    displayPrompt?: string,
    options?: AgentComposerSubmitOptions
  ) => void;
  onSendQueuedPromptNext: (queuedPromptId: string) => void;
  onRemoveQueuedPrompt: (queuedPromptId: string) => void;
  onEditQueuedPrompt: (queuedPromptId: string) => void;
  onInterruptCurrentTurn: () => void;
  onPromptImagesUnsupported?: () => void;
  onSubmitInteractivePrompt: (input: AgentInteractionResponseInput) => boolean;
  onLinkAction?: (action: WorkspaceLinkAction) => void;
  onRequestWorkspaceReferences?:
    | ((
        entity?: AgentContextMentionItem | null
      ) => Promise<WorkspaceReferencePickResult>)
    | null;
  resolveExternalPromptEntries?: AgentExternalPromptEntryResolver | null;
  prepareExternalPromptFiles?: AgentExternalPromptFilePreparer | null;
  resolvePastedPath?: AgentRichTextEditorProps["onResolvePastedPath"] | null;
  promptAssetLimit?: number | null;
  selectProjectDirectory?: () => Promise<{ path: string } | null>;
  projectSelectOptions?: AgentProjectDropdownOptions;
  /** Explicit project capability for lifecycle-free Composer embeddings. */
  userProjectApi?: WorkspaceUserProjectApi | null;
  onRequestGitBranches?: AgentComposerGitBranchLoader | null;
  referenceProvenanceFilters?: AgentComposerReferenceProvenanceFilters | null;
  /** Host-owned mention categories to omit from the palette. */
  hiddenMentionFilterIds?: readonly string[];
}

export type AgentComposerCapabilitySettingsTarget =
  | AgentSlashCommandCapability["capability"]
  | {
      kind: "connector";
      connectorKey: string;
      action?: "install" | "open";
    }
  | {
      kind: "connector";
      connectorKey: string;
      action: "set_runtime_enabled";
      enabled: boolean;
    };

export interface AgentComposerCapabilityMenuState {
  browserUse?: {
    connectionMode?: "autoConnect" | "isolated" | null;
  };
  computerUse?: {
    authorization?: AgentComposerComputerUseAuthorizationState | null;
    installed?: boolean | null;
    /** Host can present the computer-use setup surface. Fail closed. */
    presentationSupported?: boolean | null;
  };
  /**
   * Host-owned connector visibility override. Missing preserves the existing
   * catalog behavior for hosts that have not adopted this optional field.
   */
  connectors?: {
    enabled?: boolean | null;
    /** Catalog remains inspectable but cannot select, authorize, install, or manage. */
    readOnly?: boolean | null;
    /** Controls the host management footer independently from catalog visibility. */
    showViewMore?: boolean | null;
  };
}

export type AgentComposerComputerUseAuthorizationState =
  | "authorized"
  | "needs-authorization"
  | "unknown";

export interface AgentComposerGitBranches {
  branches: readonly string[];
  currentBranch?: string | null;
}

export type AgentComposerGitBranchLoader = (input: {
  agentSessionId?: string | null;
  workingDirectory?: string | null;
}) => Promise<AgentComposerGitBranches>;

export interface AgentComposerPromptTip {
  id: string;
  label: string;
  prompt: string;
}

export interface AgentComposerSlashStatus {
  agentSessionId?: string | null;
  baseUrl?: string | null;
  contextWindow?: {
    usedTokens?: number | null;
    totalTokens?: number | null;
  } | null;
  limits?: readonly AgentComposerSlashStatusLimit[];
  limitsLoading?: boolean;
  limitsUnavailable?: boolean;
  limitsResolvedEmpty?: boolean;
  limitsCapturedAtUnixMs?: number | null;
  limitsErrorMessage?: string | null;
  refreshFailed?: boolean;
  isRefreshing?: boolean;
}

export interface AgentComposerSlashStatusLimit {
  id: string;
  label: string;
  percentRemaining?: number | null;
  value: string;
  reset?: string | null;
}

export interface AgentComposerUsage {
  percentUsed: number | null;
  usedTokens: number | null;
  totalTokens: number | null;
}

export type AgentComposerControlsState = Pick<
  ComposerFeatureState,
  | "availableCommands"
  | "availableSkills"
  | "hasCompactableContext"
  | "compactSupported"
  | "gate"
  | "composerSettings"
  | "usage"
  | "promptImagesSupported"
  | "isSendingTurn"
  | "isSubmittingPrompt"
  | "activeTurnId"
>;
export type AgentComposerProps = Omit<
  AgentComposerRenderProps,
  | "availableCommands"
  | "availableSkills"
  | "hasCompactableContext"
  | "compactSupported"
  | "gate"
  | "composerSettings"
  | "usage"
  | "promptImagesSupported"
  | "isSendingTurn"
  | "isSubmittingPrompt"
  | "activeTurnId"
> & { state: AgentComposerControlsState };
