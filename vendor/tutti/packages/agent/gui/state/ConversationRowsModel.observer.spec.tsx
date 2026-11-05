import { TranscriptModel } from "@multica/conversation-client/models";
import { act, render, screen } from "@testing-library/react";
import type { AgentActivityMessage } from "@tutti-os/agent-activity-core";
import { describe, expect, it, vi } from "vitest";
import type { ConversationServices } from "../conversationServices";
import { ConversationServicesProvider } from "../conversationServices";
import { AgentConversationFlow } from "../shared/agentConversation/components/AgentConversationFlow";
import { createTestRuntimeState } from "../shared/testing/createTestRuntimeState";
import { ConversationRowsModel } from "./ConversationRowsModel";

const renders = vi.hoisted(() => new Map<string, number>());
vi.mock("../shared/agentConversation/components/AgentMessageBlock", () => ({
  AgentMessageBlock: ({
    row
  }: {
    row: { id: string; messages: { body: string }[] };
  }) => {
    renders.set(row.id, (renders.get(row.id) ?? 0) + 1);
    return <p>{row.messages.map((message) => message.body).join(" ")}</p>;
  }
}));

describe("conversation row observation", () => {
  it("streams only the changed row without rerendering another row", () => {
    renders.clear();
    const state = createTestRuntimeState("w"),
      transcript = new TranscriptModel<AgentActivityMessage>();
    state.sessions.upsertSession({
      agentSessionId: "s",
      workspaceId: "w",
      provider: "codex",
      cwd: "",
      title: "Test",
      latestTurnInteractions: [],
      pendingInteractions: []
    });
    const message = (
      id: string,
      turnId: string,
      text: string,
      role: string,
      sequence: number
    ): AgentActivityMessage => ({
      agentSessionId: "s",
      messageId: id,
      turnId,
      role,
      sequence,
      version: sequence,
      kind: "text",
      occurredAtUnixMs: sequence,
      payload: { content: text }
    });
    const initial = [
      message("q1", "t1", "Prompt one", "user", 1),
      message("a1", "t1", "First reply", "agent", 2),
      message("q2", "t2", "Prompt two", "user", 3),
      message("a2", "t2", "Second reply", "agent", 4)
    ];
    transcript.commit("s", initial);
    const runtime = { ...state, transcript } as unknown as ConversationServices;
    const rows = new ConversationRowsModel(runtime, "s", { workspaceId: "w" });
    runtime.conversationRows = () => rows;
    render(
      <ConversationServicesProvider runtime={runtime}>
        <AgentConversationFlow
          agentSessionId="s"
          isLoading={false}
          loadingLabel="Loading"
          empty={null}
          labels={{
            processing: "Working",
            thinkingLabel: "Thinking",
            toolCallsLabel: String,
            turnSummary: "Summary"
          }}
        />
      </ConversationServicesProvider>
    );
    expect(screen.getByText("First reply")).toBeTruthy();
    const before = new Map(renders);
    act(() =>
      transcript.commit("s", [
        ...initial.slice(0, 3),
        message("a2", "t2", "Second reply streamed", "agent", 4)
      ])
    );
    expect(screen.getByText("Second reply streamed")).toBeTruthy();
    const changed = [...renders].filter(
      ([id, count]) => count !== before.get(id)
    );
    expect(changed).toHaveLength(1);
    state.dispose();
  });
});
