import {
  type CSSProperties,
  useCallback,
  useMemo,
  useState
} from "react";
import { TooltipProvider } from "@tutti-os/ui-system";
import { openWorkspaceSettingsPanel } from "../../shared/workspaceSettingsPanel/workspaceSettingsPanelStore";
import {
  AgentTargetPresentationProvider,
  type AgentMessageMarkdownAgentTarget
} from "../../shared/AgentTargetPresentationContext";
import { AgentTargetInfoRendererProvider } from "../../shared/AgentTargetInfoRendererContext";
import { AgentConversationClockProvider } from "../../shared/agentConversation/components/AgentConversationClock";
import type { AgentGUINodeViewModel } from "./model/agentGuiNodeTypes";
import {
  mentionAgentTargetPresentationKey,
  projectMentionAgentTargetPresentations
} from "./model/agentGuiTargetPresentation";
import styles from "./AgentGUINode.styles";
import {
  fallbackWorkspaceFileReferenceCopy,
  useStableEventCallback
} from "./view/agentGUIViewUtils";
import { AgentGUIConfigMenu } from "./view/AgentGUIAccountConfig";
import { AgentGUIProviderRail } from "./view/AgentGUIProviderRail";
import { AgentGUIDetailPane } from "./view/AgentGUIDetailPane";
import { EMPTY_WORKSPACE_APP_ICONS } from "./view/agentGUIDetailConstants";
import { mergeWorkspaceAppIconsFromCommands } from "./view/agentGUIDetailModelHelpers";
import { AgentGUIRenameConversationDialog } from "./view/AgentGUIRenameConversationDialog";
import { AgentGUIReferencePickerSurface } from "./view/AgentGUIReferencePickerSurface";
import {
  AgentTargetSetupRoot,
  useAgentTargetSetupRoot
} from "./view/AgentTargetSetupRoot";
import { useAgentGUIWorkspaceReferencePicker } from "./view/useAgentGUIWorkspaceReferencePicker";
import type { AgentGUINodeViewProps } from "./view/AgentGUINodeView.types";
import { useAgentGUINodeEngagement } from "./engagement/useAgentGUINodeEngagement";
import { isAgentGUIProviderReady } from "./model/agentGuiProviderReadiness";
import {
  resolveAgentGUIRailConfigProvider,
  resolveAgentGUIRailStatusTarget
} from "./AgentGUINode.usage";
export type {
  AgentGUIComposerEmptySubmit,
  AgentGUIComposerFooterAccessoryContext,
  AgentGUIComposerFooterAccessoryRenderer
} from "./view/AgentGUIComposerFooterAccessory.types";
export * from "./AgentGUINodeView.publicTypes";
export {
  buildAgentConversationHandoffPrompt,
  handoffProjectPathForConversation,
  isContextCanceledMessage,
  isDifferentKnownConversationOwner,
  resolveActiveConversationBusyStatus,
  resolveConversationDetailStatus,
  resolveSlashStatus,
  useStableSlashStatus
} from "./view/agentGUIDetailModelHelpers";
export {
  resolveAgentGUIHeroIconUrl,
  shouldEmphasizeEmptyHeroProvider
} from "./view/AgentGUIEmptyState";
import { useAgentGUIExternalRequests } from "./view/useAgentGUIExternalRequests";
export function AgentGUINodeView({
  viewModel,
  mentionAgentTargets,
  hiddenMentionFilterIds = [],
  referenceProvenanceFilters = null,
  sessionInputHistoryEnabled = false,
  sideConversationEnabled = false,
  sideConversationPresentation = null,
  sessionWorktreeEnabled = false,
  sessionLaunchModesByProjectSectionKey,
  onSessionLaunchModePreferenceChange,
  renderAgentTargetInfo,
  renderProjectDirectoryPickerHeaderActions,
  projectSelectOptions,
  renderReferencePickerSidebarActions,
  renderSidebarFooter,
  renderProviderRailEmpty,
  providerRailAllPresentation,
  onLinkAction,
  onHandoffConversation,
  showHandoffTargetOwnershipLabels = false,
  capabilityMenuState,
  capabilityControlsReadOnly = false,
  onCapabilitySettingsRequest,
  isActive = true,
  isVisible = true,
  onEngagementEvent,
  composerFocusRequestSequence = null,
  workbenchCommandBridge = null,
  slashStatusLimits = [],
  slashStatusLimitsLoading = false,
  slashStatusLimitsUnavailable = false,
  slashStatusOverride = null,
  providerAuthAccountLabels,
  railConfigProvider,
  railSlashStatusLimits,
  slashStatusLimitsResolvedEmpty = false,
  slashStatusUsageCapturedAtUnixMs = null,
  slashStatusUsageDidFail = false,
  slashStatusUsageErrorMessage = null,
  slashStatusUsageAttempted = false,
  agentConfigAccountContent,
  agentConfigSystemActionsContent,
  onAgentConfigMenuClose,
  onAgentConfigMenuOpen,
  onAgentUsageRefresh,
  accountUsageRefreshInline = false,
  onSlashStatusOpen,
  onSlashStatusClose,
  onSlashStatusRefresh,
  onAgentProviderLogin,
  onAgentEnvPanelOpen,
  actions,
  conversationRailCollapsed,
  detailMinWidthPx,
  uiLanguage,
  onWorkspaceFileReferencesAdded,
  resolveExternalPromptEntries = null,
  prepareExternalPromptFiles = null,
  resolvePastedPath = null,
  promptAssetLimit = null,
  labels,
  workspaceUserProjectI18n,
  workspaceFileManagerCopy = null,
  workspaceFileReferenceAdapter = null,
  selectProjectDirectory,
  workspaceFileReferenceCopy = null,
  onRequestGitBranches = null,
  projectDirectorySourceAggregator = null,
  referenceSourceAggregator = null,
  resolveReferenceContentErrorAction,
  resolveWorkspaceReferenceEntryIconUrl,
  resolveMentionReferenceTarget = null,
  resolveWorkspaceReferenceInitialTarget = null,
  workspaceAppIcons = EMPTY_WORKSPACE_APP_ICONS,
  renderComposerFooterAccessory,
  composerEmptySubmit = null
}: AgentGUINodeViewProps): React.JSX.Element {
  "use memo";
  const isAgentProviderReady = isAgentGUIProviderReady(
    viewModel.readiness.providerReadinessGate
  );
  const { composerEngagement, layoutElementRef } = useAgentGUINodeEngagement({
    composerReady: isAgentProviderReady,
    isActive,
    isVisible,
    onEvent: onEngagementEvent,
    viewModel
  });
  const [providerManagerOpen, setProviderManagerOpen] = useState(false);
  const [
    localComposerFocusRequestSequence,
    setLocalComposerFocusRequestSequence
  ] = useState(0);
  const {
    closeWorkspaceReferencePicker,
    confirmWorkspaceReferenceBundles,
    confirmWorkspaceReferencePicker,
    isWorkspaceReferencePickerNodeSelectable,
    requestProjectDirectory,
    requestWorkspaceReferences,
    workspaceReferencePickerAggregator,
    workspaceReferencePickerOpen,
    workspaceReferencePickerPurpose,
    workspaceReferencePickerTarget
  } = useAgentGUIWorkspaceReferencePicker({
    onWorkspaceFileReferencesAdded,
    projectDirectorySourceAggregator,
    referenceSourceAggregator,
    resolveMentionReferenceTarget,
    resolveWorkspaceReferenceInitialTarget,
    viewModel,
    workspaceFileReferenceAdapter,
    workspaceFileReferenceCopy
  });
  const effectiveSelectProjectDirectory = projectDirectorySourceAggregator
    ? requestProjectDirectory
    : selectProjectDirectory;
  const createConversationDisabled =
    viewModel.rail.selectedAgentTarget.disabled === true;
  const createConversationAction = useStableEventCallback(
    actions.createConversation
  );
  const detailComposerFocusRequestSequence =
    localComposerFocusRequestSequence === 0
      ? composerFocusRequestSequence
      : (composerFocusRequestSequence ?? 0) + localComposerFocusRequestSequence;
  const requestComposerFocus = useCallback(() => {
    setLocalComposerFocusRequestSequence((current) => current + 1);
  }, []);
  const requestCreateConversation = useStableEventCallback(
    (options?: { projectPath?: string | null; source?: string }) => {
      createConversationAction(options);
      requestComposerFocus();
    }
  );
  const effectiveWorkspaceAppIcons = useMemo(
    () =>
      mergeWorkspaceAppIconsFromCommands({
        commands: viewModel.composer.availableCommands,
        workspaceAppIcons,
        workspaceId: viewModel.shell.workspaceId
      }),
    [
      viewModel.composer.availableCommands,
      viewModel.shell.workspaceId,
      workspaceAppIcons
    ]
  );
  const providerRailWidthPx = conversationRailCollapsed ? 0 : 52;

  const layoutStyle = {
    "--agent-gui-detail-min-width": `${detailMinWidthPx}px`,
    "--agent-gui-provider-rail-width": `${providerRailWidthPx}px`,
    gridTemplateColumns:
      "var(--agent-gui-provider-rail-width) minmax(var(--agent-gui-detail-min-width), 1fr)"
  } as CSSProperties;
  const effectiveRailConfigProvider = resolveAgentGUIRailConfigProvider(
    railConfigProvider,
    viewModel.shell.data.provider
  );
  const railConfigTarget = resolveAgentGUIRailStatusTarget(viewModel.rail);
  const shouldShowProviderRailConfigButton =
    viewModel.rail.conversationFilter.kind === "all" ||
    viewModel.rail.selectedAgentTarget?.disabled !== true;
  const effectiveProviderAuthAccountLabel = useMemo(() => {
    const provider = effectiveRailConfigProvider?.trim() ?? "";
    if (!provider) {
      return null;
    }
    const label = providerAuthAccountLabels?.[provider]?.trim();
    return label || null;
  }, [effectiveRailConfigProvider, providerAuthAccountLabels]);
  const {
    controller: targetSetupController,
    environmentSetupVisible,
    homeTargetProjection,
    openAgentEnvSetup
  } = useAgentTargetSetupRoot({
    activeConversationId: viewModel.rail.activeConversationId,
    agentTargets: viewModel.rail.agentTargets,
    environmentProvider: effectiveRailConfigProvider,
    openEnvironmentSetup: onAgentEnvPanelOpen,
    selectedAgentTarget: viewModel.rail.selectedAgentTarget
  });
  const openAgentSettings = useCallback(() => {
    openWorkspaceSettingsPanel({
      section: "agent",
      pane: "agents",
      provider: effectiveRailConfigProvider ?? undefined
    });
  }, [effectiveRailConfigProvider]);
  const [renameConversationTarget, setRenameConversationTarget] = useState<
    AgentGUINodeViewModel["rail"]["conversations"][number] | null
  >(null);
  const [renameConversationDialogOpen, setRenameConversationDialogOpen] =
    useState(false);
  const requestRenameConversation = useCallback(
    (conversation: AgentGUINodeViewModel["rail"]["conversations"][number]) => {
      setRenameConversationTarget(conversation);
      setRenameConversationDialogOpen(true);
    },
    []
  );
  useAgentGUIExternalRequests({
    createConversationDisabled,
    labels,
    requestCreateConversation,
    requestRenameConversation,
    uiLanguage,
    viewModel,
    workbenchCommandBridge
  });
  const targetPresentationKey = mentionAgentTargetPresentationKey(
    viewModel.rail.agentTargets,
    viewModel.composer.handoffAgentTargets,
    mentionAgentTargets
  );
  const agentTargetPresentations = useMemo<
    readonly AgentMessageMarkdownAgentTarget[]
  >(
    () =>
      projectMentionAgentTargetPresentations({
        handoffTargets: viewModel.composer.handoffAgentTargets,
        mentionTargets: mentionAgentTargets,
        ownerSeparator: labels.sharedAgentOwnerSeparator,
        railTargets: viewModel.rail.agentTargets,
        workspaceId: viewModel.shell.workspaceId
      }),
    [
      labels.sharedAgentOwnerSeparator,
      targetPresentationKey,
      viewModel.shell.workspaceId
    ]
  );

  const content = (
    <AgentTargetPresentationProvider agentTargets={agentTargetPresentations}>
      <AgentTargetSetupRoot
        controller={targetSetupController}
        openEnvironmentSetup={onAgentEnvPanelOpen}
      >
        <div
          ref={layoutElementRef}
          className={styles.layout}
          data-agent-gui-active={isActive ? "true" : "false"}
          data-agent-gui-visible={isVisible ? "true" : "false"}
          style={layoutStyle}
        >
          <aside
            className={`${styles.providerRailPanel} nodrag tsh-desktop-no-drag`}
            aria-label={labels.providerSwitchLabel}
            aria-hidden={conversationRailCollapsed ? "true" : undefined}
            inert={conversationRailCollapsed ? true : undefined}
          >
            <AgentGUIProviderRail
              activeConversation={viewModel.rail.activeConversation}
              activeConversationId={viewModel.rail.activeConversationId}
              conversationFilter={viewModel.rail.conversationFilter}
              conversations={viewModel.rail.conversations}
              labels={labels}
              selectedAgentTarget={viewModel.rail.selectedAgentTarget}
              agentTargets={viewModel.rail.agentTargets}
              agentTargetsLoading={viewModel.rail.agentTargetsLoading}
              providerRailMode={viewModel.rail.providerRailMode}
              renderProviderRailEmpty={renderProviderRailEmpty}
              providerRailAllPresentation={providerRailAllPresentation}
              comingSoonProviders={viewModel.rail.comingSoonProviders}
              managerOpen={providerManagerOpen}
              onManagerOpenChange={setProviderManagerOpen}
              onSelectHomeComposerAgentTarget={
                actions.selectHomeComposerAgentTarget
              }
              onSelectConversationFilterTarget={
                actions.selectConversationFilterTarget
              }
              onUpdateConversationFilter={actions.updateConversationFilter}
              onRequestComposerFocus={requestComposerFocus}
            />
            {renderSidebarFooter ? (
              <div
                className={`${styles.providerRailFooter} ${styles.providerRailSidebarFooter} nodrag tsh-desktop-no-drag`}
                data-testid="agent-gui-sidebar-footer-slot"
              >
                {renderSidebarFooter({
                  currentUserId: viewModel.shell.currentUserId,
                  activeConversation: viewModel.rail.activeConversation
                })}
              </div>
            ) : null}
            {shouldShowProviderRailConfigButton ? (
              <div
                className={`${styles.providerRailFooter} ${styles.providerRailConfigFooter} nodrag tsh-desktop-no-drag`}
                data-testid="agent-gui-config-footer"
              >
                <AgentGUIConfigMenu
                  environmentSetupVisible={environmentSetupVisible}
                  labels={labels}
                  providerScopedActionsVisible={
                    viewModel.rail.conversationFilter.kind !== "all"
                  }
                  slashStatusLimits={railSlashStatusLimits ?? slashStatusLimits}
                  slashStatusLimitsLoading={slashStatusLimitsLoading}
                  slashStatusLimitsResolvedEmpty={
                    slashStatusLimitsResolvedEmpty
                  }
                  slashStatusUsageCapturedAtUnixMs={
                    slashStatusUsageCapturedAtUnixMs
                  }
                  slashStatusUsageDidFail={slashStatusUsageDidFail}
                  slashStatusUsageErrorMessage={slashStatusUsageErrorMessage}
                  slashStatusUsageAttempted={slashStatusUsageAttempted}
                  provider={
                    effectiveRailConfigProvider ?? railConfigTarget?.provider
                  }
                  providerIconUrl={railConfigTarget?.iconUrl ?? null}
                  providerMaskIconUrl={railConfigTarget?.maskIconUrl ?? null}
                  providerLabel={railConfigTarget?.label}
                  providerAuthAccountLabel={effectiveProviderAuthAccountLabel}
                  accountContent={agentConfigAccountContent}
                  systemActionsContent={agentConfigSystemActionsContent}
                  onAgentConfigMenuClose={onAgentConfigMenuClose}
                  onAgentConfigMenuOpen={onAgentConfigMenuOpen}
                  onAgentUsageRefresh={onAgentUsageRefresh}
                  usageRefreshInline={accountUsageRefreshInline}
                  onOpenAgentEnvSetup={openAgentEnvSetup}
                  onOpenAgentSettings={openAgentSettings}
                />
              </div>
            ) : null}
          </aside>
          <section id="agent-gui-detail" className={styles.detailPanel}>
            <AgentConversationClockProvider isVisible={isVisible}>
              <AgentGUIDetailPane
                shell={viewModel.shell}
                rail={viewModel.rail}
                detail={viewModel.detail}
                composer={viewModel.composer}
                interaction={viewModel.interaction}
                readiness={viewModel.readiness}
                operations={viewModel.operations}
                homeTargetProjection={homeTargetProjection}
                referenceProvenanceFilters={referenceProvenanceFilters}
                hiddenMentionFilterIds={hiddenMentionFilterIds}
                sessionInputHistoryEnabled={sessionInputHistoryEnabled}
                sideConversationEnabled={sideConversationEnabled}
                sideConversationPresentation={sideConversationPresentation}
                sessionWorktreeEnabled={sessionWorktreeEnabled}
                sessionLaunchModesByProjectSectionKey={
                  sessionLaunchModesByProjectSectionKey
                }
                onSessionLaunchModePreferenceChange={
                  onSessionLaunchModePreferenceChange
                }
                composerEngagement={composerEngagement}
                actions={actions}
                labels={labels}
                uiLanguage={uiLanguage}
                isActive={isActive}
                isVisible={isVisible}
                workspaceReferencePickerOpen={workspaceReferencePickerOpen}
                composerFocusRequestSequence={
                  detailComposerFocusRequestSequence
                }
                slashStatusLimits={slashStatusLimits}
                slashStatusLimitsLoading={slashStatusLimitsLoading}
                slashStatusLimitsUnavailable={slashStatusLimitsUnavailable}
                slashStatusOverride={slashStatusOverride}
                onSlashStatusOpen={onSlashStatusOpen}
                onSlashStatusClose={onSlashStatusClose}
                onSlashStatusRefresh={onSlashStatusRefresh}
                onLinkAction={onLinkAction}
                onHandoffConversation={onHandoffConversation}
                showHandoffTargetOwnershipLabels={
                  showHandoffTargetOwnershipLabels
                }
                capabilityMenuState={capabilityMenuState}
                capabilityControlsReadOnly={capabilityControlsReadOnly}
                onCapabilitySettingsRequest={onCapabilitySettingsRequest}
                onAgentProviderLogin={onAgentProviderLogin}
                onRequestWorkspaceReferences={requestWorkspaceReferences}
                resolveExternalPromptEntries={resolveExternalPromptEntries}
                prepareExternalPromptFiles={prepareExternalPromptFiles}
                resolvePastedPath={resolvePastedPath}
                promptAssetLimit={promptAssetLimit}
                selectProjectDirectory={effectiveSelectProjectDirectory}
                projectSelectOptions={projectSelectOptions}
                onRequestGitBranches={onRequestGitBranches}
                onRequestComposerFocus={requestComposerFocus}
                workspaceAppIcons={effectiveWorkspaceAppIcons}
                workspaceUserProjectI18n={workspaceUserProjectI18n}
                renderComposerFooterAccessory={renderComposerFooterAccessory}
                composerEmptySubmit={composerEmptySubmit}
              />
            </AgentConversationClockProvider>
          </section>
        </div>
        <AgentGUIReferencePickerSurface
          aggregator={workspaceReferencePickerAggregator}
          copy={
            workspaceFileReferenceCopy ?? fallbackWorkspaceFileReferenceCopy
          }
          fileAdapter={workspaceFileReferenceAdapter}
          fileManagerCopy={workspaceFileManagerCopy}
          initialPath={viewModel.composer.composerSettings.selectedProjectPath}
          initialTarget={workspaceReferencePickerTarget}
          isNodeSelectable={isWorkspaceReferencePickerNodeSelectable}
          open={workspaceReferencePickerOpen}
          purpose={workspaceReferencePickerPurpose}
          renderDirectoryHeaderActions={
            renderProjectDirectoryPickerHeaderActions
          }
          renderSidebarActions={renderReferencePickerSidebarActions}
          resolveContentErrorAction={resolveReferenceContentErrorAction}
          resolveEntryIconUrl={resolveWorkspaceReferenceEntryIconUrl}
          workspaceId={viewModel.shell.workspaceId}
          onClose={closeWorkspaceReferencePicker}
          onConfirm={confirmWorkspaceReferencePicker}
          onConfirmBundles={
            workspaceReferencePickerPurpose === "reference"
              ? confirmWorkspaceReferenceBundles
              : undefined
          }
        />
        <AgentGUIRenameConversationDialog
          conversation={renameConversationTarget}
          open={
            renameConversationDialogOpen && renameConversationTarget !== null
          }
          labels={labels}
          onOpenChange={(open) => {
            setRenameConversationDialogOpen(open);
            if (!open) {
              setRenameConversationTarget(null);
            }
          }}
          onRename={actions.renameConversation}
        />
      </AgentTargetSetupRoot>
    </AgentTargetPresentationProvider>
  );
  return (
    <TooltipProvider>
      <AgentTargetInfoRendererProvider
        agentTargets={viewModel.rail.agentTargets}
        renderer={renderAgentTargetInfo}
      >
        {content}
      </AgentTargetInfoRendererProvider>
    </TooltipProvider>
  );
}
