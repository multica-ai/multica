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
  const handleAgentProviderLogin = useCallback(
    (provider?: string | null) => {
      const resolvedProvider = normalizeAgentGUIProviderIdentity(provider);
      onAgentProviderLogin?.(
        resolvedProvider === "unknown" ? state.provider : resolvedProvider
      );
    },
    [onAgentProviderLogin, state.provider]
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
  const agentConfigAccountContent = agentConfigMenuContext
    ? (renderAgentConfigAccount?.(agentConfigMenuContext) ?? null)
    : null;
  const agentConfigSystemActionsContent =
    renderAgentConfigSystemActions?.({ presentation: "menu" }) ?? null;

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
              onClick={(event) => {
                event.stopPropagation();
                handleConversationRailToggle();
              }}
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
              railSlashStatusLimits={controllerRailStatus?.limits ?? []}
              slashStatusUsageCapturedAtUnixMs={
                controllerRailStatus?.capturedAtUnixMs ?? null
              }
              slashStatusUsageDidFail={controllerRailStatus?.didFail ?? false}
              slashStatusUsageErrorMessage={
                controllerRailStatus?.errorMessage ?? null
              }
              slashStatusUsageAttempted={
                controllerRailStatus?.attempted ?? false
              }
              slashStatusLimitsResolvedEmpty={
                controllerRailStatus?.resolvedEmpty ?? false
              }
              agentConfigAccountContent={agentConfigAccountContent}
              agentConfigSystemActionsContent={agentConfigSystemActionsContent}
              providerAuthAccountLabels={effectiveProviderAuthAccountLabels}
              onAgentConfigMenuClose={handleAgentConfigMenuClose}
              onAgentConfigMenuOpen={handleAgentConfigMenuOpen}
              onAgentUsageRefresh={handleAgentUsageRefresh}
              accountUsageRefreshInline={accountUsageRefreshInline}
              onSlashStatusOpen={handleSlashStatusOpen}
              onSlashStatusClose={handleSlashStatusClose}
              onSlashStatusRefresh={handleSlashStatusRefresh}
              onLinkAction={handleLinkAction}
              onHandoffConversation={onHandoffConversation}
              showHandoffTargetOwnershipLabels={
                showHandoffTargetOwnershipLabels
              }
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
              projectDirectorySourceAggregator={
                projectDirectorySourceAggregator
              }
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
