import { Slot } from "radix-ui";
import { usePersistCallback } from "@multica/ui/hooks/use-persist-callback";
import { cn } from "@tutti-os/ui-system";
import { ChevronsDown } from "lucide-react";
import { observer } from "mobx-react-lite";
import { memo, type ReactNode } from "react";
import type { DetailChromeState } from "../../../state/DetailChromeState";
import { AgentComposer, type AgentComposerProps } from "../AgentComposer";
import { AgentComposerRegion } from "../AgentComposerRegion";
import styles from "../AgentGUINode.styles";
import type { AgentGUINodeViewProps } from "../AgentGUINodeView";
import { AgentInteractivePromptSurface } from "../AgentInteractivePromptSurface";
import { AgentSessionChrome } from "../AgentSessionChrome";
import type { AgentGUIInteractionViewModel } from "../model/agentGuiNodeTypes";
import { resolveAgentGUIInteractionDisabledReason } from "./agentGUIDetailModelHelpers";
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
    | AgentGUIInteractionViewModel["pendingApproval"]
    | AgentGUIInteractionViewModel["pendingInteractivePrompt"];
  // When set, this interactive prompt takes the composer's slot in the bottom
  // dock (the composer is hidden). Closing the prompt returns the composer.
  bottomDockReplacementPrompt:
    | AgentGUIInteractionViewModel["pendingApproval"]
    | AgentGUIInteractionViewModel["pendingInteractivePrompt"];
  composerProps: AgentComposerProps;
  composerSlot?: ReactNode;
  interaction: AgentGUIInteractionViewModel;
  chrome: Pick<DetailChromeState, "statusSource" | "inlineSource">;
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

export const AgentGUIBottomDockPane = observer(function AgentGUIBottomDockPane({
  bottomDockRef,
  showScrollToBottom,
  scrollToBottomLabel,
  onScrollToBottom,
  bottomDockLiftedPrompt,
  bottomDockReplacementPrompt,
  composerProps,
  composerSlot,
  interaction,
  chrome,
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
  const {
    approvalDisabledReason,
    interactivePromptDisabledReason,
    isRespondingApproval
  } = interaction;
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
          <ButtonBinding1
            scrollToBottomLabel={scrollToBottomLabel}
            onScrollToBottom={onScrollToBottom}
          />
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
          {chrome.inlineSource.chrome ? (
            <AgentSessionChrome
              source={chrome.inlineSource}
              onSubmitApprovalOption={onSubmitApprovalOption}
              onAuthLogin={onAuthLogin}
              onRetryActivation={onRetryInlineNotice}
              onContinueInNewConversation={onContinueInNewConversation}
              labels={chromeLabels}
            />
          ) : null}
          <AgentSessionChrome
            source={chrome.statusSource}
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
          composerSlot ?? <AgentComposer {...composerProps} />
        )
      }
    />
  );
});

const ButtonBinding1 = observer(function ButtonBinding1({
  scrollToBottomLabel,
  onScrollToBottom,
  ...slotProps
}: {
  scrollToBottomLabel: NonNullable<AgentGUIBottomDockPaneProps>["scrollToBottomLabel"];
  onScrollToBottom: NonNullable<AgentGUIBottomDockPaneProps>["onScrollToBottom"];
} & Omit<
  React.ComponentProps<typeof Slot.Root>,
  keyof {
    scrollToBottomLabel: NonNullable<AgentGUIBottomDockPaneProps>["scrollToBottomLabel"];
    onScrollToBottom: NonNullable<AgentGUIBottomDockPaneProps>["onScrollToBottom"];
  }
>) {
  const onPointerDownProp1 = usePersistCallback<
    NonNullable<React.ComponentProps<"button">["onPointerDown"]>
  >((event) => event.stopPropagation());
  return (
    <Slot.Root {...slotProps}>
      <button
        type="button"
        className={cn(
          styles.bottomDockScrollToBottom,
          "nodrag tsh-desktop-no-drag [-webkit-app-region:no-drag]"
        )}
        data-testid="agent-gui-scroll-to-bottom"
        aria-label={scrollToBottomLabel}
        title={scrollToBottomLabel}
        onPointerDown={onPointerDownProp1}
        onClick={onScrollToBottom}
      >
        <ChevronsDown aria-hidden="true" size={15} strokeWidth={2.2} />
      </button>
    </Slot.Root>
  );
});
