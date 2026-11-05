import { act, renderHook } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import type { AgentConversationVM } from "../../../shared/agentConversation/contracts/agentConversationVM";
import type {
  AgentGUIComposerViewModel,
  AgentGUIDetailViewModel,
  AgentGUIInteractionViewModel,
  AgentGUIOperationsViewModel,
  AgentGUIRailViewModel,
  AgentGUIReadinessViewModel,
  AgentGUIShellViewModel,
  AgentGUIViewFixture
} from "../model/agentGuiNodeTypes";
import type { AgentGUINodeViewProps } from "./AgentGUINodeView.types";
import { useAgentGUIComposerInputHistoryProps } from "./useAgentGUIComposerInputHistoryProps";
type AgentGUIViewFixture = {
  shell: AgentGUIShellViewModel;
  rail: AgentGUIRailViewModel;
  detail: AgentGUIDetailViewModel;
  composer: AgentGUIComposerViewModel;
  interaction: AgentGUIInteractionViewModel;
  readiness: AgentGUIReadinessViewModel;
  operations: AgentGUIOperationsViewModel;
};

describe("useAgentGUIComposerInputHistoryProps", () => {
  it("keeps history disabled until the host opts in", () => {
    const timeline = document.createElement("div");
    Object.defineProperties(timeline, {
      scrollHeight: { value: 900 },
      scrollTop: { value: 120, writable: true }
    });
    const loadOlderConversationMessages = vi.fn();
    const pendingPrependScrollAnchorRef = { current: null };
    const input = {
      actions: {
        loadOlderConversationMessages
      } as unknown as AgentGUINodeViewProps["actions"],
      conversation: conversationWithPrompt(),
      pendingPrependScrollAnchorRef,
      timelineRef: { current: timeline },
      ...({
        rail: { activeConversationId: "session-1" },
        detail: {
          hasOlderMessages: true,
          isLoadingOlderMessages: false
        }
      } as AgentGUIViewFixture)
    };
    const { result, rerender } = renderHook(
      ({ enabled }) =>
        useAgentGUIComposerInputHistoryProps({ ...input, enabled }),
      { initialProps: { enabled: false } }
    );

    expect(result.current).toEqual({
      inputHistory: [],
      inputHistoryHasOlderPage: false,
      inputHistoryIsLoadingOlderPage: false,
      onRequestOlderInputHistoryPage: undefined
    });

    rerender({ enabled: true });

    expect(result.current.inputHistory).toHaveLength(1);
    act(() => {
      result.current.onRequestOlderInputHistoryPage?.();
    });
    expect(loadOlderConversationMessages).toHaveBeenCalledOnce();
    expect(pendingPrependScrollAnchorRef.current).toEqual({
      conversationId: "session-1",
      scrollHeight: 900,
      scrollTop: 120
    });
  });
});

function conversationWithPrompt(): AgentConversationVM {
  return {
    sourceDetail: {
      turns: [
        {
          id: "turn-1",
          userMessages: [{ id: "message-1", body: "hello" }]
        }
      ]
    }
  } as unknown as AgentConversationVM;
}
