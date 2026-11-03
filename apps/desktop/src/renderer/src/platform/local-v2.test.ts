// @vitest-environment node
import { describe, expect, it, vi } from "vitest";
import { localV2SummarySchema, type LocalV2Transport } from "@multica/core/local-conversation/v2";
import { LOCAL_V2_FOCUS_STABLE_MS } from "@multica/core/local-conversation/focus";
import { LocalV2ScopeClient, localV2Error } from "./local-v2";

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

describe("LocalV2ScopeClient", () => {
  it("loads every summary page, then follows the scope stream", async () => {
    const { t, sent } = transport([
      { scope_seq: 2, summaries: [summary("a", 1), summary("b", 2)], has_more: true },
      { scope_seq: 3, summaries: [summary("c", 3)], has_more: false },
    ]);
    const client = new LocalV2ScopeClient(t, scope);
    const changed = vi.fn();
    client.onSummaries(changed);
    await client.loadSummaries();
    expect(client.summaries().map((s) => s.conversation_id)).toEqual(["a", "b", "c"]);
    expect(sent).toEqual([{ version: 2, type: "subscribe_scope", after_scope_seq: 3 }]);
    client.receive({ version: 2, type: "scope.summaries", scope_seq: 4, summaries: [summary("a", 4, "renamed")] });
    expect(client.summary("a")?.title).toBe("renamed");
    // An older summary arriving late never replaces a newer one.
    client.receiveSummary(summary("a", 1, "stale"));
    expect(client.summary("a")?.title).toBe("renamed");
    expect(changed).toHaveBeenCalledTimes(2);
  });

  it("tells a daemon that supports it which conversation is in focus, and again after a reconnect", async () => {
    vi.useFakeTimers();
    try {
      const { t, sent } = transport();
      t.capabilities = vi.fn(async () => ["conversation-focus-v1"]);
      const client = new LocalV2ScopeClient(t, scope);
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
      const client = new LocalV2ScopeClient(t, scope);
      client.focus("c1");
      await vi.advanceTimersByTimeAsync(LOCAL_V2_FOCUS_STABLE_MS);
      expect(sent).toEqual([]);
    } finally { vi.useRealTimers(); }
  });

  it("shares one subscription per conversation and reloads late joiners", async () => {
    const { t, sent } = transport();
    const client = new LocalV2ScopeClient(t, scope);
    const first = vi.fn(), second = vi.fn();
    const stopFirst = client.subscribe("c1", 3, "g", first);
    client.receive({ version: 2, type: "conversation.events", conversation_id: "c1", from_seq: 4, to_seq: 5, events: [
      { seq: 4, type: "turn.started", final: false, created_at: at, payload: {} },
      { seq: 5, type: "turn.completed", final: true, created_at: at, payload: {} },
    ] });
    expect(first).toHaveBeenCalledTimes(1);
    const stopSecond = client.subscribe("c1", 3, "g", second);
    await Promise.resolve();
    expect(second).toHaveBeenCalledWith(expect.objectContaining({ status: "resubscribe" }));
    stopFirst(); stopSecond();
    expect(sent.filter((m: any) => m.type === "subscribe")).toHaveLength(1);
    expect(sent.at(-1)).toEqual({ version: 2, type: "unsubscribe", conversation_id: "c1" });
  });

  it("keeps other devices' presence without touching the conversation's subscription", () => {
    const { t } = transport();
    const client = new LocalV2ScopeClient(t, scope);
    const listener = vi.fn(), changed = vi.fn();
    client.subscribe("c1", 0, "g", listener);
    client.onPresence(changed);
    const devices = [{ peer_id: "p1", device_name: "MacBook" }];
    client.receive({ version: 2, type: "scope.presence", conversation_id: "c1", devices });
    expect(client.presence("c1")).toEqual(devices);
    expect(listener).not.toHaveBeenCalled();
    client.receive({ version: 2, type: "scope.presence", conversation_id: "c1", devices: [] });
    expect(client.presence("c1")).toEqual([]);
    client.receive({ version: 2, type: "scope.presence", conversation_id: "c1", devices });
    client.connection(false);
    expect(client.presence("c1")).toEqual([]);
    expect(changed).toHaveBeenCalledTimes(4);
  });

  it("tells listeners to reload once the connection is back", () => {
    const { t } = transport();
    const client = new LocalV2ScopeClient(t, scope);
    const listener = vi.fn(), closed = vi.fn();
    client.subscribe("c1", 0, "g", listener);
    const stopClosed = client.subscribe("c2", 0, "g", closed);
    client.connection(false);
    // A reload while the daemon is down would fail and not be retried.
    expect(listener).not.toHaveBeenCalled();
    stopClosed();
    client.connection(true);
    expect(listener).toHaveBeenCalledExactlyOnceWith(expect.objectContaining({ conversation_id: "c1", status: "resubscribe" }));
    expect(closed).not.toHaveBeenCalled();
  });

  it("keeps the daemon's error kind across IPC", () => {
    const error = localV2Error(new Error("Error invoking remote method 'local-v2:request': Error: turn_running: local conversation: a turn is already running"));
    expect(error.kind).toBe("turn_running");
    expect(error.message).toBe("local conversation: a turn is already running");
  });
});
