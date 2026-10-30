// @vitest-environment node
import { afterEach, describe, expect, it, vi } from "vitest";
import { SearchIndexHost, type PortLike } from "./host";
import type { IndexTarget, TabMessage, WorkerMessage } from "./protocol";
import { MemoryIndexStore, type SearchIndexDatabaseOwner } from "./store";
import { FakeServer, issueRecord } from "./testing";

const target: IndexTarget = { userId: "user", workspaceId: "ws", workspaceSlug: "acme" };

/** A tab that answers the worker's fetch requests from a fake server. */
class FakeTab implements PortLike {
  received: WorkerMessage[] = [];
  send!: (message: TabMessage) => void;
  answer = true;
  failWith: number | null = null;

  constructor(private readonly server: FakeServer) {}

  postMessage(message: WorkerMessage): void {
    this.received.push(message);
    if (message.type !== "fetch" || !this.answer) return;
    const { op, params } = message;
    void (async () => {
      if (this.failWith !== null) {
        this.send({ type: "fetch-result", id: message.id, ok: false, status: this.failWith, message: "denied" });
        return;
      }
      const data =
        op === "manifest"
          ? await this.server.manifest()
          : op === "snapshot"
            ? await this.server.snapshot(params.afterNumber ?? 0, params.limit ?? 200)
            : await this.server.changes(params.cursor ?? "");
      this.send({ type: "fetch-result", id: message.id, ok: true, data });
    })();
  }

  last<T extends WorkerMessage["type"]>(type: T): Extract<WorkerMessage, { type: T }> | undefined {
    return this.received.filter((m) => m.type === type).at(-1) as Extract<WorkerMessage, { type: T }> | undefined;
  }
}

function setup(
  server: FakeServer,
  options: { releaseDelayMs?: number; portTimeoutMs?: number; now?: () => number } = {},
) {
  const stores = new Map<string, MemoryIndexStore>();
  // Databases on "disk": every store ever created, by user:workspace key.
  const deleteDatabases = vi.fn(async (shouldDelete: (owner: SearchIndexDatabaseOwner) => boolean) => {
    for (const key of [...stores.keys()]) {
      const [userId, workspaceId] = key.split(":") as [string, string];
      if (shouldDelete({ userId, workspaceId })) {
        await stores.get(key)!.destroy();
        stores.delete(key);
      }
    }
  });
  const host = new SearchIndexHost({
    createStore: (t) => {
      const key = `${t.userId}:${t.workspaceId}`;
      const store = stores.get(key) ?? new MemoryIndexStore();
      stores.set(key, store);
      return store;
    },
    deleteDatabases,
    indexOptions: { syncDebounceMs: 0, pollIntervalMs: 60 * 60 * 1000, retryBaseMs: 60 * 60 * 1000 },
    releaseDelayMs: options.releaseDelayMs ?? 60_000,
    portTimeoutMs: options.portTimeoutMs,
    fetchTimeoutMs: 50,
    now: options.now,
  });
  const connect = () => {
    const tab = new FakeTab(server);
    tab.send = host.connect(tab);
    return tab;
  };
  return { host, stores, deleteDatabases, connect };
}

async function settle(): Promise<void> {
  for (let i = 0; i < 20; i++) await new Promise((resolve) => setTimeout(resolve, 1));
}

afterEach(() => {
  vi.useRealTimers();
});

describe("SearchIndexHost", () => {
  it("syncs through an attached tab and answers its searches", async () => {
    const server = new FakeServer();
    server.apply({ kind: "issue", record: issueRecord(1, "Hosted issue") });
    const { connect } = setup(server);
    const tab = connect();

    expect(tab.received[0]).toEqual({ type: "hello" });
    tab.send({ type: "attach", target });
    await settle();

    expect(tab.received.filter((m) => m.type === "fetch").every((m) => m.type === "fetch" && m.workspaceSlug === "acme")).toBe(true);
    expect(tab.last("serving")).toEqual({ type: "serving", key: "user:ws", serving: true });

    tab.send({ type: "search", id: 7, kind: "issues", params: { q: "hosted" } });
    await settle();
    const result = tab.last("search-result");
    expect(result?.id).toBe(7);
    expect(result?.result && "issues" in result.result ? result.result.issues.map((i) => i.id) : null).toEqual(["issue-1"]);
  });

  it("asks another tab when the one it asked goes silent", async () => {
    const server = new FakeServer();
    server.apply({ kind: "issue", record: issueRecord(1, "Failover issue") });
    let now = 1_000;
    const { connect } = setup(server, { now: () => now });
    const silent = connect();
    silent.answer = false;
    const live = connect();
    live.send({ type: "attach", target });
    now += 1;
    // The silent tab was active most recently, so it is asked first.
    silent.send({ type: "attach", target });
    await settle();
    await new Promise((resolve) => setTimeout(resolve, 120));
    await settle();

    expect(silent.received.some((m) => m.type === "fetch")).toBe(true);
    expect(live.last("serving")?.serving).toBe(true);
  });

  it("does not retry a request the server rejected", async () => {
    const server = new FakeServer();
    const { connect } = setup(server);
    const first = connect();
    first.failWith = 404;
    const second = connect();
    first.send({ type: "attach", target });
    second.send({ type: "attach", target });
    await settle();

    // Both tabs would answer, but a server rejection must not fall through to
    // the other tab.
    const asked = [first, second].filter((tab) => tab.received.some((m) => m.type === "fetch"));
    expect(asked).toHaveLength(1);
  });

  it("frees an index a while after its last tab leaves", async () => {
    vi.useFakeTimers();
    const server = new FakeServer();
    server.apply({ kind: "issue", record: issueRecord(1, "Released") });
    const { connect } = setup(server, { releaseDelayMs: 1_000 });
    const tab = connect();
    tab.send({ type: "attach", target });
    await vi.advanceTimersByTimeAsync(10);
    expect(tab.last("serving")?.serving).toBe(true);

    tab.send({ type: "detach" });
    await vi.advanceTimersByTimeAsync(2_000);
    tab.send({ type: "attach", target });
    // A fresh index reloads from the store before it can serve again.
    expect(tab.last("serving")?.serving).toBe(false);
    await vi.advanceTimersByTimeAsync(10);
    expect(tab.last("serving")?.serving).toBe(true);
  });

  it("wipes every index on request", async () => {
    const server = new FakeServer();
    server.apply({ kind: "issue", record: issueRecord(1, "Secret") });
    const { connect, stores } = setup(server);
    const tab = connect();
    tab.send({ type: "attach", target });
    await settle();
    const store = stores.get("user:ws");

    tab.send({ type: "wipe", id: 3 });
    await settle();
    expect(tab.last("wiped")).toEqual({ type: "wiped", id: 3 });
    expect(store?.destroyed).toBe(true);
    expect(stores.size).toBe(0);

    tab.send({ type: "search", id: 4, kind: "issues", params: { q: "secret" } });
    await settle();
    expect(tab.last("search-result")?.result).toBeNull();
  });

  it("restores a tab the sweep dropped once it speaks again", async () => {
    vi.useFakeTimers();
    const server = new FakeServer();
    server.apply({ kind: "issue", record: issueRecord(1, "Needle") });
    const { connect } = setup(server, { portTimeoutMs: 1_000, releaseDelayMs: 1_000 });
    const tab = connect();
    tab.send({ type: "attach", target });
    await vi.advanceTimersByTimeAsync(10);
    expect(tab.last("serving")?.serving).toBe(true);

    // Frozen in the background: silent past the timeout, index released.
    await vi.advanceTimersByTimeAsync(4_000);
    tab.send({ type: "ping" });
    await vi.advanceTimersByTimeAsync(10);
    expect(tab.last("serving")).toEqual({ type: "serving", key: "user:ws", serving: true });

    tab.send({ type: "search", id: 77, kind: "issues", params: { q: "needle" } });
    await vi.advanceTimersByTimeAsync(10);
    expect(tab.last("search-result")).toMatchObject({ id: 77, result: { issues: [{ id: "issue-1" }] } });
  });

  it("does not restore a tab that said it was closing until it attaches again", async () => {
    const server = new FakeServer();
    server.apply({ kind: "issue", record: issueRecord(1, "Closing") });
    const { connect } = setup(server);
    const tab = connect();
    tab.send({ type: "attach", target });
    await settle();
    tab.send({ type: "close" });
    const servingBefore = tab.received.filter((m) => m.type === "serving").length;

    tab.send({ type: "ping" });
    await settle();
    expect(tab.received.filter((m) => m.type === "serving").length).toBe(servingBefore);

    // Restored from the back/forward cache: the client attaches again.
    tab.send({ type: "attach", target });
    await settle();
    expect(tab.last("serving")?.serving).toBe(true);
  });

  it("forgets every copy of a workspace, even while a tab shows it", async () => {
    const server = new FakeServer();
    server.apply({ kind: "issue", record: issueRecord(1, "Gone soon") });
    const { connect, stores } = setup(server);
    const tab = connect();
    tab.send({ type: "attach", target });
    await settle();
    const store = stores.get("user:ws")!;

    tab.send({ type: "forget", workspaceId: "ws" });
    await settle();
    expect(store.destroyed).toBe(true);
    expect(stores.has("user:ws")).toBe(false);
    expect(tab.last("serving")).toEqual({ type: "serving", key: "user:ws", serving: false });

    tab.send({ type: "search", id: 9, kind: "issues", params: { q: "gone" } });
    await settle();
    expect(tab.last("search-result")?.result).toBeNull();
  });

  it("prunes copies outside the user's workspaces but keeps ones a tab shows", async () => {
    const server = new FakeServer();
    const { connect, stores } = setup(server, { releaseDelayMs: 60_000 });
    const shown = connect();
    shown.send({ type: "attach", target: { userId: "user", workspaceId: "joined-just-now", workspaceSlug: "new" } });
    const left = connect();
    left.send({ type: "attach", target: { userId: "user", workspaceId: "left", workspaceSlug: "left" } });
    left.send({ type: "detach" });
    const kept = connect();
    kept.send({ type: "attach", target });
    kept.send({ type: "detach" });
    await settle();
    // Databases with no index in memory: another user's, and a workspace the
    // user is no longer in.
    stores.set("someone-else:ws", new MemoryIndexStore());
    stores.set("user:removed-offline", new MemoryIndexStore());

    shown.send({ type: "prune", userId: "user", workspaceIds: ["ws"] });
    await settle();

    expect([...stores.keys()].sort()).toEqual(["user:joined-just-now", "user:ws"]);
  });
});
