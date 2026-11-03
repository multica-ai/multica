// @vitest-environment node
import { afterEach, describe, expect, it, vi } from "vitest";
import { QueryClient } from "@tanstack/react-query";
import { LOCAL_V2_FOCUS_STABLE_MS } from "@multica/core/local-conversation/focus";
import { localV2EventSchema, localV2SnapshotSchema, localV2SummarySchema } from "@multica/core/local-conversation/v2";
import { AISessionClient, aiKeys } from "./client";
import { applyEvents, groupSessions, transcript, visibleSessions } from "./projection";
import { commandResultSchema } from "./schemas";
import { MobileTimingBuffer } from "./timing";

const scope = { userId: "user-a", workspaceId: "workspace-a", deviceId: "peer-a" };
const summary = (id = "session-a", seq = 1) => localV2SummarySchema.parse({ conversation_id: id, summary_seq: seq, provider: "codex", title: id, status: "idle", pinned: false, archived: false, updated_at: "2026-10-30T12:00:00Z" });
const snapshot = () => localV2SnapshotSchema.parse({ version: 2, store_generation: "generation-a", snapshot_seq: 0, conversation: summary(), turns: [] });
const event = (seq: number, type: string, payload: Record<string, unknown>, item: string | null = null) => localV2EventSchema.parse({ seq, type, payload, turn_id: "turn-a", item_id: item, final: type.endsWith("completed"), created_at: "2026-10-30T12:00:00Z" });
const unusedSocket = () => { throw new Error("Unexpected socket in a cache test"); };
const hello = () => Response.json({ version: 2, store_generation: "generation-a", scope_seq: 0, capabilities: ["device-link"] });

class FakeSocket {
  readyState = 0;
  sent: Record<string, unknown>[] = [];
  onopen: (() => void) | null = null;
  onclose: (() => void) | null = null;
  onerror: (() => void) | null = null;
  onmessage: ((event: { data: string }) => void) | null = null;
  send(data: string) { this.sent.push(JSON.parse(data)); }
  close() { this.readyState = 3; this.onclose?.(); }
  open() { this.readyState = 1; this.onopen?.(); }
  subscriptions() { return this.sent.filter((message) => message.type === "subscribe"); }
}

/** A client with a socket per dial, and a fetcher that only needs to answer non-hello requests. */
function linkedClient(read: typeof fetch = vi.fn<typeof fetch>(async () => Response.json({}))) {
  const sockets: FakeSocket[] = [];
  const qc = new QueryClient();
  const fetcher = vi.fn<typeof fetch>(async (input, init) => String(input).includes("/hello") ? hello() : read(input, init));
  const factory = () => { const socket = new FakeSocket(); sockets.push(socket); return socket as unknown as WebSocket; };
  const client = new AISessionClient({ url: "http://127.0.0.1:1234", token: "loopback" }, scope, qc, factory, fetcher);
  const flush = () => vi.advanceTimersByTimeAsync(0);
  return { client, qc, sockets, fetcher, flush };
}

afterEach(() => vi.useRealTimers());

describe("mobile AI session focus", () => {
  const focusHello = () => Response.json({ version: 2, store_generation: "generation-a", scope_seq: 0, capabilities: ["device-link", "conversation-focus-v1"] });
  const focusMessages = (socket: FakeSocket) => socket.sent.filter((message) => message.type === "conversation.focus");

  function focusClient(respond = focusHello) {
    const linked = linkedClient();
    linked.fetcher.mockImplementation(async () => respond());
    return linked;
  }

  it("reports a stable session screen, its end, and the current focus again after a reconnect", async () => {
    vi.useFakeTimers();
    const { client, sockets, flush } = focusClient();
    void client.connect();
    await flush();
    sockets[0]!.open();
    const leave = client.focus("session-a");
    await vi.advanceTimersByTimeAsync(LOCAL_V2_FOCUS_STABLE_MS);
    expect(focusMessages(sockets[0]!)).toEqual([{ version: 2, type: "conversation.focus", conversation_id: "session-a", focused: true }]);

    sockets[0]!.close();
    await flush();
    sockets[1]!.open();
    expect(focusMessages(sockets[1]!)).toEqual([{ version: 2, type: "conversation.focus", conversation_id: "session-a", focused: true }]);

    leave();
    await flush();
    expect(focusMessages(sockets[1]!)).toEqual([
      { version: 2, type: "conversation.focus", conversation_id: "session-a", focused: true },
      { version: 2, type: "conversation.focus", conversation_id: "session-a", focused: false },
    ]);
    client.close();
  });

  it("says it left when a session was closed during a drop", async () => {
    vi.useFakeTimers();
    const { client, sockets, flush } = focusClient();
    void client.connect();
    await flush();
    sockets[0]!.open();
    const leave = client.focus("session-a");
    await vi.advanceTimersByTimeAsync(LOCAL_V2_FOCUS_STABLE_MS);
    sockets[0]!.readyState = 3;
    sockets[0]!.onclose?.();
    leave();
    await flush();
    sockets[1]!.open();
    await flush();
    expect(focusMessages(sockets[1]!)).toEqual([{ version: 2, type: "conversation.focus", conversation_id: "session-a", focused: false }]);
    client.close();
  });

  it("does not send focus to a daemon without the capability", async () => {
    vi.useFakeTimers();
    const { client, sockets, flush } = focusClient(hello);
    void client.connect();
    await flush();
    sockets[0]!.open();
    client.focus("session-a");
    await vi.advanceTimersByTimeAsync(LOCAL_V2_FOCUS_STABLE_MS);
    expect(focusMessages(sockets[0]!)).toEqual([]);
    client.close();
  });
});

describe("mobile AI session event projection", () => {
  it("streams text once, accepts authoritative final text, and ignores duplicate events", () => {
    const initial = snapshot();
    const events = [event(1, "turn.started", { request_id: "send-a" }), event(2, "user.message", { message_id: "user-message", content: "hello" }),
      event(3, "item.started", { kind: "assistant_text" }, "answer"), event(4, "item.delta", { kind: "assistant_text", field: "text", text: "Hel" }, "answer"),
      event(5, "item.delta", { kind: "assistant_text", field: "text", text: "lo" }, "answer")];
    const streamed = applyEvents(initial, events)!;
    expect(transcript(streamed).map((row) => row.content)).toEqual(["hello", "Hello"]);
    expect(initial.snapshot_seq).toBe(0);
    const final = applyEvents(streamed, [events[4], event(6, "item.completed", { kind: "assistant_text", text: "Hello!" }, "answer"), event(7, "turn.completed", { status: "completed" })])!;
    expect(transcript(final).map((row) => row.content)).toEqual(["hello", "Hello!"]);
    expect(final.turns[0].status).toBe("completed");
  });

  it("presents reasoning as thinking while it runs, including before any text", () => {
    const started = applyEvents(snapshot(), [event(1, "turn.started", {}), event(2, "item.started", { kind: "reasoning" }, "think")])!;
    expect(transcript(started)).toEqual([{ id: "think", role: "reasoning", content: "", running: true, turnId: "turn-a" }]);
    const streaming = applyEvents(started, [event(3, "item.delta", { kind: "reasoning", field: "text", text: "Reading " }, "think"), event(4, "item.delta", { kind: "reasoning", field: "text", text: "files" }, "think")])!;
    expect(transcript(streaming)[0]).toMatchObject({ role: "reasoning", content: "Reading files", running: true });
    const done = applyEvents(streaming, [event(5, "item.completed", { kind: "reasoning", text: "Reading the files" }, "think")])!;
    expect(transcript(done)[0]).toMatchObject({ role: "reasoning", content: "Reading the files", running: false });
  });

  it("keeps streamed reasoning when completion has no text and drops empty reasoning that ended", () => {
    const streamed = applyEvents(snapshot(), [event(1, "turn.started", {}), event(2, "item.started", { kind: "reasoning" }, "think"), event(3, "item.delta", { kind: "reasoning", field: "text", text: "Plan" }, "think"), event(4, "item.completed", { kind: "reasoning" }, "think")])!;
    expect(transcript(streamed)[0]).toMatchObject({ content: "Plan", running: false });
    const empty = applyEvents(snapshot(), [event(1, "turn.started", {}), event(2, "item.started", { kind: "reasoning" }, "think"), event(3, "item.completed", { kind: "reasoning" }, "think")])!;
    expect(transcript(empty)).toEqual([]);
    const cancelled = applyEvents(snapshot(), [event(1, "turn.started", {}), event(2, "item.started", { kind: "reasoning" }, "think"), event(3, "turn.completed", { status: "cancelled" })])!;
    expect(transcript(cancelled)).toEqual([]);
  });

  it("requests a fresh snapshot on a sequence gap and keeps the old snapshot unchanged", () => {
    const initial = snapshot();
    expect(applyEvents(initial, [event(2, "turn.started", {})])).toBeNull();
    expect(initial.turns).toEqual([]);
  });

  it("does not duplicate persisted assistant rows and execution text", () => {
    const value = applyEvents(snapshot(), [event(1, "item.completed", { kind: "assistant_text", text: "Answer" }, "answer")])!;
    value.turns[0].messages.push({ id: "persisted", role: "assistant", content: "Answer", attachment_ids: [], steered: false, failed: false, created_at: "now" });
    expect(transcript(value).filter((row) => row.role === "assistant")).toHaveLength(1);
  });

  it("removes answered questions without deleting transcript rows", () => {
    const pending = applyEvents(snapshot(), [event(1, "interaction.requested", { interaction_id: "question-a", kind: "question", request: { questions: [] } })])!;
    expect(pending.pending_interactions).toHaveLength(1);
    expect(applyEvents(pending, [event(2, "interaction.resolved", { interaction_id: "question-a" })])!.pending_interactions).toEqual([]);
  });

  it("applies metadata and model changes made from another device", () => {
    const updated = applyEvents(snapshot(), [event(1, "conversation.updated", { title: "Renamed", archived: true, model: "model-b" })])!;
    expect(updated.conversation).toMatchObject({ title: "Renamed", archived: true });
    expect(updated.settings.model).toBe("model-b");
  });

  it("filters archived and searched sessions while keeping pinned sessions first", () => {
    const pinned = { ...summary("pinned"), pinned: true };
    const archived = { ...summary("old"), archived: true };
    expect(visibleSessions([summary(), archived, pinned], "", false).map((s) => s.conversation_id)).toEqual(["pinned", "session-a"]);
    expect(visibleSessions([summary(), archived], "old", true)).toEqual([archived]);
  });

  it("groups by the computer's working directory rather than cloud project associations", () => {
    const session = { ...summary(), cwd: "/work/repo/", project_id: "cloud-project" };
    const groups = groupSessions([session], [{ id: "folder-project", title: "Repository", path: "/work/repo" }], "Repository", false, "Other");
    expect(groups).toHaveLength(1);
    expect(groups[0].id).toBe("folder-project");
    expect(groups[0].data).toEqual([session]);
    expect(groupSessions([], [{ id: "folder-project", title: "Repository", path: "/work/repo" }], "", false, "Other")).toHaveLength(2);
  });
});

describe("mobile AI session transport and caches", () => {
  it("partitions caches by account, workspace and device", () => {
    const base = aiKeys.snapshot(scope, "same-session");
    for (const changed of [{ userId: "b" }, { workspaceId: "b" }, { deviceId: "b" }]) expect(aiKeys.snapshot({ ...scope, ...changed }, "same-session")).not.toEqual(base);
  });

  it("starts scope updates from the current checkpoint without fetching the directory", async () => {
    const { client, sockets, fetcher } = linkedClient();
    fetcher.mockResolvedValue(Response.json({ version: 2, store_generation: "generation-a", scope_seq: 900, capabilities: ["device-link"] }));
    await client.connect(); sockets[0].open();
    expect(fetcher).toHaveBeenCalledOnce();
    expect(String(fetcher.mock.calls[0][0])).toContain("/hello?scoped=true");
    expect(sockets[0].sent).toContainEqual({ version: 2, type: "subscribe_scope", after_scope_seq: 900 });
    expect(fetcher.mock.calls[0][1]?.headers).toMatchObject({ Authorization: "Bearer loopback", "X-Workspace-ID": "workspace-a" });
    client.close();
  });

  it("requires a paginated-capable daemon instead of replaying an old full directory", async () => {
    const { client, sockets, fetcher } = linkedClient();
    fetcher.mockResolvedValue(Response.json({ version: 2, store_generation: "old", capabilities: ["device-link"] }));
    await client.connect();
    expect(sockets).toHaveLength(0);
    expect(client.connectionError).toBe("incompatible");
    expect(fetcher).toHaveBeenCalledOnce();
    client.close();
  });

  it("coalesces scope updates into paged-list invalidation", async () => {
    vi.useFakeTimers();
    const { client, qc } = linkedClient();
    const key = [...aiKeys.browse(scope), "groups", false, ""];
    qc.setQueryData(key, { scope_seq: 1, groups: [] });
    const update = { version: 2, type: "scope.summaries", scope_seq: 2, summaries: [summary("new", 2)] };
    client.receive(update); client.receive(update);
    expect(qc.getQueryState(key)?.isInvalidated).toBe(false);
    await vi.advanceTimersByTimeAsync(200);
    expect(qc.getQueryState(key)?.isInvalidated).toBe(true);
    expect(qc.getQueriesData({ queryKey: aiKeys.scope(scope) })).toHaveLength(1);
    client.close(); qc.clear();
  });

  it("rejects malformed successes and forwards caller cancellation", async () => {
    const controller = new AbortController(); controller.abort();
    const fetcher = vi.fn<typeof fetch>().mockResolvedValue(Response.json({ version: 2 }));
    const client = new AISessionClient({ url: "http://127.0.0.1:1234", token: "loopback" }, scope, new QueryClient(), unusedSocket, fetcher);
    const warning = vi.spyOn(console, "warn").mockImplementation(() => {});
    await expect(client.snapshot("session-a", controller.signal)).rejects.toThrow("invalid_response");
    expect(fetcher.mock.calls[0][1]?.signal?.aborted).toBe(true);
    warning.mockRestore(); client.close();
  });

  it("ignores stale summary echoes and applies events only to the matching device cache", () => {
    const qc = new QueryClient();
    const client = new AISessionClient({ url: "http://127.0.0.1:1234", token: "loopback" }, scope, qc, unusedSocket);
    qc.setQueryData(aiKeys.snapshot(scope, "session-a"), { ...snapshot(), conversation: summary("session-a", 3) });
    const other = { ...scope, deviceId: "other-peer" };
    qc.setQueryData(aiKeys.snapshot(other, "session-a"), snapshot());
    client.receive({ version: 2, type: "scope.summaries", scope_seq: 1, summaries: [summary("session-a", 1)] });
    expect(qc.getQueryData(aiKeys.snapshot(scope, "session-a"))).toMatchObject({ conversation: summary("session-a", 3) });
    client.receive({ version: 2, type: "conversation.events", conversation_id: "session-a", from_seq: 1, to_seq: 1, events: [event(1, "turn.started", {})] });
    expect(qc.getQueryData(aiKeys.snapshot(other, "session-a"))).toEqual(snapshot());
    expect(qc.getQueryData(aiKeys.snapshot(scope, "session-a"))).toMatchObject({ snapshot_seq: 1 });
    client.close(); qc.clear();
  });

  it("reloads and resubscribes after a gap without overwriting cache after close", async () => {
    let resolve!: (res: Response) => void;
    const fetcher = vi.fn<typeof fetch>(() => new Promise((done) => { resolve = done; }));
    const qc = new QueryClient();
    qc.setQueryData(aiKeys.snapshot(scope, "session-a"), snapshot());
    const client = new AISessionClient({ url: "http://127.0.0.1:1234", token: "loopback" }, scope, qc, unusedSocket, fetcher);
    client.receive({ version: 2, type: "conversation.events", conversation_id: "session-a", from_seq: 5, to_seq: 5, events: [event(5, "turn.started", {})] });
    expect(fetcher).toHaveBeenCalledOnce();
    client.close(); resolve(Response.json({ ...snapshot(), snapshot_seq: 10 }));
    await new Promise((done) => setTimeout(done, 0));
    expect(qc.getQueryData(aiKeys.snapshot(scope, "session-a"))).toMatchObject({ snapshot_seq: 0 });
    qc.clear();
  });

  describe("reconnecting", () => {
    it("never holds two sockets while the handshake request is in flight", async () => {
      vi.useFakeTimers();
      const { client, sockets, flush } = linkedClient();
      void client.connect(); void client.connect();
      await flush();
      expect(sockets).toHaveLength(1);
      client.close();
    });

    it("retries a dropped socket immediately and shows nothing when it recovers within the window", async () => {
      vi.useFakeTimers();
      const { client, sockets, flush } = linkedClient();
      const statuses: boolean[] = [];
      client.onConnection(() => statuses.push(client.connected));
      await client.connect(); sockets[0].open();
      statuses.length = 0;
      sockets[0].close();
      await flush();
      expect(sockets).toHaveLength(2);
      expect(client.connected).toBe(true);
      sockets[1].open();
      await vi.advanceTimersByTimeAsync(5000);
      expect(statuses).not.toContain(false);
      client.close();
    });

    it("shows disconnected once the window passes without a new socket", async () => {
      vi.useFakeTimers();
      const { client, sockets, flush } = linkedClient();
      await client.connect(); sockets[0].open();
      sockets[0].close();
      await flush();
      await vi.advanceTimersByTimeAsync(2400);
      expect(client.connected).toBe(true);
      await vi.advanceTimersByTimeAsync(200);
      expect(client.connected).toBe(false);
      sockets[1].open();
      expect(client.connected).toBe(true);
      client.close();
    });

    it("backs off instead of looping when a device drops every new socket", async () => {
      vi.useFakeTimers();
      const { client, sockets, flush } = linkedClient();
      await client.connect(); sockets[0].open(); sockets[0].close();
      await flush();
      sockets[1].open(); sockets[1].close();
      await flush();
      expect(sockets).toHaveLength(2);
      await vi.advanceTimersByTimeAsync(500);
      expect(sockets).toHaveLength(3);
      client.close();
    });

    it("subscribes from the cached snapshot without reading it again", async () => {
      vi.useFakeTimers();
      const { client, qc, sockets, fetcher, flush } = linkedClient();
      qc.setQueryData(aiKeys.snapshot(scope, "session-a"), { ...snapshot(), snapshot_seq: 7 });
      await client.connect(); sockets[0].open();
      client.select("session-a");
      sockets[0].close();
      await flush();
      sockets[1].open();
      await flush();
      expect(sockets[1].subscriptions()).toEqual([{ version: 2, type: "subscribe", conversation_id: "session-a", after_seq: 7, store_generation: "generation-a" }]);
      expect(fetcher.mock.calls.filter(([url]) => String(url).includes("/snapshot"))).toEqual([]);
      client.close();
    });

    it("reads and subscribes when selected on an open socket with no snapshot yet", async () => {
      vi.useFakeTimers();
      const read = vi.fn<typeof fetch>(async () => Response.json({ ...snapshot(), snapshot_seq: 3 }));
      const { client, qc, sockets, flush } = linkedClient(read);
      await client.connect(); sockets[0].open();
      await flush();
      client.select("session-a");
      await flush();
      expect(read).toHaveBeenCalledOnce();
      expect(sockets[0].subscriptions()).toEqual([{ version: 2, type: "subscribe", conversation_id: "session-a", after_seq: 3, store_generation: "generation-a" }]);
      expect(qc.getQueryData(aiKeys.snapshot(scope, "session-a"))).toMatchObject({ snapshot_seq: 3 });
      client.close();
    });

    it("subscribes a session selected before the socket opens, whatever became of the screen's own read", async () => {
      vi.useFakeTimers();
      const read = vi.fn<typeof fetch>(async () => Response.json(snapshot()));
      const { client, sockets, flush } = linkedClient(read);
      client.select("session-a");
      expect(read).not.toHaveBeenCalled();
      await client.connect(); sockets[0].open();
      await flush();
      expect(sockets[0].subscriptions()).toHaveLength(1);
      client.close();
    });

    it("rereads only when the daemon reports the cached position is unusable", async () => {
      vi.useFakeTimers();
      const read = vi.fn<typeof fetch>(async () => Response.json({ ...snapshot(), store_generation: "generation-b", snapshot_seq: 9 }));
      const { client, qc, sockets, flush } = linkedClient(read);
      qc.setQueryData(aiKeys.snapshot(scope, "session-a"), snapshot());
      await client.connect(); sockets[0].open();
      client.select("session-a");
      client.receive({ version: 2, type: "subscription.status", conversation_id: "session-a", status: "snapshot_required" });
      await flush();
      expect(read).toHaveBeenCalledOnce();
      expect(sockets[0].subscriptions().at(-1)).toMatchObject({ after_seq: 9, store_generation: "generation-b" });
      client.close();
    });

    it("subscribes the new socket when the old socket's read fails after the socket changed", async () => {
      vi.useFakeTimers();
      let fail!: (error: Error) => void;
      const read = vi.fn<typeof fetch>(() => new Promise((_, reject) => { fail = reject; }));
      const { client, qc, sockets, flush } = linkedClient(read);
      qc.setQueryData(aiKeys.snapshot(scope, "session-a"), snapshot());
      await client.connect(); sockets[0].open();
      client.select("session-a");
      client.receive({ version: 2, type: "subscription.status", conversation_id: "session-a", status: "resubscribe" });
      await flush();
      const signal = read.mock.calls[0][1]?.signal;
      sockets[0].close();
      expect(signal?.aborted).toBe(true);
      await flush();
      sockets[1].open();
      fail(new Error("stream reset"));
      await flush();
      expect(sockets[1].subscriptions()).toHaveLength(1);
      client.close();
    });

    it("ignores the old socket's read when it succeeds after the socket changed", async () => {
      vi.useFakeTimers();
      let finish!: (response: Response) => void;
      const read = vi.fn<typeof fetch>(() => new Promise((done) => { finish = done; }));
      const { client, qc, sockets, flush } = linkedClient(read);
      qc.setQueryData(aiKeys.snapshot(scope, "session-a"), { ...snapshot(), snapshot_seq: 2 });
      await client.connect(); sockets[0].open();
      client.select("session-a");
      client.receive({ version: 2, type: "subscription.status", conversation_id: "session-a", status: "resubscribe" });
      await flush();
      sockets[0].close();
      await flush();
      sockets[1].open();
      finish(Response.json({ ...snapshot(), snapshot_seq: 50 }));
      await flush();
      expect(qc.getQueryData(aiKeys.snapshot(scope, "session-a"))).toMatchObject({ snapshot_seq: 2 });
      expect(sockets[1].subscriptions()).toHaveLength(1);
      expect(sockets[1].subscriptions()[0]).toMatchObject({ after_seq: 2 });
      client.close();
    });

    it("retries a failed read on the same socket until it subscribes", async () => {
      vi.useFakeTimers();
      const read = vi.fn<typeof fetch>().mockRejectedValueOnce(new TypeError("Network request failed"))
        .mockImplementation(async () => Response.json({ ...snapshot(), snapshot_seq: 3 }));
      const { client, qc, sockets, flush } = linkedClient(read);
      qc.setQueryData(aiKeys.snapshot(scope, "session-a"), snapshot());
      await client.connect(); sockets[0].open();
      client.select("session-a");
      client.receive({ version: 2, type: "subscription.status", conversation_id: "session-a", status: "snapshot_required" });
      await flush();
      expect(sockets[0].subscriptions()).toHaveLength(1);
      await vi.advanceTimersByTimeAsync(500);
      expect(read).toHaveBeenCalledTimes(2);
      expect(sockets[0].subscriptions().at(-1)).toMatchObject({ after_seq: 3 });
      client.close();
    });

    it("reports a rejected subscription and clears it when a retry subscribes", async () => {
      vi.useFakeTimers();
      const { client, qc, sockets, flush } = linkedClient();
      qc.setQueryData(aiKeys.snapshot(scope, "session-a"), snapshot());
      await client.connect(); sockets[0].open();
      const listener = vi.fn();
      client.onConnection(listener);
      client.receive({ version: 2, type: "subscription.status", conversation_id: "session-a", status: "rejected", error: { kind: "subscription_limit", message: "too many" } });
      expect(client.subscriptionRejected("session-a")).toBe(true);
      expect(listener).toHaveBeenCalled();
      client.receive({ version: 2, type: "subscription.status", conversation_id: "session-a", status: "subscribed", high_water: 0 });
      expect(client.subscriptionRejected("session-a")).toBe(false);
      client.receive({ version: 2, type: "subscription.status", status: "rejected", error: { kind: "internal", message: "scope" } });
      await flush();
      expect(sockets).toHaveLength(2);
      client.close();
    });

    it("gives commands the desktop timeout", async () => {
      vi.useFakeTimers();
      const aborted = (signal?: AbortSignal | null) => new Promise<Response>((_, reject) => { signal?.addEventListener("abort", () => reject(new Error("timeout"))); });
      const { client, flush } = linkedClient(vi.fn<typeof fetch>((_url, init) => aborted(init?.signal)));
      const outcome = client.command("session-a", { type: "send", request_id: "r" }, commandResultSchema).catch((error: Error) => error.message);
      await vi.advanceTimersByTimeAsync(119_000);
      await flush();
      const early = await Promise.race([outcome, Promise.resolve("pending")]);
      expect(early).toBe("pending");
      await vi.advanceTimersByTimeAsync(1_000);
      expect(await outcome).toBe("timeout");
      client.close();
    });

    it("keeps a send made during the window and repeats it with its request id once the link recovers", async () => {
      vi.useFakeTimers();
      const read = vi.fn<typeof fetch>().mockRejectedValueOnce(new TypeError("Network request failed"))
        .mockImplementation(async () => Response.json({ turn_id: "turn-a" }));
      const { client, sockets, flush } = linkedClient(read);
      await client.connect(); sockets[0].open();
      sockets[0].close();
      await flush();
      const sent = client.submit("session-a", { type: "send", request_id: "request-a" }, commandResultSchema);
      await flush();
      expect(read).toHaveBeenCalledOnce();
      sockets[1].open();
      await expect(sent).resolves.toMatchObject({ turn_id: "turn-a" });
      expect(read.mock.calls.map(([, init]) => JSON.parse(String(init?.body)).request_id)).toEqual(["request-a", "request-a"]);
      client.close();
    });

    it("surfaces a send failure when the link does not recover within the window", async () => {
      vi.useFakeTimers();
      const read = vi.fn<typeof fetch>().mockRejectedValue(new TypeError("Network request failed"));
      const { client, sockets, flush } = linkedClient(read);
      await client.connect(); sockets[0].open();
      sockets[0].close();
      await flush();
      const sent = client.submit("session-a", { type: "send", request_id: "request-a" }, commandResultSchema).catch((error: Error) => error.message);
      await vi.advanceTimersByTimeAsync(2_600);
      expect(await sent).toBe("Network request failed");
      expect(read).toHaveBeenCalledOnce();
      client.close();
    });

    it("does not repeat a send the daemon rejected", async () => {
      vi.useFakeTimers();
      const read = vi.fn<typeof fetch>(async () => Response.json({ error: { kind: "turn_running", message: "busy" } }, { status: 409 }));
      const { client, sockets, flush } = linkedClient(read);
      await client.connect(); sockets[0].open();
      sockets[0].close();
      await flush();
      await expect(client.submit("session-a", { type: "send", request_id: "request-a" }, commandResultSchema)).rejects.toThrow("turn_running");
      expect(read).toHaveBeenCalledOnce();
      client.close();
    });
  });
});

describe("mobile AI session timing", () => {
  const reportingHello = () => Response.json({ version: 2, store_generation: "generation-a", scope_seq: 0, capabilities: ["device-link", "timing-report-v1"] });
  const stagesOf = (timing: MobileTimingBuffer) => (timing.batch({ deviceId: scope.deviceId, workspaceId: scope.workspaceId })?.records ?? []).map((record) => record.stage);

  function timedClient(capable = true, read: typeof fetch = async () => Response.json({ accepted: 1 })) {
    const sockets: FakeSocket[] = [];
    const qc = new QueryClient();
    const timing = new MobileTimingBuffer();
    const fetcher = vi.fn<typeof fetch>(async (input, init) => {
      const url = String(input);
      if (url.includes("/hello")) return capable ? reportingHello() : hello();
      if (url.includes("/diagnostics")) return Response.json({ records: [], next: 0 });
      return read(input, init);
    });
    const client = new AISessionClient({ url: "http://127.0.0.1:1234", token: "loopback" }, scope, qc, () => { const socket = new FakeSocket(); sockets.push(socket); return socket as unknown as WebSocket; }, fetcher, timing);
    return { client, qc, sockets, fetcher, timing, flush: () => vi.advanceTimersByTimeAsync(0) };
  }
  const reports = (fetcher: ReturnType<typeof timedClient>["fetcher"]) => fetcher.mock.calls.filter(([url]) => String(url).endsWith("/timing-reports"));

  it("tells the link what the executor stored so a cold start does not report it again", async () => {
    vi.useFakeTimers();
    const { client, sockets, fetcher, timing, flush } = timedClient();
    const go = { version: 2, source: "mobile", at: "2026-11-02T10:00:00.000Z", stage: "connection.direct", event: "mark", elapsedMs: 0 };
    fetcher.mockImplementation(async (input) => {
      const url = String(input);
      if (url.includes("/hello")) return reportingHello();
      if (url.endsWith("/diagnostics/ack")) return new Response(null, { status: 204 });
      if (url.includes("after=0")) return Response.json({ records: [go], next: 1 });
      if (url.includes("/diagnostics")) return Response.json({ records: [], next: 1 });
      return Response.json({ accepted: 1 });
    });
    await client.connect(); await flush();
    sockets[0].open(); await flush();
    expect(timing.count).toBe(0);
    const ack = fetcher.mock.calls.find(([url]) => String(url).endsWith("/diagnostics/ack"))!;
    expect(JSON.parse(String(ack[1]!.body))).toEqual({ next: 1, dropped: 0 });
    client.close();
  });

  it("records a send and its request, tied together for the executor", async () => {
    const { client, fetcher, timing } = timedClient();
    await client.submit("session-a", { type: "send", request_id: "req-1", text: "SECRET" }, commandResultSchema, Date.UTC(2026, 10, 2));
    client.pendingShown("session-a", "req-1");
    const command = fetcher.mock.calls.find(([url]) => String(url).endsWith("/commands"))!;
    expect((command[1]!.headers as Record<string, string>)["X-Multica-Request-ID"]).toBe("req-1");
    const { records } = timing.batch({ deviceId: scope.deviceId, workspaceId: scope.workspaceId })!;
    expect(records.map((record) => record.stage)).toEqual(["client.send", "client.request", "client.request", "client.pending_shown"]);
    expect(records[0]).toMatchObject({ at: "2026-11-02T00:00:00.000Z", requestId: "req-1", conversationId: "session-a", attempt: 1, peer: "peer-a", source: "mobile", clientKind: "mobile" });
    expect(records[2]).toMatchObject({ event: "end", outcome: "completed" });
    expect(JSON.stringify(records)).not.toContain("SECRET");
  });

  it("records a failed request by category, never its text", async () => {
    const { client, timing } = timedClient(true, async () => Response.json({ error: { kind: "turn_running", message: "/Users/me/secret" } }, { status: 409 }));
    await expect(client.submit("session-a", { type: "send", request_id: "req-1", text: "x" }, commandResultSchema)).rejects.toThrow("turn_running");
    const ended = timing.batch({ deviceId: scope.deviceId, workspaceId: scope.workspaceId })!.records.at(-1)!;
    expect(ended).toMatchObject({ stage: "client.request", outcome: "failed", failure: "other" });
    expect(JSON.stringify(ended)).not.toContain("secret");
  });

  it("marks the first output and its commit once per turn, and sums traffic into one record", async () => {
    vi.useFakeTimers();
    const { client, qc, sockets, flush, timing } = timedClient();
    qc.setQueryData(aiKeys.snapshot(scope, "session-a"), snapshot());
    void client.connect();
    await flush();
    sockets[0]!.open();
    await flush();
    timing.batch({ deviceId: scope.deviceId, workspaceId: scope.workspaceId });
    const events = (seq: number, type: string, payload: Record<string, unknown>) => ({ version: 2, type: "conversation.events", conversation_id: "session-a", from_seq: seq, to_seq: seq, events: [{ ...event(seq, type, payload, type.startsWith("item.") ? "a" : null) }] });
    client.receive(events(1, "turn.started", {}), 100);
    client.receive(events(2, "item.started", { kind: "assistant_text" }), 100);
    await vi.advanceTimersByTimeAsync(5000);
    client.receive(events(3, "item.delta", { kind: "assistant_text", field: "text", delta: "hello" }), 50);
    client.committed("session-a");
    client.committed("session-a");
    client.receive(events(4, "turn.completed", { status: "completed" }), 50);
    const parsed = (timing as unknown as { entries: { record: { stage: string; count?: number; bytes?: number; maxGapMs?: number } }[] }).entries.map((entry) => entry.record);
    const count = (stage: string) => parsed.filter((record) => record.stage === stage).length;
    expect(count("client.first_event_received")).toBe(1);
    expect(count("client.first_event_applied")).toBe(1);
    expect(count("client.first_commit")).toBe(1);
    expect(count("client.terminal_applied")).toBe(1);
    expect(parsed.find((record) => record.stage === "link.event_received")).toMatchObject({ count: 4, bytes: 300, maxGapMs: 5000 });
    client.close();
  });

  it("records the socket's life with closed reasons", async () => {
    vi.useFakeTimers();
    const { client, qc, sockets, flush, timing } = timedClient(false);
    qc.setQueryData(aiKeys.snapshot(scope, "session-a"), snapshot());
    void client.connect();
    await flush();
    sockets[0]!.open();
    client.select("session-a");
    client.receive({ version: 2, type: "subscription.status", conversation_id: "session-a", status: "subscribed", high_water: 0 });
    (sockets[0]!.onclose as unknown as (event: { code: number }) => void)({ code: 1006 });
    await flush();
    sockets[1]!.open();
    client.receive({ version: 2, type: "subscription.status", conversation_id: "session-a", status: "subscribed", high_water: 0 });
    client.receive({ version: 2, type: "subscription.status", conversation_id: "session-a", status: "live", high_water: 0 });
    const records = timing.batch({ deviceId: scope.deviceId, workspaceId: scope.workspaceId })!.records;
    expect(records.map((record) => record.stage)).toEqual(["ws.hello", "ws.open", "ws.subscribe", "ws.subscribed", "ws.closed", "ws.retry", "ws.hello", "ws.open", "ws.subscribe", "ws.subscribed", "ws.resubscribed", "ws.catch_up"]);
    expect(records.find((record) => record.stage === "ws.closed")).toMatchObject({ reason: "ws_abnormal" });
    client.close();
  });

  it("records a retry only when its backoff changes", async () => {
    vi.useFakeTimers();
    const { client, fetcher, timing } = timedClient(false);
    fetcher.mockImplementation(async () => { throw new TypeError("offline"); });
    void client.connect();
    await vi.advanceTimersByTimeAsync(120_000);
    const delays = (timing as unknown as { entries: { record: { stage: string; durationMs?: number } }[] }).entries.filter((entry) => entry.record.stage === "ws.retry").map((entry) => entry.record.durationMs);
    expect(delays).toEqual([0, 500, 1000, 2000, 4000, 8000, 10000]);
    client.close();
  });

  it("reports with an acknowledgement, retries the same batch after a reconnect", async () => {
    vi.useFakeTimers();
    let fail = true;
    const { client, sockets, fetcher, flush, timing } = timedClient(true, async () => fail ? new Response("{}", { status: 502 }) : Response.json({ accepted: 3 }));
    void client.connect();
    await flush();
    sockets[0]!.open();
    await flush();
    expect(reports(fetcher)).toHaveLength(1);
    const first = JSON.parse(String(reports(fetcher)[0]![1]!.body)) as { batch_id: string; records: unknown[] };
    expect(first.records.length).toBeGreaterThan(0);
    fail = false;
    sockets[0]!.close();
    await flush();
    sockets[1]!.open();
    await flush();
    const second = JSON.parse(String(reports(fetcher)[1]![1]!.body)) as { batch_id: string };
    expect(second.batch_id).toBe(first.batch_id);
    expect(stagesOf(timing)).not.toContain("ws.hello");
    client.close();
  });

  it("does not report to a daemon that did not offer it, and keeps the marks bounded", async () => {
    vi.useFakeTimers();
    const { client, sockets, fetcher, flush, timing } = timedClient(false);
    void client.connect();
    await flush();
    sockets[0]!.open();
    await flush();
    expect(reports(fetcher)).toHaveLength(0);
    expect(fetcher.mock.calls.some(([url]) => String(url).includes("/diagnostics"))).toBe(false);
    expect(stagesOf(timing)).toContain("ws.open");
    client.close();
  });

  it("records a snapshot read with its size and when it rendered", async () => {
    const { client, timing } = timedClient(true, async () => Response.json(snapshot()));
    await client.snapshot("session-a");
    client.committed("session-a");
    client.committed("session-a");
    const records = timing.batch({ deviceId: scope.deviceId, workspaceId: scope.workspaceId })!.records;
    expect(records.map((record) => record.stage)).toEqual(["snapshot.request", "snapshot.first_byte", "snapshot.received", "snapshot.parsed", "snapshot.rendered"]);
    expect(records[2]!.bytes).toBeGreaterThan(0);
  });
});
