import { useCallback, useMemo } from "react";
import type { AgentComposerProps } from "../AgentComposer";
import { appendAgentComposerDraftQuote } from "../model/agentComposerDraft";
import type { AgentGUIDetailPaneProps } from "./AgentGUINodeView.types";

export function useAgentGUIComposerFooter({
  baseComposerProps,
  onRequestComposerFocus,
  renderComposerFooterAccessory
}: {
  baseComposerProps: AgentComposerProps;
  onRequestComposerFocus(): void;
  renderComposerFooterAccessory: AgentGUIDetailPaneProps["renderComposerFooterAccessory"];
}) {
  const hostFooterAccessory =
    renderComposerFooterAccessory?.({
      agentSessionId: baseComposerProps.agentSessionId,
      draftContent: baseComposerProps.draftContent,
      onDraftContentChange: baseComposerProps.onDraftContentChange,
      onProjectPathChange: baseComposerProps.onProjectPathChange,
      isActive: baseComposerProps.isActive,
      isSendingTurn: baseComposerProps.state.isSendingTurn,
      isSubmittingPrompt: baseComposerProps.state.isSubmittingPrompt,
      composerSettings: baseComposerProps.state.composerSettings,
      selectedAgentTarget: baseComposerProps.selectedAgentTarget
    }) ?? null;
  const bottomDockComposerProps = useMemo<AgentComposerProps>(
    () => ({ ...baseComposerProps, footerAccessory: hostFooterAccessory }),
    [baseComposerProps, hostFooterAccessory]
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
  return { bottomDockComposerProps, addSelectionToConversation };
}
