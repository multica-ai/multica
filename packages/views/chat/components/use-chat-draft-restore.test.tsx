import { describe, it, expect, beforeEach, vi } from "vitest";
import { act, renderHook, waitFor, configure } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type { ReactNode } from "react";

// --- Mocks ------------------------------------------------------------------
interface QueuedRestore {
  id: string;
  content: string;
  attachments?: unknown[];
  sessionId: string;
}

const h = vi.hoisted(() => {
  const store = {
    appliedDraftRestoreIds: [] as string[],
    markDraftRestoreApplied: vi.fn(),
    forgetDraftRestoreApplied: vi.fn(),
    inputDrafts: {} as Record<string, string>,
    inputDraftAttachments: {} as Record<string, unknown[]>,
    setInputDraft: vi.fn(),
    setInputDraftAttachments: vi.fn(),
    // The persisted per-session queue, modelled faithfully: a new object on every
    // write so the hook's selector sees a fresh reference, and the contents
    // outlive an unmount exactly as the real (storage-backed) store does.
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
    listChatDraftRestores: vi.fn(),
    consumeChatDraftRestore: vi.fn(),
    store,
  };
});

vi.mock("@multica/core/api", () => ({
  api: {
    listChatDraftRestores: h.listChatDraftRestores,
    consumeChatDraftRestore: h.consumeChatDraftRestore,
  },
}));
vi.mock("@multica/core/hooks", () => ({ useWorkspaceId: () => "ws-1" }));
vi.mock("@multica/core/chat", () => ({
  useChatStore: Object.assign(
    (sel: (s: typeof h.store) => unknown) => sel(h.store),
    { getState: () => h.store },
  ),
}));
vi.mock("@multica/core/realtime", () => ({ removeChatMessageFromCaches: vi.fn() }));
vi.mock("@multica/core/logger", () => ({
  createLogger: () => ({ info: vi.fn(), warn: vi.fn(), error: vi.fn(), debug: vi.fn() }),
}));

import type { Attachment } from "@multica/core/types";
import { useChatDraftRestore } from "./use-chat-draft-restore";

// Every assertion here drives a real react-query fetch and mutation, so each
// `waitFor` gates on a multi-hop async chain (fetch → effect → mutate → settle →
// refetch). The reconciliation case chains the most hops — consume, settle, a
// stale refetch on returning to the session, then a re-consume — and timed out
// under the default 1s `waitFor` budget when the full Vitest suite saturated the
// CI runner (the whole run took 405s, import alone 364s). Widen both the
// async-utility and per-test budgets for this file so whole-suite contention
// can't starve the chain; the defaults stay in force everywhere else (Vitest
// isolates config per file).
vi.setConfig({ testTimeout: 20000 });
configure({ asyncUtilTimeout: 5000 });

function wrapper({ children }: { children: ReactNode }) {
  const qc = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  });
  return <QueryClientProvider client={qc}>{children}</QueryClientProvider>;
}

const RESTORE = { id: "msg-1", chat_session_id: "sA", content: "run the thing" };

beforeEach(() => {
  h.listChatDraftRestores.mockReset().mockResolvedValue({ restores: [RESTORE] });
  h.consumeChatDraftRestore.mockReset().mockResolvedValue(undefined);
  h.store.appliedDraftRestoreIds = [];
  h.store.markDraftRestoreApplied.mockReset();
  h.store.forgetDraftRestoreApplied.mockReset();
  h.store.inputDrafts = {};
  h.store.inputDraftAttachments = {};
  h.store.setInputDraft.mockReset();
  h.store.setInputDraftAttachments.mockReset();
  h.store.pendingSendRestores = {};
  h.store.enqueuePendingSendRestore.mockClear();
  h.store.dequeuePendingSendRestore.mockClear();
});

// The restore is user-scoped — every client of the creator can see it, and the
// first to apply consumes it. That is only safe if the composers that can claim
// are the ones the user can actually SEE: a chat window stays mounted while
// closed, and a background claim would consume the row out from under the
// composer the user is waiting on (here, or on their other device).
describe("useChatDraftRestore ownership (#5219)", () => {
  it("does not fetch, offer, or consume from a composer the user cannot see", async () => {
    const { result } = renderHook(() => useChatDraftRestore("sA", false), { wrapper });

    await act(async () => {
      await Promise.resolve();
    });

    expect(h.listChatDraftRestores).not.toHaveBeenCalled();
    expect(result.current.restoreDraftRequest).toBeNull();
    expect(h.consumeChatDraftRestore).not.toHaveBeenCalled();
  });

  // Two clients on the same session, one visible and one hidden: only the
  // visible one may take the prompt.
  it("lets the visible composer win the race against a hidden one", async () => {
    const hidden = renderHook(() => useChatDraftRestore("sA", false), { wrapper });
    const visible = renderHook(() => useChatDraftRestore("sA", true), { wrapper });

    await waitFor(() => expect(visible.result.current.restoreDraftRequest).not.toBeNull());
    expect(hidden.result.current.restoreDraftRequest).toBeNull();

    act(() => visible.result.current.handleRestoreDraftApplied());

    // Recorded before the request goes out, and consumed exactly once — by the
    // client the user was looking at.
    expect(h.store.markDraftRestoreApplied).toHaveBeenCalledWith("msg-1");
    await waitFor(() => expect(h.consumeChatDraftRestore).toHaveBeenCalledTimes(1));
    expect(h.consumeChatDraftRestore).toHaveBeenCalledWith("sA", "msg-1");
  });

  it("drops an unclaimed offer when the composer is hidden mid-flight", async () => {
    const { result, rerender } = renderHook(
      ({ open }: { open: boolean }) => useChatDraftRestore("sA", open),
      { wrapper, initialProps: { open: true } },
    );

    await waitFor(() => expect(result.current.restoreDraftRequest).not.toBeNull());

    rerender({ open: false });

    // Never applied → never in the ledger → the server row survives for whichever
    // composer the user opens next.
    expect(result.current.restoreDraftRequest).toBeNull();
    expect(h.store.markDraftRestoreApplied).not.toHaveBeenCalled();
    expect(h.consumeChatDraftRestore).not.toHaveBeenCalled();
  });
});

// ChatInput treats a restore it cannot apply as a WAIT, so a request whose target
// session the user never returns to would hold the composer's single restore slot
// forever — starving every durable hand-off for the session they ARE looking at.
// And a server-less restore (a failed send) has no server copy: parking it in that
// component-local slot also loses its text on unmount. Both are why server-less
// restores live in a PERSISTED per-session queue, and the slot only ever holds a
// request for the session on screen.
describe("useChatDraftRestore server-less restore queue (#5219)", () => {
  const sendFailure = (sessionId: string) => ({
    id: "send-failed-1",
    content: "the text that failed to send",
    attachments: [{ id: "att-1" } as Attachment],
    sessionId,
  });

  it("queues a restore for a session the user is not looking at, leaving the active session's durable restore free to land", async () => {
    const { result, rerender } = renderHook(() => useChatDraftRestore("sA", true), { wrapper });

    // The send failed on sB while the user is looking at sA.
    act(() => result.current.enqueueLocalRestore(sendFailure("sB")));
    rerender();

    // It is persisted against its own session — not parked in the shared slot, and
    // not dumped into sB's draft (which the user may be using).
    expect(h.store.pendingSendRestores.sB).toEqual([sendFailure("sB")]);
    expect(h.store.setInputDraft).not.toHaveBeenCalled();

    // And sA's durable restore is offered rather than starved.
    await waitFor(() => expect(result.current.restoreDraftRequest?.serverRestoreId).toBe("msg-1"));
  });

  // The review's case: the source session's draft slot is occupied, the session on
  // screen has a durable restore, and the composer unmounts. Neither restore may be
  // lost, and neither may block the other.
  it("survives an unmount and is re-offered when the user returns, even with work in progress there", async () => {
    h.store.inputDrafts = { sB: "work in progress" };

    const first = renderHook(() => useChatDraftRestore("sA", true), { wrapper });
    act(() => first.result.current.enqueueLocalRestore(sendFailure("sB")));
    first.rerender();

    // sA is not wedged: its durable restore lands despite sB's pending request.
    await waitFor(() =>
      expect(first.result.current.restoreDraftRequest?.serverRestoreId).toBe("msg-1"),
    );

    // The composer goes away before either is applied.
    first.unmount();

    // The queue is storage-backed, so the failed text is still there — and the user
    // returning to sB is offered it, even though sB's draft has their own work in
    // it (ChatInput then waits for that draft to clear rather than overwrite it).
    expect(h.store.pendingSendRestores.sB).toEqual([sendFailure("sB")]);

    const second = renderHook(() => useChatDraftRestore("sB", true), { wrapper });
    await waitFor(() => expect(second.result.current.restoreDraftRequest?.id).toBe("send-failed-1"));
    expect(second.result.current.restoreDraftRequest?.sessionId).toBe("sB");
    // Still queued: offered is not applied, and only an applied restore is safe to drop.
    expect(h.store.pendingSendRestores.sB).toHaveLength(1);
  });

  it("leaves the queue entry alone until the composer reports the hand-off, then drops it", async () => {
    const { result, rerender } = renderHook(() => useChatDraftRestore("sB", true), { wrapper });

    act(() => result.current.enqueueLocalRestore(sendFailure("sB")));
    rerender();

    await waitFor(() => expect(result.current.restoreDraftRequest?.id).toBe("send-failed-1"));
    expect(h.store.dequeuePendingSendRestore).not.toHaveBeenCalled();

    act(() => result.current.handleRestoreDraftApplied());

    // The text now lives in the persisted draft, so the queue entry has nothing
    // left to protect. No server row exists, so nothing is consumed.
    expect(h.store.dequeuePendingSendRestore).toHaveBeenCalledWith("sB", "send-failed-1");
    expect(h.store.pendingSendRestores.sB).toBeUndefined();
    expect(h.consumeChatDraftRestore).not.toHaveBeenCalled();
    expect(result.current.restoreDraftRequest).toBeNull();
  });

  it("drops — never parks — a durable restore once the user navigates away from its session", async () => {
    const { result, rerender } = renderHook(
      ({ session }: { session: string }) => useChatDraftRestore(session, true),
      { wrapper, initialProps: { session: "sA" } },
    );

    await waitFor(() => expect(result.current.restoreDraftRequest?.serverRestoreId).toBe("msg-1"));

    // The user moves to another session before the composer took it.
    rerender({ session: "sB" });

    // It has a server copy: dropping loses nothing and the next fetch re-offers it.
    // Holding it would starve whatever sB has waiting.
    await waitFor(() => expect(result.current.restoreDraftRequest).toBeNull());
    expect(h.store.markDraftRestoreApplied).not.toHaveBeenCalled();
    expect(h.consumeChatDraftRestore).not.toHaveBeenCalled();
  });
});

// A consume whose retries all ran out must not wedge: the row is still on the
// server and still in the ledger, so the NEXT fetch has to try again. Holding
// the id in the in-flight set forever would strand it until a full remount.
describe("useChatDraftRestore reconciliation (#5219)", () => {
  it("re-consumes a row on the next fetch after the consume failed", async () => {
    h.store.appliedDraftRestoreIds = ["msg-1"];
    h.consumeChatDraftRestore.mockRejectedValueOnce(new Error("offline"));

    const { result, rerender } = renderHook(
      ({ session }: { session: string }) => useChatDraftRestore(session, true),
      { wrapper, initialProps: { session: "sA" } },
    );

    // Already applied on this device: reconciled, never re-offered.
    await waitFor(() => expect(h.consumeChatDraftRestore).toHaveBeenCalledTimes(1));
    expect(result.current.restoreDraftRequest).toBeNull();
    expect(h.store.forgetDraftRestoreApplied).not.toHaveBeenCalled();

    // The row is still there on the next fetch — try again, and only now is the
    // ledger entry safe to drop.
    rerender({ session: "sB" });
    rerender({ session: "sA" });

    await waitFor(() => expect(h.consumeChatDraftRestore).toHaveBeenCalledTimes(2));
    await waitFor(() => expect(h.store.forgetDraftRestoreApplied).toHaveBeenCalledWith("msg-1"));
    expect(result.current.restoreDraftRequest).toBeNull();
  });
});
