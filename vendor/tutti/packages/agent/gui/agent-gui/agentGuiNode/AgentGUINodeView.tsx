import { TooltipProvider } from "@tutti-os/ui-system";
import { observer } from "mobx-react-lite";
import { useCallback, useMemo, useState, type CSSProperties } from "react";
import { AgentTargetInfoRendererProvider } from "../../shared/AgentTargetInfoRendererContext";
import {
  AgentTargetPresentationProvider,
  type AgentMessageMarkdownAgentTarget
} from "../../shared/AgentTargetPresentationContext";
import { AgentConversationClockProvider } from "../../shared/agentConversation/components/AgentConversationClock";
import { openWorkspaceSettingsPanel } from "../../shared/workspaceSettingsPanel/workspaceSettingsPanelStore";
import styles from "./AgentGUINode.styles";
import {
  resolveAgentGUIRailConfigProvider,
  resolveAgentGUIRailStatusTarget
} from "./AgentGUINode.usage";
import { useAgentGUINodeEngagement } from "./engagement/useAgentGUINodeEngagement";
import type { AgentGUIRailViewModel } from "./model/agentGuiNodeTypes";
import { isAgentGUIProviderReady } from "./model/agentGuiProviderReadiness";
import {
  mentionAgentTargetPresentationKey,
  projectMentionAgentTargetPresentations
} from "./model/agentGuiTargetPresentation";
import { AgentGUIDetailPane } from "./view/AgentGUIDetailPane";
import type { AgentGUINodeViewProps } from "./view/AgentGUINodeView.types";
import { AgentGUIReferencePickerSurface } from "./view/AgentGUIReferencePickerSurface";
import { AgentGUIRenameConversationDialog } from "./view/AgentGUIRenameConversationDialog";
import {
  AgentTargetSetupRoot,
  useAgentTargetSetupRoot
} from "./view/AgentTargetSetupRoot";
import { EMPTY_WORKSPACE_APP_ICONS } from "./view/agentGUIDetailConstants";
import { mergeWorkspaceAppIconsFromCommands } from "./view/agentGUIDetailModelHelpers";
import {
  fallbackWorkspaceFileReferenceCopy,
  useStableEventCallback
} from "./view/agentGUIViewUtils";
import { useAgentGUIExternalRequests } from "./view/useAgentGUIExternalRequests";
import { useAgentGUIWorkspaceReferencePicker } from "./view/useAgentGUIWorkspaceReferencePicker";
export * from "./AgentGUINodeView.publicTypes";
export type {
  AgentGUIComposerEmptySubmit,
  AgentGUIComposerFooterAccessoryContext,
  AgentGUIComposerFooterAccessoryRenderer
} from "./view/AgentGUIComposerFooterAccessory.types";
export {
  resolveAgentGUIHeroIconUrl,
  shouldEmphasizeEmptyHeroProvider
} from "./view/AgentGUIEmptyState";
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
export const AgentGUINodeView = observer(function AgentGUINodeView({
  shell,
  rail,
  detail,
  composer,
  interaction,
  readiness,
  operations,
  mentionAgentTargets,
  hiddenMentionFilterIds = [],
  referenceProvenanceFilters = null,
  sessionInputHistoryEnabled = false,
  sessionWorktreeEnabled = false,
  sessionLaunchModesByProjectSectionKey,
  onSessionLaunchModePreferenceChange,
  renderAgentTargetInfo,
  renderProjectDirectoryPickerHeaderActions,
  projectSelectOptions,
  renderReferencePickerSidebarActions,
  renderSidebarFooter,

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
  const isAgentProviderReady = isAgentGUIProviderReady(
    readiness.providerReadinessGate
  );
  const { composerEngagement, layoutElementRef } = useAgentGUINodeEngagement({
    composerReady: isAgentProviderReady,
    isActive,
    isVisible,
    onEvent: onEngagementEvent,
    ...{ shell, rail, detail, composer, interaction, readiness, operations }
  });
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
    ...{ shell, rail, detail, composer, interaction, readiness, operations },
    workspaceFileReferenceAdapter,
    workspaceFileReferenceCopy
  });
  const effectiveSelectProjectDirectory = projectDirectorySourceAggregator
    ? requestProjectDirectory
    : selectProjectDirectory;
  const createConversationDisabled = rail.selectedAgentTarget.disabled === true;
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
        commands: composer.state.availableCommands,
        workspaceAppIcons,
        workspaceId: shell.workspaceId
      }),
    [composer.state.availableCommands, shell.workspaceId, workspaceAppIcons]
  );

  const layoutStyle = {
    "--agent-gui-detail-min-width": `${detailMinWidthPx}px`,
    gridTemplateColumns: "minmax(var(--agent-gui-detail-min-width), 1fr)"
  } as CSSProperties;
  const effectiveRailConfigProvider = resolveAgentGUIRailConfigProvider(
    railConfigProvider,
    shell.data.provider
  );
  const railConfigTarget = resolveAgentGUIRailStatusTarget(rail);
  const shouldShowProviderRailConfigButton =
    rail.conversationFilter.kind === "all" ||
    rail.selectedAgentTarget?.disabled !== true;
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
    activeConversationId: rail.activeConversationId,
    agentTargets: rail.agentTargets,
    environmentProvider: effectiveRailConfigProvider,
    openEnvironmentSetup: onAgentEnvPanelOpen,
    selectedAgentTarget: rail.selectedAgentTarget
  });
  const openAgentSettings = useCallback(() => {
    openWorkspaceSettingsPanel({
      section: "agent",
      pane: "agents",
      provider: effectiveRailConfigProvider ?? undefined
    });
  }, [effectiveRailConfigProvider]);
  const [renameConversationTarget, setRenameConversationTarget] = useState<
    AgentGUIRailViewModel["conversations"][number] | null
  >(null);
  const [renameConversationDialogOpen, setRenameConversationDialogOpen] =
    useState(false);
  const requestRenameConversation = useCallback(
    (conversation: AgentGUIRailViewModel["conversations"][number]) => {
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
    ...{ shell, rail, detail, composer, interaction, readiness, operations },
    workbenchCommandBridge
  });
  const targetPresentationKey = mentionAgentTargetPresentationKey(
    rail.agentTargets,
    composer.state.handoffAgentTargets,
    mentionAgentTargets
  );
  const agentTargetPresentations = useMemo<
    readonly AgentMessageMarkdownAgentTarget[]
  >(
    () =>
      projectMentionAgentTargetPresentations({
        handoffTargets: composer.state.handoffAgentTargets,
        mentionTargets: mentionAgentTargets,
        ownerSeparator: labels.sharedAgentOwnerSeparator,
        railTargets: rail.agentTargets,
        workspaceId: shell.workspaceId
      }),
    [labels.sharedAgentOwnerSeparator, targetPresentationKey, shell.workspaceId]
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
          <section id="agent-gui-detail" className={styles.detailPanel}>
            <AgentConversationClockProvider isVisible={isVisible}>
              <AgentGUIDetailPane
                shell={shell}
                rail={rail}
                detail={detail}
                composer={composer}
                interaction={interaction}
                readiness={readiness}
                operations={operations}
                homeTargetProjection={homeTargetProjection}
                referenceProvenanceFilters={referenceProvenanceFilters}
                hiddenMentionFilterIds={hiddenMentionFilterIds}
                sessionInputHistoryEnabled={sessionInputHistoryEnabled}
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
          initialPath={composer.state.composerSettings.selectedProjectPath}
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
          workspaceId={shell.workspaceId}
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
        agentTargets={rail.agentTargets}
        renderer={renderAgentTargetInfo}
      >
        {content}
      </AgentTargetInfoRendererProvider>
    </TooltipProvider>
  );
});
