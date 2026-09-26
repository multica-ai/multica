// @vitest-environment node
import { QueryClient, QueryObserver } from "@tanstack/react-query";
import type { ChatPendingTask } from "@multica/core/types";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { chatKeys } from "@/data/queries/chat";
import { useChatSessionRealtime } from "./use-chat-session-realtime";

type EventHandler = (payload: unknown) => void;
type SubscriptionSetup = (ws: {
  on: (event: string, handler: EventHandler) => () => void;
  onReconnect: (handler: () => void) => () => void;
}) => (() => void)[] | undefined;

const state = vi.hoisted(() => ({
  qc: undefined as unknown as QueryClient,
  setup: undefined as SubscriptionSetup | undefined,
}));

vi.mock("@tanstack/react-query", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@tanstack/react-query")>()),
  useQueryClient: () => state.qc,
}));
vi.mock("@/lib/use-ws-subscriptions", () => ({
  useWSSubscriptions: (setup: SubscriptionSetup) => { state.setup = setup; },
}));
vi.mock("@/data/api", () => ({ api: {} }));

describe("chat pending-task event refresh", () => {
  beforeEach(() => {
    state.qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
    state.setup = undefined;
  });

  it.each(["chat:message", "reconnect"])("replaces the first pending request on %s", async (event) => {
    const queryKey = chatKeys.pendingTask("session-1");
    let resolveOld!: (value: ChatPendingTask) => void;
    const oldResponse = new Promise<ChatPendingTask>((resolve) => { resolveOld = resolve; });
    const current = { task_id: "task-2", status: "queued" };
    const queryFn = vi.fn()
      .mockImplementationOnce(() => oldResponse)
      .mockResolvedValue(current);
    const observer = new QueryObserver(state.qc, { queryKey, queryFn, staleTime: Infinity });
    const unsubscribe = observer.subscribe(() => {});
    const handlers = new Map<string, EventHandler>();
    let reconnect = () => {};
    useChatSessionRealtime("session-1");
    state.setup!({
      on: (name, handler) => {
        handlers.set(name, handler);
        return () => {};
      },
      onReconnect: (handler) => {
        reconnect = handler;
        return () => {};
      },
    });

    try {
      if (event === "reconnect") reconnect();
      else handlers.get(event)!({ chat_session_id: "session-1" });
      await vi.waitFor(() => expect(state.qc.getQueryData(queryKey)).toEqual(current));
      expect(queryFn).toHaveBeenCalledTimes(2);
    } finally {
      resolveOld({ task_id: "task-1", status: "dispatched" });
      unsubscribe();
      state.qc.clear();
    }
  });
});
