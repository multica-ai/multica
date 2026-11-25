import { act, renderHook } from "@testing-library/react";
import type { ReactNode } from "react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { api } from "@multica/core/api";
import { chatKeys } from "@multica/core/chat/queries";
import type { ChatSession } from "@multica/core/types";
import {
  pickAdjacentChatSessions,
  useChatSessionPrefetch,
} from "./use-chat-session-prefetch";

const featureState = vi.hoisted(() => ({ appRequests: false }));
vi.mock("@multica/core/config", () => ({
  useFeatureEnabled: () => featureState.appRequests,
}));

vi.mock("@multica/core/api", () => ({
  api: {
    listChatMessagesPage: vi.fn(async (sessionId: string) => ({
      messages: [
        {
          id: `msg-${sessionId}`,
          chat_session_id: sessionId,
          role: "assistant",
          content: `reply for ${sessionId}`,
          task_id: null,
          created_at: "2026-07-08T00:00:00Z",
        },
      ],
      limit: 50,
      has_more: false,
      next_cursor: null,
    })),
    getPendingChatTask: vi.fn(async () => ({})),
    listChatSessionWakeups: vi.fn(async () => []),
    listTaskMessages: vi.fn(async () => []),
    listAppRequests: vi.fn(async () => ({ requests: [] })),
  },
}));

function makeSession(
  overrides: Partial<ChatSession> & Pick<ChatSession, "id">,
): ChatSession {
  return {
    workspace_id: "ws-1",
    agent_id: "agent-1",
    creator_id: "user-1",
    title: `Chat ${overrides.id}`,
    status: "active",
    has_unread: false,
    unread_count: 0,
    last_message: null,
    pinned: false,
    created_at: "2026-07-08T00:00:00Z",
    updated_at: "2026-07-08T00:00:00Z",
    ...overrides,
  };
}

const sessions: ChatSession[] = [
  makeSession({ id: "s1", updated_at: "2026-07-08T05:00:00Z" }),
  makeSession({ id: "s2", updated_at: "2026-07-08T04:00:00Z" }),
  makeSession({ id: "s3", updated_at: "2026-07-08T03:00:00Z" }),
  makeSession({ id: "s4", updated_at: "2026-07-08T02:00:00Z" }),
  makeSession({ id: "s5", updated_at: "2026-07-08T01:00:00Z" }),
];

describe("pickAdjacentChatSessions", () => {
  it("prioritizes next, then prev, then next+1 around the active session", () => {
    const picked = pickAdjacentChatSessions(sessions, "s2");
    expect(picked.map((s) => s.id)).toEqual(["s3", "s1", "s4"]);
  });

  it("picks prev when viewing the last session in history", () => {
    const picked = pickAdjacentChatSessions(sessions, "s5");
    expect(picked.map((s) => s.id)).toEqual(["s4"]);
  });

  it("respects pinned-first ordering when picking neighbours", () => {
    const mixed = [
      makeSession({ id: "recent", updated_at: "2026-07-08T05:00:00Z" }),
      makeSession({ id: "pinned", pinned: true, updated_at: "2026-07-08T01:00:00Z" }),
      makeSession({ id: "older", updated_at: "2026-07-08T02:00:00Z" }),
    ];
    // Sorted order: pinned, recent, older. Active = pinned -> next1=recent, next2=older.
    expect(pickAdjacentChatSessions(mixed, "pinned").map((s) => s.id)).toEqual([
      "recent",
      "older",
    ]);
  });

  it("picks adjacent archived sessions when viewing an archived chat", () => {
    const archived = [
      makeSession({ id: "a1", status: "archived", updated_at: "2026-07-08T03:00:00Z" }),
      makeSession({ id: "a2", status: "archived", updated_at: "2026-07-08T02:00:00Z" }),
      makeSession({ id: "a3", status: "archived", updated_at: "2026-07-08T01:00:00Z" }),
    ];
    expect(pickAdjacentChatSessions([...sessions, ...archived], "a2").map((s) => s.id)).toEqual([
      "a3",
      "a1",
    ]);
  });

  it("warms the top active sessions when nothing is selected", () => {
    const withArchived = [
      makeSession({ id: "arch", status: "archived", updated_at: "2026-07-08T09:00:00Z" }),
      ...sessions,
    ];
    expect(pickAdjacentChatSessions(withArchived, null).map((s) => s.id)).toEqual([
      "s1",
      "s2",
    ]);
  });
});

function setup(
  list: ChatSession[],
  activeSessionId: string | null,
  isActive = true,
) {
  const qc = new QueryClient({
    defaultOptions: { queries: { retry: false, staleTime: Infinity } },
  });
  const wrapper = ({ children }: { children: ReactNode }) => (
    <QueryClientProvider client={qc}>{children}</QueryClientProvider>
  );
  const hook = renderHook(
    ({ items, activeId, active }) =>
      useChatSessionPrefetch({
        wsId: "ws-1",
        sessions: items,
        activeSessionId: activeId,
        isActive: active,
      }),
    {
      wrapper,
      initialProps: { items: list, activeId: activeSessionId, active: isActive },
    },
  );
  return { qc, hook };
}

describe("useChatSessionPrefetch", () => {
  beforeEach(() => vi.useFakeTimers());
  afterEach(() => {
    vi.useRealTimers();
    vi.clearAllMocks();
    featureState.appRequests = false;
  });

  it("warms next, prev, and next+1 sessions sequentially on idle", async () => {
    const { qc } = setup(sessions, "s2");
    expect(api.listChatMessagesPage).not.toHaveBeenCalled();

    await act(async () => {
      await vi.advanceTimersByTimeAsync(500);
    });

    expect(vi.mocked(api.listChatMessagesPage).mock.calls.map(([id]) => id)).toEqual([
      "s3",
      "s1",
      "s4",
    ]);
    expect(vi.mocked(api.getPendingChatTask).mock.calls.map(([id]) => id)).toEqual([
      "s3",
      "s1",
      "s4",
    ]);
    expect(vi.mocked(api.listChatSessionWakeups).mock.calls.map(([id]) => id)).toEqual([
      "s3",
      "s1",
      "s4",
    ]);
    expect(api.listAppRequests).not.toHaveBeenCalled();
    expect(qc.getQueryData(chatKeys.messagesPage("s3"))).toBeDefined();
  });

  it("warms chat app requests when the Composio feature flag is enabled", async () => {
    featureState.appRequests = true;
    setup(sessions.slice(0, 2), "s1");

    await act(async () => {
      await vi.advanceTimersByTimeAsync(500);
    });

    expect(api.listAppRequests).toHaveBeenCalledWith({ chatSessionId: "s2" });
  });

  it("does not prefetch while the surface is inactive", async () => {
    setup(sessions, "s2", false);

    await act(async () => {
      await vi.advanceTimersByTimeAsync(1000);
    });

    expect(api.listChatMessagesPage).not.toHaveBeenCalled();
  });

  it("does not restart when realtime replaces identical session lists", async () => {
    const { hook } = setup(sessions.slice(0, 2), "s1");
    hook.rerender({
      items: sessions.slice(0, 2).map((s) => ({ ...s })),
      activeId: "s1",
      active: true,
    });

    await act(async () => {
      await vi.advanceTimersByTimeAsync(500);
    });

    expect(api.listChatMessagesPage).toHaveBeenCalledTimes(1);
    expect(api.listChatMessagesPage).toHaveBeenCalledWith("s2", {
      before: null,
      limit: 50,
    });
  });

  it("warms a pointed-at session on demand and skips already-warm caches", async () => {
    const { hook } = setup(sessions, "s1");
    await act(async () => {
      await vi.advanceTimersByTimeAsync(500);
    });
    // s2 and s3 were warmed on idle. Hovering s5 fetches it; hovering s2 is a cache hit.
    await act(async () => {
      await hook.result.current("s5");
    });
    await act(async () => {
      await hook.result.current("s2");
    });

    expect(vi.mocked(api.listChatMessagesPage).mock.calls.map(([id]) => id)).toEqual([
      "s2",
      "s3",
      "s5",
    ]);
  });
});
