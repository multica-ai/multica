// @vitest-environment node
import { beforeEach, describe, expect, it, vi } from "vitest";
import { QueryClient, QueryObserver } from "@tanstack/react-query";
import type { ChatPendingTask } from "@multica/core/types";

const state = vi.hoisted(() => ({
  qc: undefined as unknown as QueryClient,
  ws: {} as object,
  effectDeps: undefined as readonly unknown[] | undefined,
  setups: [] as Array<(ws: MockWS, wsId: string) => void>,
}));
type Handler = (payload: { chat_session_id?: string }) => void;
interface MockWS { on: (event: string, handler: Handler) => () => void; onReconnect: (handler: () => void) => () => void }
vi.mock("react", async (original) => ({
  ...await original<typeof import("react")>(),
  useEffect: (setup: () => void, deps: readonly unknown[]) => {
    if (!state.effectDeps || deps.some((value, i) => !Object.is(value, state.effectDeps![i]))) setup();
    state.effectDeps = deps;
  },
}));
vi.mock("@tanstack/react-query", async (original) => ({
  ...await original<typeof import("@tanstack/react-query")>(),
  useQueryClient: () => state.qc,
}));
vi.mock("./realtime-provider", () => ({ useWSClient: () => state.ws }));
vi.mock("@/lib/use-ws-subscriptions", () => ({
  useWSSubscriptions: (setup: (ws: MockWS, wsId: string) => void) => state.setups.push(setup),
}));
vi.mock("@/data/api", () => ({ api: { getPendingChatTask: vi.fn() } }));
import { api } from "@/data/api";
import { chatKeys, pendingChatTaskOptions } from "@/data/queries/chat";
import { beginChatSend } from "@/data/chat-send-lifecycle";
import { useChatSessionRealtime } from "./use-chat-session-realtime";

const TASK = "00000000-0000-4000-8000-000000000001";

describe("chat session snapshot lifecycle", () => {
  beforeEach(() => {
    state.qc = new QueryClient();
    state.ws = {};
    state.effectDeps = undefined;
    state.setups = [];
    vi.mocked(api.getPendingChatTask).mockReset();
  });

  it("invalidates the warm session on A → B → A even without reconnect events", () => {
    state.qc.setQueryData(chatKeys.messages("A"), []);
    state.qc.setQueryData(chatKeys.pendingTask("A"), { task_id: TASK });
    useChatSessionRealtime("A");
    useChatSessionRealtime("B");
    // A finished while its per-session event handler was unmounted.
    state.qc.setQueryData(chatKeys.messages("A"), []);
    state.qc.setQueryData(chatKeys.pendingTask("A"), { task_id: TASK });
    useChatSessionRealtime("A");
    expect(state.qc.getQueryState(chatKeys.messages("A"))?.isInvalidated).toBe(true);
    expect(state.qc.getQueryState(chatKeys.pendingTask("A"))?.isInvalidated).toBe(true);
  });

  it("reconciles a new WS client but does not refetch when inline callbacks change", () => {
    const invalidate = vi.spyOn(state.qc, "invalidateQueries");
    useChatSessionRealtime("A", () => {});
    expect(invalidate).toHaveBeenCalledTimes(2);
    useChatSessionRealtime("A", () => {});
    expect(invalidate).toHaveBeenCalledTimes(2);
    state.ws = {};
    useChatSessionRealtime("A", () => {});
    expect(invalidate).toHaveBeenCalledTimes(4);
  });

  it("recovers final messages from task:completed when chat:done was lost", async () => {
    useChatSessionRealtime("A");
    state.qc.setQueryData(chatKeys.messages("A"), []);
    state.qc.setQueryData(chatKeys.pendingTask("A"), { task_id: TASK });
    vi.mocked(api.getPendingChatTask).mockResolvedValue({ task_id: TASK });
    const pending = new QueryObserver(state.qc, pendingChatTaskOptions("A"));
    const unsub = pending.subscribe(() => {});
    await vi.waitFor(() => expect(state.qc.getQueryState(chatKeys.pendingTask("A"))?.fetchStatus).toBe("idle"));
    vi.mocked(api.getPendingChatTask).mockResolvedValue({});
    const handlers = new Map<string, Handler>();
    state.setups[0]({
      on: (event, handler) => { handlers.set(event, handler); return () => {}; },
      onReconnect: () => () => {},
    }, "workspace");
    handlers.get("task:completed")!({ chat_session_id: "B" });
    expect(state.qc.getQueryState(chatKeys.messages("A"))?.isInvalidated).toBe(false);
    handlers.get("task:completed")!({ chat_session_id: "A" });
    await vi.waitFor(() => expect(state.qc.getQueryData(chatKeys.pendingTask("A"))).toEqual({}));
    expect(state.qc.getQueryState(chatKeys.messages("A"))?.isInvalidated).toBe(true);
    unsub();
    state.qc.clear();
  });

  it.each<ChatPendingTask>([
    { task_id: "optimistic-task", status: "queued" },
    { task_id: TASK, queued_tasks: [{ task_id: "optimistic-follow-up", status: "queued", created_at: "2026-10-06T00:00:00Z" }] },
  ])("defers session-entry reconciliation while a send has a local placeholder", (localPending) => {
    useChatSessionRealtime(null);
    const optimistic = [{ id: "optimistic-message", content: "first message" }];
    state.qc.setQueryData(chatKeys.messages("A"), optimistic);
    state.qc.setQueryData(chatKeys.pendingTask("A"), localPending);
    const invalidate = vi.spyOn(state.qc, "invalidateQueries");
    const finishSend = beginChatSend("A");
    useChatSessionRealtime("A");
    expect(invalidate).not.toHaveBeenCalled();
    expect(state.qc.getQueryData(chatKeys.messages("A"))).toEqual(optimistic);
    expect(state.qc.getQueryData(chatKeys.pendingTask("A"))).toEqual(localPending);
    finishSend();
  });

  it("refreshes the running task's cached trace when re-entering its session", () => {
    state.qc.setQueryData(chatKeys.pendingTask("A"), { task_id: TASK });
    state.qc.setQueryData(chatKeys.taskMessages(TASK), []);
    useChatSessionRealtime("A");
    expect(state.qc.getQueryState(chatKeys.taskMessages(TASK))?.isInvalidated).toBe(true);
  });
});
