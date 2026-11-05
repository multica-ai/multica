import { Spinner } from "@tutti-os/ui-system";
import { ArrowUp as SendFilledIcon } from "lucide-react";
import {
  useCallback,
  useEffect,
  useState,
  type CSSProperties,
  type RefObject
} from "react";
import { createPortal } from "react-dom";
import { cn } from "../../../app/renderer/lib/utils";
import { useOptionalConversationServices } from "../../../conversationServices";
import type { AgentConversationPromptVM } from "../../../shared/agentConversation/contracts/agentConversationVM";
import { submitAgentInteractionResponseAndDismiss } from "../../../shared/agentConversation/interactionResponseAdmission";
import styles from "../AgentGUINode.styles";
import type { AgentMentionSearchController } from "../AgentMentionSearchController";
import type { AgentFileMentionSuggestionState } from "../agentRichText/agentFileMentionExtension";
import {
  agentComposerDraftFiles,
  agentComposerDraftHasContent,
  agentComposerDraftImages,
  agentComposerDraftLargeTexts
} from "../model/agentComposerDraft";
import type { AgentComposerDraft } from "../model/agentGuiNodeTypes";
import type {
  AgentComposerPromptTip,
  AgentComposerRenderProps as AgentComposerProps
} from "./AgentComposer.types";
import { reportAgentComposerDiagnostic } from "./agentComposerDiagnostics";

interface Input {
  draftContent: AgentComposerDraft;
  canQueueWhileBusy: boolean;
  showStopButton: boolean;
  activeTurnId?: string | null;
  draftOverridesStopButton: boolean;
  stopDisabled: boolean;
  isInterrupting: boolean;
  isSendingTurn: boolean;
  activePrompt: AgentConversationPromptVM | null;
  disabledReason?: string | null;
  placeholder: string;
  selectedProjectPath: string;
  selectedProjectSectionKey: string;
  previousSelectedProjectPathRef: RefObject<string>;
  setIsSelectedProjectMissing: (value: boolean) => void;
  fileMentionSuggestion: AgentFileMentionSuggestionState | null;
  mentionControllerRef: RefObject<AgentMentionSearchController | null>;
  workspaceId: string;
  currentUserId?: string | null;
  onSubmitInteractivePrompt: AgentComposerProps["onSubmitInteractivePrompt"];
  onInterruptCurrentTurn: () => void;
  isSelectedProjectMissing: boolean;
  submitDisabled: boolean;
  /** Keeps the send button live on an empty draft (empty-send override). */
  allowEmptySubmit: boolean;
  /** Overrides the empty-draft send copy (falls back to labels.sendAccept). */
  emptySubmitLabel?: string;
  labels: AgentComposerProps["labels"];
  activePromptTip: AgentComposerPromptTip | null;
  promptTipRef: RefObject<HTMLSpanElement | null>;
  promptTips: readonly AgentComposerPromptTip[];
  promptTipStyle?: CSSProperties;
  rotatingPromptTips: readonly AgentComposerPromptTip[];
  fileDropOverlayHost: HTMLElement | null;
  fileDropOverlayActive: boolean;
  canUploadAttachment: boolean;
  promptImagesSupported: boolean;
}

export function shouldShowAgentComposerStopButton(input: {
  draftOverridesStopButton: boolean;
  hasDraftContent: boolean;
  isQueueMode: boolean;
  showStopButton: boolean;
}): boolean {
  return (
    input.showStopButton &&
    !input.isQueueMode &&
    !(input.draftOverridesStopButton && input.hasDraftContent)
  );
}

export function useComposerPresentation(input: Input) {
  const {
    draftContent,
    canQueueWhileBusy,
    showStopButton,
    activeTurnId = null,
    draftOverridesStopButton,
    stopDisabled,
    isInterrupting,
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
    allowEmptySubmit,
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
  } = input;
  const agentActivityRuntime = useOptionalConversationServices();
  const draftImages = agentComposerDraftImages(draftContent);
  const draftFiles = agentComposerDraftFiles(draftContent);
  const draftLargeTexts = agentComposerDraftLargeTexts(draftContent);
  const hasDraftContent = agentComposerDraftHasContent(draftContent);
  const hasUploadingDraftImages = draftImages.some((image) => image.uploading);
  const hasFailedDraftImages = draftImages.some((image) => image.uploadError);
  const hasUploadingDraftFiles = draftFiles.some((file) => file.uploading);
  const hasFailedDraftFiles = draftFiles.some((file) => file.uploadError);
  const hasUploadingDraftLargeTexts = draftLargeTexts.some(
    (item) => item.uploading
  );
  const hasFailedDraftLargeTexts = draftLargeTexts.some(
    (item) => item.uploadError
  );
  const isQueueMode = canQueueWhileBusy && hasDraftContent;
  const shouldShowStopButton = shouldShowAgentComposerStopButton({
    draftOverridesStopButton,
    hasDraftContent,
    isQueueMode,
    showStopButton
  });
  const sendButtonState = isQueueMode
    ? "queue"
    : shouldShowStopButton
      ? isInterrupting
        ? "stopping"
        : "interrupt"
      : isSendingTurn
        ? "loading"
        : "send";
  const sendButtonBusy = isSendingTurn && !isQueueMode;
  const emptyDraftBlocksSend = !hasDraftContent && !allowEmptySubmit;
  const sendDisabledReasons = [
    isSelectedProjectMissing ? "project_missing" : null,
    submitDisabled ? "submit_disabled" : null,
    emptyDraftBlocksSend ? "draft_empty" : null,
    hasUploadingDraftImages ? "image_uploading" : null,
    hasFailedDraftImages ? "image_upload_failed" : null,
    hasUploadingDraftFiles ? "file_uploading" : null,
    hasFailedDraftFiles ? "file_upload_failed" : null,
    hasUploadingDraftLargeTexts ? "large_text_uploading" : null,
    hasFailedDraftLargeTexts ? "large_text_upload_failed" : null,
    sendButtonBusy ? "send_busy" : null
  ].filter((reason): reason is string => reason !== null);
  const sendDisabledReasonKey = sendDisabledReasons.join(",");
  useEffect(() => {
    reportAgentComposerDiagnostic(agentActivityRuntime, {
      details: {
        canUploadAttachment,
        draftFileCount: draftFiles.length,
        draftImageCount: draftImages.length,
        draftLargeTextCount: draftLargeTexts.length,
        hasDraftContent,
        hasFailedDraftFiles,
        hasFailedDraftImages,
        hasFailedDraftLargeTexts,
        hasUploadingDraftFiles,
        hasUploadingDraftImages,
        hasUploadingDraftLargeTexts,
        isSelectedProjectMissing,
        promptImagesSupported,
        sendButtonBusy,
        sendDisabledReason: sendDisabledReasonKey || null,
        submitDisabled,
        uploadFunctionAvailable: Boolean(
          agentActivityRuntime?.attachments?.uploadPromptContent
        )
      },
      event: "agent.gui.composer.submit_state_changed",
      level: "info",
      source: "agent-gui",
      workspaceId
    });
  }, [
    agentActivityRuntime,
    canUploadAttachment,
    draftFiles.length,
    draftImages.length,
    draftLargeTexts.length,
    hasDraftContent,
    hasFailedDraftFiles,
    hasFailedDraftImages,
    hasFailedDraftLargeTexts,
    hasUploadingDraftFiles,
    hasUploadingDraftImages,
    hasUploadingDraftLargeTexts,
    isSelectedProjectMissing,
    promptImagesSupported,
    sendButtonBusy,
    sendDisabledReasonKey,
    submitDisabled,
    workspaceId
  ]);
  const activePromptRequestId = activePrompt?.requestId ?? null;
  const [dismissedPromptRequestId, setDismissedPromptRequestId] = useState<
    string | null
  >(null);
  const visibleActivePrompt =
    activePrompt && dismissedPromptRequestId !== activePromptRequestId
      ? activePrompt
      : null;
  const disabledReasonText = disabledReason?.trim() ?? "";
  const effectivePlaceholder = disabledReasonText || placeholder;
  const visibleDraftLargeTexts = draftLargeTexts;
  useEffect(() => {
    if (previousSelectedProjectPathRef.current === selectedProjectPath) {
      return;
    }
    previousSelectedProjectPathRef.current = selectedProjectPath;
    setIsSelectedProjectMissing(false);
    if (!fileMentionSuggestion) {
      return;
    }
    mentionControllerRef.current?.updateQuery({
      workspaceId,
      currentUserId,
      query: fileMentionSuggestion.query,
      sectionKey: selectedProjectSectionKey || null,
      sessionCwd: selectedProjectPath || null
    });
  }, [
    currentUserId,
    fileMentionSuggestion,
    selectedProjectPath,
    selectedProjectSectionKey,
    workspaceId
  ]);

  useEffect(() => {
    setDismissedPromptRequestId(null);
  }, [activePromptRequestId]);

  const submitInteractivePromptAndDismiss = useCallback(
    (input: Parameters<AgentComposerProps["onSubmitInteractivePrompt"]>[0]) => {
      return submitAgentInteractionResponseAndDismiss({
        response: input,
        submit: onSubmitInteractivePrompt,
        dismiss: setDismissedPromptRequestId
      });
    },
    [activePromptRequestId, onSubmitInteractivePrompt]
  );

  // While the empty-send override is active (plan review) the send button
  // carries explicit decision copy: an empty draft accepts, typed feedback
  // requests changes. A Host empty-send action labels only the empty draft;
  // typed content is an ordinary send. Icon-only otherwise.
  const planReviewSendLabel = allowEmptySubmit
    ? ((hasDraftContent
        ? emptySubmitLabel === undefined
          ? labels.sendRequestChanges
          : null
        : (emptySubmitLabel ?? labels.sendAccept)) ?? null)
    : null;
  const composerActionButton = shouldShowStopButton ? (
    <button
      type="button"
      className={`${styles.composerStopButton} relative inline-flex size-7 shrink-0 items-center justify-center rounded-full border border-transparent bg-transparent p-0 text-[var(--text-primary)] transition-[color,opacity] duration-150 hover:bg-transparent focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-[color:color-mix(in_srgb,var(--text-primary)_34%,transparent)] focus-visible:ring-offset-2 focus-visible:ring-offset-[var(--background-panel)] active:bg-transparent disabled:cursor-not-allowed disabled:opacity-45`}
      data-testid={
        activeTurnId?.trim() ? "agent-gui-composer-stop-active-turn" : undefined
      }
      data-agent-turn-id={activeTurnId?.trim() || undefined}
      disabled={isInterrupting || stopDisabled}
      aria-label={isInterrupting ? labels.stopping : labels.stop}
      title={isInterrupting ? labels.stopping : labels.stop}
      onClick={onInterruptCurrentTurn}
    >
      <Spinner
        className="size-7 text-[var(--text-primary)]"
        size={28}
        strokeWidth={2}
        trackColor="var(--transparency-hover)"
        testId="agent-gui-composer-stop-spinner"
      />
      <span
        aria-hidden="true"
        className="pointer-events-none absolute left-1/2 top-1/2 size-2 -translate-x-1/2 -translate-y-1/2 rounded-[2px] bg-current"
      />
    </button>
  ) : (
    <button
      type="submit"
      className={styles.composerSendButton}
      data-testid="agent-gui-composer-send"
      data-state={sendButtonState}
      data-disabled-reason={sendDisabledReasonKey || undefined}
      data-plan-review-label={planReviewSendLabel ? "true" : undefined}
      disabled={
        isSelectedProjectMissing ||
        submitDisabled ||
        emptyDraftBlocksSend ||
        hasUploadingDraftImages ||
        hasFailedDraftImages ||
        hasUploadingDraftFiles ||
        hasFailedDraftFiles ||
        hasUploadingDraftLargeTexts ||
        hasFailedDraftLargeTexts ||
        sendButtonBusy
      }
      aria-label={planReviewSendLabel ?? labels.send}
      title={planReviewSendLabel ?? labels.send}
      aria-busy={sendButtonBusy}
    >
      {sendButtonBusy ? (
        <Spinner
          className="text-[var(--text-primary)]"
          size={16}
          strokeWidth={2.5}
          trackColor="var(--transparency-hover)"
          testId="agent-gui-composer-send-spinner"
        />
      ) : planReviewSendLabel ? (
        <>
          <span
            className="min-w-0 truncate"
            data-testid="agent-gui-composer-send-label"
          >
            {planReviewSendLabel}
          </span>
          <SendFilledIcon />
        </>
      ) : (
        <SendFilledIcon />
      )}
    </button>
  );

  const promptTipNode = activePromptTip ? (
    <span
      key={activePromptTip.id}
      ref={promptTipRef}
      className={styles.composerPromptTip}
      data-rotating={rotatingPromptTips.length > 1 ? "true" : undefined}
      data-testid="agent-gui-prompt-tip"
      style={promptTipStyle}
    >
      <span className={styles.composerPromptTipTrack}>
        {rotatingPromptTips.map((tip, index) => (
          <span
            key={`${tip.id}:${index}`}
            className={styles.composerPromptTipItem}
            aria-hidden={index >= promptTips.length ? true : undefined}
          >
            <span className={styles.composerPromptTipPrefix}>
              {labels.promptTipsPrefix}
            </span>
            <span className={styles.composerPromptTipLabel}>{tip.label}</span>
            <span className={styles.composerPromptTipText}>
              {" · "}
              {tip.prompt}
            </span>
          </span>
        ))}
      </span>
    </span>
  ) : null;

  const fileDropOverlay =
    fileDropOverlayHost !== null
      ? createPortal(
          <div
            aria-hidden="true"
            data-testid="agent-gui-composer-file-drop-overlay"
            data-active={fileDropOverlayActive ? "true" : "false"}
            className={cn(
              styles.composerFileDropOverlay,
              fileDropOverlayActive && styles.composerFileDropOverlayActive
            )}
          >
            <span className={styles.composerFileDropOverlayCard}>
              {labels.fileDropHint}
            </span>
          </div>,
          fileDropOverlayHost
        )
      : null;

  return {
    composerActionButton,
    disabledReasonText,
    effectivePlaceholder,
    fileDropOverlay,
    promptTipNode,
    submitInteractivePromptAndDismiss,
    visibleActivePrompt,
    visibleDraftLargeTexts
  };
}
