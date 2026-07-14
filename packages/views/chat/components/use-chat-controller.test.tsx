import { describe, it, expect, beforeEach, afterEach, vi } from "vitest";
import { act, renderHook } from "@testing-library/react";
import type { Agent, ChatSession } from "@multica/core/types";

interface QueuedRestore {
  id: string;
  content: string;
  attachments?: unknown[];
  sessionId: string;
}

// --- Shared mutable state (hoisted so vi.mock factories can reach it) --------
const h = vi.hoisted(() => {
  const store = {
    activeSessionId: null as string | null,
    selectedAgentId: null as string | null,
    setActiveSession: vi.fn((id: string | null) => {
      store.activeSessionId = id;
    }),
    setSelectedAgentId: vi.fn((id: string | null) => {
      store.selectedAgentId = id;
    }),
    appliedDraftRestoreIds: [] as string[],
    markDraftRestoreApplied: vi.fn((id: string) => {
      if (!store.appliedDraftRestoreIds.includes(id)) {
        store.appliedDraftRestoreIds = [...store.appliedDraftRestoreIds, id];
      }
    }),
    forgetDraftRestoreApplied: vi.fn((id: string) => {
      store.appliedDraftRestoreIds = store.appliedDraftRestoreIds.filter((x) => x !== id);
    }),
    // Server-less restores (a failed send, a synchronous cancel) queue per session
    // and are persisted; the shared slot never holds one for another session.
    pendingSendRestores: {} as Record<string, QueuedRestore[]>,
    enqueuePendingSendRestore: vi.fn((r: QueuedRestore) => {
      const existing = store.pendingSendRestores[r.sessionId] ?? [];
      if (existing.some((q) => q.id === r.id)) return;
      store.pendingSendRestores = {
        ...store.pendingSendRestores,
        [r.sessionId]: [...existing, r],
      };
    }),
    dequeuePendingSendRestore: vi.fn((sessionId: string, restoreId: string) => {
      const existing = store.pendingSendRestores[sessionId] ?? [];
      const remaining = existing.filter((q) => q.id !== restoreId);
      const next = { ...store.pendingSendRestores };
      if (remaining.length > 0) next[sessionId] = remaining;
      else delete next[sessionId];
      store.pendingSendRestores = next;
    }),
  };
  return {
    store,
    archivedMutate: vi.fn(),
    markReadMutate: vi.fn(),
    // Foreground gate for the auto mark-read effect; tests flip it.
    appForeground: { value: true },
    consumeRestoreMutate: vi.fn(),
    removeFromCaches: vi.fn(),
    // useQuery reads these so each test can vary the loaded data.
    sessions: [] as ChatSession[],
    agents: [] as Agent[],
    draftRestores: null as
      | { restores: { id: string; chat_session_id: string; content: string }[] }
      | null,
  };
});

vi.mock("@multica/core/hooks", () => ({ useWorkspaceId: () => "ws-1" }));
vi.mock("@multica/core/auth", () => ({
  useAuthStore: (sel: (s: { user: { id: string } }) => unknown) =>
    sel({ user: { id: "user-1" } }),
}));
vi.mock("@multica/core/workspace/queries", () => ({
  agentListOptions: () => ({ queryKey: ["agents"] }),
  memberListOptions: () => ({ queryKey: ["members"] }),
}));
vi.mock("@multica/views/issues/components", () => ({ canAssignAgent: () => true }));
vi.mock("@multica/core/api", () => ({
  api: { sendChatMessage: vi.fn(), cancelTaskById: vi.fn() },
}));
vi.mock("@multica/core/agents", () => ({
  useAgentPresenceDetail: () => ({ availability: "online" }),
  useWorkspaceAgentAvailability: () => "available",
}));
vi.mock("@multica/core/hooks/use-file-upload", () => ({
  useFileUpload: () => ({ uploadWithToast: vi.fn() }),
}));
vi.mock("@multica/core/chat/mutations", () => ({
  useCreateChatSession: () => ({ mutateAsync: vi.fn() }),
  useMarkChatSessionRead: () => ({ mutate: h.markReadMutate }),
  useSetChatSessionArchived: () => ({ mutate: h.archivedMutate }),
  useConsumeChatDraftRestore: () => ({ mutate: h.consumeRestoreMutate }),
}));
vi.mock("../../common/use-app-foreground", () => ({
  useAppForeground: () => h.appForeground.value,
}));
vi.mock("@multica/core/chat", () => ({
  useChatStore: Object.assign(
    (sel: (s: typeof h.store) => unknown) => sel(h.store),
    { getState: () => h.store },
  ),
}));
vi.mock("@multica/core/realtime", () => ({
  removeChatMessageFromCaches: h.removeFromCaches,
}));
vi.mock("@multica/core/logger", () => ({
  createLogger: () => ({ info: vi.fn(), warn: vi.fn(), error: vi.fn(), debug: vi.fn() }),
}));
vi.mock("../../i18n", () => ({ useT: () => ({ t: () => "x" }) }));
vi.mock("sonner", () => ({ toast: { error: vi.fn(), success: vi.fn() } }));

vi.mock("@tanstack/react-query", async (importOriginal) => {
  const actual = await importOriginal<typeof import("@tanstack/react-query")>();
  return {
    ...actual,
    useQuery: (options: { queryKey?: unknown[] }) => {
      const key = options.queryKey ?? [];
      if (key.includes("agents")) return { data: h.agents };
      if (key.includes("members")) {
        return { data: [{ user_id: "user-1", role: "admin" }] };
      }
      if (key.includes("sessions")) return { data: h.sessions, isSuccess: true };
      if (key.includes("draft-restores")) return { data: h.draftRestores };
      return { data: null };
    },
    useInfiniteQuery: () => ({
      data: undefined,
      isLoading: false,
      fetchNextPage: vi.fn(),
      hasNextPage: false,
      isFetchingNextPage: false,
    }),
    useQueryClient: () => ({
      getQueryData: vi.fn(),
      setQueryData: vi.fn(),
      invalidateQueries: vi.fn(),
    }),
  };
});

import { useChatController } from "./use-chat-controller";

// --- Fixtures ---------------------------------------------------------------
function makeSession(
  overrides: Partial<ChatSession> & Pick<ChatSession, "id" | "agent_id">,
): ChatSession {
  return {
    workspace_id: "ws-1",
    creator_id: "user-1",
    title: `Chat ${overrides.id}`,
    status: "active",
    has_unread: false,
    unread_count: 0,
    last_message: null,
    pinned: false,
    created_at: new Date(0).toISOString(),
    updated_at: new Date(0).toISOString(),
    ...overrides,
  };
}

const agentA = { id: "agent-a", name: "Alpha" } as unknown as Agent;
const agentB = { id: "agent-b", name: "Beta" } as unknown as Agent;

// Descending updated_at → sortChatSessions renders them sA, sB, sC.
const sA = makeSession({ id: "sA", agent_id: "agent-a", updated_at: "2026-07-08T03:00:00Z" });
const sB = makeSession({ id: "sB", agent_id: "agent-b", updated_at: "2026-07-08T02:00:00Z" });
const sC = makeSession({ id: "sC", agent_id: "agent-a", updated_at: "2026-07-08T01:00:00Z" });

function setup(activeSessionId: string | null, sessions: ChatSession[], agents: Agent[]) {
  h.store.activeSessionId = activeSessionId;
  h.store.selectedAgentId = null;
  h.sessions = sessions;
  h.agents = agents;
  const { result } = renderHook(() => useChatController());
  // Ignore any render-time store writes (self-heal etc.); we assert only the
  // effect of the call under test.
  h.store.setActiveSession.mockClear();
  h.store.setSelectedAgentId.mockClear();
  h.archivedMutate.mockClear();
  return result;
}

describe("useChatController.advanceSelectionAfterArchive", () => {
  beforeEach(() => {
    h.store.setActiveSession.mockClear();
    h.store.setSelectedAgentId.mockClear();
    h.archivedMutate.mockClear();
  });

  it("advances to the next chat and syncs the selected agent across agents", () => {
    const result = setup("sA", [sA, sB, sC], [agentA, agentB]);
    act(() => result.current.advanceSelectionAfterArchive(sA));

    expect(h.store.setActiveSession).toHaveBeenCalledWith("sB");
    // The next chat belongs to a different agent — selectedAgentId must follow
    // so a subsequent "new chat" defaults to the right agent (the review bug).
    expect(h.store.setSelectedAgentId).toHaveBeenCalledWith("agent-b");
  });

  it("does not touch the selected agent when the next chat is the same agent", () => {
    // Both chats belong to agent-a; archiving the open one advances within the
    // same agent, so there is no reason to rewrite selectedAgentId.
    const a1 = makeSession({ id: "a1", agent_id: "agent-a", updated_at: "2026-07-08T03:00:00Z" });
    const a2 = makeSession({ id: "a2", agent_id: "agent-a", updated_at: "2026-07-08T02:00:00Z" });
    const result = setup("a1", [a1, a2], [agentA, agentB]);
    act(() => result.current.advanceSelectionAfterArchive(a1));

    expect(h.store.setActiveSession).toHaveBeenCalledWith("a2");
    expect(h.store.setSelectedAgentId).not.toHaveBeenCalled();
  });

  it("falls back to the previous chat when archiving the last open one", () => {
    const result = setup("sC", [sA, sB, sC], [agentA, agentB]);
    act(() => result.current.advanceSelectionAfterArchive(sC));

    expect(h.store.setActiveSession).toHaveBeenCalledWith("sB");
  });

  it("clears the selection when archiving the only chat", () => {
    const only = makeSession({ id: "only", agent_id: "agent-a" });
    const result = setup("only", [only], [agentA]);
    act(() => result.current.advanceSelectionAfterArchive(only));

    expect(h.store.setActiveSession).toHaveBeenCalledWith(null);
    expect(h.store.setSelectedAgentId).not.toHaveBeenCalled();
  });

  it("is a no-op when the archived chat is not the open one", () => {
    const result = setup("sB", [sA, sB, sC], [agentA, agentB]);
    act(() => result.current.advanceSelectionAfterArchive(sA));

    expect(h.store.setActiveSession).not.toHaveBeenCalled();
    expect(h.store.setSelectedAgentId).not.toHaveBeenCalled();
  });
});

describe("useChatController.archiveSession", () => {
  it("fires the archive mutation for the given session", () => {
    const result = setup("sA", [sA, sB, sC], [agentA, agentB]);
    act(() => result.current.archiveSession("sA"));

    expect(h.archivedMutate).toHaveBeenCalledWith({ sessionId: "sA", archived: true });
  });
});

// MUL-4360 mount race: `activeSessionId` is persisted, so on a bare `/chat`
// navigation the page restores the last session as active for one frame before
// its URL→store effect clears it back to null. The auto-mark-read must NOT fire
// for that transiently-active session — otherwise the badge vanishes though the
// user never opened it (the exact "no active session yet the red dot cleared"
// report). The read is deferred a tick and cancelled when activeSessionId moves.
describe("useChatController auto mark-read", () => {
  const unread = makeSession({
    id: "sA",
    agent_id: "agent-a",
    has_unread: true,
    unread_count: 2,
  });

  beforeEach(() => {
    vi.useFakeTimers();
    h.store.activeSessionId = null;
    h.store.selectedAgentId = null;
    h.markReadMutate.mockClear();
    h.appForeground.value = true;
    h.sessions = [unread];
    h.agents = [agentA];
  });

  afterEach(() => {
    vi.useRealTimers();
  });

  it("marks read a session that stays active past the tick", () => {
    h.store.activeSessionId = "sA";
    renderHook(() => useChatController());

    // Deferred, not synchronous — nothing fires on the mount frame.
    expect(h.markReadMutate).not.toHaveBeenCalled();

    act(() => vi.advanceTimersByTime(1));
    expect(h.markReadMutate).toHaveBeenCalledWith("sA");
  });

  it("does NOT mark read a session that was only momentarily active on mount", () => {
    // Mount restores sA as active (persisted, unread)...
    h.store.activeSessionId = "sA";
    const { rerender } = renderHook(() => useChatController());

    // ...then the page's URL→store effect clears it before the tick elapses.
    h.store.activeSessionId = null;
    rerender();

    act(() => vi.advanceTimersByTime(1));
    expect(h.markReadMutate).not.toHaveBeenCalled();
  });
});

// Foreground gating (MUL-4485): a reply that lands while the app is backgrounded
// must stay unread and clear once the user returns. This composes with the
// MUL-4360 mount-race defer above — the read is scheduled a tick after the
// effect runs — so each assertion advances fake timers to let it (or not) fire.
describe("useChatController auto mark-read — foreground gating (MUL-4485)", () => {
  const unreadActive = makeSession({ id: "sU", agent_id: "agent-a", has_unread: true });

  beforeEach(() => {
    vi.useFakeTimers();
    h.markReadMutate.mockClear();
    h.appForeground.value = true;
  });

  afterEach(() => {
    vi.useRealTimers();
  });

  function renderController(activeSessionId: string | null, sessions: ChatSession[]) {
    h.store.activeSessionId = activeSessionId;
    h.store.selectedAgentId = null;
    h.sessions = sessions;
    h.agents = [agentA];
    return renderHook(() => useChatController({ isActive: true }));
  }

  it("marks the active unread session read while the app is in the foreground", () => {
    renderController("sU", [unreadActive]);
    act(() => vi.advanceTimersByTime(1));
    expect(h.markReadMutate).toHaveBeenCalledWith("sU");
  });

  it("does NOT mark read while the app is backgrounded, so the reply stays unread", () => {
    h.appForeground.value = false;
    renderController("sU", [unreadActive]);
    act(() => vi.advanceTimersByTime(1));
    expect(h.markReadMutate).not.toHaveBeenCalled();
  });

  it("marks read once the user returns to the foreground", () => {
    h.appForeground.value = false;
    const { rerender } = renderController("sU", [unreadActive]);
    act(() => vi.advanceTimersByTime(1));
    expect(h.markReadMutate).not.toHaveBeenCalled();

    act(() => {
      h.appForeground.value = true;
      rerender();
    });
    act(() => vi.advanceTimersByTime(1));
    expect(h.markReadMutate).toHaveBeenCalledWith("sU");
  });
});

describe("useChatController durable draft restores (#5219)", () => {
  beforeEach(() => {
    h.draftRestores = null;
    h.consumeRestoreMutate.mockClear();
    h.removeFromCaches.mockClear();
    h.store.appliedDraftRestoreIds = [];
    h.store.markDraftRestoreApplied.mockClear();
    h.store.forgetDraftRestoreApplied.mockClear();
    h.store.pendingSendRestores = {};
    h.store.enqueuePendingSendRestore.mockClear();
    h.store.dequeuePendingSendRestore.mockClear();
    h.appForeground.value = true;
  });

  // The reconnect/offline recovery path: no chat:cancel_finalized event was
  // seen — the restore arrives purely through the draft-restores query on
  // composer mount, is handed to the composer, and is consumed server-side
  // only after the composer reports the hand-off complete.
  it("hands a fetched restore to the composer and consumes it after the hand-off", () => {
    h.draftRestores = {
      restores: [{ id: "msg-1", chat_session_id: "sA", content: "run the thing" }],
    };
    const result = setup("sA", [sA, sB, sC], [agentA, agentB]);

    expect(result.current.restoreDraftRequest).toEqual({
      id: "msg-1",
      content: "run the thing",
      attachments: undefined,
      sessionId: "sA",
      serverRestoreId: "msg-1",
    });
    // The deleted bubble may still be cached when the event was missed.
    expect(h.removeFromCaches).toHaveBeenCalledWith(expect.anything(), "sA", "msg-1");
    // Not yet consumed: the draft isn't persisted client-side until the
    // composer takes it.
    expect(h.consumeRestoreMutate).not.toHaveBeenCalled();

    act(() => {
      result.current.handleRestoreDraftApplied();
    });

    // The ledger entry is written BEFORE the request goes out: a consume that
    // never lands must not let this restore be offered (and re-sent) again.
    expect(h.store.markDraftRestoreApplied).toHaveBeenCalledWith("msg-1");
    expect(h.consumeRestoreMutate).toHaveBeenCalledWith(
      { sessionId: "sA", restoreId: "msg-1" },
      expect.anything(),
    );
    expect(result.current.restoreDraftRequest).toBeNull();
  });

  // The lost-consume case (#5219 review): the DELETE never reached the server,
  // so the row comes back on the next fetch — but it was already applied, and
  // the user may have sent it. It must be reconciled (consumed again), never
  // re-offered to the composer.
  it("reconciles a row whose consume was lost instead of re-offering it", () => {
    h.store.appliedDraftRestoreIds = ["msg-1"];
    h.draftRestores = {
      restores: [{ id: "msg-1", chat_session_id: "sA", content: "run the thing" }],
    };
    const result = setup("sA", [sA, sB, sC], [agentA, agentB]);

    expect(result.current.restoreDraftRequest).toBeNull();
    expect(h.consumeRestoreMutate).toHaveBeenCalledWith(
      { sessionId: "sA", restoreId: "msg-1" },
      expect.anything(),
    );
  });

  it("leaves a restore belonging to another session pending", () => {
    h.draftRestores = {
      restores: [{ id: "msg-2", chat_session_id: "sB", content: "other session draft" }],
    };
    const result = setup("sA", [sA, sB, sC], [agentA, agentB]);

    expect(result.current.restoreDraftRequest).toBeNull();
    expect(h.consumeRestoreMutate).not.toHaveBeenCalled();
  });

  // A backgrounded browser tab still renders this controller. It must not claim a
  // restore the user is waiting on in a foreground surface — the same silent-theft
  // hazard the hidden chat window guards against, reached here through the app
  // foreground gate rather than isOpen.
  it("does NOT offer or consume a restore while the app is backgrounded", () => {
    h.appForeground.value = false;
    h.draftRestores = {
      restores: [{ id: "msg-1", chat_session_id: "sA", content: "run the thing" }],
    };
    const result = setup("sA", [sA, sB, sC], [agentA, agentB]);

    expect(result.current.restoreDraftRequest).toBeNull();
    expect(h.consumeRestoreMutate).not.toHaveBeenCalled();
  });

  it("offers the restore once the surface returns to the foreground", () => {
    h.appForeground.value = false;
    h.draftRestores = {
      restores: [{ id: "msg-1", chat_session_id: "sA", content: "run the thing" }],
    };
    h.store.activeSessionId = "sA";
    h.store.selectedAgentId = null;
    h.sessions = [sA, sB, sC];
    h.agents = [agentA, agentB];
    const { result, rerender } = renderHook(() => useChatController());

    expect(result.current.restoreDraftRequest).toBeNull();

    act(() => {
      h.appForeground.value = true;
      rerender();
    });

    expect(result.current.restoreDraftRequest?.serverRestoreId).toBe("msg-1");
  });
});
