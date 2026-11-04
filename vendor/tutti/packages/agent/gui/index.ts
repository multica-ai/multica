export {
  getAgentCustomMentionKind,
  registerAgentCustomMentionKind,
  resetAgentCustomMentionKindsForTests,
  type AgentCustomMentionChipContext,
  type AgentCustomMentionIdentity,
  type AgentCustomMentionKindDefinition,
  type AgentCustomMentionPresentation
} from "./shared/agentCustomMentionKinds";
export {
  AGENT_PASTED_TEXT_BLOCK_KIND,
  AGENT_PASTED_TEXT_MENTION_KIND
} from "./shared/pastedTextKinds";
export { AgentGUI } from "./AgentGUI";
export {
  AgentSideConversationRuntimeProvider,
  useAgentSideConversationSnapshot,
  useOptionalAgentSideConversationRuntime
} from "./agentSideConversationRuntime";
export type {
  AgentSideCapabilities,
  AgentSideConversationRuntime,
  AgentSideConversationSnapshot,
  AgentSideConversationState,
  AgentSideInteraction,
  AgentSideInteractionAction
} from "./agentSideConversationRuntime";
export { createAgentSideConversationRuntime } from "./agentSideConversationController";
export type {
  AgentSideConversationStreamEvent,
  AgentSideConversationTransport
} from "./agentSideConversationController";
export {
  createAgentGUISideConversationPresentation,
  type AgentGUISideConversationIdentity,
  type AgentGUISideConversationPresentation,
  type AgentGUISideConversationProjection
} from "./agentSideConversationPresentation";
export {
  AgentGUISideConversationSurface,
  type AgentGUISideConversationSurfaceProps
} from "./agent-gui/agentGuiNode/view/AgentGUISideConversationPane";
export type {
  AgentGUIProps,
  AgentGUIReferenceProvenanceFilterCatalog
} from "./AgentGUI";
export type { AgentGUIAgentConfigMenuContext } from "./agent-gui/agentGuiNode/AgentGUINode.types";
export type { AgentGUISessionLaunchMode } from "./agent-gui/agentGuiNode/model/agentSessionLaunchMode";
export { AgentGUIConfigAccountFallbackSuppressed } from "./agent-gui/agentGuiNode/view/AgentGUIAccountConfig";
export type { AgentGUIComposerAppendRequest } from "./agent-gui/agentGuiNode/controller/useAgentGUIComposerAppendRequest";
export {
  createAgentStatusController,
  selectAgentStatusControllerSnapshot,
  type AgentStatusController,
  type AgentStatusControllerOptions,
  type AgentStatusControllerSnapshot,
  type AgentStatusFrame,
  type AgentStatusQuery,
  type AgentStatusRequestPhase,
  type AgentStatusRequestReason,
  type AgentStatusSelectionKey,
  type AgentStatusSectionState,
  type AgentStatusSource,
  type AgentStatusSourceError,
  type AgentStatusStreamObserver,
  type AgentStatusValue
} from "./agent-gui/agentGuiNode/controller/AgentStatusController";
export {
  AgentHandoffMenu,
  type AgentHandoffMenuLabels,
  type AgentHandoffMenuProps
} from "./agent-gui/agentGuiNode/composer/AgentHandoffMenu";
export {
  createAgentSessionHandoffPrompt,
  createAgentSessionMarkdownLink
} from "./agent-gui/agentGuiNode/agentRichText/agentMentionMarkdown";
export type {
  CreateAgentSessionHandoffPromptInput,
  CreateAgentSessionMarkdownLinkInput
} from "./agent-gui/agentGuiNode/agentRichText/agentMentionMarkdown";
export type { AgentComposerDraftFile } from "./agent-gui/agentGuiNode/model/agentGuiNodeTypes";
export type {
  AgentExternalPromptFilePreparationErrorCode,
  AgentExternalPromptFilePreparationResult,
  AgentExternalPromptFilePreparer,
  AgentPreparedExternalPromptFile
} from "./agent-gui/agentGuiNode/model/agentExternalPromptFiles";
export type {
  AgentExternalPromptEntryResolution,
  AgentExternalPromptEntryResolver
} from "./agent-gui/agentGuiNode/model/agentExternalPromptEntries";
export type {
  AgentRunErrorCode,
  AgentVisibleErrorOverride,
  AgentVisibleErrorOverrideCode,
  AgentVisibleErrorOverrides,
  AgentVisibleErrorPresentationScope
} from "./shared/agentEnv/agentErrorPresentation";
export type {
  AgentGUIComposerContentType,
  AgentGUIComposerFocusMethod,
  AgentGUIEngagementContext,
  AgentGUIEngagementEvent,
  AgentGUIEngagementEventSink,
  AgentGUIQuickPromptType
} from "./agent-gui/agentGuiNode/engagement/agentGUIEngagement.types";
export {
  agentGUIAgentIsReady,
  normalizeAgentGUIAgents,
  projectAgentGUIAgentsToTargets,
  resolveAgentGUISelectedDirectoryAgent
} from "./agents";
export {
  agentGUIDefaultTargetProviders,
  createLocalAgentGUIAgentTarget,
  createLocalAgentGUIAgentTargets,
  createSharedAgentGUIAgentTarget,
  localAgentGUIAgentTargetId,
  normalizeAgentGUIAgentTargets,
  resolveAgentGUIAgentTarget
} from "./agentTargets";
export type {
  AgentGUIAgent,
  AgentGUIAgentDirectoryPort,
  AgentGUIAgentDirectorySnapshot,
  AgentGUIAgentDirectoryStatus,
  AgentGUIAgentAvailability,
  AgentGUIAgentAvailabilityAction,
  AgentGUIAgentAvailabilityStatus,
  AgentGUIAgentOwner,
  AgentGUIAgentOwnership,
  AgentGUIHomeSuggestionId,
  AgentGUIAllAgentsPresentation,
  AgentGUIProvider,
  AgentGUIProviderRailAllPresentation,
  AgentGUIProviderRailMode,
  AgentGUIProviderReadinessGate,
  AgentGUIProviderReadinessGateAction,
  AgentGUIProviderReadinessGateStatus,
  AgentGUIInteractionReadiness,
  AgentGUIInteractionReadinessIdentity,
  AgentGUIInteractionReadinessReason,
  AgentGUIInteractionReadinessSource,
  AgentGUIObservationGap,
  AgentGUIObservationGapSource,
  AgentGUITargetConnectionSource,
  AgentGUITargetConnectionState,
  AgentGUITargetConnectionStatus,
  AgentGUIAgentTarget,
  AgentGUIAgentTargetBadge,
  AgentGUIAgentTargetInfoRenderContext,
  AgentGUIAgentTargetInfoRenderer,
  AgentGUIAgentTargetInfoSurface,
  AgentGUIAgentTargetRef
} from "./types";
export {
  AgentGuiI18nProvider,
  agentGuiI18nModule,
  agentGuiI18nResources
} from "./i18n/index";
export type { AgentGuiI18nLocale } from "./i18n/index";
export { agentGuiDockIconUrl, agentGuiDockIconUrls } from "./dockIcons";
export {
  AGENT_GUI_COLLAPSED_MIN_WIDTH_PX,
  AGENT_GUI_DETAIL_MIN_WIDTH_PX,
  AGENT_GUI_EXPANDED_TARGET_WIDTH_PX,
  AGENT_GUI_STANDALONE_MIDDLE_CONTENT_MIN_WIDTH_PX,
  resolveAgentGUIConversationRailPresentation,
  resolveAgentGUIExpandedWindowFrame,
  resolveStandaloneAgentGUIViewportMinimumWidthPx,
  shouldAutoCollapseAgentGUIConversationRail
} from "./agent-gui/agentGuiNode/model/agentGuiRailLayout";
export type {
  AgentGUIConversationRailAutoCollapseMode,
  AgentGUIConversationRailPresentation
} from "./agent-gui/agentGuiNode/model/agentGuiRailLayout";
export type {
  AgentGUIAgentsEmptyRenderer,
  AgentGUIConversationRailLayout,
  AgentGUISidebarFooterContext,
  AgentGUISidebarFooterRenderer
} from "./agent-gui/agentGuiNode/AgentGUINodeView";
export {
  AGENT_CONTEXT_MENTION_PROVIDER_IDS,
  type AgentContextMentionProviderId,
  type AgentContextMentionProvider
} from "./agent-gui/agentGuiNode/agentContextMentionProvider";
export { preloadAgentMentionBrowse } from "./agent-gui/agentGuiNode/AgentMentionSearchController";
export { AgentGUIActivityHostProvider } from "./agentActivityHost";
export type { AgentGUIActivityHostProviderProps } from "./agentActivityHost";
export { useEngineSelector } from "./shared/engine/useEngineSelector";
export type { EngineStateStore } from "./shared/engine/useEngineSelector";
export {
  AgentGUIRuntimeProvider,
  resetAgentGUIRuntimeForTests,
  setAgentGUIRuntimeForTests,
  useAgentActivitySessionMessages,
  useAgentActivitySnapshot,
  useAgentGUIRuntime,
  useOptionalAgentGUIRuntime
} from "./agentActivityRuntime";
export type {
  AgentGUIRuntime,
  AgentGUIRuntimeProviderProps,
  AgentActivitySessionMessages,
  AgentActivityRuntimeActivateSessionInput,
  AgentActivityRuntimeListSessionMessagesInput,
  AgentActivityRuntimePromptContentBlock,
  AgentActivityRuntimeUploadPromptContentInput,
  AgentActivityRuntimeUploadPromptContentResult,
  AgentActivityRuntimeUnactivateSessionInput,
  AgentActivityRuntimeUpdateSessionSettingsInput,
  AgentActivityRuntimeUpdateSessionSettingsResult
} from "./agentActivityRuntime";
export {
  agentGUIPerformanceDuration,
  createAgentGUIPerformanceMonitor,
  trackAgentGUIComposerOptionsLoad
} from "./agentGUIPerformanceMonitor";
export type {
  AgentGUIComposerOptionsLoadInput,
  AgentGUIComposerOptionsLoadSource,
  AgentGUIComposerOptionsPerformanceEvent,
  AgentGUIComposerOptionsPerformanceTrackerInput,
  AgentGUIFirstTokenKind,
  AgentGUIPerformanceDurationBucket,
  AgentGUIPerformanceEvent,
  AgentGUIPerformanceFailureStage,
  AgentGUIPerformanceMonitor
} from "./agentGUIPerformanceMonitor";
export type {
  AgentHostApi,
  AgentHostAgentTargetAuthenticatedAccount,
  AgentHostAgentTargetSetupSnapshot,
  AgentHostAgentTargetSetupState,
  AgentHostAgentTargetSetupWatch,
  AgentHostApplyWorkspaceGitPatchInput,
  AgentHostResolveSessionWorktreeSupportInput,
  AgentHostResolveSessionWorktreeSupportResult,
  AgentHostInputApi,
  AgentHostQuickPrompt,
  AgentHostQuickPromptSnapshot,
  AgentHostQuickPromptsApi,
  AgentHostSelectFilesInput,
  AgentHostTerminalStartupAction,
  AgentHostRuntimeApi,
  AgentHostUserProject,
  AgentProviderProbeListInput,
  AgentProviderProbeListResult
} from "./host/agentHostApi";
export type {
  AgentProbeProvider,
  AgentProbeSnapshot,
  PersistWriteResult,
  ReadWorkspaceAgentReadStateInput,
  AgentUsageQuota,
  AgentUsageSnapshot,
  WorkspaceAgentReadStateSnapshot,
  WriteWorkspaceAgentReadStateInput
} from "./shared/contracts/dto";
export {
  selectNeedsAttentionCount,
  selectNeedsAttentionItems
} from "@tutti-os/agent-activity-core";
export type {
  AgentActivityAdapter,
  AgentActivityMessage,
  AgentActivityNeedsAttentionItem,
  AgentActivitySnapshot
} from "@tutti-os/agent-activity-core";
