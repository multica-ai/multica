// @vitest-environment node
import { describe, expect, it, vi, beforeEach, afterEach } from "vitest";
import {
  CommentLocateController,
  LOCATE_EXPAND_SETTLE_MS,
  LOCATE_LAYOUT_RETRY_MS,
  LOCATE_TIMEOUT_MS,
  type CommentLocateAdapter,
  type LocateResult,
} from "./comment-locate";
import { buildTimelineRows } from "./timeline-thread";
import type { TimelineEntry } from "@multica/core/types";
import {
  resolveExpandRootId,
  resolvePublishedRootIds,
} from "./comment-locate";

/**
 * Virtual-clock harness: `schedule` returns handles backed by vi timers.
 * Advance with `advance` so both the settle delay and the probe backoff
 * run deterministically — real-clock tests would only cover the happy
 * path and time out the rest.
 */
interface Harness {
  adapter: CommentLocateAdapter & {
    setIndex: (i: number) => void;
    setLayoutReady: (b: boolean) => void;
    pendingScroll: () => { resolve: () => void; reject: (e: unknown) => void };
    viewable: Set<string>;
    scrollCalls: number;
    findCalls: number;
  };
  results: LocateResult[];
}

function makeHarness(): Harness {
  const results: LocateResult[] = [];
  const state = { index: 3, layoutReady: true };
  // Deferred created by the LAST scrollToIndex call — the controller holds
  // its promise; the test settles it through `pendingScroll()`.
  let deferred: {
    resolve: () => void;
    reject: (e: unknown) => void;
  } | null = null;
  const adapter: Harness["adapter"] = {
    findCalls: 0,
    scrollCalls: 0,
    viewable: new Set<string>(),
    setIndex: (i) => (state.index = i),
    setLayoutReady: (b) => (state.layoutReady = b),
    pendingScroll: () => {
      if (!deferred) throw new Error("no pending scrollToIndex promise");
      return deferred;
    },
    findIndex: () => {
      adapter.findCalls += 1;
      return state.index;
    },
    getLayout: () => (state.layoutReady ? { y: 120 } : null),
    scrollToIndex: () => {
      adapter.scrollCalls += 1;
      return new Promise<void>((res, rej) => {
        deferred = { resolve: res, reject: rej };
      });
    },
    isViewable: (rootId) => adapter.viewable.has(rootId),
    schedule: (fn, ms) => {
      const id = setTimeout(fn, ms);
      return { cancel: () => clearTimeout(id) };
    },
  };
  return { adapter, results };
}

function makeController(h: Harness) {
  return new CommentLocateController(h.adapter, (r) => h.results.push(r));
}

const REQ = { issueId: "i1", rootId: "r1", nonce: 1 };

beforeEach(() => {
  vi.useFakeTimers();
});

afterEach(() => {
  vi.useRealTimers();
});

describe("CommentLocateController — happy path", () => {
  it("pending → scroll → viewability → located", async () => {
    const h = makeHarness();
    const c = makeController(h);
    c.start(REQ);
    // Nothing before the expand-settle delay.
    expect(h.results).toEqual([]);
    vi.advanceTimersByTime(LOCATE_EXPAND_SETTLE_MS);
    expect(h.adapter.scrollCalls).toBe(1);
    const d = h.adapter.pendingScroll();
    d.resolve();
    await vi.advanceTimersByTimeAsync(0);
    // Scroll resolved but the row is NOT viewable yet — must not locate.
    expect(h.results).toEqual([]);
    h.adapter.viewable.add("r1");
    c.confirmViewable();
    expect(h.results).toEqual([
      { ...REQ, status: "located" },
    ]);
  });

  it("already-viewable row locates without a second viewability event", async () => {
    const h = makeHarness();
    h.adapter.viewable.add("r1");
    const c = makeController(h);
    c.start(REQ);
    vi.advanceTimersByTime(LOCATE_EXPAND_SETTLE_MS);
    h.adapter.pendingScroll().resolve();
    await vi.advanceTimersByTimeAsync(0);
    expect(h.results).toEqual([{ ...REQ, status: "located" }]);
  });
});

describe("CommentLocateController — viewability gate", () => {
  it("viewability of a DIFFERENT root does not close the run", async () => {
    const h = makeHarness();
    const c = makeController(h);
    c.start(REQ);
    vi.advanceTimersByTime(LOCATE_EXPAND_SETTLE_MS);
    h.adapter.pendingScroll().resolve();
    await vi.advanceTimersByTimeAsync(0);
    h.adapter.viewable.add("some-other-root");
    c.confirmViewable();
    expect(h.results).toEqual([]);
    // Still waiting — the timeout eventually fails it, not a false close.
    vi.advanceTimersByTime(LOCATE_TIMEOUT_MS);
    expect(h.results).toEqual([{ ...REQ, status: "failed", reason: "timeout" }]);
  });
});

describe("CommentLocateController — failure paths", () => {
  it("scroll reject → failed(scroll)", async () => {
    const h = makeHarness();
    const c = makeController(h);
    c.start(REQ);
    vi.advanceTimersByTime(LOCATE_EXPAND_SETTLE_MS);
    h.adapter.pendingScroll().reject(new Error("estimate miss"));
    await vi.advanceTimersByTimeAsync(0);
    expect(h.results).toEqual([{ ...REQ, status: "failed", reason: "scroll" }]);
  });

  it("row never lays out after retries → failed(layout)", () => {
    const h = makeHarness();
    h.adapter.setLayoutReady(false);
    const c = makeController(h);
    c.start(REQ);
    vi.advanceTimersByTime(
      LOCATE_EXPAND_SETTLE_MS + LOCATE_LAYOUT_RETRY_MS * 10,
    );
    expect(h.results).toEqual([{ ...REQ, status: "failed", reason: "layout" }]);
    expect(h.adapter.findCalls).toBe(4);
  });

  it("row missing from data → failed(not-found)", () => {
    const h = makeHarness();
    h.adapter.setIndex(-1);
    const c = makeController(h);
    c.start(REQ);
    vi.advanceTimersByTime(
      LOCATE_EXPAND_SETTLE_MS + LOCATE_LAYOUT_RETRY_MS * 10,
    );
    expect(h.results).toEqual([{ ...REQ, status: "failed", reason: "not-found" }]);
  });

  it("whole-run timeout fires while awaiting scroll → failed(timeout)", () => {
    const h = makeHarness();
    const c = makeController(h);
    c.start(REQ);
    vi.advanceTimersByTime(LOCATE_EXPAND_SETTLE_MS);
    // Scroll promise never settles.
    vi.advanceTimersByTime(LOCATE_TIMEOUT_MS);
    expect(h.results).toEqual([{ ...REQ, status: "failed", reason: "timeout" }]);
  });

  it("timeout fires while awaiting viewability → failed(timeout)", async () => {
    const h = makeHarness();
    const c = makeController(h);
    c.start(REQ);
    vi.advanceTimersByTime(LOCATE_EXPAND_SETTLE_MS);
    h.adapter.pendingScroll().resolve();
    await vi.advanceTimersByTimeAsync(LOCATE_TIMEOUT_MS);
    expect(h.results).toEqual([{ ...REQ, status: "failed", reason: "timeout" }]);
  });
});

describe("CommentLocateController — retry / cancel semantics", () => {
  it("retrying the SAME root with a NEW nonce runs a fresh scroll", async () => {
    const h = makeHarness();
    h.adapter.viewable.add("r1");
    const c = makeController(h);
    // First attempt fails on scroll.
    c.start({ ...REQ, nonce: 1 });
    vi.advanceTimersByTime(LOCATE_EXPAND_SETTLE_MS);
    expect(h.adapter.scrollCalls).toBe(1);
    h.adapter.pendingScroll().reject(new Error("miss"));
    await vi.advanceTimersByTimeAsync(0);
    expect(h.results).toEqual([
      { ...REQ, nonce: 1, status: "failed", reason: "scroll" },
    ]);
    // Same root, new nonce — must scroll again and succeed.
    c.start({ ...REQ, nonce: 2 });
    vi.advanceTimersByTime(LOCATE_EXPAND_SETTLE_MS);
    expect(h.adapter.scrollCalls).toBe(2);
    h.adapter.pendingScroll().resolve();
    await vi.advanceTimersByTimeAsync(0);
    expect(h.results[1]).toEqual({ ...REQ, nonce: 2, status: "located" });
  });

  it("start with an unchanged nonce is a no-op (no double scroll)", () => {
    const h = makeHarness();
    const c = makeController(h);
    c.start(REQ);
    c.start(REQ); // effect re-ran with the same intent
    vi.advanceTimersByTime(LOCATE_EXPAND_SETTLE_MS);
    expect(h.adapter.scrollCalls).toBe(1);
  });

  it("a new nonce cancels the in-flight run without emitting its result", async () => {
    const h = makeHarness();
    h.adapter.viewable.add("r1");
    const c = makeController(h);
    c.start({ ...REQ, nonce: 1 });
    vi.advanceTimersByTime(LOCATE_EXPAND_SETTLE_MS);
    const first = h.adapter.pendingScroll();
    // User re-taps before the first scroll settles.
    c.start({ ...REQ, nonce: 2 });
    first.resolve(); // stale settlement — must be ignored
    await vi.advanceTimersByTimeAsync(LOCATE_EXPAND_SETTLE_MS);
    expect(h.adapter.scrollCalls).toBe(2);
    expect(h.results).toEqual([]);
    h.adapter.pendingScroll().resolve();
    await vi.advanceTimersByTimeAsync(0);
    expect(h.results).toEqual([{ ...REQ, nonce: 2, status: "located" }]);
  });

  it("cancel() drops the run and all timers without emitting", () => {
    const h = makeHarness();
    const c = makeController(h);
    c.start(REQ);
    c.cancel();
    vi.advanceTimersByTime(LOCATE_TIMEOUT_MS * 2);
    expect(h.results).toEqual([]);
  });
});

describe("resolveExpandRootId", () => {
  const root = (id: string, parentId: string | null = null): TimelineEntry =>
    ({
      type: "comment",
      id,
      actor_type: "member",
      actor_id: `u-${id}`,
      content: `c-${id}`,
      parent_id: parentId,
      created_at: "2026-01-01T00:00:00Z",
      updated_at: "2026-01-01T00:00:00Z",
      comment_type: "comment",
      reactions: [],
      attachments: [],
    }) as TimelineEntry;

  it("maps a root id to itself", () => {
    const rows = buildTimelineRows([root("a"), root("b")]);
    expect(resolveExpandRootId(rows, "a")).toBe("a");
  });

  it("maps a nested reply to its owning root", () => {
    const rows = buildTimelineRows([
      root("a"),
      root("r1", "a"),
      root("r2", "r1"), // reply-to-reply still bundles under the root
    ]);
    expect(resolveExpandRootId(rows, "r2")).toBe("a");
  });

  it("returns null when the comment is not in the rows yet", () => {
    const rows = buildTimelineRows([root("a")]);
    expect(resolveExpandRootId(rows, "zz")).toBeNull();
  });

  it("ignores activity rows", () => {
    const activity = {
      type: "activity",
      id: "act",
      actor_type: "member",
      actor_id: "u",
      created_at: "2026-01-01T00:00:00Z",
    } as unknown as TimelineEntry;
    const rows = buildTimelineRows([activity, root("a")]);
    expect(resolveExpandRootId(rows, "a")).toBe("a");
  });
});

describe("resolvePublishedRootIds", () => {
  const root = (id: string, parentId: string | null = null): TimelineEntry =>
    ({
      type: "comment",
      id,
      actor_type: "member",
      actor_id: `u-${id}`,
      content: `c-${id}`,
      parent_id: parentId,
      created_at: "2026-01-01T00:00:00Z",
      updated_at: "2026-01-01T00:00:00Z",
      comment_type: "comment",
      reactions: [],
      attachments: [],
    }) as TimelineEntry;

  it("resolves each queued publish to its owning root, in row order", () => {
    const rows = buildTimelineRows([
      root("a"),
      root("b"),
      root("b-reply", "b"),
    ]);
    const resolved = resolvePublishedRootIds(rows, ["b-reply", "a"]);
    // Map keys follow the ROW order of the owning roots (a before b),
    // not the queue order — callers get a deterministic expansion order.
    expect([...resolved.keys()]).toEqual(["a", "b-reply"]);
    expect(resolved.get("b-reply")).toBe("b");
    expect(resolved.get("a")).toBe("a");
  });

  it("keeps unresolved ids out of the result — the caller retries them later", () => {
    const rows = buildTimelineRows([root("a")]);
    const resolved = resolvePublishedRootIds(rows, ["a", "still-optimistic"]);
    expect([...resolved.keys()]).toEqual(["a"]);
    expect(resolved.has("still-optimistic")).toBe(false);
  });

  it("returns an empty map for an empty queue", () => {
    const rows = buildTimelineRows([root("a")]);
    expect(resolvePublishedRootIds(rows, []).size).toBe(0);
  });

  it("collapses multiple publishes in the same thread to one root entry", () => {
    // Root + its reply published back-to-back → one expansion of root "a"
    // (Map keyed by published id keeps both entries, but the root VALUE is
    // shared; callers expanding per entry stay idempotent — and the
    // root-only view is a single Set entry).
    const rows = buildTimelineRows([
      root("a"),
      root("a-r", "a"),
    ]);
    const resolved = resolvePublishedRootIds(rows, ["a", "a-r"]);
    expect(new Set(resolved.values())).toEqual(new Set(["a"]));
  });
});
