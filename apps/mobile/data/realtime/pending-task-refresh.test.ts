// @vitest-environment node
import { QueryClient, QueryObserver } from "@tanstack/react-query";
import type { ChatPendingTask } from "@multica/core/types";
import { describe, expect, it, vi } from "vitest";
import { chatKeys } from "@/data/queries/chat";
import { invalidatePendingTask, seedAcceptedPendingTask } from "./chat-ws-updaters";

vi.mock("@/data/api", () => ({ api: {} }));

const sessionId = "session-1";
const queryKey = chatKeys.pendingTask(sessionId);
const oldTask: ChatPendingTask = { task_id: "task-1", status: "dispatched" };
const currentTask: ChatPendingTask = { task_id: "task-1", status: "running" };

function deferred<T>() {
  let resolve!: (value: T) => void;
  const promise = new Promise<T>((done) => { resolve = done; });
  return { promise, resolve };
}

describe("pending-task refresh after a server change", () => {
  it.each([
    { cached: false, readsSignal: true },
    { cached: false, readsSignal: false },
    { cached: true, readsSignal: true },
    { cached: true, readsSignal: false },
  ])("replaces the old request (cached=$cached, readsSignal=$readsSignal)", async ({ cached, readsSignal }) => {
    const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
    if (cached) qc.setQueryData(queryKey, oldTask);
    const oldResponse = deferred<ChatPendingTask>();
    let oldSignal: AbortSignal | undefined;
    const queryFn = vi.fn((context: { signal: AbortSignal }) => {
      if (queryFn.mock.calls.length === 1) {
        // Even a transport that reads but ignores abort must not overwrite
        // the new result when its pre-event response eventually arrives.
        if (readsSignal) oldSignal = context.signal;
        return oldResponse.promise;
      }
      return Promise.resolve(currentTask);
    });
    const observer = new QueryObserver(qc, { queryKey, queryFn, staleTime: Infinity });
    const unsubscribe = observer.subscribe(() => {});
    void observer.refetch();

    try {
      expect(queryFn).toHaveBeenCalledTimes(1);
      const refresh = invalidatePendingTask(qc, sessionId);
      await vi.waitFor(() => expect(queryFn).toHaveBeenCalledTimes(2));
      await refresh;
      expect(qc.getQueryData(queryKey)).toEqual(currentTask);
      if (readsSignal) expect(oldSignal?.aborted).toBe(true);

      oldResponse.resolve(oldTask);
      await oldResponse.promise;
      await new Promise<void>((resolve) => setImmediate(resolve));
      expect(qc.getQueryData(queryKey)).toEqual(currentTask);
      expect(qc.getQueryState(queryKey)?.isInvalidated).toBe(false);
    } finally {
      oldResponse.resolve(oldTask);
      unsubscribe();
      qc.clear();
    }
  });

  it("refreshes again when another event arrives during the replacement request", async () => {
    const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
    const first = deferred<ChatPendingTask>();
    const second = deferred<ChatPendingTask>();
    const queryFn = vi.fn()
      .mockImplementationOnce(() => first.promise)
      .mockImplementationOnce(() => second.promise)
      .mockResolvedValue({});
    const observer = new QueryObserver(qc, { queryKey, queryFn, staleTime: Infinity });
    const unsubscribe = observer.subscribe(() => {});

    try {
      const firstRefresh = invalidatePendingTask(qc, sessionId);
      await vi.waitFor(() => expect(queryFn).toHaveBeenCalledTimes(2));
      await invalidatePendingTask(qc, sessionId);
      await firstRefresh;
      expect(queryFn).toHaveBeenCalledTimes(3);
      expect(qc.getQueryData(queryKey)).toEqual({});

      first.resolve(oldTask);
      second.resolve(currentTask);
      await Promise.all([first.promise, second.promise]);
      await new Promise<void>((resolve) => setImmediate(resolve));
      expect(qc.getQueryData(queryKey)).toEqual({});
    } finally {
      first.resolve(oldTask);
      second.resolve(currentTask);
      unsubscribe();
      qc.clear();
    }
  });

  it.each([false, true])("preserves an accepted-send cache patch (cached head=%s)", async (cached) => {
    const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
    if (cached) qc.setQueryData(queryKey, oldTask);
    const first = deferred<ChatPendingTask>();
    const replacement = deferred<ChatPendingTask>();
    const queryFn = vi.fn()
      .mockImplementationOnce(() => first.promise)
      .mockImplementation(() => replacement.promise);
    const observer = new QueryObserver(qc, { queryKey, queryFn, staleTime: Infinity });
    const unsubscribe = observer.subscribe(() => {});
    void observer.refetch();

    try {
      seedAcceptedPendingTask(qc, {
        chat_session_id: sessionId,
        task_id: "accepted-task",
        created_at: "2026-09-22T00:00:00Z",
        queued: cached,
      });
      await vi.waitFor(() => expect(queryFn).toHaveBeenCalledTimes(2));
      expect(qc.getQueryData(queryKey)).toMatchObject(cached ? {
        ...oldTask,
        queued_tasks: [{ task_id: "accepted-task", status: "queued" }],
      } : { task_id: "accepted-task", status: "queued" });
      replacement.resolve({ task_id: "accepted-task", status: "running" });
      await vi.waitFor(() => expect(qc.getQueryData(queryKey)).toMatchObject({ status: "running" }));
    } finally {
      first.resolve(oldTask);
      replacement.resolve(currentTask);
      unsubscribe();
      qc.clear();
    }
  });

  it.each(["inactive", "disabled"] as const)("does not start a fetch for an %s query", async (state) => {
    const qc = new QueryClient();
    qc.setQueryData(queryKey, oldTask);
    const queryFn = vi.fn().mockResolvedValue(currentTask);
    const observer = new QueryObserver(qc, {
      queryKey, queryFn, staleTime: Infinity, enabled: state !== "disabled",
    });
    const unsubscribe = state === "disabled" ? observer.subscribe(() => {}) : () => {};

    try {
      await invalidatePendingTask(qc, sessionId);
      expect(queryFn).not.toHaveBeenCalled();
      expect(qc.getQueryData(queryKey)).toEqual(oldTask);
      expect(qc.getQueryState(queryKey)?.isInvalidated).toBe(true);
    } finally {
      unsubscribe();
      qc.clear();
    }
  });
});
