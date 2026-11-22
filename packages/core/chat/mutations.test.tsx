/**
 * @vitest-environment jsdom
 */
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { act, renderHook, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider, useQuery } from "@tanstack/react-query";
import type { ReactNode } from "react";

import { setApiInstance } from "../api";
import type { ApiClient } from "../api/client";
import {
  useConsumeChatDraftRestore,
  useMarkAllChatSessionsRead,
  useSetChatSessionArchived,
  useSetChatSessionProject,
} from "./mutations";
import { chatKeys, chatSessionsOptions } from "./queries";
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

describe("useMarkAllChatSessionsRead", () => {
  let qc: QueryClient;
  let markChatSessionRead: ReturnType<typeof vi.fn<(id: string) => Promise<void>>>;

  beforeEach(() => {
    qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
    markChatSessionRead = vi.fn();
    setApiInstance({ markChatSessionRead } as unknown as ApiClient);
  });

  afterEach(() => {
    qc.clear();
    vi.restoreAllMocks();
  });

  it("reads each session and zeroes their unread", async () => {
    markChatSessionRead.mockResolvedValue(undefined);
    qc.setQueryData<ChatSession[]>(chatKeys.sessions(WS_ID), [
      makeSession({ id: "s1", unread_count: 3 }),
      makeSession({ id: "s2", unread_count: 4 }),
      makeSession({ id: "s3", unread_count: 0, has_unread: false }),
    ]);

    const { result } = renderHook(() => useMarkAllChatSessionsRead(), {
      wrapper: createWrapper(qc),
    });
    await act(async () => {
      await result.current.mutateAsync(["s1", "s2"]);
    });

    expect(markChatSessionRead.mock.calls.map(([id]) => id)).toEqual(["s1", "s2"]);
    const rows = qc.getQueryData<ChatSession[]>(chatKeys.sessions(WS_ID))!;
    expect(rows.map((s) => s.unread_count)).toEqual([0, 0, 0]);
    expect(rows.every((s) => !s.has_unread)).toBe(true);
  });

  // One failed read must not stop the others; the failed row gets its unread
  // back and the one that went through stays read.
  it("restores only the sessions whose read failed", async () => {
    markChatSessionRead.mockImplementation(async (id) => {
      if (id === "s1") throw new Error("boom");
    });
    qc.setQueryData<ChatSession[]>(chatKeys.sessions(WS_ID), [
      makeSession({ id: "s1", unread_count: 2 }),
      makeSession({ id: "s2", unread_count: 5 }),
    ]);

    const { result } = renderHook(() => useMarkAllChatSessionsRead(), {
      wrapper: createWrapper(qc),
    });
    await act(async () => {
      await expect(result.current.mutateAsync(["s1", "s2"])).rejects.toThrow();
    });

    expect(markChatSessionRead).toHaveBeenCalledTimes(2);
    const rows = qc.getQueryData<ChatSession[]>(chatKeys.sessions(WS_ID))!;
    expect(rows.map((s) => [s.id, s.unread_count, s.has_unread])).toEqual([
      ["s1", 2, true],
      ["s2", 0, false],
    ]);
  });

  // Offline: the read and the follow-up refetch both fail, so the rollback is
  // all that keeps the badge (and the menu item) from hiding real unread.
  it("keeps unread when the read and the refetch both fail", async () => {
    const listChatSessions = vi.fn().mockRejectedValue(new Error("offline"));
    markChatSessionRead.mockRejectedValue(new Error("offline"));
    setApiInstance({ listChatSessions, markChatSessionRead } as unknown as ApiClient);
    qc.setQueryData<ChatSession[]>(chatKeys.sessions(WS_ID), [
      makeSession({ id: "s1", unread_count: 2 }),
    ]);

    const { result } = renderHook(
      () => {
        useQuery(chatSessionsOptions(WS_ID));
        return useMarkAllChatSessionsRead();
      },
      { wrapper: createWrapper(qc) },
    );
    await act(async () => {
      await expect(result.current.mutateAsync(["s1"])).rejects.toThrow();
    });
    await waitFor(() => {
      expect(listChatSessions).toHaveBeenCalled();
      expect(qc.isFetching()).toBe(0);
    });

    const row = qc.getQueryData<ChatSession[]>(chatKeys.sessions(WS_ID))![0]!;
    expect(row.unread_count).toBe(2);
    expect(row.has_unread).toBe(true);
  });
});

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

describe("useSetChatSessionProject", () => {
  let qc: QueryClient;
  let updateChatSession: ReturnType<
    typeof vi.fn<
      (
        id: string,
        data: { title: string } | { project_id: string | null },
      ) => Promise<ChatSession>
    >
  >;

  beforeEach(() => {
    qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
    updateChatSession = vi.fn();
    setApiInstance({ updateChatSession } as unknown as ApiClient);
  });

  afterEach(() => {
    qc.clear();
    vi.restoreAllMocks();
  });

  it("optimistically removes the project from the existing session", async () => {
    updateChatSession.mockResolvedValue(makeSession({ project_id: null }));
    qc.setQueryData<ChatSession[]>(chatKeys.sessions(WS_ID), [
      makeSession({ project_id: "project-1" }),
    ]);

    const { result } = renderHook(() => useSetChatSessionProject(), {
      wrapper: createWrapper(qc),
    });

    await act(async () => {
      await result.current.mutateAsync({ sessionId: "s1", projectId: null });
    });

    expect(updateChatSession).toHaveBeenCalledWith("s1", { project_id: null });
    expect(qc.getQueryData<ChatSession[]>(chatKeys.sessions(WS_ID))![0]!.project_id).toBeNull();
  });

  it("rolls the project back when the update fails", async () => {
    updateChatSession.mockRejectedValue(new Error("boom"));
    qc.setQueryData<ChatSession[]>(chatKeys.sessions(WS_ID), [
      makeSession({ project_id: "project-1" }),
    ]);

    const { result } = renderHook(() => useSetChatSessionProject(), {
      wrapper: createWrapper(qc),
    });

    await act(async () => {
      await expect(
        result.current.mutateAsync({ sessionId: "s1", projectId: null }),
      ).rejects.toThrow("boom");
    });

    expect(
      qc.getQueryData<ChatSession[]>(chatKeys.sessions(WS_ID))![0]!.project_id,
    ).toBe("project-1");
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
