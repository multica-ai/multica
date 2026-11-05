// @vitest-environment node
import { reaction } from "mobx";
import { describe, expect, it, vi } from "vitest";
import { localV2SnapshotSchema, localV2SummarySchema, type LocalV2Transport } from "@multica/core/local-conversation/v2";
import { LOCAL_V2_FOCUS_STABLE_MS } from "@multica/core/local-conversation/focus";
import { ConversationService, localV2Error } from "./conversation-service";
import { ConversationServiceManager } from "./conversation-service-manager";

const scope = { userId: "u", workspaceId: "w" };
const at = "2026-10-29T00:00:00Z";
const summary = (id: string, seq: number, title = id) => localV2SummarySchema.parse({ conversation_id: id, summary_seq: seq, provider: "claude", title, status: "idle", pinned: false, archived: false, updated_at: at });

function transport(pages: unknown[] = []) {
  const sent: unknown[] = [];
  const t: LocalV2Transport = {
    request: vi.fn(async () => pages.shift() ?? { scope_seq: 0, summaries: [], has_more: false }),
    send: vi.fn(async (_scope, message) => { sent.push(message); }),
    subscribe: vi.fn(() => () => {}), onConnection: vi.fn(() => () => {}),
    fileUrl: (_scope, path) => `file:${path}`, upload: vi.fn(), choose: vi.fn(),
  };
  return { t, sent };
}

const snapshots = () => ({ read: async () => null, write: async () => {} });
const newStore = (t: LocalV2Transport, snapshots: never | null = null) => new ConversationService(t, scope, { deviceId: "local", snapshots, onGeneration: vi.fn(), onUsed: vi.fn() });

describe("ConversationService", () => {
  it("pages through a list longer than one response, whose scope_seq is already the latest", async () => {
    const { t } = transport([
      { scope_seq: 9, summaries: [summary("a", 1), summary("b", 2)], has_more: true },
      { scope_seq: 9, summaries: [summary("c", 9)], has_more: false },
    ]);
    const client = newStore(t);
    await client.loadSummaries();
    expect(vi.mocked(t.request).mock.calls[1]?.join(" ")).toContain("/sessions?after_scope_seq=2");
    expect(client.list.summaries.map((item) => item.conversation_id).sort()).toEqual(["a", "b", "c"]);
  });

  it("loads every summary page, then follows the scope stream", async () => {
    const { t, sent } = transport([
      { scope_seq: 2, summaries: [summary("a", 1), summary("b", 2)], has_more: true },
      { scope_seq: 3, summaries: [summary("c", 3)], has_more: false },
    ]);
    const client = newStore(t);
    const changed = vi.fn();
    const stop = reaction(() => client.list.summaries, changed);
    await client.loadSummaries();
    expect(client.list.summaries.map((s) => s.conversation_id)).toEqual(["a", "b", "c"]);
    expect(sent).toEqual([{ version: 2, type: "subscribe_scope", after_scope_seq: 3 }]);
    client.receive({ version: 2, type: "scope.summaries", scope_seq: 4, summaries: [summary("a", 4, "renamed")] });
    expect(client.list.summary("a")?.title).toBe("renamed");
    // An older summary arriving late never replaces a newer one.
    client.receiveSummary(summary("a", 1, "stale"));
    expect(client.list.summary("a")?.title).toBe("renamed");
    stop();
    // Each page, then the stream's change; the stale one changes nothing.
    expect(changed).toHaveBeenCalledTimes(3);
  });

  it("hands out the list without waiting for the scope subscription, and retries a failed one", async () => {
    const { t, sent } = transport([{ scope_seq: 1, summaries: [summary("a", 1)], has_more: false }]);
    vi.mocked(t.send).mockReturnValueOnce(new Promise(() => {}));
    const client = newStore(t);
    expect(client.list.read).toBe(false);
    await client.loadSummaries();
    expect(client.list.summaries.map((s) => s.conversation_id)).toEqual(["a"]);
    expect(client.list.read).toBe(true);

    const failing = transport([{ scope_seq: 1, summaries: [], has_more: false }]);
    vi.mocked(failing.t.send).mockRejectedValueOnce(new Error("socket down"));
    const retried = newStore(failing.t);
    await retried.loadSummaries();
    await vi.waitFor(async () => { await retried.loadSummaries(); expect(failing.sent).toHaveLength(1); });
    expect(sent).toEqual([]);
  });

  it("tells a daemon that supports it which conversation is in focus, and again after a reconnect", async () => {
    vi.useFakeTimers();
    try {
      const { t, sent } = transport();
      t.capabilities = vi.fn(async () => ["conversation-focus-v1"]);
      const client = newStore(t);
      const release = client.focus("c1");
      await vi.advanceTimersByTimeAsync(LOCAL_V2_FOCUS_STABLE_MS);
      expect(sent).toEqual([{ version: 2, type: "conversation.focus", conversation_id: "c1", focused: true }]);
      client.connection(true);
      await vi.advanceTimersByTimeAsync(0);
      expect(sent).toHaveLength(2);
      release();
      await vi.advanceTimersByTimeAsync(0);
      expect(sent[2]).toEqual({ version: 2, type: "conversation.focus", conversation_id: "c1", focused: false });
    } finally { vi.useRealTimers(); }
  });

  it("does not send focus to a daemon without the capability", async () => {
    vi.useFakeTimers();
    try {
      const { t, sent } = transport();
      t.capabilities = vi.fn(async () => ["device-link"]);
      const client = newStore(t);
      client.focus("c1");
      await vi.advanceTimersByTimeAsync(LOCAL_V2_FOCUS_STABLE_MS);
      expect(sent).toEqual([]);
    } finally { vi.useRealTimers(); }
  });

  it("keeps other devices' presence without touching the conversation's subscription", () => {
    const { t } = transport();
    const client = newStore(t);
    const listener = vi.fn(), changed = vi.fn();
    client.observe("c1", listener);
    const stop = reaction(() => client.list.presence("c1"), changed);
    const devices = [{ peer_id: "p1", device_name: "MacBook" }];
    client.receive({ version: 2, type: "scope.presence", conversation_id: "c1", devices });
    expect(client.list.presence("c1")).toEqual(devices);
    expect(listener).not.toHaveBeenCalled();
    client.receive({ version: 2, type: "scope.presence", conversation_id: "c1", devices: [] });
    expect(client.list.presence("c1")).toEqual([]);
    client.receive({ version: 2, type: "scope.presence", conversation_id: "c1", devices });
    client.connection(false);
    expect(client.list.presence("c1")).toEqual([]);
    stop();
    expect(changed).toHaveBeenCalledTimes(4);
  });

  it("keeps the daemon's error kind across IPC", () => {
    const error = localV2Error(new Error("Error invoking remote method 'local-v2:request': Error: turn_running: local conversation: a turn is already running"));
    expect(error.kind).toBe("turn_running");
    expect(error.message).toBe("local conversation: a turn is already running");
  });
});

const snapshotOf = (id: string, seq: number, fields: Record<string, unknown> = {}) => ({
  version: 2, store_generation: "g1", snapshot_seq: seq, conversation: summary(id, 1), turns: [], has_older_turns: false, pending_interactions: [],
  settings: { model: "m1", reasoning_effort: null, permission_mode: "default" }, queue: { revision: 1, paused: false, items: [] }, created_at: at, ...fields,
});
const turnOf = (id: string, index: number) => ({ id, turn_index: index, request_id: id, status: "completed", started_at: at, completed_at: at, messages: [], events: [] });
const event = (seq: number, type = "usage.updated", fields: Record<string, unknown> = {}) => ({ seq, turn_id: null, turn_seq: null, item_id: null, type, final: false, created_at: at, payload: {}, ...fields });
const events = (id: string, ...list: ReturnType<typeof event>[]) => ({ version: 2, type: "conversation.events", conversation_id: id, from_seq: list[0]!.seq, to_seq: list.at(-1)!.seq, events: list });
const status = (id: string, value: string, highWater?: number) => ({ version: 2, type: "subscription.status", conversation_id: id, status: value, ...(highWater === undefined ? {} : { high_water: highWater }) });

function daemon(routes: Record<string, unknown> = {}) {
  const sent: { type: string; conversation_id?: string; after_seq?: number }[] = [];
  const reads: string[] = [];
  const t: LocalV2Transport = {
    request: vi.fn(async (_scope, _method, path: string) => {
      reads.push(path);
      const key = path.split("?")[0]!;
      const value = routes[key];
      if (value === undefined) throw new Error(`not_found: ${path}`);
      return typeof value === "function" ? (value as () => unknown)() : value;
    }),
    send: vi.fn(async (_scope, message) => { sent.push(message as never); }),
    subscribe: vi.fn(() => () => {}), onConnection: vi.fn(() => () => {}),
    fileUrl: (_scope, path) => path, upload: vi.fn(), choose: vi.fn(), release: vi.fn(),
  };
  return { t, sent, reads, subscribes: () => sent.filter((message) => message.type === "subscribe"), unsubscribes: () => sent.filter((message) => message.type === "unsubscribe") };
}
const settle = () => new Promise((resolve) => setTimeout(resolve));

describe("ConversationService content", () => {
  it.each([4, 5, 6, 7, 8, 9, 10, 100])("fills the rendered first page up to min(%i, 10) without rereading its five turns", async (total) => {
    const count = Math.min(total, 5);
    const target = Math.min(total, 10);
    const start = total - count;
    const targetStart = total - target;
    const d = daemon({
      "/sessions/a/snapshot": snapshotOf("a", 9, {
        conversation: { ...summary("a", 1), turn_count: total },
        turns: Array.from({ length: count }, (_, index) => turnOf(`t${start + index}`, start + index)),
        has_older_turns: start > 0,
      }),
      "/sessions/a/history": { store_generation: "g1", has_older_turns: targetStart > 0,
        turns: Array.from({ length: target - count }, (_, index) => turnOf(`t${targetStart + index}`, targetStart + index)) },
    });
    const store = newStore(d.t);
    const leave = store.open("a");
    await store.ensure("a");
    const navigation = store.navigation("a");
    expect(d.reads).toEqual(["/sessions/a/snapshot?turn_limit=5"]);
    expect(store.conversation("a")!.turns.size).toBe(count);
    await navigation.completeInitialWindow();
    await navigation.completeInitialWindow();
    expect(d.reads.filter((path) => path.includes("history"))).toEqual(target > count
      ? [`/sessions/a/history?before_index=${start}&turn_limit=${target - count}`] : []);
    expect(store.conversation("a")!.turns.size).toBe(target);
    leave();
  });

  it("does not start the initial supplement after the conversation is left", async () => {
    const d = daemon({ "/sessions/a/snapshot": snapshotOf("a", 1, {
      conversation: { ...summary("a", 1), turn_count: 100 },
      turns: Array.from({ length: 5 }, (_, i) => turnOf(`t${i + 95}`, i + 95)), has_older_turns: true,
    }) });
    const store = newStore(d.t);
    const leave = store.open("a");
    await store.ensure("a");
    const navigation = store.navigation("a");
    leave();
    await navigation.completeInitialWindow();
    expect(d.reads).toHaveLength(1);
  });

  it("does not infer a supplement size when an older response omits the total count", async () => {
    const d = daemon({ "/sessions/a/snapshot": snapshotOf("a", 1, {
      turns: Array.from({ length: 5 }, (_, i) => turnOf(`t${i + 95}`, i + 95)), has_older_turns: true,
    }) });
    const store = newStore(d.t);
    const leave = store.open("a");
    await store.ensure("a");
    await store.navigation("a").completeInitialWindow();
    expect(d.reads).toEqual(["/sessions/a/snapshot?turn_limit=5"]);
    leave();
  });

  it("reads the first turns once and asks only for later events when reopened", async () => {
    const d = daemon({ "/sessions/a/snapshot": snapshotOf("a", 4) });
    const store = newStore(d.t);
    const leave = store.open("a");
    await settle();
    expect(d.reads).toEqual(["/sessions/a/snapshot?turn_limit=5"]);
    expect(d.subscribes().at(-1)).toMatchObject({ conversation_id: "a", after_seq: 4 });
    store.receive(status("a", "subscribed", 4));
    store.receive(events("a", event(5)));
    leave();
    expect(d.unsubscribes()).toHaveLength(1);

    store.open("a");
    await settle();
    expect(d.reads).toHaveLength(1);
    expect(d.subscribes().at(-1)).toMatchObject({ after_seq: 5 });
  });

  it("finishes the batch in progress after the view leaves, then stops", async () => {
    const d = daemon({ "/sessions/a/snapshot": snapshotOf("a", 4) });
    const store = newStore(d.t);
    const leave = store.open("a");
    await settle();
    store.receive(status("a", "subscribed", 7));
    store.receive(events("a", event(5)));
    leave();
    expect(d.unsubscribes()).toHaveLength(0);
    store.receive(events("a", event(6), event(7)));
    expect(store.conversation("a")?.seq).toBe(7);
    expect(d.unsubscribes()).toHaveLength(1);
  });

  it("keeps a read that finishes after the view left, without subscribing", async () => {
    let answer!: (value: unknown) => void;
    const d = daemon({ "/sessions/a/snapshot": () => new Promise((resolve) => { answer = resolve; }) });
    const store = newStore(d.t);
    store.open("a")();
    await settle();
    answer(snapshotOf("a", 4));
    await settle();
    expect(store.conversation("a")?.seq).toBe(4);
    expect(d.subscribes()).toHaveLength(0);
  });

  it("shares one conversation and subscription among views, and leaves with the last", async () => {
    const d = daemon({ "/sessions/a/snapshot": snapshotOf("a", 4) });
    const store = newStore(d.t);
    const first = store.open("a"), second = store.open("a");
    await settle();
    store.receive(status("a", "subscribed", 4));
    expect(d.subscribes()).toHaveLength(1);
    first();
    first();
    expect(d.unsubscribes()).toHaveLength(0);
    second();
    expect(d.unsubscribes()).toHaveLength(1);
  });

  it("notifies observers without observing affecting the network", async () => {
    const d = daemon({ "/sessions/a/snapshot": snapshotOf("a", 4) });
    const store = newStore(d.t);
    const seen = vi.fn();
    store.observe("a", seen);
    expect(d.reads).toEqual([]);
    store.open("a");
    await settle();
    store.receive(status("a", "subscribed", 4));
    store.receive(events("a", event(5)));
    expect(seen).toHaveBeenLastCalledWith([expect.objectContaining({ seq: 5 })]);
  });

  it("drops events from a cancelled subscription until the new one answers", async () => {
    const d = daemon({ "/sessions/a/snapshot": snapshotOf("a", 4) });
    const store = newStore(d.t);
    store.open("a");
    await settle();
    store.receive(events("a", event(9)));
    expect(store.conversation("a")?.seq).toBe(4);
    expect(d.reads).toHaveLength(1);
  });

  it("continues from what is applied on a gap and only reads a snapshot if that keeps failing", async () => {
    const d = daemon({ "/sessions/a/snapshot": snapshotOf("a", 4) });
    const store = newStore(d.t);
    store.open("a");
    await settle();
    for (let attempt = 0; attempt < 2; attempt++) {
      store.receive(status("a", "subscribed", 4));
      store.receive(events("a", event(9)));
      expect(d.subscribes().at(-1)).toMatchObject({ after_seq: 4 });
      expect(d.reads).toHaveLength(1);
    }
    store.receive(status("a", "subscribed", 4));
    store.receive(events("a", event(9)));
    await settle();
    expect(d.reads).toHaveLength(2);
  });

  it("resumes from the cursor on resubscribe, and rejected waits for a retry instead of looping", async () => {
    const d = daemon({ "/sessions/a/snapshot": snapshotOf("a", 4) });
    const store = newStore(d.t);
    store.open("a");
    await settle();
    store.receive(status("a", "subscribed", 4));
    store.receive(status("a", "resubscribe"));
    expect(d.subscribes()).toHaveLength(2);
    expect(d.reads).toHaveLength(1);
    store.receive({ ...status("a", "rejected"), error: { kind: "subscription_limit", message: "too many" } });
    await settle();
    expect(store.status("a").error?.message).toBe("too many");
    expect(d.subscribes()).toHaveLength(2);
    expect(d.reads).toHaveLength(1);
  });

  it("reads the items of a queue change once, and keeps the snapshot's settings until they change", async () => {
    let revision = 2;
    const d = daemon({ "/sessions/a/snapshot": snapshotOf("a", 4), "/sessions/a/queue": () => ({ revision, paused: false, items: [{ request_id: "r" }] }) });
    const store = newStore(d.t);
    store.open("a");
    await settle();
    store.receive(status("a", "subscribed", 4));
    expect(store.conversation("a")?.settings.model).toBe("m1");
    store.receive(events("a", event(5, "queue.updated", { payload: { revision: 2, paused: false, count: 1 } })));
    store.receive(events("a", event(6, "conversation.updated", { payload: { model: "m2" } })));
    await settle();
    expect(store.conversation("a")?.queue.items).toEqual([{ request_id: "r" }]);
    expect(store.conversation("a")?.settings.model).toBe("m2");
    expect(d.reads.filter((path) => path.endsWith("/queue"))).toHaveLength(1);
    revision = 3;
  });

  it("pages older turns once at a time and never moves the cursor", async () => {
    const d = daemon({
      "/sessions/a/snapshot": snapshotOf("a", 9, { turns: [turnOf("t5", 5)], has_older_turns: true }),
      "/sessions/a/history": { store_generation: "g1", turns: [turnOf("t3", 3), turnOf("t4", 4)], has_older_turns: true },
    });
    const store = newStore(d.t);
    await store.ensure("a");
    await Promise.all([store.loadOlder("a"), store.loadOlder("a")]);
    expect(d.reads.filter((path) => path.includes("history"))).toEqual(["/sessions/a/history?before_index=5&turn_limit=10"]);
    const conversation = store.conversation("a")!;
    expect([...conversation.turns.keys()]).toEqual(["t3", "t4", "t5"]);
    expect(conversation.seq).toBe(9);
    expect(conversation.hasOlderTurns).toBe(true);
  });

  it("drops a page that arrives after its conversation was evicted", async () => {
    let answer!: (value: unknown) => void;
    const d = daemon({
      "/sessions/a/snapshot": snapshotOf("a", 9, { turns: [turnOf("t5", 5)], has_older_turns: true }),
      "/sessions/a/history": () => new Promise((resolve) => { answer = resolve; }),
    });
    const store = newStore(d.t);
    await store.ensure("a");
    const page = store.loadOlder("a");
    await settle();
    store.evict("a");
    await store.ensure("a");
    answer({ store_generation: "g1", turns: [turnOf("t4", 4)], has_older_turns: false });
    await page;
    expect([...store.conversation("a")!.turns.keys()]).toEqual(["t5"]);
  });

  it("shows another device's stored first screen before the current one arrives", async () => {
    let answer!: (value: unknown) => void;
    const d = daemon({ "/sessions/a/snapshot": () => new Promise((resolve) => { answer = resolve; }) });
    const snapshots = { read: vi.fn(async () => localV2SnapshotSchema.parse(snapshotOf("a", 2))), write: vi.fn(async () => {}) };
    const store = new ConversationService(d.t, scope, { deviceId: "peer", snapshots, onGeneration: vi.fn(), onUsed: vi.fn() });
    const loading = store.ensure("a");
    await settle();
    expect(store.conversation("a")?.seq).toBe(2);
    expect(d.reads).toEqual(["/sessions/a/snapshot?turn_limit=5"]);
    answer(snapshotOf("a", 8));
    await loading;
    expect(store.conversation("a")?.seq).toBe(8);
    expect(snapshots.write).toHaveBeenCalledWith(JSON.stringify(["peer", "u", "w", "a"]), expect.objectContaining({ snapshot_seq: 8 }));
  });

  it("recovers the list from the cursor after a reconnect, retrying while it fails", async () => {
    vi.useFakeTimers();
    try {
      const d = daemon({ "/sessions": { scope_seq: 3, summaries: [summary("a", 3)], has_more: false } });
      const store = newStore(d.t);
      await store.loadSummaries();
      vi.mocked(d.t.request).mockRejectedValueOnce(new Error("device_unreachable: offline"));
      store.connection(true);
      await vi.advanceTimersByTimeAsync(0);
      expect(d.reads).toEqual(["/sessions?after_scope_seq=0"]);
      await vi.advanceTimersByTimeAsync(1_000);
      expect(d.reads).toEqual(["/sessions?after_scope_seq=0", "/sessions?after_scope_seq=3"]);
    } finally { vi.useRealTimers(); }
  });

  it("does nothing after dispose, and releases its connection", async () => {
    const d = daemon({ "/sessions/a/snapshot": snapshotOf("a", 4) });
    const store = newStore(d.t);
    store.open("a");
    await settle();
    store.dispose();
    expect(d.t.release).toHaveBeenCalledWith(scope);
    const before = d.sent.length;
    store.connection(true);
    store.receive(status("a", "subscribed", 4));
    expect(d.sent).toHaveLength(before);
  });
});

describe("ConversationServiceManager", () => {
  it("keeps one store per device and workspace", () => {
    const manager = new ConversationServiceManager({ snapshots });
    const a = daemon().t, b = daemon().t;
    expect(manager.get(a, scope)).toBe(manager.get(a, scope));
    expect(manager.get(a, scope)).not.toBe(manager.get(b, scope));
    expect(manager.get(a, scope)).not.toBe(manager.get(a, { ...scope, workspaceId: "other" }));
  });

  it("evicts only content that is not in use, oldest first, across stores", async () => {
    const manager = new ConversationServiceManager({ snapshots, limit: 2 });
    const d = daemon({ "/sessions/a/snapshot": snapshotOf("a", 1), "/sessions/b/snapshot": snapshotOf("b", 1), "/sessions/c/snapshot": snapshotOf("c", 1), "/sessions": { scope_seq: 1, summaries: [summary("a", 1)], has_more: false } });
    const store = manager.get(d.t, scope);
    await store.loadSummaries();
    const held = store.open("a");
    await settle();
    await store.ensure("b");
    await store.ensure("c");
    await settle();
    expect(store.conversation("a")).toBeDefined();
    expect(store.conversation("b")).toBeUndefined();
    expect(store.conversation("c")).toBeDefined();
    expect(store.list.summary("a")).toBeDefined();
    held();
  });

  it("invalidates every store of the device when its database is replaced, and no other device", async () => {
    const manager = new ConversationServiceManager({ snapshots });
    const first = daemon({ "/sessions/a/snapshot": snapshotOf("a", 4), "/sessions": { scope_seq: 1, summaries: [], has_more: false } });
    const other = daemon({ "/sessions/a/snapshot": snapshotOf("a", 4) });
    const here = manager.get(first.t, scope), sibling = manager.get(first.t, { ...scope, workspaceId: "other" }), elsewhere = manager.get(other.t, scope);
    await here.ensure("a");
    await elsewhere.ensure("a");
    vi.mocked(first.t.request).mockImplementation(async (_scope, _method, path: string) => path.includes("snapshot") ? snapshotOf("a", 1, { store_generation: "g2" }) : { scope_seq: 1, summaries: [], has_more: false });
    await sibling.ensure("a");
    await sibling.refresh("a");
    await settle();
    expect(elsewhere.conversation("a")).toBeDefined();
    expect(here.conversation("a")).toBeUndefined();
  });

  it("disposes everything on sign-out", () => {
    const manager = new ConversationServiceManager({ snapshots });
    const d = daemon();
    const store = manager.get(d.t, scope);
    manager.disposeAll();
    expect(d.t.release).toHaveBeenCalledWith(scope);
    expect(manager.get(d.t, scope)).not.toBe(store);
  });
});
