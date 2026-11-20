// @vitest-environment node
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { StorageAdapter } from "../../types/storage";
import {
  MAX_ENTRIES_PER_WORKSPACE,
  assigneeUsageStorageKey,
  compareAssigneeUsage,
  getAssigneeUsage,
  pruneAssigneeUsageWorkspaces,
  recordAssigneeUsage,
  resetAssigneeUsageForTests,
  subscribeAssigneeUsage,
} from "./assignee-usage-store";

/**
 * An in-memory adapter standing in for one origin's localStorage. Several
 * "tabs" share one backing map; `notifyOthers` plays the browser's `storage`
 * event, which only reaches the contexts that did not make the write.
 */
function createSharedStorage() {
  const data = new Map<string, string>();
  const listeners = new Set<(key: string | null) => void>();
  const adapter: StorageAdapter & { failWrites: boolean } = {
    failWrites: false,
    getItem: (k) => data.get(k) ?? null,
    setItem: (k, v) => {
      if (adapter.failWrites) {
        throw new DOMException("The quota has been exceeded.", "QuotaExceededError");
      }
      data.set(k, v);
    },
    removeItem: (k) => {
      data.delete(k);
    },
    keys: () => [...data.keys()],
    subscribe: (listener) => {
      listeners.add(listener);
      return () => listeners.delete(listener);
    },
  };
  return {
    adapter,
    data,
    notifyOthers: (key: string | null) => {
      for (const l of listeners) l(key);
    },
  };
}

let storage: ReturnType<typeof createSharedStorage>;

beforeEach(() => {
  storage = createSharedStorage();
  resetAssigneeUsageForTests(storage.adapter);
  vi.useFakeTimers();
  vi.setSystemTime(1_000);
});

afterEach(() => {
  vi.useRealTimers();
});

const ranked = (userId: string, wsId: string) =>
  [...getAssigneeUsage(userId, wsId)]
    .sort(compareAssigneeUsage)
    .map((e) => `${e.type}:${e.id}:${e.count}`);

const stored = (userId: string) =>
  JSON.parse(storage.data.get(assigneeUsageStorageKey(userId)) ?? "{}");

describe("recordAssigneeUsage", () => {
  it("counts repeat picks and ranks the most-used first", () => {
    recordAssigneeUsage("me", "ws-a", { type: "member", id: "u1" });
    recordAssigneeUsage("me", "ws-a", { type: "agent", id: "a1" });
    recordAssigneeUsage("me", "ws-a", { type: "agent", id: "a1" });
    recordAssigneeUsage("me", "ws-a", { type: "squad", id: "s1" });
    recordAssigneeUsage("me", "ws-a", { type: "agent", id: "a1" });
    recordAssigneeUsage("me", "ws-a", { type: "squad", id: "s1" });

    expect(ranked("me", "ws-a")).toEqual(["agent:a1:3", "squad:s1:2", "member:u1:1"]);
  });

  it("breaks count ties in favour of the more recent pick", () => {
    recordAssigneeUsage("me", "ws-a", { type: "member", id: "u1" });
    vi.setSystemTime(2_000);
    recordAssigneeUsage("me", "ws-a", { type: "member", id: "u2" });

    expect(ranked("me", "ws-a")).toEqual(["member:u2:1", "member:u1:1"]);
  });

  it("keeps the same id under different types apart", () => {
    recordAssigneeUsage("me", "ws-a", { type: "member", id: "x" });
    recordAssigneeUsage("me", "ws-a", { type: "agent", id: "x" });

    expect(ranked("me", "ws-a")).toHaveLength(2);
  });

  it("keeps each workspace's counts separate", () => {
    recordAssigneeUsage("me", "ws-a", { type: "member", id: "u1" });
    recordAssigneeUsage("me", "ws-b", { type: "member", id: "u2" });

    expect(ranked("me", "ws-a")).toEqual(["member:u1:1"]);
    expect(ranked("me", "ws-b")).toEqual(["member:u2:1"]);
  });

  it("persists across reloads", () => {
    recordAssigneeUsage("me", "ws-a", { type: "member", id: "u1" });
    resetAssigneeUsageForTests(storage.adapter);

    expect(ranked("me", "ws-a")).toEqual(["member:u1:1"]);
  });

  // Review #1: a full or unavailable storage must not break assigning.
  it("does not throw when storage rejects the write, and still counts in memory", () => {
    storage.adapter.failWrites = true;

    expect(() =>
      recordAssigneeUsage("me", "ws-a", { type: "member", id: "u1" }),
    ).not.toThrow();
    expect(ranked("me", "ws-a")).toEqual(["member:u1:1"]);
  });

  it("ignores malformed stored data", () => {
    storage.data.set(
      assigneeUsageStorageKey("me"),
      JSON.stringify({ "ws-a": [{ type: "member", id: "u1", count: 2, lastUsedAt: 1 }, { bogus: true }], "ws-b": "x" }),
    );
    expect(ranked("me", "ws-a")).toEqual(["member:u1:2"]);
    expect(ranked("me", "ws-b")).toEqual([]);

    storage.data.set(assigneeUsageStorageKey("other"), "{not json");
    expect(() => recordAssigneeUsage("other", "ws-a", { type: "member", id: "u1" })).not.toThrow();
    expect(ranked("other", "ws-a")).toEqual(["member:u1:1"]);
  });
});

// Review #2: two people signing in on the same browser.
describe("per-user isolation", () => {
  it("never shows or adds to another signed-in user's counts", () => {
    for (let i = 0; i < 7; i++) recordAssigneeUsage("alice", "ws-shared", { type: "member", id: "u1" });

    expect(getAssigneeUsage("bob", "ws-shared")).toEqual([]);
    recordAssigneeUsage("bob", "ws-shared", { type: "member", id: "u1" });

    expect(ranked("bob", "ws-shared")).toEqual(["member:u1:1"]);
    expect(ranked("alice", "ws-shared")).toEqual(["member:u1:7"]);

    // And the same holds for what is on disk after a reload.
    resetAssigneeUsageForTests(storage.adapter);
    expect(ranked("bob", "ws-shared")).toEqual(["member:u1:1"]);
    expect(ranked("alice", "ws-shared")).toEqual(["member:u1:7"]);
  });

  it("returns nothing without a signed-in user or workspace", () => {
    recordAssigneeUsage("me", "ws-a", { type: "member", id: "u1" });
    expect(getAssigneeUsage(null, "ws-a")).toEqual([]);
    expect(getAssigneeUsage("me", null)).toEqual([]);
  });
});

// Review #3: two tabs of the same origin.
describe("multiple tabs", () => {
  /** Another tab's pick: it lands in shared storage, but no event reaches this tab yet. */
  function pickInOtherTab(wsId: string, id: string) {
    const current = stored("me");
    const bucket = current[wsId] ?? [];
    bucket.push({ type: "member", id, count: 1, lastUsedAt: 500 });
    storage.data.set(assigneeUsageStorageKey("me"), JSON.stringify({ ...current, [wsId]: bucket }));
  }

  it("does not drop a pick made in another tab when this tab picks next", () => {
    // This tab has loaded the (empty) history.
    expect(getAssigneeUsage("me", "ws-a")).toEqual([]);
    pickInOtherTab("ws-a", "grace");

    recordAssigneeUsage("me", "ws-a", { type: "member", id: "ada" });

    expect(ranked("me", "ws-a")).toEqual(["member:ada:1", "member:grace:1"]);
    resetAssigneeUsageForTests(storage.adapter);
    expect(ranked("me", "ws-a")).toEqual(["member:ada:1", "member:grace:1"]);
  });

  it("picks up another tab's write through the storage subscription", () => {
    const listener = vi.fn();
    expect(getAssigneeUsage("me", "ws-a")).toEqual([]);
    const unsubscribe = subscribeAssigneeUsage(listener);

    storage.data.set(
      assigneeUsageStorageKey("me"),
      JSON.stringify({ "ws-a": [{ type: "member", id: "u9", count: 3, lastUsedAt: 5 }] }),
    );
    storage.notifyOthers(assigneeUsageStorageKey("me"));

    expect(listener).toHaveBeenCalled();
    expect(ranked("me", "ws-a")).toEqual(["member:u9:3"]);
    unsubscribe();
  });

  it("an unchanged prune writes nothing, so a stale tab cannot overwrite newer picks", () => {
    recordAssigneeUsage("me", "ws-a", { type: "member", id: "u1" });
    const setItem = vi.spyOn(storage.adapter, "setItem");

    pruneAssigneeUsageWorkspaces("me", ["ws-a"]);

    expect(setItem).not.toHaveBeenCalled();
  });

  it("a prune that removes a workspace keeps picks another tab just made", () => {
    recordAssigneeUsage("me", "ws-a", { type: "member", id: "u1" });
    recordAssigneeUsage("me", "ws-gone", { type: "member", id: "u1" });
    pickInOtherTab("ws-a", "grace");

    pruneAssigneeUsageWorkspaces("me", ["ws-a"]);

    expect(Object.keys(stored("me"))).toEqual(["ws-a"]);
    expect(ranked("me", "ws-a")).toEqual(["member:u1:1", "member:grace:1"]);
  });
});

// Review #4: the cap must not freeze the ranking.
describe("capacity", () => {
  it("lets a new assignee climb into the top three after many others were used", () => {
    // Thirty-one assignees, each picked twice, well before the newcomer.
    for (let i = 0; i < 31; i++) {
      recordAssigneeUsage("me", "ws-a", { type: "member", id: `old-${i}` });
      recordAssigneeUsage("me", "ws-a", { type: "member", id: `old-${i}` });
    }
    vi.setSystemTime(2_000);
    for (let i = 0; i < 5; i++) recordAssigneeUsage("me", "ws-a", { type: "agent", id: "new" });

    expect(ranked("me", "ws-a")[0]).toBe("agent:new:5");
  });

  it("evicts the least recently used entry, not the least counted", () => {
    // A heavily used, recently picked assignee …
    for (let i = 0; i < 10; i++) recordAssigneeUsage("me", "ws-a", { type: "member", id: "fav" });
    // … then enough one-off picks to fill the bucket …
    for (let i = 0; i < MAX_ENTRIES_PER_WORKSPACE - 1; i++) {
      vi.setSystemTime(2_000 + i);
      recordAssigneeUsage("me", "ws-a", { type: "member", id: `once-${i}` });
    }
    vi.setSystemTime(10_000);
    recordAssigneeUsage("me", "ws-a", { type: "member", id: "fav" });
    // … and one more to overflow it.
    vi.setSystemTime(10_001);
    recordAssigneeUsage("me", "ws-a", { type: "member", id: "newcomer" });

    const entries = getAssigneeUsage("me", "ws-a");
    expect(entries).toHaveLength(MAX_ENTRIES_PER_WORKSPACE);
    expect(ranked("me", "ws-a")[0]).toBe("member:fav:11");
    // The oldest one-off pick made room.
    expect(entries.some((e) => e.id === "once-0")).toBe(false);
  });
});

describe("pruneAssigneeUsageWorkspaces", () => {
  it("drops workspaces the user no longer belongs to", () => {
    recordAssigneeUsage("me", "ws-a", { type: "member", id: "u1" });
    recordAssigneeUsage("me", "ws-b", { type: "member", id: "u1" });

    pruneAssigneeUsageWorkspaces("me", ["ws-a"]);

    expect(Object.keys(stored("me"))).toEqual(["ws-a"]);
    expect(getAssigneeUsage("me", "ws-b")).toEqual([]);
  });
});
