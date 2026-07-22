/**
 * @vitest-environment jsdom
 */
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { act, renderHook } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type { ReactNode } from "react";

import { setApiInstance } from "../api";
import type { ApiClient } from "../api/client";
import { useConsumeChatDraftRestore, useSetChatSessionArchived } from "./mutations";
import { chatKeys } from "./queries";
import type { ChatSession } from "../types";

vi.mock("../hooks", () => ({
  useWorkspaceId: () => "ws-1",
}));

const WS_ID = "ws-1";

function makeSession(overrides: Partial<ChatSession> = {}): ChatSession {
  return {
    id: "s1",
    workspace_id: WS_ID,
    agent_id: "agent-1",
    creator_id: "user-1",
    title: "Session 1",
    status: "active",
    has_unread: true,
    unread_count: 2,
    created_at: "2026-07-10T00:00:00Z",
    updated_at: "2026-07-10T00:00:00Z",
    ...overrides,
  };
}

function createWrapper(qc: QueryClient) {
  return function Wrapper({ children }: { children: ReactNode }) {
    return <QueryClientProvider client={qc}>{children}</QueryClientProvider>;
  };
}

describe("useSetChatSessionArchived", () => {
  let qc: QueryClient;
  let setChatSessionArchived: ReturnType<
    typeof vi.fn<(id: string, archived: boolean) => Promise<ChatSession>>
  >;

  beforeEach(() => {
    qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
    setChatSessionArchived = vi.fn();
    setApiInstance({ setChatSessionArchived } as unknown as ApiClient);
  });

  afterEach(() => {
    qc.clear();
    vi.restoreAllMocks();
  });

  // MUL-4360: archiving must zero the row's unread locally so no badge (FAB,
  // sidebar Chat tab, chat-window header) keeps counting a just-archived
  // session in the frame before the refetch lands. Mirrors the backend, which
  // forces unread to 0 for archived rows in ListAllChatSessionsByCreator.
  it("optimistically zeroes unread when archiving", async () => {
    setChatSessionArchived.mockResolvedValue(
      makeSession({ status: "archived" }),
    );
    qc.setQueryData<ChatSession[]>(chatKeys.sessions(WS_ID), [makeSession()]);

    const { result } = renderHook(() => useSetChatSessionArchived(), {
      wrapper: createWrapper(qc),
    });

    await act(async () => {
      await result.current.mutateAsync({ sessionId: "s1", archived: true });
    });

    const row = qc.getQueryData<ChatSession[]>(chatKeys.sessions(WS_ID))![0]!;
    expect(row.status).toBe("archived");
    expect(row.unread_count).toBe(0);
    expect(row.has_unread).toBe(false);
  });

  // Unarchive must NOT fabricate an unread count — the true state comes back
  // from the server refetch (last_read_at is untouched), so the optimistic
  // patch leaves the row's unread fields as-is.
  it("does not resurrect unread when unarchiving", async () => {
    setChatSessionArchived.mockResolvedValue(makeSession({ status: "active" }));
    qc.setQueryData<ChatSession[]>(chatKeys.sessions(WS_ID), [
      makeSession({ status: "archived", has_unread: false, unread_count: 0 }),
    ]);

    const { result } = renderHook(() => useSetChatSessionArchived(), {
      wrapper: createWrapper(qc),
    });

    await act(async () => {
      await result.current.mutateAsync({ sessionId: "s1", archived: false });
    });

    const row = qc.getQueryData<ChatSession[]>(chatKeys.sessions(WS_ID))![0]!;
    expect(row.status).toBe("active");
    expect(row.unread_count).toBe(0);
    expect(row.has_unread).toBe(false);
  });

  // On failure the optimistic patch (status + zeroed unread) rolls back whole.
  it("rolls back the unread patch when the request fails", async () => {
    setChatSessionArchived.mockRejectedValue(new Error("boom"));
    qc.setQueryData<ChatSession[]>(chatKeys.sessions(WS_ID), [makeSession()]);

    const { result } = renderHook(() => useSetChatSessionArchived(), {
      wrapper: createWrapper(qc),
    });

    await act(async () => {
      await expect(
        result.current.mutateAsync({ sessionId: "s1", archived: true }),
      ).rejects.toThrow("boom");
    });

    const row = qc.getQueryData<ChatSession[]>(chatKeys.sessions(WS_ID))![0]!;
    expect(row.status).toBe("active");
    expect(row.unread_count).toBe(2);
    expect(row.has_unread).toBe(true);
  });
});

// The consume DELETE is the last step of a durable draft restore (#5219), and
// mutations are `retry: false` app-wide — a single dropped request would leave
// the row behind, to be re-offered later. The endpoint is idempotent, so this
// one retries instead of giving up on the first failure.
describe("useConsumeChatDraftRestore", () => {
  let qc: QueryClient;

  beforeEach(() => {
    vi.useFakeTimers();
    qc = new QueryClient();
  });

  afterEach(() => {
    vi.useRealTimers();
    qc.clear();
    vi.restoreAllMocks();
  });

  it("retries a lost consume until the server acknowledges it", async () => {
    const consumeChatDraftRestore = vi
      .fn<(sessionId: string, restoreId: string) => Promise<void>>()
      .mockRejectedValueOnce(new Error("network"))
      .mockResolvedValueOnce(undefined);
    setApiInstance({ consumeChatDraftRestore } as unknown as ApiClient);

    const onSuccess = vi.fn();
    const { result } = renderHook(() => useConsumeChatDraftRestore(), {
      wrapper: createWrapper(qc),
    });

    act(() => {
      result.current.mutate({ sessionId: "s1", restoreId: "r1" }, { onSuccess });
    });

    await act(async () => {
      await vi.advanceTimersByTimeAsync(10_000);
    });

    expect(consumeChatDraftRestore).toHaveBeenCalledTimes(2);
    expect(onSuccess).toHaveBeenCalledTimes(1);
  });

  it("drops the row from the cache immediately so it is not re-offered", async () => {
    const consumeChatDraftRestore = vi.fn().mockResolvedValue(undefined);
    setApiInstance({ consumeChatDraftRestore } as unknown as ApiClient);
    qc.setQueryData(chatKeys.draftRestores("s1"), {
      restores: [
        { id: "r1", chat_session_id: "s1", content: "keep me" },
        { id: "r2", chat_session_id: "s1", content: "other" },
      ],
    });

    const { result } = renderHook(() => useConsumeChatDraftRestore(), {
      wrapper: createWrapper(qc),
    });
    act(() => {
      result.current.mutate({ sessionId: "s1", restoreId: "r1" });
    });

    const cached = qc.getQueryData(chatKeys.draftRestores("s1")) as {
      restores: { id: string }[];
    };
    expect(cached.restores.map((r) => r.id)).toEqual(["r2"]);
  });
});
