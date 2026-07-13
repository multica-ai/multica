/**
 * @vitest-environment jsdom
 */
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { act, renderHook } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type { ReactNode } from "react";

import { setApiInstance } from "../api";
import type { ApiClient } from "../api/client";
import { useSetChatSessionArchived } from "./mutations";
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
