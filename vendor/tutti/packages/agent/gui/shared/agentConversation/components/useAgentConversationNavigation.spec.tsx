import { act, cleanup, fireEvent, render, renderHook, screen, waitFor } from "@testing-library/react";
import { ConversationModel } from "@multica/conversation-client/models";
import { ConversationNavigation } from "@multica/conversation-client/services";
import { localV2SnapshotSchema, type LocalV2NavigationQuery } from "@multica/core/local-conversation/v2";
import { observer } from "mobx-react-lite";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import type { ReactVirtualizer } from "@tanstack/react-virtual";
import { AgentMessageLocatorRail } from "./AgentMessageLocatorRail";
import { useAgentConversationNavigation } from "./useAgentConversationNavigation";
import { conversationUserMessageRowKey } from "../projection/agentConversationUserProjection";

beforeEach(() => { vi.spyOn(HTMLElement.prototype, "offsetHeight", "get").mockReturnValue(600); });
afterEach(() => { cleanup(); vi.restoreAllMocks(); });
const at = "2026-11-05T00:00:00Z";
const turn = (index: number) => ({ id: `t${index}`, turn_index: index, request_id: `r${index}`, status: "completed", started_at: at, messages: [{ id: `m${index}`, role: "user", content: `Prompt ${index}`, created_at: at }], events: [] });

it("lets the first page paint before supplementing and cancels the scheduled read when hidden", async () => {
  const frames = new Map<number, FrameRequestCallback>();
  let sequence = 0;
  vi.spyOn(globalThis, "requestAnimationFrame").mockImplementation((callback) => { frames.set(++sequence, callback); return sequence; });
  vi.spyOn(globalThis, "cancelAnimationFrame").mockImplementation((id) => { frames.delete(id); });
  const complete = vi.fn(async () => {});
  const navigation = new ConversationNavigation({ model: () => undefined, request: vi.fn(), changed: () => {},
    initialWindow: { pending: () => true, complete } });
  const nextFrame = () => act(() => {
    const pending = [...frames.values()]; frames.clear();
    pending.forEach((callback) => callback(performance.now()));
  });
  const app = renderHook(({ visible }) => useAgentConversationNavigation({
    navigation, loadedItems: [], turnGroups: [{ key: "t", turnId: "t" }],
    virtualizer: {} as ReactVirtualizer<HTMLElement, Element>, shouldVirtualize: false, isVisible: visible,
  }), { initialProps: { visible: true } });
  nextFrame();
  expect(complete).not.toHaveBeenCalled();
  app.rerender({ visible: false });
  nextFrame();
  expect(complete).not.toHaveBeenCalled();
  app.rerender({ visible: true });
  nextFrame();
  expect(complete).not.toHaveBeenCalled();
  nextFrame();
  await waitFor(() => expect(complete).toHaveBeenCalledOnce());
});

it("shows the full directory before history is loaded, prefetches on hover, and locates only on click", async () => {
  const model = ConversationModel.fromSnapshot(localV2SnapshotSchema.parse({ version: 2, store_generation: "g", snapshot_seq: 100, conversation: { conversation_id: "c", summary_seq: 1, provider: "claude", title: "c", status: "idle", pinned: false, archived: false, updated_at: at, turn_count: 100 }, turns: Array.from({ length: 10 }, (_, i) => turn(i + 90)), has_older_turns: true }));
  const request = vi.fn(async (query: LocalV2NavigationQuery) => {
    const requestedCount = query.includeBody === false ? 20 : 10;
    const start = "nodeIndex" in query ? Math.max(0, query.nodeIndex! - (query.includeBody === false ? 9 : 4)) : "afterIndex" in query ? query.afterIndex! + 1 : Math.max(0, query.beforeIndex! - requestedCount);
    const count = Math.min(requestedCount, 100 - start);
    return { store_generation: "g", snapshot_seq: 100, node_count: 100,
      nodes: Array.from({ length: count }, (_, i) => ({ node_index: i + start, message_id: `m${i + start}`, turn_id: `t${i + start}`, turn_index: i + start, summary: `Prompt ${i + start}`, has_agent_response: false })),
      turns: query.includeBody === false ? [] : Array.from({ length: 10 }, (_, i) => turn(i + start)), directory_range: { start_index: start, end_index: start + count - 1, has_before: true, has_after: start + count < 100 }, range: query.includeBody === false ? null : { start_index: start, end_index: start + count - 1, has_before: true, has_after: start + count < 100 } };
  });
  const navigation = new ConversationNavigation({ model: () => model, request, changed: () => {} });
  const Harness = observer(() => {
    const body = navigation.visibleTurnIds().map((id, index) => ({ key: id, rowKey: conversationUserMessageRowKey(`m${id.slice(1)}`), rowIndex: index, turnGroupIndex: index, turnId: id, messageId: `m${id.slice(1)}`, summary: `Prompt ${id.slice(1)}`, hasAgentResponse: false }));
    const directory = useAgentConversationNavigation({ navigation, loadedItems: body.filter((item) => item.messageId !== "m10"), turnGroups: body, virtualizer: {} as ReactVirtualizer<HTMLElement, Element>, shouldVirtualize: false, isVisible: true, label: "User messages" });
    return <div data-testid="agent-gui-timeline"><div ref={directory.containerRef} data-agent-navigation-target={directory.targetMessageId}>
      <AgentMessageLocatorRail items={directory.items} label="User messages" panelItems={directory.panelItems} onPanelMore={directory.more} onPreview={directory.preview} onLocate={directory.locate} />
      {body.map((item) => <div key={item.key} data-agent-transcript-row={item.rowKey}>{item.summary}</div>)}
    </div></div>;
  });
  render(<Harness />);
  await waitFor(() => expect(screen.getByTestId("agent-message-locator").querySelectorAll(".agent-gui-message-locator__tick")).toHaveLength(100));
  expect(screen.getAllByText("Prompt 90")[0]).toBeTruthy();
  const tick = screen.getByTestId("agent-message-locator").querySelectorAll(".agent-gui-message-locator__tick")[10]!;
  fireEvent.mouseEnter(tick);
  await waitFor(() => expect(request).toHaveBeenCalledWith(expect.objectContaining({ nodeIndex: 10, includeBody: false })));
  expect(navigation.window).toBeNull();
  expect(model.turns.has("t10")).toBe(false);
  await waitFor(() => expect(tick.getAttribute("title")).toBe("Prompt 10"));
  expect(request.mock.calls.every(([query]) => query.includeBody === false)).toBe(true);
  fireEvent.click(tick);
  await waitFor(() => expect(navigation.window?.start_index).toBe(6));
  expect(model.historyStartIndex).toBe(90);
  expect(request.mock.calls.filter(([query]) => query.includeBody !== false)).toHaveLength(1);
  expect(navigation.visibleTurnIds()).not.toContain("t90");
  expect(screen.getAllByText("Prompt 10").length).toBeGreaterThan(0);
  await waitFor(() => expect(document.querySelector("[data-agent-navigation-target]")).toBeNull());
});
