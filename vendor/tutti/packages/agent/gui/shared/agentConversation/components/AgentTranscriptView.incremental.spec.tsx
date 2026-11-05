import { TranscriptModel } from "@multica/conversation-client/models";
import { act, fireEvent, render, screen } from "@testing-library/react";
import { normalizeAgentActivitySession, type AgentActivityMessage, type AgentActivityTurn } from "@tutti-os/agent-activity-core";
import { afterEach, expect, it, vi } from "vitest";
import { ConversationServicesProvider, type ConversationServices } from "../../../conversationServices";
import { ConversationRowsModel } from "../../../state/ConversationRowsModel";
import { createTestRuntimeState } from "../../testing/createTestRuntimeState";
import { AgentConversationFlow } from "./AgentConversationFlow";
import { buildAgentTurnWorkSectionModel } from "./agentTurnWorkSectionModel";
import { AgentTranscriptLayout } from "./AgentTranscriptView";
import type { AgentConversationVM } from "../contracts/agentConversationVM";

const fixture = vi.hoisted(() => ({ indexes: [117, 118, 119], renders: new Map<string, number>() }));
vi.mock("../../../i18n/index", () => ({
  getActiveUiLanguage: () => "en",
  useTranslation: () => ({ t: (key: string) => key }),
  translate: (key: string) => key
}));
vi.mock("./agentTurnWorkSectionModel", async (importOriginal) => {
  const actual = await importOriginal<typeof import("./agentTurnWorkSectionModel")>();
  return { ...actual, buildAgentTurnWorkSectionModel: vi.fn(actual.buildAgentTurnWorkSectionModel) };
});
vi.mock("./AgentMessageBlock", () => ({
  AgentMessageBlock: ({ row }: { row: { id: string; messages: { body: string }[] } }) => {
    fixture.renders.set(row.id, (fixture.renders.get(row.id) ?? 0) + 1);
    return <p>{row.messages.map((message) => message.body).join(" ")}</p>;
  }
}));
vi.mock("./AgentToolGroupRow", () => ({
  AgentToolGroupRow: ({ expanded, onExpandedChange, expansionKey }: {
    expanded: boolean; expansionKey: string; onExpandedChange(key: string, expanded: boolean): void;
  }) => <button aria-expanded={expanded} onClick={() => onExpandedChange(expansionKey, !expanded)}>Toggle tools</button>
}));
vi.mock("@tanstack/react-virtual", () => ({
  useVirtualizer: (options: { getItemKey(index: number): string }) => ({
    containerRef: () => {}, measureElement: () => {},
    getVirtualItems: () => fixture.indexes.map((index) => ({ index, key: options.getItemKey(index), start: index * 100 })),
    isAtEnd: () => true, scrollToIndex: () => {}, scrollToOffset: () => {}, scrollToEnd: () => {}
  })
}));
afterEach(() => vi.clearAllMocks());

it("updates one of 120 turns without rebuilding or rendering unrelated turns", () => {
  fixture.indexes = [117, 118, 119];
  fixture.renders.clear();
  const state = createTestRuntimeState("w");
  const transcript = new TranscriptModel<AgentActivityMessage>();
  const turns: AgentActivityTurn[] = Array.from({ length: 120 }, (_, index) => ({
    agentSessionId: "s", turnId: `t${index}`, origin: "user_prompt", phase: "settled", outcome: "completed",
    startedAtUnixMs: index * 10, settledAtUnixMs: index * 10 + 5, updatedAtUnixMs: index * 10 + 5
  }));
  state.sessions.replaceHistory({
    agentSessionId: "s", workspaceId: "w", provider: "codex", cwd: "", title: "Test",
    latestTurnInteractions: [], pendingInteractions: [], latestTurn: turns.at(-1)
  }, turns);
  const message = (index: number, role: "user" | "agent", text: string): AgentActivityMessage => ({
    agentSessionId: "s", messageId: `${role}-${index}`, turnId: `t${index}`, role, kind: "text",
    sequence: index * 2 + (role === "user" ? 1 : 2), version: 1, occurredAtUnixMs: index * 10,
    payload: { content: text }
  });
  let messages = turns.flatMap((_, index) => [message(index, "user", `Prompt ${index}`), message(index, "agent", `Reply ${index}`)]);
  transcript.commit("s", messages);
  const runtime = { ...state, transcript } as unknown as ConversationServices;
  const rows = new ConversationRowsModel(runtime, "s", { workspaceId: "w" });
  runtime.conversationRows = () => rows;
  const app = render(
    <ConversationServicesProvider runtime={runtime}>
      <div data-testid="agent-gui-timeline" style={{ overflow: "auto" }}>
        <AgentConversationFlow agentSessionId="s" isLoading={false} loadingLabel="Loading" empty={null}
          labels={{ processing: "Working", thinkingLabel: "Thinking", toolCallsLabel: String, turnSummary: "Summary" }} />
      </div>
    </ConversationServicesProvider>
  );
  expect(screen.getByText("Prompt 119")).toBeTruthy();
  vi.mocked(buildAgentTurnWorkSectionModel).mockClear();
  fixture.renders.clear();
  messages = [...messages.slice(0, -2), message(119, "user", "Edited prompt 119"), messages.at(-1)!];
  act(() => transcript.commit("s", messages));
  expect(screen.getByText("Edited prompt 119")).toBeTruthy();
  expect(buildAgentTurnWorkSectionModel).toHaveBeenCalledTimes(1);
  expect([...fixture.renders.keys()].every((id) => id.includes("119"))).toBe(true);

  vi.mocked(buildAgentTurnWorkSectionModel).mockClear();
  fixture.renders.clear();
  fixture.indexes = [118, 119, 120];
  messages = [...messages, message(120, "user", "Prompt 120"), message(120, "agent", "Reply 120")];
  act(() => transcript.commit("s", messages));
  expect(screen.getByText("Prompt 120")).toBeTruthy();
  expect(buildAgentTurnWorkSectionModel).toHaveBeenCalledTimes(1);
  expect([...fixture.renders.keys()].every((id) => id.includes("120"))).toBe(true);

  vi.mocked(buildAgentTurnWorkSectionModel).mockClear();
  fixture.renders.clear();
  fixture.indexes = [119, 120, 121];
  messages = [message(-1, "user", "Earlier prompt"), message(-1, "agent", "Earlier reply"), ...messages];
  act(() => transcript.commit("s", messages));
  expect(screen.getByText("Reply 120")).toBeTruthy();
  expect(buildAgentTurnWorkSectionModel).not.toHaveBeenCalled();
  expect(fixture.renders.size).toBe(0);

  fixture.indexes = [119, 120];
  act(() => transcript.commit("s", messages.slice(0, -2)));
  expect(screen.queryByText("Reply 120")).toBeNull();
  expect(buildAgentTurnWorkSectionModel).not.toHaveBeenCalled();
  app.unmount();
  state.dispose();
});

it("restores each conversation's tool disclosure when switching back", () => {
  const snapshot = (id: string): AgentConversationVM => {
    const activity = { sessionId: id, agentProvider: "codex" } as AgentConversationVM["activity"];
    return {
      activity, workspaceRoot: null,
      sourceDetail: {
        activity, cwd: "", workspaceRoot: "", turns: [], showProcessingIndicator: false,
        session: normalizeAgentActivitySession({ agentSessionId: id, workspaceId: "w", provider: "codex", cwd: "", title: id,
          activeTurnId: null, latestTurnInteractions: [], pendingInteractions: [] })
      },
      rows: [{ kind: "tool-group", id: "tools", turnId: null, grouped: true, calls: [], entries: [], occurredAtUnixMs: 1 }]
    };
  };
  const labels = { processing: "Working", thinkingLabel: "Thinking", toolCallsLabel: String, turnSummary: "Summary" };
  const first = snapshot("first"), second = snapshot("second");
  const app = render(<AgentTranscriptLayout conversation={first} labels={labels} />);
  fireEvent.click(screen.getByRole("button", { name: "Toggle tools" }));
  expect(screen.getByRole("button", { name: "Toggle tools" })).toHaveAttribute("aria-expanded", "true");
  app.rerender(<AgentTranscriptLayout conversation={second} labels={labels} />);
  expect(screen.getByRole("button", { name: "Toggle tools" })).toHaveAttribute("aria-expanded", "false");
  app.rerender(<AgentTranscriptLayout conversation={first} labels={labels} />);
  expect(screen.getByRole("button", { name: "Toggle tools" })).toHaveAttribute("aria-expanded", "true");
});
