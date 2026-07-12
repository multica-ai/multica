import { describe, it, expect } from "vitest";
import { QueryClient, type InfiniteData } from "@tanstack/react-query";
import { chatKeys } from "@multica/core/chat/queries";
import type { ChatMessage, ChatMessagesPage, ChatPendingTask } from "@multica/core/types";
import { hasOptimisticInFlight } from "./use-chat-controller";

// hasOptimisticInFlight is the discriminator the stale-session self-heal uses
// to EXEMPT a just-created session (awaiting the list refetch) from being
// dropped as dangling. The critical property (MUL-4171 review): it must key
// off OPTIMISTIC in-flight writes, NOT merely "has cached messages" — a session
// deleted elsewhere can still hold real cached history and must remain eligible
// for self-heal.
const sid = "session-1";

function msg(id: string): ChatMessage {
  return {
    id,
    chat_session_id: sid,
    role: "user",
    content: "hi",
    task_id: null,
    created_at: "2026-07-08T00:00:00Z",
  };
}

describe("hasOptimisticInFlight", () => {
  it("is false with an empty cache", () => {
    expect(hasOptimisticInFlight(new QueryClient(), sid)).toBe(false);
  });

  it("is false when only real (non-optimistic) cached history exists", () => {
    const qc = new QueryClient();
    qc.setQueryData<ChatMessage[]>(chatKeys.messages(sid), [msg("real-1"), msg("real-2")]);
    qc.setQueryData<InfiniteData<ChatMessagesPage>>(chatKeys.messagesPage(sid), {
      pages: [{ messages: [msg("real-1")], limit: 50, has_more: false, next_cursor: null }],
      pageParams: [null],
    });
    // A completed session sets pendingTask to {} (no task_id).
    qc.setQueryData<ChatPendingTask>(chatKeys.pendingTask(sid), {} as ChatPendingTask);
    expect(hasOptimisticInFlight(qc, sid)).toBe(false);
  });

  it("is true while a pending task is in flight (task_id set)", () => {
    const qc = new QueryClient();
    qc.setQueryData<ChatPendingTask>(chatKeys.pendingTask(sid), {
      task_id: "optimistic-abc",
      status: "queued",
      created_at: "2026-07-08T00:00:00Z",
    });
    expect(hasOptimisticInFlight(qc, sid)).toBe(true);
  });

  it("is true when an optimistic- message sits in the flat cache", () => {
    const qc = new QueryClient();
    qc.setQueryData<ChatMessage[]>(chatKeys.messages(sid), [msg("optimistic-123")]);
    expect(hasOptimisticInFlight(qc, sid)).toBe(true);
  });

  it("is true when an optimistic- message sits in the paged cache", () => {
    const qc = new QueryClient();
    qc.setQueryData<InfiniteData<ChatMessagesPage>>(chatKeys.messagesPage(sid), {
      pages: [{ messages: [msg("optimistic-xyz")], limit: 50, has_more: false, next_cursor: null }],
      pageParams: [null],
    });
    expect(hasOptimisticInFlight(qc, sid)).toBe(true);
  });
});
