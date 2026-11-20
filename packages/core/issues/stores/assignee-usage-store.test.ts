// @vitest-environment node
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { StorageAdapter } from "../../types/storage";

type UsageModule = typeof import("./assignee-usage-store");

/**
 * One origin's localStorage shared by several tabs. Each tab gets its own
 * adapter: `storage` events reach the other tabs only, and `update` holds an
 * origin-wide lock the way Web Locks do.
 */
function createOrigin() {
  const data = new Map<string, string>();
  const tabs = new Set<{ listeners: Set<(key: string | null) => void> }>();
  let lockHeld = false;
  const waiting: Array<() => void> = [];
  const acquire = (fn: () => void) => {
    if (lockHeld) waiting.push(fn);
    else {
      lockHeld = true;
      fn();
    }
  };
  const release = () => {
    const next = waiting.shift();
    if (next) next();
    else lockHeld = false;
  };

  function createTab() {
    const listeners = new Set<(key: string | null) => void>();
    const tab = {
      listeners,
      failWrites: false,
      /** Runs once, right after this tab's next read and before that read returns. */
      afterNextRead: undefined as (() => void) | undefined,
      setItemCalls: 0,
      adapter: undefined as unknown as StorageAdapter,
    };
    const adapter: StorageAdapter = {
      getItem: (k) => {
        const value = data.get(k) ?? null;
        const hook = tab.afterNextRead;
        tab.afterNextRead = undefined;
        hook?.();
        return value;
      },
      setItem: (k, v) => {
        tab.setItemCalls++;
        if (tab.failWrites) {
          throw new DOMException("The quota has been exceeded.", "QuotaExceededError");
        }
        data.set(k, v);
        for (const other of tabs) {
          if (other !== tab) for (const l of other.listeners) l(k);
        }
      },
      removeItem: (k) => {
        data.delete(k);
      },
      keys: () => [...data.keys()],
      subscribe: (listener) => {
        listeners.add(listener);
        return () => listeners.delete(listener);
      },
      update: (k, updater) =>
        new Promise<void>((resolve, reject) => {
          acquire(() => {
            try {
              const next = updater(adapter.getItem(k));
              if (next !== null) adapter.setItem(k, next);
              resolve();
            } catch (error) {
              reject(error);
            } finally {
              release();
            }
          });
        }),
    };
    tab.adapter = adapter;
    tabs.add(tab);
    return tab;
  }

  return { data, createTab };
}

type Origin = ReturnType<typeof createOrigin>;
type Tab = ReturnType<Origin["createTab"]>;

/** A fresh copy of the store module, as a newly opened tab would load it. */
async function openTab(origin: Origin): Promise<{ tab: Tab; usage: UsageModule }> {
  vi.resetModules();
  const usage = await import("./assignee-usage-store");
  const tab = origin.createTab();
  usage.resetAssigneeUsageForTests(tab.adapter);
  return { tab, usage };
}

const ranked = (usage: UsageModule, userId: string, wsId: string) =>
  [...usage.getAssigneeUsage(userId, wsId)]
    .sort(usage.compareAssigneeUsage)
    .map((e) => `${e.type}:${e.id}:${e.count}`);

let origin: Origin;
let tab: Tab;
let usage: UsageModule;

const stored = (userId: string) =>
  JSON.parse(origin.data.get(usage.assigneeUsageStorageKey(userId)) ?? "{}");

const pick = (u: UsageModule, id: string, times = 1, userId = "me", wsId = "ws-a") => {
  let last: Promise<void> | void = undefined;
  for (let i = 0; i < times; i++) last = u.recordAssigneeUsage(userId, wsId, { type: "member", id });
  return last;
};

beforeEach(async () => {
  vi.useFakeTimers({ toFake: ["Date"] });
  vi.setSystemTime(1_000);
  origin = createOrigin();
  ({ tab, usage } = await openTab(origin));
});

afterEach(() => {
  vi.useRealTimers();
});

describe("recordAssigneeUsage", () => {
  it("counts repeat picks and ranks the most-used first", async () => {
    usage.recordAssigneeUsage("me", "ws-a", { type: "member", id: "u1" });
    usage.recordAssigneeUsage("me", "ws-a", { type: "agent", id: "a1" });
    usage.recordAssigneeUsage("me", "ws-a", { type: "agent", id: "a1" });
    usage.recordAssigneeUsage("me", "ws-a", { type: "squad", id: "s1" });
    usage.recordAssigneeUsage("me", "ws-a", { type: "agent", id: "a1" });
    await usage.recordAssigneeUsage("me", "ws-a", { type: "squad", id: "s1" });

    expect(ranked(usage, "me", "ws-a")).toEqual(["agent:a1:3", "squad:s1:2", "member:u1:1"]);
  });

  it("shows a pick right away, before it is saved", () => {
    void pick(usage, "u1");
    expect(ranked(usage, "me", "ws-a")).toEqual(["member:u1:1"]);
  });

  it("breaks count ties in favour of the more recent pick", async () => {
    await pick(usage, "u1");
    vi.setSystemTime(2_000);
    await pick(usage, "u2");

    expect(ranked(usage, "me", "ws-a")).toEqual(["member:u2:1", "member:u1:1"]);
  });

  it("keeps the same id under different types apart", async () => {
    usage.recordAssigneeUsage("me", "ws-a", { type: "member", id: "x" });
    await usage.recordAssigneeUsage("me", "ws-a", { type: "agent", id: "x" });

    expect(ranked(usage, "me", "ws-a")).toHaveLength(2);
  });

  it("keeps each workspace's counts separate", async () => {
    await pick(usage, "u1", 1, "me", "ws-a");
    await pick(usage, "u2", 1, "me", "ws-b");

    expect(ranked(usage, "me", "ws-a")).toEqual(["member:u1:1"]);
    expect(ranked(usage, "me", "ws-b")).toEqual(["member:u2:1"]);
  });

  it("persists across reloads", async () => {
    await pick(usage, "u1", 2);
    const reloaded = await openTab(origin);

    expect(ranked(reloaded.usage, "me", "ws-a")).toEqual(["member:u1:2"]);
  });

  it("keeps counting in memory while saving fails, then saves the full count", async () => {
    tab.failWrites = true;

    await expect(pick(usage, "u1")).resolves.toBeUndefined();
    await pick(usage, "u1");
    expect(ranked(usage, "me", "ws-a")).toEqual(["member:u1:2"]);
    expect(stored("me")).toEqual({});

    tab.failWrites = false;
    await pick(usage, "u1");
    expect(stored("me")["ws-a"][0].count).toBe(3);
    expect(ranked(usage, "me", "ws-a")).toEqual(["member:u1:3"]);
  });

  it("ignores malformed stored data", async () => {
    origin.data.set(
      usage.assigneeUsageStorageKey("me"),
      JSON.stringify({
        "ws-a": [{ type: "member", id: "u1", count: 2, lastUsedAt: 1 }, { bogus: true }],
        "ws-b": "x",
      }),
    );
    expect(ranked(usage, "me", "ws-a")).toEqual(["member:u1:2"]);
    expect(ranked(usage, "me", "ws-b")).toEqual([]);

    origin.data.set(usage.assigneeUsageStorageKey("other"), "{not json");
    await pick(usage, "u1", 1, "other");
    expect(ranked(usage, "other", "ws-a")).toEqual(["member:u1:1"]);
  });
});

describe("per-user isolation", () => {
  it("never shows or adds to another signed-in user's counts", async () => {
    await pick(usage, "u1", 7, "alice", "ws-shared");

    expect(usage.getAssigneeUsage("bob", "ws-shared")).toEqual([]);
    await pick(usage, "u1", 1, "bob", "ws-shared");

    expect(ranked(usage, "bob", "ws-shared")).toEqual(["member:u1:1"]);
    expect(ranked(usage, "alice", "ws-shared")).toEqual(["member:u1:7"]);

    const reloaded = await openTab(origin);
    expect(ranked(reloaded.usage, "bob", "ws-shared")).toEqual(["member:u1:1"]);
    expect(ranked(reloaded.usage, "alice", "ws-shared")).toEqual(["member:u1:7"]);
  });

  it("returns nothing without a signed-in user or workspace", async () => {
    await pick(usage, "u1");
    expect(usage.getAssigneeUsage(null, "ws-a")).toEqual([]);
    expect(usage.getAssigneeUsage("me", null)).toEqual([]);
  });
});

describe("multiple tabs", () => {
  it("keeps both picks when another tab saves between this tab's read and write", async () => {
    const other = await openTab(origin);
    let otherSave: Promise<void> | void = undefined;
    // Tab B picks Grace at the worst moment: after tab A read the stored
    // value, before A writes Ada back.
    tab.afterNextRead = () => {
      otherSave = pick(other.usage, "grace");
    };

    await pick(usage, "ada");
    await otherSave;

    const third = await openTab(origin);
    expect(ranked(third.usage, "me", "ws-a").sort()).toEqual(["member:ada:1", "member:grace:1"]);
  });

  it("does not drop a pick another tab saved after this tab loaded", async () => {
    const other = await openTab(origin);
    expect(usage.getAssigneeUsage("me", "ws-a")).toEqual([]);

    await pick(other.usage, "grace");
    await pick(usage, "ada");

    const third = await openTab(origin);
    expect(ranked(third.usage, "me", "ws-a").sort()).toEqual(["member:ada:1", "member:grace:1"]);
  });

  it("shows another tab's save while subscribed", async () => {
    const other = await openTab(origin);
    const listener = vi.fn();
    expect(usage.getAssigneeUsage("me", "ws-a")).toEqual([]);
    const unsubscribe = usage.subscribeAssigneeUsage(listener);

    await pick(other.usage, "grace", 3);

    expect(listener).toHaveBeenCalled();
    expect(ranked(usage, "me", "ws-a")).toEqual(["member:grace:3"]);
    unsubscribe();
  });

  it("shows another tab's save made while nothing was subscribed", async () => {
    const other = await openTab(origin);
    // The picker opened and closed once, so this tab has a cached copy.
    usage.subscribeAssigneeUsage(() => {})();
    expect(usage.getAssigneeUsage("me", "ws-a")).toEqual([]);

    await pick(other.usage, "grace");

    // Opening the picker again subscribes, then reads.
    const unsubscribe = usage.subscribeAssigneeUsage(() => {});
    expect(ranked(usage, "me", "ws-a")).toEqual(["member:grace:1"]);
    unsubscribe();
  });

  it("does not lose unsaved picks when another tab saves", async () => {
    const other = await openTab(origin);
    const unsubscribe = usage.subscribeAssigneeUsage(() => {});
    tab.failWrites = true;
    await pick(usage, "ada", 2);

    await pick(other.usage, "grace");

    expect(ranked(usage, "me", "ws-a")).toEqual(["member:ada:2", "member:grace:1"]);

    tab.failWrites = false;
    await pick(usage, "ada");
    const third = await openTab(origin);
    expect(ranked(third.usage, "me", "ws-a")).toEqual(["member:ada:3", "member:grace:1"]);
    unsubscribe();
  });

  it("an unchanged prune writes nothing, so a stale tab cannot overwrite newer picks", async () => {
    await pick(usage, "u1");
    const writes = tab.setItemCalls;

    await usage.pruneAssigneeUsageWorkspaces("me", ["ws-a"]);

    expect(tab.setItemCalls).toBe(writes);
  });

  it("a prune that removes a workspace keeps picks another tab just saved", async () => {
    await pick(usage, "u1", 1, "me", "ws-a");
    await pick(usage, "u1", 1, "me", "ws-gone");
    const other = await openTab(origin);
    tab.afterNextRead = () => {
      void pick(other.usage, "grace");
    };

    await usage.pruneAssigneeUsageWorkspaces("me", ["ws-a"]);

    expect(Object.keys(stored("me"))).toEqual(["ws-a"]);
    const third = await openTab(origin);
    expect(ranked(third.usage, "me", "ws-a").sort()).toEqual(["member:grace:1", "member:u1:1"]);
  });
});

describe("capacity", () => {
  it("lets a new assignee climb into the top three after many others were used", async () => {
    for (let i = 0; i < 31; i++) await pick(usage, `old-${i}`, 2);
    vi.setSystemTime(2_000);
    await pick(usage, "new", 5);

    expect(ranked(usage, "me", "ws-a")[0]).toBe("member:new:5");
  });

  it("never evicts the most-picked assignee, however many others follow", async () => {
    await pick(usage, "fav", 200);
    for (let i = 0; i < usage.MAX_ENTRIES_PER_WORKSPACE + 50; i++) {
      vi.setSystemTime(2_000 + i);
      await pick(usage, `once-${i}`);
    }
    await pick(usage, "fav");

    expect(usage.getAssigneeUsage("me", "ws-a")).toHaveLength(usage.MAX_ENTRIES_PER_WORKSPACE);
    expect(ranked(usage, "me", "ws-a")[0]).toBe("member:fav:201");
  });

  it("keeps a newcomer when the bucket is full, so it can keep climbing", async () => {
    for (let i = 0; i < usage.MAX_ENTRIES_PER_WORKSPACE; i++) await pick(usage, `regular-${i}`, 2);
    vi.setSystemTime(2_000);

    await pick(usage, "newcomer");
    expect(ranked(usage, "me", "ws-a")).toContain("member:newcomer:1");

    await pick(usage, "newcomer", 2);
    expect(ranked(usage, "me", "ws-a")[0]).toBe("member:newcomer:3");
    expect(usage.getAssigneeUsage("me", "ws-a")).toHaveLength(usage.MAX_ENTRIES_PER_WORKSPACE);
  });
});

describe("pruneAssigneeUsageWorkspaces", () => {
  it("drops workspaces the user no longer belongs to", async () => {
    await pick(usage, "u1", 1, "me", "ws-a");
    await pick(usage, "u1", 1, "me", "ws-b");

    await usage.pruneAssigneeUsageWorkspaces("me", ["ws-a"]);

    expect(Object.keys(stored("me"))).toEqual(["ws-a"]);
    expect(usage.getAssigneeUsage("me", "ws-b")).toEqual([]);
  });
});
