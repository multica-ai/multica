// @vitest-environment jsdom

import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { cleanup, renderHook, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type { InfiniteData } from "@tanstack/react-query";
import { createElement, type ReactNode } from "react";

const h = vi.hoisted(() => ({
  sendChatMessage: vi.fn(),
  cancelTaskById: vi.fn(),
  deleteChatSession: vi.fn(),
  listChatMessages: vi.fn(),
  getPendingChatTask: vi.fn(),
  listChatDraftRestores: vi.fn(),
  store: {
    appliedDraftRestoreIds: [] as string[],
    markDraftRestoreApplied: vi.fn(),
    forgetDraftRestoreApplied: vi.fn(),
    inputDrafts: {} as Record<string, string>,
    inputDraftAttachments: {} as Record<string, unknown[]>,
    setInputDraft: vi.fn(),
    setInputDraftAttachments: vi.fn(),
    pendingSendRestores: {} as Record<string, unknown[]>,
    enqueuePendingSendRestore: vi.fn(),
    dequeuePendingSendRestore: vi.fn(),
  },
}));

vi.mock("@multica/core/api", async () => {
  const actual = await vi.importActual<typeof import("@multica/core/api")>(
    "@multica/core/api",
  );
  return {
    ...actual,
    api: {
      sendChatMessage: h.sendChatMessage,
      cancelTaskById: h.cancelTaskById,
      deleteChatSession: h.deleteChatSession,
      listChatMessages: h.listChatMessages,
      getPendingChatTask: h.getPendingChatTask,
      listChatDraftRestores: h.listChatDraftRestores,
    },
  };
});
vi.mock("@multica/core/hooks", () => ({ useWorkspaceId: () => "ws-1" }));
vi.mock("@multica/core/chat", () => ({
  useChatStore: Object.assign((sel: (s: typeof h.store) => unknown) => sel(h.store), {
    getState: () => h.store,
  }),
}));
// `@multica/core/realtime` is deliberately NOT mocked: removeChatMessageFromCaches
// is the behaviour under test.

import { chatKeys } from "@multica/core/chat/queries";
import type { ChatMessage, ChatMessagesPage } from "@multica/core/types";
import { useBuilderSession } from "./use-builder-session";

const sessionId = "session-1";

function harness() {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  const wrapper = ({ children }: { children: ReactNode }) =>
    createElement(QueryClientProvider, { client: qc }, children);
  const { result } = renderHook(
    () => useBuilderSession({ sessionId, encodeInput: (text: string) => text }),
    { wrapper },
  );
  return { qc, result };
}

function pagedIds(qc: QueryClient) {
  return qc
    .getQueryData<InfiniteData<ChatMessagesPage>>(chatKeys.messagesPage(sessionId))
    ?.pages.flatMap((p) => p.messages.map((m) => m.id));
}

function flatIds(qc: QueryClient) {
  return qc
    .getQueryData<ChatMessage[]>(chatKeys.messages(sessionId))
    ?.map((m) => m.id);
}

beforeEach(() => {
  vi.clearAllMocks();
  h.listChatMessages.mockResolvedValue([]);
  h.getPendingChatTask.mockResolvedValue({ task_id: "task-1", status: "running" });
  h.listChatDraftRestores.mockResolvedValue({ restores: [] });
  h.sendChatMessage.mockResolvedValue({
    message_id: "msg-1",
    task_id: "task-1",
    created_at: "2026-08-07T00:00:00Z",
  });
  h.deleteChatSession.mockResolvedValue(undefined);
});

afterEach(cleanup);

// Regression (MUL-5711 review): the Builder send seeds BOTH message caches, so
// every Builder cleanup path has to clear both. `messagesPage` is
// staleTime: Infinity — a copy left behind there stays fresh forever, and any
// chat surface opening the same session reads it instead of the server.
describe("useBuilderSession cache cleanup", () => {
  it("seeds both caches on send", async () => {
    // Server truth once the send is accepted. The Builder holds an active
    // observer on the flat cache, so its reconciling refetch lands on top of the
    // seed within the test — the paged cache has no observer here and keeps the
    // seeded row until something reads it.
    h.listChatMessages.mockResolvedValue([
      {
        id: "msg-1",
        chat_session_id: sessionId,
        role: "user",
        content: "hello",
        task_id: "task-1",
        created_at: "2026-08-07T00:00:00Z",
      },
    ]);
    const { qc, result } = harness();

    await result.current.send("hello");

    await waitFor(() => expect(pagedIds(qc)).toEqual(["msg-1"]));
    await waitFor(() => expect(flatIds(qc)).toEqual(["msg-1"]));
  });

  it("drops both caches when the conversation is discarded", async () => {
    const { qc, result } = harness();

    await result.current.send("hello");
    await waitFor(() => expect(pagedIds(qc)).toEqual(["msg-1"]));

    await result.current.destroy();

    expect(qc.getQueryData(chatKeys.messages(sessionId))).toBeUndefined();
    expect(qc.getQueryData(chatKeys.messagesPage(sessionId))).toBeUndefined();
  });

  it("removes a restored prompt from both caches on stop", async () => {
    const { qc, result } = harness();
    h.cancelTaskById.mockResolvedValue({
      cancelled_chat_message: {
        chat_session_id: sessionId,
        message_id: "msg-1",
        content: "hello",
        restore_to_input: true,
      },
    });

    await result.current.send("hello");
    await waitFor(() => expect(pagedIds(qc)).toEqual(["msg-1"]));

    await result.current.stop();

    // The server deleted this prompt; neither cache may keep serving it.
    expect(pagedIds(qc) ?? []).toEqual([]);
    expect(flatIds(qc) ?? []).toEqual([]);
  });

  it("marks both caches stale when stop fails", async () => {
    const { qc, result } = harness();
    h.cancelTaskById.mockRejectedValue(new Error("nope"));

    await result.current.send("hello");
    await waitFor(() => expect(pagedIds(qc)).toEqual(["msg-1"]));

    // Clear the flag the send's own invalidate raised, so this asserts what
    // STOP did rather than what send did (setQueryData settles the query).
    qc.setQueryData(
      chatKeys.messagesPage(sessionId),
      qc.getQueryData(chatKeys.messagesPage(sessionId)),
    );
    expect(qc.getQueryState(chatKeys.messagesPage(sessionId))?.isInvalidated).toBe(false);

    await result.current.stop();

    // A failed cancel may still have landed server-side, so both caches have to
    // be re-read rather than left pinned by staleTime: Infinity.
    await waitFor(() => {
      expect(qc.getQueryState(chatKeys.messagesPage(sessionId))?.isInvalidated).toBe(true);
    });
  });
});
