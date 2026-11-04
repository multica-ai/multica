import { memo } from "react";
import { ChevronsDown } from "lucide-react";
import { cn } from "@tutti-os/ui-system";
import styles from "../AgentGUINode.styles";
import { AgentInteractivePromptSurface } from "../AgentInteractivePromptSurface";
import { AgentComposerRegion } from "../AgentComposerRegion";
import { AgentSessionChrome } from "../AgentSessionChrome";
import { AgentComposer, type AgentComposerProps } from "../AgentComposer";
import { resolveAgentGUIInteractionDisabledReason } from "./agentGUIDetailModelHelpers";
import type {
  AgentGUINodeViewModel,
  AgentGUISessionChrome
} from "../model/agentGuiNodeTypes";
import type { AgentGUINodeViewProps } from "../AgentGUINodeView";
import type {
  ChromeLabels,
  InteractivePromptLabels
} from "./AgentGUINodeView.types";

interface AgentGUIBottomDockPaneProps {
  bottomDockRef: React.RefObject<HTMLDivElement | null>;
  showScrollToBottom: boolean;
  scrollToBottomLabel: string;
  onScrollToBottom: () => void;
  // Approval / ask-user prompts lifted above the inline notice (composer stays
  // visible below). Mutually exclusive with bottomDockReplacementPrompt.
  bottomDockLiftedPrompt:
    | AgentGUINodeViewModel["interaction"]["pendingApproval"]
    | AgentGUINodeViewModel["interaction"]["pendingInteractivePrompt"];
  // When set, this interactive prompt takes the composer's slot in the bottom
  // dock (the composer is hidden). Closing the prompt returns the composer.
  bottomDockReplacementPrompt:
    | AgentGUINodeViewModel["interaction"]["pendingApproval"]
    | AgentGUINodeViewModel["interaction"]["pendingInteractivePrompt"];
  composerProps: AgentComposerProps;
  approvalDisabledReason: string | null;
  interactivePromptDisabledReason: string | null;
  inlineNoticeChrome: AgentGUISessionChrome | null;
  isRespondingApproval: boolean;
  sessionChrome: AgentGUISessionChrome;
  keyboardShortcutsEnabled: boolean;
  chromeLabels: ChromeLabels;
  promptLabels: InteractivePromptLabels;
  onSubmitApprovalOption: AgentGUINodeViewProps["actions"]["submitApprovalOption"];
  onAuthLogin?: (provider?: string | null) => void;
  onRetryActivation: AgentGUINodeViewProps["actions"]["retryActivation"];
  onRetryInlineNotice: () => void;
  onContinueInNewConversation: AgentGUINodeViewProps["actions"]["continueInNewConversation"];
  onSubmitBottomDockInteractivePrompt: AgentGUINodeViewProps["actions"]["submitInteractivePrompt"];
}

export const AgentGUIBottomDockPane = memo(function AgentGUIBottomDockPane({
  bottomDockRef,
  showScrollToBottom,
  scrollToBottomLabel,
  onScrollToBottom,
  bottomDockLiftedPrompt,
  bottomDockReplacementPrompt,
  composerProps,
  approvalDisabledReason,
  interactivePromptDisabledReason,
  inlineNoticeChrome,
  isRespondingApproval,
  sessionChrome,
  keyboardShortcutsEnabled,
  chromeLabels,
  promptLabels,
  onSubmitApprovalOption,
  onAuthLogin,
  onRetryActivation,
  onRetryInlineNotice,
  onContinueInNewConversation,
  onSubmitBottomDockInteractivePrompt
}: AgentGUIBottomDockPaneProps): React.JSX.Element {
  "use memo";

  const liftedPromptDisabledReason = resolveAgentGUIInteractionDisabledReason({
    promptKind: bottomDockLiftedPrompt?.kind,
    approvalReason: approvalDisabledReason,
    interactivePromptReason: interactivePromptDisabledReason
  });
  const replacementPromptDisabledReason =
    resolveAgentGUIInteractionDisabledReason({
      promptKind: bottomDockReplacementPrompt?.kind,
      approvalReason: approvalDisabledReason,
      interactivePromptReason: interactivePromptDisabledReason
    });


  return (
    <AgentComposerRegion
      regionRef={bottomDockRef}
      floating={
        showScrollToBottom ? (
          <button
            type="button"
            className={cn(
              styles.bottomDockScrollToBottom,
              "nodrag tsh-desktop-no-drag [-webkit-app-region:no-drag]"
            )}
            data-testid="agent-gui-scroll-to-bottom"
            aria-label={scrollToBottomLabel}
            title={scrollToBottomLabel}
            onPointerDown={(event) => event.stopPropagation()}
            onClick={onScrollToBottom}
          >
            <ChevronsDown aria-hidden="true" size={15} strokeWidth={2.2} />
          </button>
        ) : null
      }
      lifted={
        bottomDockLiftedPrompt ? (
          <div
            className={styles.bottomDockPrompt}
            data-testid="agent-gui-bottom-dock-active-prompt"
          >
            <AgentInteractivePromptSurface
              prompt={bottomDockLiftedPrompt}
              conversationReturn={promptLabels.conversationReturn}
              embedded={true}
              edgeGlow={true}
              keyboardShortcuts={keyboardShortcutsEnabled}
              isSubmitting={isRespondingApproval}
              isInteractionDisabled={Boolean(liftedPromptDisabledReason)}
              interactionDisabledReason={liftedPromptDisabledReason}
              onSubmit={onSubmitBottomDockInteractivePrompt}
              labels={promptLabels}
            />
          </div>
        ) : null
      }
      accessories={
        <>
          {inlineNoticeChrome ? (
            <AgentSessionChrome
              chrome={inlineNoticeChrome}
              approvalDisabledReason={approvalDisabledReason}
              isRespondingApproval={isRespondingApproval}
              onSubmitApprovalOption={onSubmitApprovalOption}
              onAuthLogin={onAuthLogin}
              onRetryActivation={onRetryInlineNotice}
              onContinueInNewConversation={onContinueInNewConversation}
              labels={chromeLabels}
            />
          ) : null}
          <AgentSessionChrome
            chrome={sessionChrome}
            approvalDisabledReason={approvalDisabledReason}
            isRespondingApproval={isRespondingApproval}
            onSubmitApprovalOption={onSubmitApprovalOption}
            onAuthLogin={onAuthLogin}
            onRetryActivation={onRetryActivation}
            onContinueInNewConversation={onContinueInNewConversation}
            labels={chromeLabels}
          />
        </>
      }
      primary={
        bottomDockReplacementPrompt ? (
          <div
            className={styles.bottomDockPrompt}
            data-testid="agent-gui-bottom-dock-active-prompt"
          >
            <AgentInteractivePromptSurface
              prompt={bottomDockReplacementPrompt}
              embedded={true}
              edgeGlow={true}
              keyboardShortcuts={keyboardShortcutsEnabled}
              isSubmitting={isRespondingApproval}
              isInteractionDisabled={Boolean(replacementPromptDisabledReason)}
              interactionDisabledReason={replacementPromptDisabledReason}
              onSubmit={onSubmitBottomDockInteractivePrompt}
              labels={promptLabels}
            />
          </div>
        ) : (
          <AgentComposer {...composerProps} />
        )
      }
    />
  );
});
