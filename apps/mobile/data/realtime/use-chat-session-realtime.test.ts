// @vitest-environment node
import { QueryClient, QueryObserver } from "@tanstack/react-query";
import type { ChatPendingTask } from "@multica/core/types";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { chatKeys } from "@/data/queries/chat";
import { useChatSessionRealtime } from "./use-chat-session-realtime";

type EventHandler = (payload: unknown) => void;
interface MockWS {
  on: (event: string, handler: EventHandler) => () => void;
  onReconnect: (handler: () => void) => () => void;
}
type SubscriptionSetup = (ws: MockWS) => (() => void)[] | undefined;

const state = vi.hoisted(() => ({
  qc: undefined as unknown as QueryClient,
  subscriptionSetups: [] as SubscriptionSetup[],
}));

vi.mock("@tanstack/react-query", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@tanstack/react-query")>()),
  useQueryClient: () => state.qc,
}));
vi.mock("@/lib/use-ws-subscriptions", () => ({
  useWSSubscriptions: (setup: SubscriptionSetup) => {
    state.subscriptionSetups.push(setup);
  },
}));
vi.mock("@/data/api", () => ({ api: {} }));

const sessionId = "session-1";
const pendingKey = chatKeys.pendingTask(sessionId);

function connect() {
  const handlers = new Map<string, EventHandler>();
  expect(state.subscriptionSetups).toHaveLength(1);
  state.subscriptionSetups[0]({
    on: (event, handler) => {
      handlers.set(event, handler);
      return () => {};
    },
    onReconnect: () => () => {},
  });
  return (event: string, payload: unknown) => {
    const handler = handlers.get(event);
    if (!handler) throw new Error(`no handler for ${event}`);
    handler(payload);
  };
}

describe("useChatSessionRealtime task lifecycle", () => {
  beforeEach(() => {
    state.qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
    state.subscriptionSetups.length = 0;
  });

  afterEach(() => state.qc.clear());

  it("refetches the authoritative pending task when it waits and resumes", async () => {
    let serverTask: ChatPendingTask = { task_id: "task-1", status: "dispatched" };
    state.qc.setQueryData(pendingKey, serverTask);
    const queryFn = vi.fn(async () => serverTask);
    const observer = new QueryObserver(state.qc, {
      queryKey: pendingKey,
      queryFn,
      staleTime: Infinity,
    });
    const unsubscribe = observer.subscribe(() => {});
    useChatSessionRealtime(sessionId);
    const emit = connect();

    try {
      serverTask = {
        task_id: "task-1",
        status: "waiting_local_directory",
        wait_reason: "checkout (holder: task-2)",
      };
      emit("task:waiting_local_directory", {
        ...serverTask,
        agent_id: "agent-1",
        issue_id: "",
        chat_session_id: sessionId,
      });
      await vi.waitFor(() => expect(state.qc.getQueryData(pendingKey)).toEqual(serverTask));

      serverTask = { task_id: "task-1", status: "running" };
      emit("task:running", {
        ...serverTask,
        agent_id: "agent-1",
        issue_id: "",
        chat_session_id: sessionId,
      });
      await vi.waitFor(() => expect(state.qc.getQueryData(pendingKey)).toEqual(serverTask));
      expect(queryFn).toHaveBeenCalledTimes(2);
    } finally {
      unsubscribe();
    }
  });

  it.each(["task:waiting_local_directory", "task:running"])(
    "ignores %s from another chat or an issue task",
    (event) => {
      state.qc.setQueryData(pendingKey, { task_id: "task-1", status: "running" });
      useChatSessionRealtime(sessionId);
      const emit = connect();

      for (const chatSessionId of ["other-session", undefined]) {
        emit(event, {
          task_id: "task-2",
          agent_id: "agent-1",
          issue_id: "issue-1",
          chat_session_id: chatSessionId,
          status: event.slice("task:".length),
        });
      }

      expect(state.qc.getQueryState(pendingKey)?.isInvalidated).toBe(false);
      expect(state.qc.getQueryData(pendingKey)).toEqual({
        task_id: "task-1",
        status: "running",
      });
    },
  );
});
