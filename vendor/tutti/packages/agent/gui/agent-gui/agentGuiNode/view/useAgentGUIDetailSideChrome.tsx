import { useCallback, useMemo, type ReactNode } from "react";
import type { AgentGUISideConversationPresentation } from "../../../agentSideConversationPresentation";
import type { AgentComposerProps } from "../AgentComposer";
import type { AgentGUIDetailPaneProps } from "./AgentGUINodeView.types";
import type { useAgentGUIDetailSideConversation } from "./useAgentGUIDetailSideConversation";
import {
  projectAgentSideCapabilityMenuState,
  projectAgentSideComposerGate,
  projectAgentSideComposerSettings
} from "../model/agentGuiSideComposerPolicy";
import { useTranslation } from "../../../i18n/index";
import {
  AgentGUISideConversationPane,
  type AgentGUISideConversationPaneProps,
  type AgentGUISideConversationSurfaceProps
} from "./AgentGUISideConversationPane";
import { appendAgentComposerDraftQuote } from "../model/agentComposerDraft";
import { AgentGUISideConversationPresentationPublisher } from "./AgentGUISideConversationPresentationPublisher";

const EMPTY_WORKSPACE_APP_ICONS: NonNullable<
  AgentComposerProps["workspaceAppIcons"]
> = [];

interface UseAgentGUIDetailSideChromeInput {
  availableSkills: AgentGUISideConversationPaneProps["availableSkills"];
  baseComposerProps: AgentComposerProps;
  controller: ReturnType<typeof useAgentGUIDetailSideConversation>;
  conversationFlowLabels: AgentGUISideConversationPaneProps["conversationFlowLabels"];
  isVisible: boolean;
  presentation?: AgentGUISideConversationPresentation | null;
  textSelectionActionsEnabled: boolean;
  onRequestComposerFocus: () => void;
  renderComposerFooterAccessory: AgentGUIDetailPaneProps["renderComposerFooterAccessory"];
}

export function useAgentGUIDetailSideChrome({
  availableSkills,
  baseComposerProps,
  controller,
  conversationFlowLabels,
  isVisible,
  presentation = null,
  textSelectionActionsEnabled,
  onRequestComposerFocus,
  renderComposerFooterAccessory
}: UseAgentGUIDetailSideChromeInput): {
  bottomDockComposerProps: AgentComposerProps;
  selectionProps: {
    onAddSelectionToConversation: (text: string) => void;
    onAskSelectionInSide?: (text: string) => void;
    textSelectionActionsEnabled: boolean;
  };
  sidePane: ReactNode;
} {
  const { t } = useTranslation();
  const {
    active,
    close,
    draftContent,
    entryError,
    focused,
    focusRequestSequence,
    interactionSubmitting,
    interactivePrompt,
    interrupt,
    setDraftContent,
    setFocused,
    sourceAgentSessionId,
    stageSelection,
    submitInteraction,
    submitSide
  } = controller;
  const hostFooterAccessory = useMemo(
    () =>
      renderComposerFooterAccessory?.({
        agentSessionId: baseComposerProps.agentSessionId,
        draftContent: baseComposerProps.draftContent,
        onDraftContentChange: baseComposerProps.onDraftContentChange,
        onProjectPathChange: baseComposerProps.onProjectPathChange,
        isActive: baseComposerProps.isActive,
        isSendingTurn: baseComposerProps.isSendingTurn,
        isSubmittingPrompt: baseComposerProps.isSubmittingPrompt,
        composerSettings: baseComposerProps.composerSettings,
        selectedAgentTarget: baseComposerProps.selectedAgentTarget
      }) ?? null,
    [
      baseComposerProps.agentSessionId,
      baseComposerProps.draftContent,
      baseComposerProps.onDraftContentChange,
      baseComposerProps.onProjectPathChange,
      baseComposerProps.composerSettings,
      baseComposerProps.isActive,
      baseComposerProps.isSendingTurn,
      baseComposerProps.isSubmittingPrompt,
      baseComposerProps.selectedAgentTarget,
      renderComposerFooterAccessory
    ]
  );
  const footerAccessory = useMemo(() => {
    if (!entryError && hostFooterAccessory === null) return null;
    return (
      <>
        {entryError ? (
          <span className="text-xs text-destructive">
            {entryError === "content_unsupported"
              ? t("agentHost.agentGui.sideContentUnsupported")
              : t("agentHost.agentGui.sideOperationFailed")}
          </span>
        ) : null}
        {hostFooterAccessory}
      </>
    );
  }, [entryError, hostFooterAccessory, t]);
  const bottomDockComposerProps = useMemo<AgentComposerProps>(
    () => ({ ...baseComposerProps, footerAccessory }),
    [baseComposerProps, footerAccessory]
  );
  const addSelectionToConversation = useCallback(
    (text: string) => {
      baseComposerProps.onDraftContentChange(
        appendAgentComposerDraftQuote(baseComposerProps.draftContent, {
          type: "quote",
          id: crypto.randomUUID(),
          text
        })
      );
      onRequestComposerFocus();
    },
    [baseComposerProps, onRequestComposerFocus]
  );
  const askSelectionInSide = useCallback(
    (text: string) => {
      void stageSelection(text).catch(() => {});
    },
    [stageSelection]
  );
  const selectionProps = useMemo(
    () => ({
      onAddSelectionToConversation: addSelectionToConversation,
      onAskSelectionInSide: controller.canOpen ? askSelectionInSide : undefined,
      textSelectionActionsEnabled
    }),
    [
      addSelectionToConversation,
      askSelectionInSide,
      controller.canOpen,
      textSelectionActionsEnabled
    ]
  );
  const sideComposerProps = useMemo<AgentComposerProps | null>(() => {
    if (!active) return null;
    return {
      workspaceId: baseComposerProps.workspaceId,
      agentSessionId: active.sideAgentSessionId,
      workspacePath: baseComposerProps.workspacePath,
      currentUserId: baseComposerProps.currentUserId,
      provider: baseComposerProps.provider,
      draftContent,
      draftScopeKey: `side:${active.sideAgentSessionId}`,
      inputHistory: [],
      availableCommands: [],
      hasCompactableContext: false,
      compactSupported: false,
      availableSkills: [],
      gate: projectAgentSideComposerGate(active),
      presentationEditorDisabled: false,
      presentationSubmitDisabled: false,
      placeholder: t("agentHost.agentGui.sideInputPlaceholder"),
      composerSettings: projectAgentSideComposerSettings(
        baseComposerProps.composerSettings
      ),
      queuedPrompts: [],
      drainingQueuedPromptId: null,
      workspaceAppIcons: baseComposerProps.workspaceAppIcons,
      selectedAgentTarget: baseComposerProps.selectedAgentTarget,
      agentTargets: [],
      handoffAgentTargets: [],
      providerSelectReadonly: true,
      showStopButton: Boolean(active.activeTurnId),
      stopDisabled: false,
      activePrompt: interactivePrompt,
      activePromptKeyboardShortcutsEnabled:
        baseComposerProps.isActive && focused,
      composerFocusRequestSequence: focusRequestSequence,
      promptTips: [],
      isInterrupting: false,
      isSendingTurn: Boolean(active.activeTurnId),
      isSubmittingPrompt: interactionSubmitting,
      projectMissingProbeEnabled: false,
      uiLanguage: baseComposerProps.uiLanguage,
      // isActive controls whether the editor installs its focus and input
      // behavior. It cannot depend on focus itself or the first click has no
      // active editor with which to establish that focus.
      isActive: baseComposerProps.isActive && isVisible,
      workspaceReferencePickerOpen: false,
      promptImagesSupported: false,
      canUploadAttachment: false,
      labels: {
        ...baseComposerProps.labels,
        conversationReturn: undefined
      },
      workspaceUserProjectI18n: baseComposerProps.workspaceUserProjectI18n,
      capabilityMenuState: projectAgentSideCapabilityMenuState(
        baseComposerProps.capabilityMenuState
      ),
      capabilityControlsReadOnly: true,
      onDraftContentChange: setDraftContent,
      onSettingsChange: () => {},
      onSubmit: submitSide,
      onSendQueuedPromptNext: () => {},
      onRemoveQueuedPrompt: () => {},
      onEditQueuedPrompt: () => {},
      onInterruptCurrentTurn: interrupt,
      onSubmitInteractivePrompt: (input) => {
        void submitInteraction(input).catch(() => {});
        // Side is event-confirmed rather than optimistically dismissed. Its
        // transient runtime keeps the prompt live on transport failure and
        // clears it when the Side event stream confirms the response.
        return false;
      },
      onLinkAction: baseComposerProps.onLinkAction
    };
  }, [
    active,
    baseComposerProps,
    draftContent,
    focused,
    focusRequestSequence,
    interactionSubmitting,
    interactivePrompt,
    interrupt,
    isVisible,
    setDraftContent,
    submitInteraction,
    submitSide,
    t
  ]);
  const sideMatchesSource =
    active !== null && active.sourceAgentSessionId === sourceAgentSessionId;
  const sideSurfaceProps = useMemo<AgentGUISideConversationSurfaceProps | null>(
    () =>
      active && sideComposerProps && sideMatchesSource
        ? {
            active,
            availableSkills,
            composerProps: sideComposerProps,
            conversationFlowLabels,
            isVisible,
            loadingLabel: t("agentHost.agentGui.loadingConversation"),
            workspaceAppIcons:
              baseComposerProps.workspaceAppIcons ?? EMPTY_WORKSPACE_APP_ICONS,
            onFocusChange: setFocused,
            onLinkAction: baseComposerProps.onLinkAction
          }
        : null,
    [
      active,
      availableSkills,
      baseComposerProps.onLinkAction,
      baseComposerProps.workspaceAppIcons,
      conversationFlowLabels,
      isVisible,
      setFocused,
      sideComposerProps,
      sideMatchesSource,
      t
    ]
  );
  const projection = useMemo(
    () =>
      sideSurfaceProps && active
        ? {
            close,
            sideAgentSessionId: active.sideAgentSessionId,
            sourceAgentSessionId: active.sourceAgentSessionId,
            surfaceProps: sideSurfaceProps
          }
        : null,
    [active, close, sideSurfaceProps]
  );
  const sidePane = presentation ? (
    <AgentGUISideConversationPresentationPublisher
      presentation={presentation}
      projection={projection}
      sourceMismatch={active !== null && !sideMatchesSource}
      onClose={close}
    />
  ) : sideSurfaceProps ? (
    <AgentGUISideConversationPane {...sideSurfaceProps} onClose={close} />
  ) : null;
  return { bottomDockComposerProps, selectionProps, sidePane };
}
