import { repairMentionPaletteHighlight } from "@tutti-os/ui-rich-text/at-panel";
import { observer } from "mobx-react-lite";
import { useCallback, useEffect, useRef, useState } from "react";
import { useOptionalConversationServices } from "../../conversationServices";
import { useActiveConversationState } from "../../state/useActiveConversationState";
import {
  agentMentionItemKey,
  isAgentMentionItemDisabled
} from "./AgentFileMentionPalette";
import { type AgentFileMentionSuggestionState } from "./agentRichText/agentFileMentionExtension";
import type { AgentRichTextEditorHandle } from "./agentRichText/AgentRichTextEditor";
import { formatSlashStatusTokenCount } from "./AgentSlashStatusPanel";
import type { AgentComposerProps } from "./composer/AgentComposer.types";
import {
  EMPTY_PROMPT_TIPS,
  EMPTY_PROVIDER_SKILLS
} from "./composer/AgentComposerChrome";
import { AgentComposerView } from "./composer/AgentComposerView";
import { useAgentQuickPromptLibrary } from "./composer/quickPrompts/useAgentQuickPromptLibrary";
import { useAgentMentionSearchController } from "./composer/useAgentMentionSearchController";
import { useComposerDraftAttachmentsWithConnectors } from "./composer/useComposerDraftAttachmentsWithConnectors";
import { useComposerDraftCapabilitiesRequest } from "./composer/useComposerDraftCapabilitiesRequest";
import { useComposerFocusAndDrop } from "./composer/useComposerFocusAndDrop";
import { useComposerInputHistory } from "./composer/useComposerInputHistory";
import {
  INITIAL_DOCK_COMPOSER_METRICS,
  useComposerLayout
} from "./composer/useComposerLayout";
import { useComposerMentionActions } from "./composer/useComposerMentionActions";
import { useComposerPaletteCatalog } from "./composer/useComposerPaletteCatalog";
import { useComposerPresentation } from "./composer/useComposerPresentation";
import { useComposerProviderTargets } from "./composer/useComposerProviderTargets";
import { useComposerSlashActions } from "./composer/useComposerSlashActions";
import { refreshComposerSlashCapabilities } from "./composer/useComposerSlashCapabilitiesRefresh";
import { useMentionPaletteFrame } from "./composer/useMentionPaletteFrame";
import { useScopedProjectMissingState } from "./composer/useScopedProjectMissingState";
import { useSessionWorktreeLaunch } from "./composer/useSessionWorktreeLaunch";
import type { AgentGUIComposerContentType } from "./engagement/agentGUIEngagement.types";
import {
  agentComposerDraftFiles,
  agentComposerDraftHasContent,
  agentComposerDraftImages,
  agentComposerDraftLargeTexts,
  agentComposerDraftPrompt,
  agentComposerDraftQuotes
} from "./model/agentComposerDraft";
import { agentComposerDraftAttachmentProjection } from "./model/agentComposerDraftAttachmentProjection";
import {
  groupAgentExternalPromptEntryInsertions,
  resolveAgentExternalPromptEntries
} from "./model/agentExternalPromptEntries";
import { projectAgentGUIComposerGateControls } from "./model/agentGuiComposerGate";
import type {
  AgentComposerDraft,
  AgentComposerDraftFile,
  AgentComposerDraftImage,
  AgentComposerDraftLargeText
} from "./model/agentGuiNodeTypes";
import { clampSlashCommandHighlight } from "./model/agentSlashCommands";

export { formatSlashStatusTokenCount };

/**
 * 引用 picker 的确认结果:松散文件按 file mention 插入;mentionItems(如文件夹 bundle)
 * 作为整体节点插入。两者各走各的插入路径,composer 不需要理解 bundle 内部结构。
 */
export type {
  AgentComposerCapabilityMenuState,
  AgentComposerCapabilitySettingsTarget,
  AgentComposerComputerUseAuthorizationState,
  AgentComposerGitBranches,
  AgentComposerGitBranchLoader,
  AgentComposerPromptTip,
  AgentComposerProps,
  AgentComposerReferenceProvenanceFilter,
  AgentComposerReferenceProvenanceFilters,
  AgentComposerSlashStatus,
  AgentComposerSlashStatusLimit,
  AgentComposerSubmitOptions,
  AgentComposerUsage
} from "./composer/AgentComposer.types";
export type { WorkspaceReferencePickResult } from "./composer/useComposerDraftAttachments";

const EMPTY_HIDDEN_MENTION_FILTER_IDS: readonly string[] = [];

export const AgentComposer = observer(function AgentComposer(
  inputProps: AgentComposerProps
): React.JSX.Element {
  const nativeState = useActiveConversationState(
    inputProps.agentSessionId ?? null
  );
  const state = inputProps.state;
  const props = {
    ...inputProps,
    availableCommands: state.availableCommands,
    availableSkills: state.availableSkills,
    hasCompactableContext: state.hasCompactableContext,
    compactSupported: state.compactSupported,
    gate: state.gate,
    composerSettings: state.composerSettings,
    usage: state.usage,
    promptImagesSupported: state.promptImagesSupported,
    isSendingTurn: state.isSendingTurn,
    isSubmittingPrompt: state.isSubmittingPrompt,
    activeTurnId: state.activeTurnId
  };
  const isInterrupting =
    nativeState.isInterrupting || nativeState.isCancelPending;
  const {
    workspaceId,
    agentSessionId = null,
    workspacePath,
    currentUserId,
    provider,
    slashStatus = null,
    draftContent,
    engagement,
    draftScopeKey = "current",
    inputHistory = [],
    inputHistoryHasOlderPage = false,
    inputHistoryIsLoadingOlderPage = false,
    onRequestOlderInputHistoryPage,
    availableCommands,
    hasCompactableContext = true,
    compactSupported = null,
    availableSkills = EMPTY_PROVIDER_SKILLS,
    gate,
    presentationEditorDisabled,
    disabledReason,
    presentationSubmitDisabled,
    placeholder,
    composerSettings,
    selectedAgentTarget = null,
    agentTargets = [],
    handoffAgentTargets,
    providerSelectReadonly = false,
    onHandoffConversation,
    showStopButton,
    activeTurnId = null,
    draftOverridesStopButton = false,
    stopDisabled,
    activePrompt,
    promptTips = EMPTY_PROMPT_TIPS,
    isSendingTurn,
    isSubmittingPrompt,
    projectMissingProbeEnabled = true,
    uiLanguage = "en",
    isActive = true,
    workspaceReferencePickerOpen = false,
    promptImagesSupported = true,
    canUploadAttachment = true,
    composerFocusRequestSequence = null,
    layoutMode = "dock",
    menuViewportTopInset = 8,
    handoffLabel,
    handoffMenuLabel,
    labels,
    onDraftContentChange,
    onSettingsChange,
    capabilityMenuState,
    capabilityControlsReadOnly = false,
    onSubmit,
    onSubmitEmpty,
    emptySubmitLabel,
    onSubmitGuidance,
    onInterruptCurrentTurn,
    onPromptImagesUnsupported,
    onSubmitInteractivePrompt,
    onCapabilitySettingsRequest,
    onRetryComposerOptions,
    onSlashStatusOpen,
    onLinkAction,
    onRequestWorkspaceReferences = null,
    resolveExternalPromptEntries = null,
    prepareExternalPromptFiles = null,
    promptAssetLimit = null,
    onRequestGitBranches = null,
    referenceProvenanceFilters = null,
    hiddenMentionFilterIds = EMPTY_HIDDEN_MENTION_FILTER_IDS
  } = props;
  const slashCapabilitiesRefreshedSessionRef = useRef<string | null>(null);
  const handleDraftContentChange = useComposerDraftCapabilitiesRequest({
    agentSessionId,
    onDraftContentChange,
    onRetryComposerOptions,
    provider
  });
  const {
    canQueueWhileBusy,
    editorDisabled: disabled,
    submissionDisabled: submitDisabled
  } = projectAgentGUIComposerGateControls({
    gate,
    presentationEditorDisabled,
    presentationSubmitDisabled
  });
  const draftPrompt = agentComposerDraftPrompt(draftContent);
  const draftQuoteCount = agentComposerDraftQuotes(draftContent).length;
  const {
    images: draftImages,
    files: draftFiles,
    largeTexts: draftLargeTexts
  } = agentComposerDraftAttachmentProjection(draftContent);
  const agentActivityRuntime = useOptionalConversationServices();
  const promptFilesSupported = Boolean(
    canUploadAttachment && prepareExternalPromptFiles
  );
  const externalPromptEntriesSupported = Boolean(
    resolveExternalPromptEntries || promptFilesSupported
  );
  const pastedTextStagingSupported = Boolean(
    canUploadAttachment && agentActivityRuntime?.attachments?.stagePastedText
  );
  const reportContentEntered = (
    contentType: AgentGUIComposerContentType
  ): void => {
    engagement?.contentEntered({
      contentType,
      hadPrefill: agentComposerDraftHasContent(draftContent)
    });
  };
  const [isPaletteOpen, setIsPaletteOpen] = useState(true);
  const [isReviewPickerOpen, setIsReviewPickerOpen] = useState(false);
  const submitGuidanceWithComposerModifiers: NonNullable<
    AgentComposerProps["onSubmitGuidance"]
  > = (content, displayPrompt) => {
    onSubmitGuidance?.(content, displayPrompt);
  };
  const [highlightedIndex, setHighlightedIndex] = useState(0);
  const [mentionHighlightedKey, setMentionHighlightedKey] = useState<
    string | null
  >(null);
  const [shouldCenterMentionHighlight, setShouldCenterMentionHighlight] =
    useState(false);
  const [
    shouldResetMentionHighlightToFilter,
    setShouldResetMentionHighlightToFilter
  ] = useState(false);
  const [paletteDraftPrompt, setPaletteDraftPrompt] = useState(draftPrompt);
  const [fileMentionSuggestion, setFileMentionSuggestion] =
    useState<AgentFileMentionSuggestionState | null>(null);
  const selectedProjectPath =
    composerSettings.selectedProjectPath?.trim() ?? "";
  const [isSelectedProjectMissing, setIsSelectedProjectMissing] =
    useScopedProjectMissingState(selectedProjectPath);
  const [isSlashStatusPanelOpen, setIsSlashStatusPanelOpen] = useState(false);
  const selectedProjectSectionKey =
    composerSettings.selectedProjectSectionKey?.trim() ?? "";
  const sessionWorktreeLaunch = useSessionWorktreeLaunch({
    agentSessionId: props.agentSessionId,
    enabled:
      props.sessionWorktreeEnabled &&
      Boolean(
        props.labels.sessionLaunchModeLabel &&
          props.labels.sessionLaunchModeLocal &&
          props.labels.sessionLaunchModeWorktree
      ),
    mode: props.sessionLaunchMode,
    onModeChange: props.onSessionLaunchModeChange,
    projectSectionKey: selectedProjectSectionKey,
    selectedAgentTarget: props.selectedAgentTarget,
    selectedProjectPath
  });
  const submitWithComposerModifiers: AgentComposerProps["onSubmit"] = (
    content,
    displayPrompt,
    options
  ) => {
    onSubmit(
      content,
      displayPrompt,
      sessionWorktreeLaunch.mode === "worktree"
        ? { ...options, isolation: "worktree" }
        : options
    );
  };
  const slashStatusAgentSessionId = slashStatus?.agentSessionId ?? null;
  const previousSlashStatusAgentSessionIdRef = useRef<string | null>(
    slashStatusAgentSessionId
  );
  const previousSelectedProjectPathRef = useRef(selectedProjectPath);
  const composerRef = useRef<HTMLFormElement | null>(null);
  const inputShellRef = useRef<HTMLDivElement | null>(null);
  const promptInputAreaRef = useRef<HTMLDivElement | null>(null);
  const paletteContentRef = useRef<HTMLDivElement | null>(null);
  const draftPromptRef = useRef(draftPrompt);
  const draftImagesRef = useRef<AgentComposerDraftImage[]>(draftImages);
  const draftFilesRef = useRef<AgentComposerDraftFile[]>(draftFiles);
  const draftLargeTextsRef =
    useRef<AgentComposerDraftLargeText[]>(draftLargeTexts);
  const draftByScopeKeyRef = useRef<Record<string, AgentComposerDraft>>({
    [draftScopeKey]: draftContent
  });
  draftByScopeKeyRef.current[draftScopeKey] = draftContent;
  const {
    disposeInputHistory,
    onHistoryNavigation,
    settlePendingInputHistory
  } = useComposerInputHistory({
    agentSessionId,
    currentDraft: draftContent,
    draftByScopeKeyRef,
    draftScopeKey,
    entries: inputHistory,
    hasOlderPage: inputHistoryHasOlderPage,
    isLoadingOlderPage: inputHistoryIsLoadingOlderPage,
    onDraftContentChange: handleDraftContentChange,
    onRequestOlderPage: onRequestOlderInputHistoryPage,
    runtime: agentActivityRuntime,
    workspaceId
  });
  const promptTipRef = useRef<HTMLSpanElement | null>(null);
  const { mentionControllerRef, mentionSearchState } =
    useAgentMentionSearchController(
      referenceProvenanceFilters,
      hiddenMentionFilterIds
    );
  const editorHandleRef = useRef<AgentRichTextEditorHandle | null>(null);
  const wasActiveRef = useRef(isActive);
  const lastComposerFocusRequestRef = useRef<number | null>(null);
  const autoMentionHighlightedKeyRef = useRef<string | null>(null);
  const [isPromptTipOverflowing, setIsPromptTipOverflowing] = useState(false);
  const [dockComposerMetrics, setDockComposerMetrics] = useState(
    INITIAL_DOCK_COMPOSER_METRICS
  );
  const paletteCatalog = useComposerPaletteCatalog({
    provider,
    paletteDraftPrompt,
    availableCommands,
    availableSkills,
    hasCompactableContext,
    compactSupported,
    composerSettings,
    capabilityMenuState,
    capabilityControlsReadOnly,
    labels,
    uiLanguage,
    editorHandleRef
  });
  const {
    computerExecutable,
    filteredSkills,
    resolvedSlashCommands,
    skillQueryMatch,
    slashPaletteEntries,
    slashQuery,
    slashCommandPolicy,
    promptBeforeSelection
  } = paletteCatalog;
  const showFileMentionPalette =
    !disabled && isActive && isPaletteOpen && fileMentionSuggestion !== null;
  const showSlashPalette =
    !showFileMentionPalette &&
    !disabled &&
    isActive &&
    isPaletteOpen &&
    ((slashQuery !== null &&
      (slashPaletteEntries.length > 0 ||
        composerSettings.isCapabilityOptionsLoading === true)) ||
      (slashQuery === null &&
        skillQueryMatch !== null &&
        filteredSkills.length > 0));
  const showPalette = showFileMentionPalette || showSlashPalette;
  const showCommandMenuPanel = isSlashStatusPanelOpen || isReviewPickerOpen;
  const showFloatingCommandMenu = showSlashPalette || showCommandMenuPanel;
  const activeHighlight = clampSlashCommandHighlight(
    highlightedIndex,
    slashPaletteEntries.length
  );
  const mentionFrame = useMentionPaletteFrame(
    inputShellRef,
    showFileMentionPalette,
    menuViewportTopInset
  );

  useEffect(() => {
    setHighlightedIndex(0);
    refreshComposerSlashCapabilities({
      agentSessionId,
      isPaletteOpen,
      onRetryComposerOptions,
      refreshedSessionRef: slashCapabilitiesRefreshedSessionRef,
      slashQuery
    });
  }, [
    agentSessionId,
    isPaletteOpen,
    onRetryComposerOptions,
    skillQueryMatch?.prefix,
    skillQueryMatch?.query,
    slashQuery
  ]);

  useEffect(() => {
    const preferredKey =
      shouldResetMentionHighlightToFilter &&
      mentionSearchState.mode === "browse"
        ? `category:${mentionSearchState.filter}`
        : null;
    if (shouldResetMentionHighlightToFilter) {
      const nextKey = repairMentionPaletteHighlight({
        state: mentionSearchState,
        currentKey: null,
        preferredKey,
        getItemKey: agentMentionItemKey,
        isItemDisabled: isAgentMentionItemDisabled
      });
      autoMentionHighlightedKeyRef.current = nextKey;
      setMentionHighlightedKey(nextKey);
      setShouldResetMentionHighlightToFilter(false);
      return;
    }
    setMentionHighlightedKey((current) => {
      const nextKey = repairMentionPaletteHighlight({
        state: mentionSearchState,
        currentKey: current,
        getItemKey: agentMentionItemKey,
        isItemDisabled: isAgentMentionItemDisabled
      });
      if (
        nextKey === current &&
        current !== autoMentionHighlightedKeyRef.current
      ) {
        return current;
      }
      autoMentionHighlightedKeyRef.current = nextKey;
      return nextKey;
    });
  }, [
    mentionSearchState.filter,
    mentionSearchState.mode,
    mentionSearchState,
    shouldResetMentionHighlightToFilter
  ]);

  useEffect(() => {
    draftImagesRef.current = agentComposerDraftImages(draftContent);
    draftFilesRef.current = agentComposerDraftFiles(draftContent);
    draftLargeTextsRef.current = agentComposerDraftLargeTexts(draftContent);
    const isExternalDraftReplacement = draftPromptRef.current !== draftPrompt;
    draftPromptRef.current = draftPrompt;
    setPaletteDraftPrompt(draftPrompt);
    settlePendingInputHistory();
    if (isExternalDraftReplacement && draftPrompt) {
      window.requestAnimationFrame(() => {
        window.requestAnimationFrame(() => {
          // Prefer end so continued typing (and shared home drafts after a
          // project switch) keep the caret after the text, not at the start.
          editorHandleRef.current?.focusAtEnd();
        });
      });
    }
  }, [draftContent, draftPrompt, settlePendingInputHistory]);

  useEffect(() => {
    if (
      previousSlashStatusAgentSessionIdRef.current === slashStatusAgentSessionId
    ) {
      return disposeInputHistory;
    }
    previousSlashStatusAgentSessionIdRef.current = slashStatusAgentSessionId;
    setIsSlashStatusPanelOpen(false);
    return disposeInputHistory;
  }, [disposeInputHistory, draftScopeKey, slashStatusAgentSessionId]);

  const slashActions = useComposerSlashActions({
    workspaceId,
    provider,
    disabled,
    submitDisabled,
    canQueueWhileBusy,
    isSendingTurn,
    isSubmittingPrompt,
    showStopButton,
    promptImagesSupported: canUploadAttachment && promptImagesSupported,
    availableSkills,
    composerSettings,
    computerExecutable,
    // Fail closed like other unsupported host capabilities — do not treat a
    // missing flag as enabled.
    capabilityControlsReadOnly,
    onDraftContentChange: handleDraftContentChange,
    onSettingsChange,
    onSubmit: submitWithComposerModifiers,
    onSubmitEmpty,
    onSubmitGuidance: submitGuidanceWithComposerModifiers,
    onCapabilitySettingsRequest,
    onSlashStatusOpen,
    onPromptImagesUnsupported,
    onRequestGitBranches,
    draftContent,
    selectedProjectPath,
    slashStatusAgentSessionId,
    isSlashStatusPanelOpen,
    slashCommandPolicy,
    skillQueryMatch,
    promptBeforeSelection,
    resolvedSlashCommands,
    slashPaletteEntries,
    activeHighlight,
    showSlashPalette,
    showCommandMenuPanel,
    isSelectedProjectMissing,
    editorHandleRef,
    draftPromptRef,
    draftImagesRef,
    draftFilesRef,
    draftLargeTextsRef,
    setPaletteDraftPrompt,
    setIsPaletteOpen,
    setIsReviewPickerOpen,
    setIsSlashStatusPanelOpen,
    setHighlightedIndex
  });
  const {
    composerControlsHardDisabled,
    handleSlashCommandMenuKeyDown,
    handleSlashPaletteKeyDown
  } = slashActions;
  const mentionActions = useComposerMentionActions({
    workspaceId,
    currentUserId,
    selectedProjectPath,
    selectedProjectSectionKey,
    draftContent,
    fileMentionSuggestion,
    setFileMentionSuggestion,
    mentionControllerRef,
    editorHandleRef,
    draftPromptRef,
    setPaletteDraftPrompt,
    setIsPaletteOpen,
    onDraftContentChange: handleDraftContentChange,
    showFileMentionPalette,
    mentionHighlightedKey,
    mentionSearchState,
    setMentionHighlightedKey,
    setShouldCenterMentionHighlight,
    setShouldResetMentionHighlightToFilter,
    autoMentionHighlightedKeyRef,
    composerSettings,
    isSendingTurn,
    isSubmittingPrompt,
    showStopButton,
    isActive,
    onSettingsChange,
    handleSlashPaletteKeyDown,
    handleSlashCommandMenuKeyDown,
    showPalette,
    workspaceReferencePickerOpen,
    composerRef,
    paletteContentRef,
    shouldCenterMentionHighlight
  });
  const { clearActiveFileMentionTrigger } = mentionActions;

  const attachments = useComposerDraftAttachmentsWithConnectors({
    workspaceId,
    workspacePath,
    draftContent,
    draftScopeKey,
    draftByScopeKeyRef,
    promptImagesSupported: canUploadAttachment && promptImagesSupported,
    promptFilesSupported,
    promptAssetLimit,
    pastedTextStagingSupported,
    editorHandleRef,
    draftPromptRef,
    draftImagesRef,
    draftFilesRef,
    draftLargeTextsRef,
    setPaletteDraftPrompt,
    setIsPaletteOpen,
    clearActiveFileMentionTrigger,
    onDraftContentChange: handleDraftContentChange,
    onPromptImagesUnsupported,
    onContentEntered: reportContentEntered,
    onRequestWorkspaceReferences,
    prepareExternalPromptFiles,
    onLinkAction
  });
  const { addDraftFiles, addDraftImages } = attachments;
  const addExternalPromptEntries = useCallback(
    (files: readonly File[]): void => {
      const entries = resolveAgentExternalPromptEntries(
        files,
        resolveExternalPromptEntries
      );
      for (const insertion of groupAgentExternalPromptEntryInsertions(
        entries
      )) {
        if (insertion.disposition === "prepare") {
          addDraftFiles(insertion.files);
        } else {
          editorHandleRef.current?.insertWorkspaceReferences([
            insertion.reference
          ]);
        }
      }
    },
    [addDraftFiles, editorHandleRef, resolveExternalPromptEntries]
  );

  const providerState = useComposerProviderTargets({
    layoutMode,
    provider,
    agentTargets,
    handoffAgentTargets,
    selectedAgentTarget,
    providerSelectReadonly,
    composerControlsHardDisabled,
    isSelectedProjectMissing,
    disabled,
    onHandoffConversation,
    handoffLabel,
    handoffMenuLabel,
    defaultHandoffLabel: labels.handoffConversation,
    defaultHandoffMenuLabel: labels.handoffConversationMenu
  });
  const { inputDisabled, isHeroLayout } = providerState;
  const closeQuickPromptCompetingDisclosure = useCallback((): void => {
    mentionActions.closeFileMentionPalette();
    slashActions.closeSlashFloatingMenu();
  }, [mentionActions, slashActions]);
  const insertQuickPrompt = useCallback((content: string): boolean => {
    return editorHandleRef.current?.insertPlainTextAtSelection(content) != null;
  }, []);
  const quickPromptLibrary = useAgentQuickPromptLibrary({
    disabled: composerControlsHardDisabled || inputDisabled,
    labels: labels.quickPrompts,
    onBeforeOpen: closeQuickPromptCompetingDisclosure,
    onInsertPrompt: insertQuickPrompt,
    onQuickPromptPanelOpened: () => engagement?.quickPromptPanelOpened?.(),
    onQuickPromptUsed: (promptType) => engagement?.quickPromptUsed?.(promptType)
  });
  const restoreComposerCaretAfterProjectMenu = (event: Event): void => {
    event.preventDefault();
    if (inputDisabled) {
      return;
    }
    editorHandleRef.current?.focusAtEnd();
  };
  const focusAndDrop = useComposerFocusAndDrop({
    composerControlsHardDisabled,
    inputDisabled,
    editorHandleRef,
    composerRef,
    wasActiveRef,
    lastComposerFocusRequestRef,
    isActive,
    composerFocusRequestSequence,
    promptFilesSupported: externalPromptEntriesSupported,
    promptImagesSupported: canUploadAttachment && promptImagesSupported,
    addDraftImages,
    addDraftFiles: addExternalPromptEntries,
    onPromptImagesUnsupported
  });
  const { fileDropOverlayActive, fileDropOverlayHost } = focusAndDrop;
  const layout = useComposerLayout({
    isActive,
    isDockLayout: layoutMode === "dock",
    isHeroLayout,
    inputDisabled,
    projectMissingProbeEnabled,
    showFileMentionPalette,
    showFloatingCommandMenu,
    promptTips,
    promptTipsPrefix: labels.promptTipsPrefix,
    composerSettings,
    selectedProjectPath,
    promptTipRef,
    promptInputAreaRef,
    setIsPromptTipOverflowing,
    dockComposerMetrics,
    setDockComposerMetrics,
    draftImages,
    draftLargeTexts,
    draftQuoteCount
  });
  const { activePromptTip, promptTipStyle, rotatingPromptTips } = layout;
  const presentation = useComposerPresentation({
    isInterrupting,
    draftContent,
    canQueueWhileBusy,
    showStopButton,
    activeTurnId,
    draftOverridesStopButton,
    stopDisabled,
    isSendingTurn,
    activePrompt,
    disabledReason,
    placeholder,
    selectedProjectPath,
    selectedProjectSectionKey,
    previousSelectedProjectPathRef,
    setIsSelectedProjectMissing,
    fileMentionSuggestion,
    mentionControllerRef,
    workspaceId,
    currentUserId,
    onSubmitInteractivePrompt,
    onInterruptCurrentTurn,
    isSelectedProjectMissing,
    submitDisabled,
    allowEmptySubmit: onSubmitEmpty !== undefined,
    emptySubmitLabel,
    labels,
    activePromptTip,
    promptTipRef,
    promptTips,
    promptTipStyle,
    rotatingPromptTips,
    fileDropOverlayHost,
    fileDropOverlayActive,
    canUploadAttachment,
    promptImagesSupported
  });
  return (
    <AgentComposerView
      props={props}
      paletteCatalog={paletteCatalog}
      mentionFrame={mentionFrame}
      slashActions={slashActions}
      mentionActions={mentionActions}
      attachments={attachments}
      providerState={providerState}
      focusAndDrop={focusAndDrop}
      layout={layout}
      presentation={presentation}
      composerRef={composerRef}
      inputShellRef={inputShellRef}
      promptInputAreaRef={promptInputAreaRef}
      paletteContentRef={paletteContentRef}
      promptTipRef={promptTipRef}
      editorHandleRef={editorHandleRef}
      mentionControllerRef={mentionControllerRef}
      externalPromptEntriesSupported={externalPromptEntriesSupported}
      addExternalPromptEntries={addExternalPromptEntries}
      onDismissProjectMenuAutoFocus={restoreComposerCaretAfterProjectMenu}
      paletteDraftPrompt={paletteDraftPrompt}
      showFileMentionPalette={showFileMentionPalette}
      showSlashPalette={showSlashPalette}
      activeHighlight={activeHighlight}
      mentionSearchState={mentionSearchState}
      quickPromptLibrary={quickPromptLibrary}
      mentionHighlightedKey={mentionHighlightedKey}
      shouldCenterMentionHighlight={shouldCenterMentionHighlight}
      isSlashStatusPanelOpen={isSlashStatusPanelOpen}
      isReviewPickerOpen={isReviewPickerOpen}
      isSelectedProjectMissing={isSelectedProjectMissing}
      setIsSelectedProjectMissing={setIsSelectedProjectMissing}
      setIsPaletteOpen={setIsPaletteOpen}
      setHighlightedIndex={setHighlightedIndex}
      isPlanModeActive={composerSettings.draftSettings.planMode}
      onClearPlanMode={() => onSettingsChange({ planMode: false })}
      isPromptTipOverflowing={isPromptTipOverflowing}
      onHistoryNavigation={onHistoryNavigation}
      sessionWorktreeLaunch={sessionWorktreeLaunch}
    />
  );
});
