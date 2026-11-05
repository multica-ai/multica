import { usePersistCallback } from "@multica/ui/hooks/use-persist-callback";
import { createWorkspaceFileManagerI18nRuntime } from "@tutti-os/workspace-file-manager";
import type { WorkspaceFileReference } from "@tutti-os/workspace-file-reference/contracts";
import { createWorkspaceUserProjectI18nRuntime } from "@tutti-os/workspace-user-project/i18n";
import { observer } from "mobx-react-lite";
import { useCallback, useMemo, useRef } from "react";
import type { WorkspaceLinkAction } from "../../actions/workspaceLinkActions";
import { useTranslation } from "../../i18n/index";
import type { AgentGUIAgentTarget, AgentGUINodeData } from "../../types";
import { resolveCanonicalNodeMinSize } from "../../utils/workspaceNodeSizing";
import { CanvasNodeGhostIconButton } from "../shared/CanvasNodeGhostIconButton";
import { WorkspaceNodeWindow } from "../shared/WorkspaceNodeWindow";
import { CanvasNodePanelLinedIcon } from "../shared/canvasNodeChromeIcons";
import { AgentProbeInfoPopover } from "../workspaceDesktop/view/AgentProbeInfoPopover";
import { AgentGUIMentionServiceBoundary } from "./AgentGUIMentionServiceBoundary";
import {
  useAgentGUIViewLabels,
  useAgentGUIWorkspaceFileReferenceCopy
} from "./AgentGUINode.labels";
import styles from "./AgentGUINode.styles";
import type {
  AgentGUIAgentConfigMenuContext,
  AgentGUINodeProps
} from "./AgentGUINode.types";
import { AgentGUINodeView } from "./AgentGUINodeView";
import type { AgentGUIFeatureProps } from "./AgentGUIPageProvider";
import { useAgentGUIShell } from "./AgentGUIShellContext";
import { useAgentMentionProvenanceFilters } from "./composer/useAgentMentionProvenanceFilters";
import { agentTargetForConversation } from "./controller/agentGuiController.providerHelpers";
import { useAgentGUIStatus } from "./controller/useAgentGUIStatus";
import {
  normalizeAgentGUIProviderIdentity,
  resolveAgentGUIProviderDisplayLabel
} from "./model/agentGuiProviderIdentity";
import {
  AGENT_GUI_COLLAPSED_MIN_WIDTH_PX,
  AGENT_GUI_DETAIL_MIN_WIDTH_PX,
  resolveAgentGUIConversationRailPresentation,
  resolveAgentGUIExpandedWindowFrame
} from "./model/agentGuiRailLayout";

export type { AgentGUINodeProps } from "./AgentGUINode.types";

export const AgentGUIFrame = observer(function AgentGUIFrame({
  identity,
  workspace,
  frame,
  state,
  runtimeRequests,
  hostCapabilities,
  hostActions,
  renderSlots,
  rail,
  detail,
  composer,
  interaction,
  readiness,
  operations,
  actions
}: AgentGUINodeProps & AgentGUIFeatureProps): React.JSX.Element {
  "use memo";
  const { nodeId, workspaceId, currentUserId, title } = identity;
  const {
    path: workspacePath,
    fileReferenceAdapter: workspaceFileReferenceAdapter = null,
    onRequestGitBranches = null,
    selectProjectDirectory,
    resolveExternalPromptEntries = null,
    prepareExternalPromptFiles = null,
    resolvePastedPath = null,
    promptAssetLimit = null,
    projectDirectorySourceAggregator = null,
    referenceSourceAggregator = null,
    resolveReferenceContentErrorAction:
      resolveWorkspaceReferenceContentErrorAction,
    resolveReferenceEntryIconUrl: resolveWorkspaceReferenceEntryIconUrl,
    resolveMentionReferenceTarget = null,
    resolveReferenceInitialTarget:
      resolveWorkspaceReferenceInitialTarget = null,
    onFileReferencesAdded: onWorkspaceFileReferencesAdded,
    agentSettings
  } = workspace;
  const {
    position,
    width,
    height,
    desktopSize,
    isMaximized = false,
    isActive,
    isVisible = true,
    embedded = false
  } = frame;
  const widthRef = useRef(width);
  widthRef.current = width;
  const {
    composerAppend: composerAppendRequest = null,
    composerFocusSequence: composerFocusRequestSequence = null,
    workbench: workbenchCommandBridge = null,
    openSession: openSessionRequest = null,
    prefillPrompt: prefillPromptRequest = null,
    agentStatusController
  } = runtimeRequests;
  const {
    capabilityMenuState,
    capabilityControlsReadOnly = false,
    agentTargets,
    agentTargetsLoading = false,
    mentionAgentTargets,
    handoffAgentTargets,
    handoffAgentTargetsLoading = false,
    showHandoffTargetOwnershipLabels = false,

    providerRailMode = "catalog",
    comingSoonProviders,
    providerReadinessGates = null,
    accountUsageRefreshInline = false,
    targetConnectionSource = null,
    interactionReadinessSource = null,
    observationGapSource = null,
    defaultAgentTargetId = null,
    providerAuthAccountLabels,
    mentionService,
    workspaceAppIcons,
    hiddenMentionFilterIds,
    disabledHomeSuggestions,
    referenceProvenanceFilterCatalog: injectedReferenceProvenanceFilterCatalog,
    referenceProvenanceFilterEnabled = false,
    sessionInputHistoryEnabled = false,
    sessionWorktreeEnabled = false,
    sessionLaunchModesByProjectSectionKey,
    codexSaverModeEntryEnabled = false,
    rtkSaverModeEntryEnabled = false
  } = hostCapabilities;
  const referenceProvenanceFilters = useAgentMentionProvenanceFilters({
    agentTargets,
    injectedCatalog: injectedReferenceProvenanceFilterCatalog,
    legacyAgentFilterEnabled: referenceProvenanceFilterEnabled
  });
  const {
    onComposerAppendHandled,
    onLinkAction,
    onHandoffConversation,
    onCapabilitySettingsRequest,
    onAgentProviderLogin,
    onAgentEnvPanelOpen,
    onAgentConfigMenuOpen: onHostAgentConfigMenuOpen,
    onOpenConversationWindow,
    onClose,
    onResize,
    onUpdateNode,
    onRememberComposerDefaults,
    onSessionLaunchModePreferenceChange,
    isMuted = false,
    onMinimize,
    onToggleMaximize,
    onShowMessage,
    onEngagementEvent
  } = hostActions;
  const {
    agentConfigAccount: renderAgentConfigAccount,
    agentConfigSystemActions: renderAgentConfigSystemActions,
    agentTargetInfo: renderAgentTargetInfo,
    composerFooterAccessory: renderComposerFooterAccessory,
    composerEmptySubmit,
    projectDirectoryPickerHeaderActions:
      renderProjectDirectoryPickerHeaderActions,
    projectSelectOptions,
    referencePickerSidebarActions: renderReferencePickerSidebarActions,

    sidebarFooter: renderSidebarFooter
  } = renderSlots;
  const { i18n, locale, t } = useTranslation();
  const workspaceUserProjectI18n = useMemo(
    () => createWorkspaceUserProjectI18nRuntime(i18n),
    [i18n]
  );
  const workspaceFileManagerI18n = useMemo(
    () =>
      typeof i18n?.t === "function"
        ? createWorkspaceFileManagerI18nRuntime(i18n)
        : null,
    [i18n]
  );
  const handleLinkAction = useCallback(
    (action: WorkspaceLinkAction) => {
      onLinkAction?.(action);
    },
    [onLinkAction]
  );
  const handleLinkActionCallback = usePersistCallback(handleLinkAction);
  const handleAgentProviderLogin = useCallback(
    (provider?: string | null) => {
      const resolvedProvider = normalizeAgentGUIProviderIdentity(provider);
      onAgentProviderLogin?.(
        resolvedProvider === "unknown" ? state.provider : resolvedProvider
      );
    },
    [onAgentProviderLogin, state.provider]
  );
  const handleAgentProviderLoginCallback = usePersistCallback(
    handleAgentProviderLogin
  );
  const handleWorkspaceFileReferencesAdded = useCallback(
    (references: readonly WorkspaceFileReference[]) => {
      onWorkspaceFileReferencesAdded?.({
        provider: state.provider,
        references
      });
    },
    [onWorkspaceFileReferencesAdded, state.provider]
  );
  const handleWorkspaceFileReferencesAddedCallback = usePersistCallback(
    handleWorkspaceFileReferencesAdded
  );
  const handleDataChange = useCallback(
    (updater: (current: AgentGUINodeData) => AgentGUINodeData) => {
      onUpdateNode(updater);
    },
    [onUpdateNode]
  );
  const conversationRailPresentation =
    resolveAgentGUIConversationRailPresentation({
      autoCollapseMode: frame.conversationRailAutoCollapseMode,
      containerWidthPx: width,
      conversationRailCollapsed: state.conversationRailCollapsed,
      conversationRailWidthPx: state.conversationRailWidthPx
    });
  const isConversationRailAutoCollapsed =
    conversationRailPresentation.isAutoCollapsed;
  const isConversationRailCollapsed = conversationRailPresentation.isCollapsed;
  const minSize = useMemo(
    () => ({
      ...resolveCanonicalNodeMinSize("agentGui"),
      width: AGENT_GUI_COLLAPSED_MIN_WIDTH_PX
    }),
    []
  );
  const toggleConversationRailCollapsed = useCallback(() => {
    onUpdateNode((current) => ({
      ...current,
      conversationRailCollapsed: current.conversationRailCollapsed !== true
    }));
  }, [onUpdateNode]);
  const handleConversationRailToggle = useCallback(() => {
    if (!isConversationRailAutoCollapsed) {
      toggleConversationRailCollapsed();
      return;
    }

    onResize(
      resolveAgentGUIExpandedWindowFrame({
        position,
        width,
        height,
        desktopSize,
        conversationRailWidthPx: state.conversationRailWidthPx
      })
    );
    onUpdateNode((current) => {
      if (current.conversationRailCollapsed !== true) {
        return current;
      }
      return {
        ...current,
        conversationRailCollapsed: false
      };
    });
  }, [
    desktopSize,
    height,
    isConversationRailAutoCollapsed,
    onResize,
    onUpdateNode,
    position,
    state.conversationRailWidthPx,
    toggleConversationRailCollapsed,
    width
  ]);
  const shell = useAgentGUIShell();
  const handleCreateConversation = useCallback(
    (...args: Parameters<typeof actions.createConversation>) => {
      {
        onUpdateNode((current) =>
          current.lastActiveAgentSessionId === null
            ? current
            : {
                ...current,
                lastActiveAgentSessionId: null
              }
        );
      }
      actions.createConversation(...args);
    },
    [actions, onUpdateNode]
  );
  const viewActions = useMemo(
    () => ({
      ...actions,
      createConversation: handleCreateConversation
    }),
    [actions, handleCreateConversation]
  );

  const fallbackAgentTitle = t("sidebar.fallbackAgentLabel");
  const activeProvider = rail.activeConversation?.provider ?? state.provider;
  const activeConversationAgentTarget = agentTargetForConversation(
    rail.activeConversation,
    rail.agentTargets
  );
  const displayProviderLabel = resolveAgentGUIProviderDisplayLabel(
    activeProvider,
    fallbackAgentTitle,
    rail.activeConversation
      ? activeConversationAgentTarget?.label
      : rail.selectedAgentTarget?.label
  );
  const labels = useAgentGUIViewLabels({
    disabledHomeSuggestions,
    displayProviderLabel,
    fallbackAgentTitle,
    t,
    workspaceAppIcons: workspaceAppIcons ?? [],
    workspaceId
  });
  const workspaceFileReferenceCopy = useAgentGUIWorkspaceFileReferenceCopy(t);
  const windowTitle = title;
  const {
    agentProbeLines,
    controllerRailStatus,
    handleAgentConfigMenuClose,
    handleAgentConfigMenuOpen: handleStatusAgentConfigMenuOpen,
    handleAgentProbeInfoClose,
    handleAgentProbeInfoOpen,
    handleAgentUsageRefresh,
    handleSlashStatusClose,
    handleSlashStatusOpen,
    handleSlashStatusRefresh,
    railStatusProvider,
    slashStatusLimits,
    slashStatusLimitsUnavailable,
    slashStatusOverride
  } = useAgentGUIStatus({
    activeProvider,
    agentStatusController,
    t,
    ...{ shell, rail, detail, composer, interaction, readiness, operations }
  });
  const effectiveProviderAuthAccountLabels = projectProviderAccountLabel(
    providerAuthAccountLabels,
    railStatusProvider,
    controllerRailStatus?.accountLabel
  );
  const agentConfigMenuContext =
    rail.conversationFilter.kind === "all"
      ? null
      : resolveAgentConfigMenuContext(rail.selectedAgentTarget);
  const handleAgentConfigMenuOpen = () => {
    handleStatusAgentConfigMenuOpen();
    if (agentConfigMenuContext) {
      onHostAgentConfigMenuOpen?.(agentConfigMenuContext);
    }
  };
  const handleAgentConfigMenuOpenCallback = usePersistCallback(
    handleAgentConfigMenuOpen
  );
  const agentConfigAccountContent = agentConfigMenuContext
    ? (renderAgentConfigAccount?.(agentConfigMenuContext) ?? null)
    : null;
  const agentConfigSystemActionsContent =
    renderAgentConfigSystemActions?.({ presentation: "menu" }) ?? null;

  const onClickProp1 = usePersistCallback<
    NonNullable<
      React.ComponentProps<typeof CanvasNodeGhostIconButton>["onClick"]
    >
  >((event) => {
    event.stopPropagation();
    handleConversationRailToggle();
  });
  return (
    <AgentGUIMentionServiceBoundary
      service={mentionService}
      observationGapSource={observationGapSource}
    >
      <WorkspaceNodeWindow
        nodeId={nodeId}
        kind="agentGui"
        title={windowTitle}
        titleIcon={null}
        position={position}
        width={width}
        height={height}
        desktopSize={desktopSize}
        minSize={minSize}
        appearance={embedded ? "embedded" : "window"}
        className="size-full bg-transparent"
        bodyClassName={`${styles.shell} nodrag size-full min-h-0 min-w-0 !bg-transparent p-0`}
        hideHeader={embedded}
        titleAccessory={
          <span className="inline-flex flex-none items-center gap-1">
            <AgentProbeInfoPopover
              lines={agentProbeLines}
              testId="agent-gui-window-agent-info"
              className={styles.windowAgentInfo}
              onOpen={handleAgentProbeInfoOpen}
              onClose={handleAgentProbeInfoClose}
            />
            <CanvasNodeGhostIconButton
              aria-label={
                isConversationRailCollapsed
                  ? t("agentHost.agentGui.expandConversationRail")
                  : t("agentHost.agentGui.collapseConversationRail")
              }
              title={
                isConversationRailCollapsed
                  ? t("agentHost.agentGui.expandConversationRail")
                  : t("agentHost.agentGui.collapseConversationRail")
              }
              data-testid="agent-gui-toggle-conversation-rail"
              data-agent-gui-conversation-rail-collapsed={
                isConversationRailCollapsed ? "true" : "false"
              }
              data-agent-gui-conversation-rail-auto-collapsed={
                isConversationRailAutoCollapsed ? "true" : "false"
              }
              onClick={onClickProp1}
            >
              <CanvasNodePanelLinedIcon
                width={18}
                height={18}
                aria-hidden="true"
              />
            </CanvasNodeGhostIconButton>
          </span>
        }
        onClose={onClose}
        onResize={onResize}
        isMaximized={isMaximized}
        isMuted={isMuted}
        hideMaximizeButton
        onMinimize={onMinimize}
        onToggleMaximize={onToggleMaximize}
      >
        {(renderFrame) => {
          const renderedWidth = renderFrame.size.width;
          const isRenderedConversationRailCollapsed =
            isConversationRailCollapsed ||
            resolveAgentGUIConversationRailPresentation({
              autoCollapseMode: frame.conversationRailAutoCollapseMode,
              containerWidthPx: renderedWidth,
              conversationRailCollapsed: state.conversationRailCollapsed,
              conversationRailWidthPx: state.conversationRailWidthPx
            }).isCollapsed;

          return (
            <AgentGUINodeViewBinding1
              shell={shell}
              rail={rail}
              detail={detail}
              composer={composer}
              interaction={interaction}
              readiness={readiness}
              operations={operations}
              mentionAgentTargets={mentionAgentTargets}
              hiddenMentionFilterIds={hiddenMentionFilterIds}
              renderAgentTargetInfo={renderAgentTargetInfo}
              renderSidebarFooter={renderSidebarFooter}
              viewActions={viewActions}
              isActive={isActive}
              isVisible={isVisible}
              onEngagementEvent={onEngagementEvent}
              composerFocusRequestSequence={composerFocusRequestSequence}
              workbenchCommandBridge={workbenchCommandBridge}
              slashStatusLimits={slashStatusLimits}
              controllerRailStatus={controllerRailStatus}
              slashStatusLimitsUnavailable={slashStatusLimitsUnavailable}
              slashStatusOverride={slashStatusOverride}
              railStatusProvider={railStatusProvider}
              agentConfigAccountContent={agentConfigAccountContent}
              agentConfigSystemActionsContent={agentConfigSystemActionsContent}
              effectiveProviderAuthAccountLabels={
                effectiveProviderAuthAccountLabels
              }
              handleAgentConfigMenuClose={handleAgentConfigMenuClose}
              handleAgentConfigMenuOpenCallback={
                handleAgentConfigMenuOpenCallback
              }
              handleAgentUsageRefresh={handleAgentUsageRefresh}
              accountUsageRefreshInline={accountUsageRefreshInline}
              handleSlashStatusOpen={handleSlashStatusOpen}
              handleSlashStatusClose={handleSlashStatusClose}
              handleSlashStatusRefresh={handleSlashStatusRefresh}
              handleLinkActionCallback={handleLinkActionCallback}
              onHandoffConversation={onHandoffConversation}
              showHandoffTargetOwnershipLabels={
                showHandoffTargetOwnershipLabels
              }
              capabilityMenuState={capabilityMenuState}
              capabilityControlsReadOnly={capabilityControlsReadOnly}
              onCapabilitySettingsRequest={onCapabilitySettingsRequest}
              onAgentProviderLogin={onAgentProviderLogin}
              handleAgentProviderLogin={handleAgentProviderLoginCallback}
              onAgentEnvPanelOpen={onAgentEnvPanelOpen}
              isRenderedConversationRailCollapsed={
                isRenderedConversationRailCollapsed
              }
              locale={locale}
              onWorkspaceFileReferencesAdded={onWorkspaceFileReferencesAdded}
              handleWorkspaceFileReferencesAdded={
                handleWorkspaceFileReferencesAddedCallback
              }
              resolveExternalPromptEntries={resolveExternalPromptEntries}
              prepareExternalPromptFiles={prepareExternalPromptFiles}
              resolvePastedPath={resolvePastedPath}
              promptAssetLimit={promptAssetLimit}
              labels={labels}
              workspaceUserProjectI18n={workspaceUserProjectI18n}
              workspaceFileManagerI18n={workspaceFileManagerI18n}
              workspaceFileReferenceAdapter={workspaceFileReferenceAdapter}
              onOpenConversationWindow={onOpenConversationWindow}
              onRequestGitBranches={onRequestGitBranches}
              selectProjectDirectory={selectProjectDirectory}
              projectDirectorySourceAggregator={
                projectDirectorySourceAggregator
              }
              referenceSourceAggregator={referenceSourceAggregator}
              resolveWorkspaceReferenceContentErrorAction={
                resolveWorkspaceReferenceContentErrorAction
              }
              resolveWorkspaceReferenceEntryIconUrl={
                resolveWorkspaceReferenceEntryIconUrl
              }
              resolveMentionReferenceTarget={resolveMentionReferenceTarget}
              resolveWorkspaceReferenceInitialTarget={
                resolveWorkspaceReferenceInitialTarget
              }
              workspaceFileReferenceCopy={workspaceFileReferenceCopy}
              workspaceAppIcons={workspaceAppIcons}
              referenceProvenanceFilters={referenceProvenanceFilters}
              sessionInputHistoryEnabled={sessionInputHistoryEnabled}
              sessionWorktreeEnabled={sessionWorktreeEnabled}
              sessionLaunchModesByProjectSectionKey={
                sessionLaunchModesByProjectSectionKey
              }
              onSessionLaunchModePreferenceChange={
                onSessionLaunchModePreferenceChange
              }
              renderProjectDirectoryPickerHeaderActions={
                renderProjectDirectoryPickerHeaderActions
              }
              projectSelectOptions={projectSelectOptions}
              renderReferencePickerSidebarActions={
                renderReferencePickerSidebarActions
              }
              renderComposerFooterAccessory={renderComposerFooterAccessory}
              composerEmptySubmit={composerEmptySubmit}
            />
          );
        }}
      </WorkspaceNodeWindow>
    </AgentGUIMentionServiceBoundary>
  );
});

function projectProviderAccountLabel(
  labels: Partial<Record<string, string>> | undefined,
  providerValue: string | null | undefined,
  accountLabelValue: string | null | undefined
): Partial<Record<string, string>> | undefined {
  const provider = providerValue?.trim();
  const accountLabel = accountLabelValue?.trim();
  if (!provider || !accountLabel || labels?.[provider]?.trim()) {
    return labels;
  }
  return {
    ...labels,
    [provider]: accountLabel
  };
}

function resolveAgentConfigMenuContext(
  target: AgentGUIAgentTarget
): AgentGUIAgentConfigMenuContext | null {
  const agentTargetId = target.agentTargetId?.trim() || target.targetId.trim();
  const provider = target.provider.trim();
  if (!agentTargetId || !provider) {
    return null;
  }
  return {
    agentTargetId,
    provider,
    label: target.label,
    presentation: "menu",
    ...(target.ownership ? { ownership: target.ownership } : {})
  };
}

const AgentGUINodeViewBinding1 = observer(function AgentGUINodeViewBinding1({
  shell,
  rail,
  detail,
  composer,
  interaction,
  readiness,
  operations,
  mentionAgentTargets,
  hiddenMentionFilterIds,
  renderAgentTargetInfo,
  renderSidebarFooter,
  viewActions,
  isActive,
  isVisible,
  onEngagementEvent,
  composerFocusRequestSequence,
  workbenchCommandBridge,
  slashStatusLimits,
  controllerRailStatus,
  slashStatusLimitsUnavailable,
  slashStatusOverride,
  railStatusProvider,
  agentConfigAccountContent,
  agentConfigSystemActionsContent,
  effectiveProviderAuthAccountLabels,
  handleAgentConfigMenuClose,
  handleAgentConfigMenuOpenCallback,
  handleAgentUsageRefresh,
  accountUsageRefreshInline,
  handleSlashStatusOpen,
  handleSlashStatusClose,
  handleSlashStatusRefresh,
  handleLinkActionCallback,
  onHandoffConversation,
  showHandoffTargetOwnershipLabels,
  capabilityMenuState,
  capabilityControlsReadOnly,
  onCapabilitySettingsRequest,
  onAgentProviderLogin,
  handleAgentProviderLogin,
  onAgentEnvPanelOpen,
  isRenderedConversationRailCollapsed,
  locale,
  onWorkspaceFileReferencesAdded,
  handleWorkspaceFileReferencesAdded,
  resolveExternalPromptEntries,
  prepareExternalPromptFiles,
  resolvePastedPath,
  promptAssetLimit,
  labels,
  workspaceUserProjectI18n,
  workspaceFileManagerI18n,
  workspaceFileReferenceAdapter,
  onOpenConversationWindow,
  onRequestGitBranches,
  selectProjectDirectory,
  projectDirectorySourceAggregator,
  referenceSourceAggregator,
  resolveWorkspaceReferenceContentErrorAction,
  resolveWorkspaceReferenceEntryIconUrl,
  resolveMentionReferenceTarget,
  resolveWorkspaceReferenceInitialTarget,
  workspaceFileReferenceCopy,
  workspaceAppIcons,
  referenceProvenanceFilters,
  sessionInputHistoryEnabled,
  sessionWorktreeEnabled,
  sessionLaunchModesByProjectSectionKey,
  onSessionLaunchModePreferenceChange,
  renderProjectDirectoryPickerHeaderActions,
  projectSelectOptions,
  renderReferencePickerSidebarActions,
  renderComposerFooterAccessory,
  composerEmptySubmit
}: AgentGUINodeViewBinding1Props) {
  const railSlashStatusLimitsProp1 = useMemo<
    NonNullable<
      React.ComponentProps<typeof AgentGUINodeView>["railSlashStatusLimits"]
    >
  >(() => [], []);
  return (
    <AgentGUINodeView
      shell={shell}
      rail={rail}
      detail={detail}
      composer={composer}
      interaction={interaction}
      readiness={readiness}
      operations={operations}
      mentionAgentTargets={mentionAgentTargets}
      hiddenMentionFilterIds={hiddenMentionFilterIds}
      renderAgentTargetInfo={renderAgentTargetInfo}
      renderSidebarFooter={renderSidebarFooter}
      actions={viewActions}
      isActive={isActive}
      isVisible={isVisible}
      onEngagementEvent={onEngagementEvent}
      composerFocusRequestSequence={composerFocusRequestSequence}
      workbenchCommandBridge={workbenchCommandBridge}
      slashStatusLimits={slashStatusLimits}
      slashStatusLimitsLoading={controllerRailStatus?.loading ?? false}
      slashStatusLimitsUnavailable={slashStatusLimitsUnavailable}
      slashStatusOverride={slashStatusOverride}
      railConfigProvider={railStatusProvider}
      railSlashStatusLimits={
        controllerRailStatus?.limits ?? railSlashStatusLimitsProp1
      }
      slashStatusUsageCapturedAtUnixMs={
        controllerRailStatus?.capturedAtUnixMs ?? null
      }
      slashStatusUsageDidFail={controllerRailStatus?.didFail ?? false}
      slashStatusUsageErrorMessage={controllerRailStatus?.errorMessage ?? null}
      slashStatusUsageAttempted={controllerRailStatus?.attempted ?? false}
      slashStatusLimitsResolvedEmpty={
        controllerRailStatus?.resolvedEmpty ?? false
      }
      agentConfigAccountContent={agentConfigAccountContent}
      agentConfigSystemActionsContent={agentConfigSystemActionsContent}
      providerAuthAccountLabels={effectiveProviderAuthAccountLabels}
      onAgentConfigMenuClose={handleAgentConfigMenuClose}
      onAgentConfigMenuOpen={handleAgentConfigMenuOpenCallback}
      onAgentUsageRefresh={handleAgentUsageRefresh}
      accountUsageRefreshInline={accountUsageRefreshInline}
      onSlashStatusOpen={handleSlashStatusOpen}
      onSlashStatusClose={handleSlashStatusClose}
      onSlashStatusRefresh={handleSlashStatusRefresh}
      onLinkAction={handleLinkActionCallback}
      onHandoffConversation={onHandoffConversation}
      showHandoffTargetOwnershipLabels={showHandoffTargetOwnershipLabels}
      capabilityMenuState={capabilityMenuState}
      capabilityControlsReadOnly={capabilityControlsReadOnly}
      onCapabilitySettingsRequest={onCapabilitySettingsRequest}
      onAgentProviderLogin={
        onAgentProviderLogin ? handleAgentProviderLogin : undefined
      }
      onAgentEnvPanelOpen={onAgentEnvPanelOpen}
      conversationRailCollapsed={isRenderedConversationRailCollapsed}
      detailMinWidthPx={AGENT_GUI_DETAIL_MIN_WIDTH_PX}
      uiLanguage={locale}
      onWorkspaceFileReferencesAdded={
        onWorkspaceFileReferencesAdded
          ? handleWorkspaceFileReferencesAdded
          : undefined
      }
      resolveExternalPromptEntries={resolveExternalPromptEntries}
      prepareExternalPromptFiles={prepareExternalPromptFiles}
      resolvePastedPath={resolvePastedPath}
      promptAssetLimit={promptAssetLimit}
      labels={labels}
      workspaceUserProjectI18n={workspaceUserProjectI18n}
      workspaceFileManagerCopy={workspaceFileManagerI18n}
      workspaceFileReferenceAdapter={workspaceFileReferenceAdapter}
      onOpenConversationWindow={onOpenConversationWindow}
      onRequestGitBranches={onRequestGitBranches}
      selectProjectDirectory={selectProjectDirectory}
      projectDirectorySourceAggregator={projectDirectorySourceAggregator}
      referenceSourceAggregator={referenceSourceAggregator}
      resolveReferenceContentErrorAction={
        resolveWorkspaceReferenceContentErrorAction
      }
      resolveWorkspaceReferenceEntryIconUrl={
        resolveWorkspaceReferenceEntryIconUrl
      }
      resolveMentionReferenceTarget={resolveMentionReferenceTarget}
      resolveWorkspaceReferenceInitialTarget={
        resolveWorkspaceReferenceInitialTarget
      }
      workspaceFileReferenceCopy={workspaceFileReferenceCopy}
      workspaceAppIcons={workspaceAppIcons}
      referenceProvenanceFilters={referenceProvenanceFilters}
      sessionInputHistoryEnabled={sessionInputHistoryEnabled}
      sessionWorktreeEnabled={sessionWorktreeEnabled}
      sessionLaunchModesByProjectSectionKey={
        sessionLaunchModesByProjectSectionKey
      }
      onSessionLaunchModePreferenceChange={onSessionLaunchModePreferenceChange}
      renderProjectDirectoryPickerHeaderActions={
        renderProjectDirectoryPickerHeaderActions
      }
      projectSelectOptions={projectSelectOptions}
      renderReferencePickerSidebarActions={renderReferencePickerSidebarActions}
      renderComposerFooterAccessory={renderComposerFooterAccessory}
      composerEmptySubmit={composerEmptySubmit}
    />
  );
});

type AgentGUINodeViewBinding1Props = {
  shell: import("./model/agentGuiNodeTypes").AgentGUIShellViewModel;
  rail: import("../../state/RailPresentationState").RailPresentationState;
  detail: import("../../state/ConversationDetailState").ConversationDetailState;
  composer: {
    state: import("../../state/ComposerFeatureState").ComposerFeatureState;
    draftContent: import("./model/agentGuiNodeTypes").AgentComposerDraftContent;
    draftPrompt: string;
  };
  interaction: import("../../state/SessionPresentationState").SessionPresentationState;
  readiness: import("../../state/SessionPresentationState").SessionPresentationState;
  operations: {
    isDeletingConversation: boolean;
    isUserProjectMutationPending: boolean;
    pendingDeleteConversation:
      | import("./model/agentGuiConversationTypes").AgentGUIConversationSummary
      | null;
    pendingDeleteProjectConversations:
      | import("./model/agentGuiNodeTypes").AgentGUIProjectConversationDeleteTarget
      | null;
  };
  mentionAgentTargets: readonly AgentGUIAgentTarget[] | undefined;
  hiddenMentionFilterIds: readonly string[] | undefined;
  renderAgentTargetInfo:
    | import("../../types").AgentGUIAgentTargetInfoRenderer
    | undefined;
  renderSidebarFooter:
    | ((
        ctx: import("./AgentGUINodeView.publicTypes").AgentGUISidebarFooterContext
      ) => import("react").ReactNode)
    | undefined;
  viewActions: {
    createConversation: (
      options?: { projectPath?: string | null; source?: string } | undefined
    ) => void;
    updateConversationFilter: (
      filter: import("./model/agentGuiNodeTypes").AgentGUIRailViewModel["conversationFilter"]
    ) => void;
    selectConversationFilterTarget: (input: {
      provider: import("../../types").AgentGUIProvider;
      agentTargetId: string;
    }) => void;
    selectConversation: (agentSessionId: string) => void;
    submitPrompt: (
      content: import("../../shared/contracts/dto/agentSession").AgentPromptContentBlock[],
      displayPrompt?: string,
      options?: Parameters<
        import("./AgentComposer").AgentComposerProps["onSubmit"]
      >[2]
    ) => void;
    submitGuidancePrompt: (
      content: import("../../shared/contracts/dto/agentSession").AgentPromptContentBlock[],
      displayPrompt?: string,
      options?: Parameters<
        import("./AgentComposer").AgentComposerProps["onSubmit"]
      >[2]
    ) => void;
    loadOlderConversationMessages: () => void;
    showPromptImagesUnsupported: () => void;
    submitApprovalOption: (
      input: import("../../shared/agentConversation/contracts/agentConversationVM").AgentInteractionResponseInput
    ) => boolean;
    submitInteractivePrompt: (
      input: import("../../shared/agentConversation/contracts/agentConversationVM").AgentInteractionResponseInput
    ) => boolean;
    interruptCurrentTurn: (noRunningResponseMessage: string) => void;
    updateDraftContent: (
      draftContent: import("./model/agentGuiNodeTypes").AgentComposerDraft,
      sourceScopeKey?: string
    ) => void;
    updateSelectedProjectPath?: import("./AgentComposer").AgentComposerProps["onProjectPathChange"];
    updateComposerSettings: (settings: {
      model?: string | null;
      reasoningEffort?: string | null;
      planMode?: boolean;
      permissionMode?: string;
    }) => void;
    retryComposerOptions: NonNullable<
      import("./AgentComposer").AgentComposerProps["onRetryComposerOptions"]
    >;
    updatePlanIssueBudgetPreset: (
      preset: import("../../shared/agentConversation/planImplementationPresentation").PlanIssueBudgetPreset
    ) => void;
    selectHomeComposerAgentTarget: (input: {
      provider: import("../../types").AgentGUIProvider;
      agentTargetId?: string | null;
    }) => void;
    sendQueuedPromptNext: (queuedPromptId: string) => void;
    removeQueuedPrompt: (queuedPromptId: string) => void;
    editQueuedPrompt: (queuedPromptId: string) => void;
    retryActivation: () => void;
    continueInNewConversation: () => void;
    toggleConversationPinned: (agentSessionId: string, pinned: boolean) => void;
    renameConversation: (
      agentSessionId: string,
      title: string
    ) => Promise<void>;
    forkConversationThroughTurn: (
      agentSessionId: string,
      turnId: string
    ) => Promise<void>;
    openForkSourceConversation: (agentSessionId: string) => Promise<void>;
    removeProject: (path: string) => Promise<boolean>;
    toggleProjectPinned: (projectId: string, pinned: boolean) => Promise<void>;
    requestDeleteConversation: (agentSessionId: string) => void;
    cancelDeleteConversation: () => void;
    confirmDeleteConversation: () => void;
  };
  isActive: boolean;
  isVisible: boolean;
  onEngagementEvent:
    | import("./engagement/agentGUIEngagement.types").AgentGUIEngagementEventSink
    | undefined;
  composerFocusRequestSequence: number | null;
  workbenchCommandBridge:
    | import("../../workbench/commands").AgentGuiWorkbenchCommandBridge
    | null;
  slashStatusLimits: import("./AgentComposer").AgentComposerSlashStatusLimit[];
  controllerRailStatus: {
    accountLabel: string | null;
    limits: import("./AgentComposer").AgentComposerSlashStatusLimit[];
    loading: boolean;
    capturedAtUnixMs: number | null;
    didFail: boolean;
    errorMessage: string | null;
    attempted: boolean;
    resolvedEmpty: boolean;
  } | null;
  slashStatusLimitsUnavailable: boolean;
  slashStatusOverride:
    | import("./AgentComposer").AgentComposerSlashStatus
    | null;
  railStatusProvider: string | null;
  agentConfigAccountContent:
    | string
    | number
    | bigint
    | boolean
    | import("react").ReactElement<
        unknown,
        string | import("react").JSXElementConstructor<any>
      >
    | Iterable<import("react").ReactNode>
    | Promise<
        | string
        | number
        | bigint
        | boolean
        | import("react").ReactPortal
        | import("react").ReactElement<
            unknown,
            string | import("react").JSXElementConstructor<any>
          >
        | Iterable<import("react").ReactNode>
        | null
        | undefined
      >
    | null;
  agentConfigSystemActionsContent:
    | string
    | number
    | bigint
    | boolean
    | import("react").ReactElement<
        unknown,
        string | import("react").JSXElementConstructor<any>
      >
    | Iterable<import("react").ReactNode>
    | Promise<
        | string
        | number
        | bigint
        | boolean
        | import("react").ReactPortal
        | import("react").ReactElement<
            unknown,
            string | import("react").JSXElementConstructor<any>
          >
        | Iterable<import("react").ReactNode>
        | null
        | undefined
      >
    | null;
  effectiveProviderAuthAccountLabels:
    | Partial<Record<string, string>>
    | undefined;
  handleAgentConfigMenuClose: () => void;
  handleAgentConfigMenuOpenCallback: () => void;
  handleAgentUsageRefresh: (() => void) | undefined;
  accountUsageRefreshInline: boolean;
  handleSlashStatusOpen: () => void;
  handleSlashStatusClose: () => void;
  handleSlashStatusRefresh: (() => void) | undefined;
  handleLinkActionCallback: (action: WorkspaceLinkAction) => void;
  onHandoffConversation:
    | ((input: {
        agentTargetId?: string | null;
        draftPrompt: string;
        provider: import("../../types").AgentGUIProvider;
        sourceAgentSessionId: string;
        userProjectPath?: string | null;
      }) => void | Promise<void>)
    | undefined;
  showHandoffTargetOwnershipLabels: boolean;
  capabilityMenuState:
    | import("./AgentComposer").AgentComposerCapabilityMenuState
    | undefined;
  capabilityControlsReadOnly: boolean;
  onCapabilitySettingsRequest:
    | ((
        capability: import("./AgentComposer").AgentComposerCapabilitySettingsTarget
      ) => void | Promise<void>)
    | undefined;
  onAgentProviderLogin:
    | ((provider: import("../../types").AgentGUIProvider) => void)
    | undefined;
  handleAgentProviderLogin: (provider?: string | null) => void;
  onAgentEnvPanelOpen:
    | ((
        input?: import("../../shared/agentEnv/agentEnvPanelActions").OpenAgentEnvPanelInput
      ) => void)
    | undefined;
  isRenderedConversationRailCollapsed: boolean;
  locale: import("../../i18n/runtime").AgentGuiI18nLocale;
  onWorkspaceFileReferencesAdded:
    | ((input: {
        provider: import("../../types").AgentGUIProvider;
        references: readonly WorkspaceFileReference[];
      }) => void | Promise<void>)
    | undefined;
  handleWorkspaceFileReferencesAdded: (
    references: readonly WorkspaceFileReference[]
  ) => void;
  resolveExternalPromptEntries:
    | import("./model/agentExternalPromptEntries").AgentExternalPromptEntryResolver
    | null;
  prepareExternalPromptFiles:
    | import("./model/agentExternalPromptFiles").AgentExternalPromptFilePreparer
    | null;
  resolvePastedPath:
    | ((text: string) => Promise<WorkspaceFileReference | null>)
    | null;
  promptAssetLimit: number | null;
  labels: import("./AgentGUINodeView.publicTypes").AgentGUIViewLabels;
  workspaceUserProjectI18n: import("../../../../workspace/user-project/src/i18n/workspaceUserProjectI18n").WorkspaceUserProjectI18nRuntime;
  workspaceFileManagerI18n:
    | import("../../../../workspace/file-manager/src/index").WorkspaceFileManagerI18nRuntime
    | null;
  workspaceFileReferenceAdapter:
    | import("../../../../workspace/file-reference/src/contracts/index").WorkspaceFileReferenceAdapter
    | null;
  onOpenConversationWindow: ((agentSessionId: string) => void) | undefined;
  onRequestGitBranches:
    | import("./AgentComposer").AgentComposerGitBranchLoader
    | null;
  selectProjectDirectory: (() => Promise<{ path: string } | null>) | undefined;
  projectDirectorySourceAggregator:
    | import("../../../../workspace/file-reference/src/core/referenceSourceAggregator").ReferenceSourceAggregator
    | null;
  referenceSourceAggregator:
    | import("../../../../workspace/file-reference/src/core/referenceSourceAggregator").ReferenceSourceAggregator
    | null;
  resolveWorkspaceReferenceContentErrorAction:
    | ((
        error: Error
      ) =>
        | import("../../../../workspace/file-reference/src/ui/index").ReferenceSourceContentErrorAction
        | null)
    | undefined;
  resolveWorkspaceReferenceEntryIconUrl:
    | ((
        entry: import("../../../../workspace/file-manager/src/index").WorkspaceFileEntry
      ) => Promise<string | null | undefined>)
    | undefined;
  resolveMentionReferenceTarget:
    | import("./AgentGUINodeView.publicTypes").AgentMentionReferenceTargetResolver
    | null;
  resolveWorkspaceReferenceInitialTarget:
    | import("./AgentGUINodeView.publicTypes").AgentWorkspaceReferenceInitialTargetResolver
    | null;
  workspaceFileReferenceCopy: import("../../../../workspace/file-reference/src/contracts/index").WorkspaceFileReferenceCopy;
  workspaceAppIcons:
    | readonly import("../../shared/AgentMessageMarkdown").AgentMessageMarkdownWorkspaceAppIcon[]
    | undefined;
  referenceProvenanceFilters:
    | import("./AgentComposer").AgentComposerReferenceProvenanceFilters
    | null;
  sessionInputHistoryEnabled: boolean;
  sessionWorktreeEnabled: boolean;
  sessionLaunchModesByProjectSectionKey:
    | Readonly<
        Record<
          string,
          import("./model/agentSessionLaunchMode").AgentGUISessionLaunchMode
        >
      >
    | undefined;
  onSessionLaunchModePreferenceChange:
    | ((input: {
        mode: import("./model/agentSessionLaunchMode").AgentGUISessionLaunchMode;
        projectSectionKey: string;
      }) => void | Promise<void>)
    | undefined;
  renderProjectDirectoryPickerHeaderActions:
    | ((context: {
        refresh: () => void;
        selectTarget: (
          target: import("../../../../workspace/file-reference/src/contracts/referenceSource").ReferenceLocateTarget
        ) => Promise<boolean>;
        selectTargets: (
          targets: readonly import("../../../../workspace/file-reference/src/contracts/referenceSource").ReferenceLocateTarget[]
        ) => Promise<number>;
      }) => import("react").ReactNode)
    | undefined;
  projectSelectOptions:
    | import("./AgentComposerProjectMenu").AgentProjectDropdownOptions
    | undefined;
  renderReferencePickerSidebarActions:
    | ((
        context: Parameters<
          NonNullable<
            import("../../../../workspace/file-reference/src/ui/index").ReferenceSourcePickerProps["renderSidebarActions"]
          >
        >[0] & { purpose: "directory" | "reference" }
      ) => import("react").ReactNode)
    | undefined;
  renderComposerFooterAccessory:
    | import("./AgentGUINodeView").AgentGUIComposerFooterAccessoryRenderer
    | undefined;
  composerEmptySubmit:
    | import("./AgentGUINodeView").AgentGUIComposerEmptySubmit
    | null
    | undefined;
};
