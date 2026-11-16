// @vitest-environment node
import { QueryClient, QueryObserver } from "@tanstack/react-query";
import type { TaskMessagePayload } from "@multica/core/types";
import { afterEach, expect, it, vi } from "vitest";
import { api } from "@/data/api";
import { chatKeys, taskMessagesOptions } from "@/data/queries/chat";
import { useChatSessionRealtime } from "./use-chat-session-realtime";
import type { WSSubscriptionSetup } from "@/lib/use-ws-subscriptions";
import type { WSClient } from "./ws-client";

const state = vi.hoisted(() => ({
  qc: undefined as unknown as QueryClient,
  setup: undefined as WSSubscriptionSetup | undefined,
}));
vi.mock("@/data/api", () => ({ api: { listTaskMessages: vi.fn() } }));
vi.mock("@tanstack/react-query", async (original) => ({
  ...await original<typeof import("@tanstack/react-query")>(), useQueryClient: () => state.qc,
}));
vi.mock("@/lib/use-ws-subscriptions", () => ({
  useWSSubscriptions: (setup: WSSubscriptionSetup) => { state.setup = setup; },
}));
afterEach(() => state.qc.clear());

it("backfills the current session's missed trace and preserves racing live rows", async () => {
  state.qc = new QueryClient({ defaultOptions: { queries: { retry: false, gcTime: Infinity } } });
  const taskId = "123e4567-e89b-12d3-a456-426614174000";
  const unrelated = "123e4567-e89b-12d3-a456-426614174001";
  const session = "session";
  const row = (seq: number): TaskMessagePayload => ({
    task_id: taskId, issue_id: "", chat_session_id: session, seq, type: "text", content: `${seq}`,
  });
  const key = chatKeys.taskMessages(taskId);
  state.qc.setQueryData(chatKeys.messages(session), [{ task_id: taskId }]);
  state.qc.setQueryData(chatKeys.pendingTask(session), { task_id: taskId });
  state.qc.setQueryData(key, [row(1)]);
  state.qc.setQueryData(chatKeys.taskMessages(unrelated), [row(9)]);
  const observer = new QueryObserver(state.qc, taskMessagesOptions(taskId));
  const stop = observer.subscribe(() => {});
  let resolve!: (rows: TaskMessagePayload[]) => void;
  vi.mocked(api.listTaskMessages).mockImplementation(() => new Promise((done) => { resolve = done; }));
  const handlers = new Map<string, (payload: unknown) => void>();
  let reconnect!: () => void;
  useChatSessionRealtime(session);
  state.setup!({
    on: (event: string, handler: (payload: unknown) => void) => { handlers.set(event, handler); return () => {}; },
    onReconnect: (handler: () => void) => { reconnect = handler; return () => {}; },
  } as unknown as WSClient, "workspace");
  reconnect();
  await vi.waitFor(() => expect(api.listTaskMessages).toHaveBeenCalledWith(taskId, { signal: expect.any(AbortSignal) }));
  handlers.get("task:message")!(row(3));
  resolve([row(1), row(2)]);
  await vi.waitFor(() => expect(state.qc.getQueryData<TaskMessagePayload[]>(key)?.map((r) => r.seq)).toEqual([1, 2, 3]));
  expect(state.qc.getQueryState(chatKeys.taskMessages(unrelated))?.isInvalidated).toBe(false);
  expect(api.listTaskMessages).toHaveBeenCalledOnce();
  stop();
});
