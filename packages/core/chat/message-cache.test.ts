import { QueryClient, type InfiniteData } from "@tanstack/react-query";
import { describe, expect, it } from "vitest";
import { chatKeys } from "./queries";
import { upsertChatMessageToCaches } from "./message-cache";
import type { ChatMessage, ChatMessagesPage } from "../types";

const sessionId = "session-1";
const flatKey = chatKeys.messages(sessionId);
const pageKey = chatKeys.messagesPage(sessionId);

function qcWith(messages: ChatMessage[] | null, pages?: ChatMessagesPage[]) {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  if (messages) qc.setQueryData<ChatMessage[]>(flatKey, messages);
  if (pages) {
    qc.setQueryData<InfiniteData<ChatMessagesPage>>(pageKey, {
      pages,
      pageParams: pages.map((_, i) => (i === 0 ? null : { created_at: "x", id: "x" })),
    });
  }
  return qc;
}

function message(overrides: Partial<ChatMessage> = {}): ChatMessage {
  return {
    id: "msg-1",
    chat_session_id: sessionId,
    role: "user",
    content: "hello",
    task_id: "task-1",
    created_at: "2026-05-13T05:00:00Z",
    ...overrides,
  };
}

function page(messages: ChatMessage[]): ChatMessagesPage {
  return { messages, limit: 50, has_more: false, next_cursor: null };
}

function flatIds(qc: QueryClient) {
  return qc.getQueryData<ChatMessage[]>(flatKey)?.map((m) => m.id);
}

function pageIds(qc: QueryClient) {
  return qc
    .getQueryData<InfiniteData<ChatMessagesPage>>(pageKey)
    ?.pages.map((p) => p.messages.map((m) => m.id));
}

describe("upsertChatMessageToCaches", () => {
  it("writes into both caches in one call", () => {
    const existing = message({ id: "msg-0" });
    const qc = qcWith([existing], [page([existing])]);

    upsertChatMessageToCaches(qc, sessionId, message());

    expect(flatIds(qc)).toEqual(["msg-0", "msg-1"]);
    expect(pageIds(qc)).toEqual([["msg-0", "msg-1"]]);
  });

  it("appends to the newest page only, never to older pages", () => {
    const older = message({ id: "msg-old", created_at: "2026-05-13T04:00:00Z" });
    const latest = message({ id: "msg-latest" });
    // Page 0 is the newest window; later pages are older cursor pages.
    const qc = qcWith(null, [page([latest]), page([older])]);

    upsertChatMessageToCaches(qc, sessionId, message({ id: "msg-new" }));
    upsertChatMessageToCaches(qc, sessionId, message({ id: "msg-new" }));

    expect(pageIds(qc)).toEqual([["msg-latest", "msg-new"], ["msg-old"]]);
  });

  it("merges a repeat instead of appending, wherever the row lives", () => {
    const inOlderPage = message({ id: "msg-old" });
    const qc = qcWith([inOlderPage], [page([]), page([inOlderPage])]);

    upsertChatMessageToCaches(qc, sessionId, message({ id: "msg-old", elapsed_ms: 42 }));

    expect(pageIds(qc)).toEqual([[], ["msg-old"]]);
    expect(
      qc.getQueryData<InfiniteData<ChatMessagesPage>>(pageKey)?.pages[1]?.messages[0]
        ?.elapsed_ms,
    ).toBe(42);
  });

  // The reason the merge direction matters: the send response carries draft
  // attachments, the chat:message echo of the same send has no attachments
  // field at all. Whichever lands first, the attachments must survive.
  it("keeps the richer row regardless of arrival order", () => {
    const withAttachments = message({ attachments: [{ id: "att-1" } as never] });
    const fromWs = message();

    const sendFirst = qcWith([withAttachments], [page([withAttachments])]);
    upsertChatMessageToCaches(sendFirst, sessionId, fromWs);
    expect(sendFirst.getQueryData<ChatMessage[]>(flatKey)?.[0]?.attachments).toHaveLength(1);

    const wsFirst = qcWith([fromWs], [page([fromWs])]);
    upsertChatMessageToCaches(wsFirst, sessionId, withAttachments);
    expect(wsFirst.getQueryData<ChatMessage[]>(flatKey)?.[0]?.attachments).toHaveLength(1);
  });

  it("keeps the row reference stable when nothing was filled", () => {
    const existing = message({ elapsed_ms: 7 });
    const qc = qcWith([existing], [page([existing])]);

    upsertChatMessageToCaches(qc, sessionId, message({ elapsed_ms: 999, content: "x" }));

    expect(qc.getQueryData<ChatMessage[]>(flatKey)?.[0]).toBe(existing);
  });

  it("leaves an unfetched cache alone by default", () => {
    const qc = qcWith(null);

    upsertChatMessageToCaches(qc, sessionId, message());

    expect(qc.getQueryData(flatKey)).toBeUndefined();
    expect(qc.getQueryData(pageKey)).toBeUndefined();
  });

  it("seeds both caches for a brand-new session when asked", () => {
    const qc = qcWith(null);

    upsertChatMessageToCaches(qc, sessionId, message(), { seedIfMissing: true });

    expect(flatIds(qc)).toEqual(["msg-1"]);
    expect(pageIds(qc)).toEqual([["msg-1"]]);
    const seeded = qc.getQueryData<InfiniteData<ChatMessagesPage>>(pageKey);
    expect(seeded?.pages[0]?.has_more).toBe(false);
    expect(seeded?.pageParams).toEqual([null]);
  });

  it("does not seed twice when the same send is applied again", () => {
    const qc = qcWith(null);

    upsertChatMessageToCaches(qc, sessionId, message(), { seedIfMissing: true });
    upsertChatMessageToCaches(qc, sessionId, message(), { seedIfMissing: true });

    expect(flatIds(qc)).toEqual(["msg-1"]);
    expect(pageIds(qc)).toEqual([["msg-1"]]);
  });
});
