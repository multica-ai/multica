import { describe, it, expect, vi } from "vitest";
import { renderHook, act } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { create } from "zustand";

// JEH-654 regression guard. The chat:done / task:completed / task:failed
// handlers used to write `{}` into the pending-task cache before
// invalidating. That created a flicker window: a queued successor task
// for the same session would be hidden until refetch returned. We assert
// the handlers leave the existing cache value alone — invalidation alone
// drives the refetch and the next-task surface.
//
// Lives in packages/views (not packages/core) because it uses
// @testing-library/react, which is only configured under views' jsdom
// environment.

// useRealtimeSync's platform helpers (getCurrentWsId etc.) are only read
// inside the workspace:deleted/member:removed handlers — irrelevant to
// the chat lifecycle assertions below. We let them resolve normally.

import { useRealtimeSync } from "@multica/core/realtime";
import type { WSClient } from "@multica/core/api";
import { chatKeys } from "@multica/core/chat/queries";

type Listener = (payload: unknown) => void;

function makeMockWS() {
  const handlers = new Map<string, Set<Listener>>();
  const ws = {
    on(event: string, handler: Listener) {
      if (!handlers.has(event)) handlers.set(event, new Set());
      handlers.get(event)!.add(handler);
      return () => handlers.get(event)?.delete(handler);
    },
    onAny() {
      return () => {};
    },
    onReconnect() {
      return () => {};
    },
    fire(event: string, payload: unknown) {
      handlers.get(event)?.forEach((h) => h(payload));
    },
  };
  return ws as unknown as WSClient & { fire: (e: string, p: unknown) => void };
}

const SESSION_ID = "session-1";
const RUNNING_TASK = { task_id: "task-running", status: "running" };
const SUCCESSOR_TASK = { task_id: "task-successor", status: "queued" };

function makeAuthStore() {
  return create<{ user: { id: string } | null }>(() => ({
    user: { id: "user-1" },
  }));
}

function renderSync(qc: QueryClient, ws: WSClient) {
  const authStore = makeAuthStore();
  return renderHook(
    () =>
      useRealtimeSync(ws, {
        authStore: authStore as unknown as Parameters<typeof useRealtimeSync>[1]["authStore"],
      }),
    {
      wrapper: ({ children }) => (
        <QueryClientProvider client={qc}>{children}</QueryClientProvider>
      ),
    },
  );
}

describe("useRealtimeSync chat lifecycle (JEH-654)", () => {
  it("chat:done invalidates pending-task without clearing it to {}", () => {
    const ws = makeMockWS();
    const qc = new QueryClient({
      defaultOptions: { queries: { retry: false, gcTime: 0 } },
    });
    qc.setQueryData(chatKeys.pendingTask(SESSION_ID), RUNNING_TASK);
    const invalidate = vi.spyOn(qc, "invalidateQueries");

    renderSync(qc, ws);

    act(() => {
      ws.fire("chat:done", { task_id: "task-running", chat_session_id: SESSION_ID });
    });

    // The cache value is left intact: the refetch driven by invalidation
    // is the single thing that updates it. Pre-clearing was the flicker.
    expect(qc.getQueryData(chatKeys.pendingTask(SESSION_ID))).toEqual(RUNNING_TASK);
    // But we did invalidate so the refetch will run.
    const calls = invalidate.mock.calls.map((c) => c[0]);
    expect(calls).toEqual(
      expect.arrayContaining([
        expect.objectContaining({ queryKey: chatKeys.pendingTask(SESSION_ID) }),
      ]),
    );
  });

  it("task:completed for a chat session does not clobber a queued successor in cache", () => {
    const ws = makeMockWS();
    const qc = new QueryClient({
      defaultOptions: { queries: { retry: false, gcTime: 0 } },
    });
    qc.setQueryData(chatKeys.pendingTask(SESSION_ID), SUCCESSOR_TASK);

    renderSync(qc, ws);

    act(() => {
      ws.fire("task:completed", {
        task_id: "task-running",
        chat_session_id: SESSION_ID,
      });
    });

    // The successor must remain — pre-JEH-654 code wrote {} here, which
    // briefly removed the spinner / waiting indicator before the
    // invalidation refetch put the same successor back.
    expect(qc.getQueryData(chatKeys.pendingTask(SESSION_ID))).toEqual(SUCCESSOR_TASK);
  });

  it("task:failed for a chat session does not clobber the cache", () => {
    const ws = makeMockWS();
    const qc = new QueryClient({
      defaultOptions: { queries: { retry: false, gcTime: 0 } },
    });
    qc.setQueryData(chatKeys.pendingTask(SESSION_ID), SUCCESSOR_TASK);

    renderSync(qc, ws);

    act(() => {
      ws.fire("task:failed", {
        task_id: "task-running",
        chat_session_id: SESSION_ID,
      });
    });

    expect(qc.getQueryData(chatKeys.pendingTask(SESSION_ID))).toEqual(SUCCESSOR_TASK);
  });
});
