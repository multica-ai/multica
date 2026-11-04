import { createPortal } from "react-dom";
import {
  Popover,
  PopoverAnchor,
  Tooltip,
  TooltipContent,
  TooltipProvider,
  TooltipTrigger
} from "@tutti-os/ui-system";
import { cn } from "../../../app/renderer/lib/utils";
import styles from "../AgentGUINode.styles";
import { AgentInteractivePromptSurface } from "../AgentInteractivePromptSurface";
import { AgentQueuedPromptPanel } from "../AgentQueuedPromptPanel";
import {
  AgentProjectDropdown,
  AgentProjectMissingStatusProbe
} from "../AgentComposerSettingsMenus";
import { AgentChromeNotice } from "../AgentSessionChrome";
import { AgentFullAccessRestoredWarning } from "../AgentFullAccessRestoredWarning";
import { AgentRichTextEditor } from "../agentRichText/AgentRichTextEditor";
import { AgentFileMentionPalette } from "../AgentFileMentionPalette";
import { AgentReferenceProvenanceFilterControl } from "../AgentReferenceProvenanceFilterControl";
import type { AgentMentionFilterId } from "../AgentMentionSearchController";
import { AgentSlashCommandPalette } from "../AgentSlashCommandPalette";
import { AgentSlashStatusPanel } from "../AgentSlashStatusPanel";
import { AgentReviewPickerPanel } from "../AgentReviewPickerPanel";
import { ComposerFloatingMenuSurface } from "../composerFloatingMenu/ComposerFloatingMenuSurface";
import type { AgentComposerViewProps } from "./AgentComposerView.types";
import {
  EMPTY_PROVIDER_SKILLS,
  EMPTY_WORKSPACE_APP_ICONS,
  SLASH_PALETTE_HEIGHT_PX,
  composerStyles
} from "./AgentComposerChrome";
import { ComposerDraftAttachments } from "./ComposerDraftAttachments";
import { pickComposerFiles } from "@multica/tutti-agent-gui-host/composer-file-picker";
import { readAgentRichTextPromptImages, supportedPromptImageMimeType } from "../agentRichText/agentRichTextPromptImages";
import { useOptionalAgentHostApi } from "../../../agentActivityHost";
import { ComposerFooter } from "./ComposerFooter";
import { AgentSessionLaunchModeSelect } from "./AgentSessionLaunchModeSelect";
import { AgentQuickPromptPopover } from "./quickPrompts/AgentQuickPromptPopover";
import {
  agentComposerDraftHasContent,
  agentComposerDraftConnectors,
  agentComposerDraftImages,
  agentComposerDraftPrompt,
  agentComposerDraftQuotes,
  updateAgentComposerDraft
} from "../model/agentComposerDraft";

export function AgentComposerView(
  input: AgentComposerViewProps
): React.JSX.Element {
  const filePickerHost = useOptionalAgentHostApi();
  const {
    provider,
    slashStatus = null,
    usage = null,
    draftContent,
    engagement,
    availableSkills = EMPTY_PROVIDER_SKILLS,
    composerSettings,
    workspaceId,
    queueStatus = "active",
    queuedPrompts,
    drainingQueuedPromptId,
    workspaceAppIcons = EMPTY_WORKSPACE_APP_ICONS,
    activePromptDisabledReason = null,
    activePromptKeyboardShortcutsEnabled = true,
    promptImagesSupported = true,
    layoutMode = "dock",
    providerSelectLabel = "",
    composerActionAccessory,
    labels,
    workspaceUserProjectI18n,
    isSubmittingPrompt,
    onSendQueuedPromptNext,
    onRemoveQueuedPrompt,
    onEditQueuedPrompt,
    onPromptImagesUnsupported,
    onRequestWorkspaceReferences,
    referenceProvenanceFilters,
    selectProjectDirectory,
    projectSelectOptions,
    userProjectApi,
    onProjectPathChange = () => {},
    onSettingsChange,
    onLinkAction,
    onSubmit,
    onProviderSelect,
    onHandoffConversation,
    onSlashStatusRefresh,
    compactSupported = null,
    hasCompactableContext = true
  } = input.props;
  const draftImages = agentComposerDraftImages(draftContent);
  const draftQuotes = agentComposerDraftQuotes(draftContent);
  const draftConnectors = agentComposerDraftConnectors(draftContent);
  const slashStatusAgentSessionId = slashStatus?.agentSessionId ?? null;
  const draftPrompt = agentComposerDraftPrompt(draftContent);
  const { availableCapabilities, slashPaletteEntries, slashQuery } =
    input.paletteCatalog;
  const { mentionPaletteFrame, mentionPaletteHeightPx, mentionPaletteStyle } =
    input.mentionFrame;
  const {
    closeReviewPicker,
    closeSlashFloatingMenu,
    closeSlashStatusPanel,
    composerControlsHardDisabled,
    permissionModeControlsDisabled,
    reviewBranchLoader,
    selectCapability,
    selectCapabilitySettings,
    selectCommand,
    selectSkill,
    settingsControlsDisabled,
    submit,
    submitCurrentPrompt,
    submitReviewCommand
  } = input.slashActions;
  const {
    handleFileMentionSuggestionChange,
    handleFileMentionKeyDown,
    handlePaletteKeyDown,
    handleMentionHighlightChange,
    navigateFileMentionHierarchy,
    navigateIntoFileMentionItem,
    selectFileMention
  } = input.mentionActions;
  const {
    expandDraftLargeTextToPrompt,
    handleDraftChange,
    handleLinkClick,
    handleOpenReferencesForEntity,
    handlePastedLargeText,
    handleWorkspaceReferencePicker,
    removeDraftImage,
    removeDraftLargeText,
    removeDraftQuotes,
    setDraftConnectorSelected
  } = input.attachments;
  const {
    composerClassName,
    effectiveHandoffLabel,
    effectiveHandoffMenuLabel,
    handoffDisabled,
    handoffMenuTargets,
    inputDisabled,
    inputShellClassName,
    isHeroLayout,
    providerMenuTargets,
    providerSelectDisabled,
    selectedProviderLabel,
    selectedProviderSwitchTarget,
    showHandoffSelect,
    showProviderSelect
  } = input.providerState;
  const { handleMentionPaletteButton, handlePastedImages } = input.focusAndDrop;
  const {
    activePromptTip,
    activePromptTipText,
    composerStyle,
    inputShellStyle,
    promptInputAreaStyle,
    showEdgeGlow,
    showHeroProjectSelector,
    showProjectMissingProbe,
    showProjectRow
  } = input.layout;
  const {
    composerActionButton,
    disabledReasonText,
    effectivePlaceholder,
    fileDropOverlay,
    promptTipNode,
    submitInteractivePromptAndDismiss,
    visibleActivePrompt,
    visibleDraftLargeTexts
  } = input.presentation;
  const composerActionNode = composerActionAccessory ? (
    <div className="inline-flex shrink-0 items-center gap-2">
      {composerActionAccessory}
      {composerActionButton}
    </div>
  ) : (
    composerActionButton
  );
  const showComposerActionInFooter =
    true;
  const showProjectSelectorInFooter =
    !isHeroLayout && input.props.showProjectSelectorInFooter === true;
  const projectSelectorNode =
    showHeroProjectSelector || showProjectSelectorInFooter ? (
      <AgentProjectDropdown
        composerSettings={composerSettings}
        i18n={workspaceUserProjectI18n}
        labels={{
          projectLocked: labels.projectLocked,
          projectMissingDescription: labels.projectMissingDescription
        }}
        selectProjectDirectory={selectProjectDirectory}
        options={projectSelectOptions}
        userProjectApi={userProjectApi}
        onDismissAutoFocus={input.onDismissProjectMenuAutoFocus}
        onProjectMissingChange={input.setIsSelectedProjectMissing}
        onProjectPathChange={onProjectPathChange}
      />
    ) : null;
  const sessionLaunchModeNode =
    input.sessionWorktreeLaunch.visible &&
    input.props.labels.sessionLaunchModeLabel &&
    input.props.labels.sessionLaunchModeLocal &&
    input.props.labels.sessionLaunchModeWorktree ? (
      <AgentSessionLaunchModeSelect
        labels={{
          launchMode: input.props.labels.sessionLaunchModeLabel,
          local: input.props.labels.sessionLaunchModeLocal,
          worktree: input.props.labels.sessionLaunchModeWorktree
        }}
        mode={input.sessionWorktreeLaunch.mode}
        onModeChange={input.sessionWorktreeLaunch.onModeChange}
      />
    ) : null;
  const projectControlsNode =
    projectSelectorNode || sessionLaunchModeNode ? (
      <div className={styles.composerProjectControls}>
        {projectSelectorNode}
        {sessionLaunchModeNode}
      </div>
    ) : null;

  return (
    <form
      ref={input.composerRef}
      className={composerClassName}
      data-testid={
        input.props.activeTurnId?.trim()
          ? "agent-gui-composer-active-turn"
          : undefined
      }
      data-agent-turn-id={input.props.activeTurnId?.trim() || undefined}
      data-fill-available-height={
        input.props.fillAvailableHeight ? "true" : undefined
      }
      data-layout={layoutMode}
      style={composerStyle}
      onSubmit={submit}
    >
      {fileDropOverlay}
      {visibleActivePrompt ? (
        <div
          className={styles.composerFloatingPrompt}
          data-testid="agent-gui-composer-floating-prompt"
        >
          <AgentInteractivePromptSurface
            prompt={visibleActivePrompt}
            conversationReturn={labels.conversationReturn}
            embedded={true}
            edgeGlow={true}
            keyboardShortcuts={activePromptKeyboardShortcutsEnabled}
            isSubmitting={isSubmittingPrompt}
            isInteractionDisabled={Boolean(activePromptDisabledReason)}
            interactionDisabledReason={activePromptDisabledReason}
            onSubmit={submitInteractivePromptAndDismiss}
            labels={{
              approvalLead: labels.approvalLead,
              fileChangeApprovalLead: labels.fileChangeApprovalLead,
              planLead: labels.planLead,
              planModes: labels.planModes,
              stayInPlan: labels.stayInPlan,
              sendFeedback: labels.sendFeedback,
              feedbackPlaceholder: labels.feedbackPlaceholder,
              previousQuestion: labels.previousQuestion,
              nextQuestion: labels.nextQuestion,
              submitAnswers: labels.submitAnswers,
              answerPlaceholder: labels.answerPlaceholder,
              waitingForAnswer: labels.waitingForAnswer,
              planImplementationLead: labels.planImplementationLead,
              planImplementationConfirm: labels.planImplementationConfirm,
              planImplementationFeedbackPlaceholder:
                labels.planImplementationFeedbackPlaceholder,
              planImplementationSend: labels.planImplementationSend,
              planImplementationSkip: labels.planImplementationSkip
            }}
          />
        </div>
      ) : null}
      {queuedPrompts.length > 0 ? (
        <div
          className={cn(
            styles.composerFloatingPrompt,
            styles.composerQueuedPromptFloating
          )}
          data-testid="agent-gui-composer-queued-prompts"
        >
          <AgentQueuedPromptPanel
            queueStatus={queueStatus}
            queuedPrompts={queuedPrompts}
            drainingQueuedPromptId={drainingQueuedPromptId}
            labels={{
              queuedLabel: labels.queuedLabel,
              queuePausedByUserLabel: labels.queuePausedByUserLabel,
              sendQueuedPromptNext: labels.sendQueuedPromptNext,
              editQueuedPrompt: labels.editQueuedPrompt,
              deleteQueuedPrompt: labels.deleteQueuedPrompt,
              queuedPromptMoreActions: labels.queuedPromptMoreActions
            }}
            onSendQueuedPromptNext={onSendQueuedPromptNext}
            onRemoveQueuedPrompt={onRemoveQueuedPrompt}
            onEditQueuedPrompt={onEditQueuedPrompt}
            agentSessionId={slashStatusAgentSessionId}
            onLinkClick={handleLinkClick}
            workspaceId={workspaceId}
            workspaceAppIcons={workspaceAppIcons}
          />
        </div>
      ) : null}
      {showProjectMissingProbe ? (
        <AgentProjectMissingStatusProbe
          composerSettings={composerSettings}
          onProjectMissingChange={input.setIsSelectedProjectMissing}
        />
      ) : null}
      <AgentFullAccessRestoredWarning
        isSettingsLoading={composerSettings.isSettingsLoading}
        permissionModeId={
          composerSettings.selectedPermissionModeValue ??
          composerSettings.draftSettings.permissionModeId
        }
        provider={provider}
        visibleOnHome={isHeroLayout}
      />
      <div
        className={cn(
          styles.composerInputGroup,
          layoutMode === "hero" && styles.composerInputGroupHero
        )}
        data-edge-glow={showEdgeGlow ? "true" : undefined}
      >
        {input.isSelectedProjectMissing ? (
          <AgentChromeNotice
            tone="danger"
            role="alert"
            testId="agent-gui-missing-project-notice"
            title={workspaceUserProjectI18n.tFirst([
              "projectSelect.projectMissingTitle"
            ])}
            description={labels.projectMissingDescription}
          />
        ) : null}
        <div
          ref={input.inputShellRef}
          className={cn(inputShellClassName, "relative")}
          data-testid="agent-gui-composer-input-shell"
          data-input-disabled={inputDisabled ? "true" : undefined}
          title={
            inputDisabled && disabledReasonText ? disabledReasonText : undefined
          }
          style={inputShellStyle}
        >
          <Popover
            open={input.showFileMentionPalette}
            onOpenChange={input.setIsPaletteOpen}
            modal={false}
          >
            <PopoverAnchor asChild>
              <div
                ref={input.promptInputAreaRef}
                className={cn(
                  "w-full min-w-0 self-start",
                  !isHeroLayout && "agent-gui-node__composer-prompt-input-area",
                  isHeroLayout &&
                    "agent-gui-node__composer-hero-prompt-input-area"
                )}
                data-has-draft-images={
                  draftImages.length > 0 ? "true" : undefined
                }
                data-has-draft-attachments={
                  draftImages.length > 0 ||
                  visibleDraftLargeTexts.length > 0 ||
                  draftQuotes.length > 0
                    ? "true"
                    : undefined
                }
                style={promptInputAreaStyle}
              >
                <ComposerDraftAttachments
                  draftImages={draftImages}
                  draftLargeTexts={visibleDraftLargeTexts}
                  draftQuotes={draftQuotes}
                  removeLabel={labels.removeMention}
                  onRemoveImage={removeDraftImage}
                  onRemoveLargeText={removeDraftLargeText}
                  onExpandLargeText={expandDraftLargeTextToPrompt}
                  onRemoveQuotes={removeDraftQuotes}
                />
                <div
                  className={cn(
                    "w-full min-w-0 self-start",
                    "agent-gui-node__composer-prompt-input-line"
                  )}
                >
                  <AgentRichTextEditor
                    ref={input.editorHandleRef}
                    value={input.paletteDraftPrompt}
                    contentScopeKey={input.props.draftScopeKey}
                    placeholder={effectivePlaceholder}
                    disabled={inputDisabled}
                    className={styles.composerTextarea}
                    testId="agent-gui-composer-editor"
                    onChange={handleDraftChange}
                    onContentLayoutInvalidated={
                      input.layout.invalidateComposerMeasurement
                    }
                    onFocus={(method) => engagement?.focused(method)}
                    onUserContentChange={(nextPrompt) => {
                      if (
                        agentComposerDraftHasContent(
                          updateAgentComposerDraft(draftContent, {
                            prompt: nextPrompt
                          })
                        )
                      ) {
                        engagement?.contentEntered({
                          contentType: "text",
                          hadPrefill: agentComposerDraftHasContent(draftContent)
                        });
                      }
                    }}
                    onSubmit={submitCurrentPrompt}
                    onSubmitGuidance={() =>
                      submitCurrentPrompt({ guidance: true })
                    }
                    availableSkills={availableSkills}
                    availableCapabilities={availableCapabilities}
                    removeMentionLabel={labels.removeMention}
                    onKeyDownForPalette={handlePaletteKeyDown}
                    onHistoryNavigation={input.onHistoryNavigation}
                    onFileMentionSuggestionChange={
                      handleFileMentionSuggestionChange
                    }
                    onFileMentionSuggestionKeyDown={handleFileMentionKeyDown}
                    onLinkClick={handleLinkClick}
                    promptImagesSupported={promptImagesSupported}
                    onPromptImagesUnsupported={onPromptImagesUnsupported}
                    onPasteImages={handlePastedImages}
                    onPasteLargeText={handlePastedLargeText}
                    onPasteFiles={
                      input.externalPromptEntriesSupported
                        ? input.addExternalPromptEntries
                        : undefined
                    }
                    onDropFiles={
                      input.externalPromptEntriesSupported
                        ? input.addExternalPromptEntries
                        : undefined
                    }
                    onResolvePastedPath={
                      input.props.resolvePastedPath ?? undefined
                    }
                  />
                  {!showComposerActionInFooter
                    ? composerActionNode
                    : null}
                </div>
              </div>
            </PopoverAnchor>
            {input.showFileMentionPalette && mentionPaletteFrame
              ? createPortal(
                  <div
                    data-testid="agent-gui-mention-palette-surface"
                    ref={input.paletteContentRef}
                    className={cn(
                      composerStyles.dropdownSurface,
                      "max-h-[320px] overflow-hidden border-[var(--line-1)] p-0"
                    )}
                    style={mentionPaletteStyle}
                  >
                    <AgentFileMentionPalette
                      state={input.mentionSearchState}
                      highlightedKey={input.mentionHighlightedKey}
                      label={labels.fileMentionPalette}
                      loadingLabel={labels.fileMentionLoading}
                      emptyLabel={labels.fileMentionEmpty}
                      errorLabel={labels.fileMentionError}
                      tabHintLabel={labels.fileMentionTabHint}
                      maxHeightPx={mentionPaletteHeightPx}
                      shouldCenterHighlightedItem={
                        input.shouldCenterMentionHighlight
                      }
                      onHighlightChange={handleMentionHighlightChange}
                      onSelectItem={selectFileMention}
                      onSelectCategory={(filter) =>
                        input.mentionControllerRef.current?.setFilter(filter)
                      }
                      onSelectFilter={(filter) =>
                        input.mentionControllerRef.current?.setFilter(filter)
                      }
                      onExpandGroup={(groupId) =>
                        input.mentionControllerRef.current?.expandGroup(groupId)
                      }
                      onNavigateHierarchy={undefined}
                      onNavigateIntoItem={navigateIntoFileMentionItem}
                      onOpenReferences={
                        onRequestWorkspaceReferences
                          ? handleOpenReferencesForEntity
                          : undefined
                      }
                      provenanceFilterControl={
                        referenceProvenanceFilters ? (
                          <AgentReferenceProvenanceFilterControl
                            filter={
                              referenceProvenanceFilters.byFilter[
                                input.mentionSearchState
                                  .filter as AgentMentionFilterId
                              ]
                            }
                          />
                        ) : undefined
                      }
                    />
                  </div>,
                  mentionPaletteFrame.portalTarget
                )
              : null}
            <ComposerFloatingMenuSurface
              anchorRef={input.inputShellRef}
              className={cn(
                composerStyles.dropdownSurface,
                "max-h-[320px] overflow-hidden border-[var(--line-1)] p-0"
              )}
              contentClassName="h-full min-h-0"
              dismissBoundaryRef={input.promptInputAreaRef}
              maxHeight={SLASH_PALETTE_HEIGHT_PX}
              onDismiss={closeSlashFloatingMenu}
              open={input.showSlashPalette}
              placement="fixed-height"
              surfaceRef={input.paletteContentRef}
              testId="agent-gui-slash-palette-surface"
            >
              <AgentSlashCommandPalette
                entries={slashPaletteEntries}
                highlightedIndex={input.activeHighlight}
                label={
                  slashQuery === null
                    ? labels.skillPickerPalette
                    : labels.slashCommandPalette
                }
                commandsGroupLabel={labels.slashPaletteCommandsGroup}
                capabilitiesGroupLabel={labels.slashPaletteCapabilitiesGroup}
                capabilitiesLoading={
                  composerSettings.isCapabilityOptionsLoading === true
                }
                capabilitiesLoadingLabel={
                  labels.slashPaletteCapabilitiesLoading
                }
                skillsGroupLabel={labels.slashPaletteSkillsGroup}
                pluginsGroupLabel={labels.slashPalettePluginsGroup}
                connectorsGroupLabel={labels.slashPaletteConnectorsGroup}
                connectorConnectedLabel={labels.slashPaletteConnectorConnected}
                connectorNotConnectedLabel={
                  labels.slashPaletteConnectorNotConnected
                }
                connectorUnsupportedLabel={
                  labels.slashPaletteConnectorUnsupported
                }
                mcpGroupLabel={labels.slashPaletteMcpGroup}
                onHighlightChange={input.setHighlightedIndex}
                onSelect={selectCommand}
                onSelectCapability={selectCapability}
                onSelectCapabilitySettings={selectCapabilitySettings}
                onSelectSkill={selectSkill}
              />
            </ComposerFloatingMenuSurface>
            <ComposerFloatingMenuSurface
              anchorRef={input.inputShellRef}
              className="border-0 p-0"
              dismissBoundaryRef={input.promptInputAreaRef}
              maxHeight={SLASH_PALETTE_HEIGHT_PX}
              onDismiss={closeSlashFloatingMenu}
              open={input.isSlashStatusPanelOpen}
              placement="dynamic-above"
              surfaceRef={input.paletteContentRef}
              testId="agent-gui-command-menu-surface"
            >
              <AgentSlashStatusPanel
                status={slashStatus}
                labels={{
                  slashStatusTitle: labels.slashStatusTitle,
                  slashStatusSession: labels.slashStatusSession,
                  slashStatusBaseUrl: labels.slashStatusBaseUrl,
                  slashStatusContext: labels.slashStatusContext,
                  slashStatusLimits: labels.slashStatusLimits,
                  slashStatusClose: labels.slashStatusClose,
                  slashStatusContextValue: labels.slashStatusContextValue,
                  slashStatusContextUnavailable:
                    labels.slashStatusContextUnavailable,
                  slashStatusLimitsUnavailable:
                    labels.slashStatusLimitsUnavailable,
                  slashStatusEmptyValue: labels.slashStatusEmptyValue,
                  slashStatusUsageJustUpdated:
                    labels.slashStatusUsageJustUpdated,
                  slashStatusUsageMinutesAgo: labels.slashStatusUsageMinutesAgo,
                  slashStatusUsageHoursAgo: labels.slashStatusUsageHoursAgo,
                  slashStatusUsageUpdating: labels.slashStatusUsageUpdating,
                  slashStatusUsageRefreshFailed:
                    labels.slashStatusUsageRefreshFailed,
                  slashStatusUsageRefreshAria:
                    labels.slashStatusUsageRefreshAria
                }}
                onClose={closeSlashStatusPanel}
                onRefresh={onSlashStatusRefresh}
              />
            </ComposerFloatingMenuSurface>
            <ComposerFloatingMenuSurface
              anchorRef={input.inputShellRef}
              className="border-0 p-0"
              dismissBoundaryRef={input.promptInputAreaRef}
              maxHeight={SLASH_PALETTE_HEIGHT_PX}
              onDismiss={closeSlashFloatingMenu}
              open={input.isReviewPickerOpen}
              placement="dynamic-above"
              surfaceRef={input.paletteContentRef}
              testId="agent-gui-command-menu-surface"
            >
              <AgentReviewPickerPanel
                labels={labels.reviewPicker}
                onRequestGitBranches={reviewBranchLoader}
                onSubmitReview={submitReviewCommand}
                onClose={closeReviewPicker}
              />
            </ComposerFloatingMenuSurface>
          </Popover>
          <ComposerFooter
            workspaceId={workspaceId}
            labels={labels}
            provider={provider}
            composerSettings={composerSettings}
            usage={usage}
            compactSupported={compactSupported}
            hasCompactableContext={hasCompactableContext}
            composerControlsHardDisabled={composerControlsHardDisabled}
            inputDisabled={inputDisabled}
            settingsControlsDisabled={settingsControlsDisabled}
            codexSaverModeDisabled={
              settingsControlsDisabled || Boolean(input.props.agentSessionId)
            }
            rtkSaverModeDisabled={
              settingsControlsDisabled || Boolean(input.props.agentSessionId)
            }
            permissionModeControlsDisabled={permissionModeControlsDisabled}
            isSendingTurn={input.props.isSendingTurn}
            showComposerAction={showComposerActionInFooter}
            isPlanModeActive={input.isPlanModeActive}
            connectorsVisible={
              input.props.capabilityMenuState?.connectors?.enabled !== false
            }
            connectorsReadOnly={
              input.props.capabilityControlsReadOnly === true ||
              input.props.capabilityMenuState?.connectors?.readOnly === true
            }
            showConnectorViewMore={
              input.props.capabilityMenuState?.connectors?.showViewMore !==
              false
            }
            onClearPlanMode={input.onClearPlanMode}
            composerAction={composerActionNode}
            projectControl={
              showProjectSelectorInFooter ? projectControlsNode : null
            }
            quickPromptControl={
              <AgentQuickPromptPopover
                controller={input.quickPromptLibrary}
                disabled={composerControlsHardDisabled || inputDisabled}
              />
            }
            footerAccessory={input.props.footerAccessory}
            showHandoffSelect={showHandoffSelect}
            handoffDisabled={handoffDisabled}
            effectiveHandoffLabel={effectiveHandoffLabel}
            effectiveHandoffMenuLabel={effectiveHandoffMenuLabel}
            handoffMenuTargets={handoffMenuTargets}
            onHandoffConversation={onHandoffConversation}
            showHandoffTargetOwnershipLabels={
              input.props.showHandoffTargetOwnershipLabels
            }
            showProviderSelect={showProviderSelect}
            selectedProviderSwitchTarget={selectedProviderSwitchTarget}
            providerSelectDisabled={providerSelectDisabled}
            providerSelectLabel={providerSelectLabel}
            selectedProviderLabel={selectedProviderLabel}
            providerMenuTargets={providerMenuTargets}
            menuViewportTopInset={input.props.menuViewportTopInset}
            onProviderSelect={onProviderSelect}
            onLinkAction={onLinkAction}
            availableSkills={availableSkills}
            selectedConnectorKeys={draftConnectors.map(
              (connector) => connector.connectorKey
            )}
            onConnectorSelected={setDraftConnectorSelected}
            onRetryComposerOptions={input.props.onRetryComposerOptions}
            onCapabilitySettingsRequest={
              input.props.onCapabilitySettingsRequest
            }
            onRequestWorkspaceReferences={onRequestWorkspaceReferences}
            onWorkspaceReferencePicker={input.externalPromptEntriesSupported ? async () => {
              try {
                const files = await pickComposerFiles();
                const images = files.filter((file) => supportedPromptImageMimeType(file.type));
                const regularFiles = files.filter((file) => !supportedPromptImageMimeType(file.type));
                if (images.length) handlePastedImages(await readAgentRichTextPromptImages(images));
                if (regularFiles.length) input.addExternalPromptEntries(regularFiles);
              } catch {
                filePickerHost?.toast?.error(document.documentElement.lang.startsWith("zh") ? "无法读取所选文件，请重新选择" : "Could not read the selected files. Please select them again.");
              }
            } : handleWorkspaceReferencePicker}
            onMentionPaletteButton={handleMentionPaletteButton}
            onSettingsChange={onSettingsChange}
            onSubmit={onSubmit}
            draftPrompt={draftPrompt}
          />
        </div>
        {showProjectRow ? (
          <div
            className={styles.composerProjectRow}
            data-project-missing={
              input.isSelectedProjectMissing ? "true" : undefined
            }
          >
            {showHeroProjectSelector ? projectControlsNode : null}
            {activePromptTip ? (
              <div
                className={styles.composerPromptTips}
                data-testid="agent-gui-prompt-tips"
              >
                {input.isPromptTipOverflowing && promptTipNode ? (
                  <TooltipProvider delayDuration={0}>
                    <Tooltip>
                      <TooltipTrigger asChild>{promptTipNode}</TooltipTrigger>
                      <TooltipContent
                        align="end"
                        className={styles.composerPromptTipTooltip}
                        side="bottom"
                      >
                        {activePromptTipText}
                      </TooltipContent>
                    </Tooltip>
                  </TooltipProvider>
                ) : (
                  promptTipNode
                )}
              </div>
            ) : null}
          </div>
        ) : null}
      </div>
    </form>
  );
}
