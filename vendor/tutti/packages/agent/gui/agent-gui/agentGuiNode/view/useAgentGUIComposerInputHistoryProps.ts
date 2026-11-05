import { useMemo, type RefObject } from "react";
import type { AgentConversationVM } from "../../../shared/agentConversation/contracts/agentConversationVM";
import type { AgentComposerProps } from "../AgentComposer";
import type { AgentGUINodeViewProps } from "../AgentGUINodeView";
import { projectAgentComposerInputHistory } from "../model/agentComposerInputHistory";
import type {
  AgentGUIComposerViewModel,
  AgentGUIDetailViewModel,
  AgentGUIInteractionViewModel,
  AgentGUIOperationsViewModel,
  AgentGUIRailViewModel,
  AgentGUIReadinessViewModel,
  AgentGUIShellViewModel
} from "../model/agentGuiNodeTypes";
import { useStableEventCallback } from "./agentGUIViewUtils";

type InputHistoryProps = Pick<
  AgentComposerProps,
  | "inputHistory"
  | "inputHistoryHasOlderPage"
  | "inputHistoryIsLoadingOlderPage"
  | "onRequestOlderInputHistoryPage"
>;
const EMPTY_INPUT_HISTORY: NonNullable<InputHistoryProps["inputHistory"]> = [];

export function useAgentGUIComposerInputHistoryProps(input: {
  actions: AgentGUINodeViewProps["actions"];
  conversation: AgentConversationVM | null;
  enabled: boolean;
  pendingPrependScrollAnchorRef: RefObject<{
    conversationId: string;
    scrollHeight: number;
    scrollTop: number;
  } | null>;
  timelineRef: RefObject<HTMLDivElement | null>;
  shell: AgentGUIShellViewModel;
  rail: AgentGUIRailViewModel;
  detail: AgentGUIDetailViewModel;
  composer: AgentGUIComposerViewModel;
  interaction: AgentGUIInteractionViewModel;
  readiness: AgentGUIReadinessViewModel;
  operations: AgentGUIOperationsViewModel;
}): InputHistoryProps {
  const onRequestOlderInputHistoryPage = useStableEventCallback(() => {
    const activeConversationId = input.rail.activeConversationId;
    if (
      !activeConversationId ||
      !input.detail.hasOlderMessages ||
      input.detail.isLoadingOlderMessages
    ) {
      return;
    }
    const timeline = input.timelineRef.current;
    if (timeline) {
      input.pendingPrependScrollAnchorRef.current = {
        conversationId: activeConversationId,
        scrollHeight: timeline.scrollHeight,
        scrollTop: timeline.scrollTop
      };
    }
    input.actions.loadOlderConversationMessages();
  });
  const inputHistory = input.enabled
    ? projectAgentComposerInputHistory(input.conversation)
    : EMPTY_INPUT_HISTORY;
  return useMemo(
    () => ({
      inputHistory,
      inputHistoryHasOlderPage: input.enabled && input.detail.hasOlderMessages,
      inputHistoryIsLoadingOlderPage:
        input.enabled && input.detail.isLoadingOlderMessages,
      onRequestOlderInputHistoryPage: input.enabled
        ? onRequestOlderInputHistoryPage
        : undefined
    }),
    [
      input.detail.hasOlderMessages,
      input.detail.isLoadingOlderMessages,
      input.enabled,
      inputHistory,
      onRequestOlderInputHistoryPage
    ]
  );
}
