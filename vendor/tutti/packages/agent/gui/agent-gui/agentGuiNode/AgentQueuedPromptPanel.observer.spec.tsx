import { act, render, screen } from "@testing-library/react";
import { expect, it, vi } from "vitest";
import {
  ConversationServicesProvider,
  type ConversationServices
} from "../../conversationServices";
import { createTestRuntimeState } from "../../shared/testing/createTestRuntimeState";
import { AgentQueuedPromptPanel } from "./AgentQueuedPromptPanel";

it("observes its queue without rendering its parent or another conversation's queue", () => {
  const state = createTestRuntimeState("workspace");
  let parentRenders = 0;
  function Parent() {
    parentRenders++;
    return (
      <ConversationServicesProvider
        runtime={state as unknown as ConversationServices}
      >
        <AgentQueuedPromptPanel
          agentSessionId="a"
          labels={{
            queuedLabel: "Queued",
            queuePausedByUserLabel: "Paused",
            sendQueuedPromptNext: "Send now",
            editQueuedPrompt: "Edit",
            deleteQueuedPrompt: "Remove",
            queuedPromptMoreActions: "More"
          }}
          onSendQueuedPromptNext={vi.fn()}
          onRemoveQueuedPrompt={vi.fn()}
          onEditQueuedPrompt={vi.fn()}
        />
      </ConversationServicesProvider>
    );
  }
  const view = render(<Parent />);
  const before = parentRenders;
  const prompt = (id: string) => ({
    id,
    createdAtUnixMs: 1,
    content: [{ type: "text" as const, text: id }]
  });
  act(() => state.seedQueue("a", [prompt("visible message")]));
  expect(screen.getByText("visible message")).toBeTruthy();
  expect(parentRenders).toBe(before);
  act(() => state.seedQueue("b", [prompt("other conversation")]));
  expect(screen.queryByText("other conversation")).toBeNull();
  expect(parentRenders).toBe(before);
  act(() => state.seedQueue("a", []));
  expect(screen.queryByTestId("agent-gui-composer-queued-prompts")).toBeNull();
  view.unmount();
  state.dispose();
});
