// @vitest-environment node
import { describe, expect, it } from "vitest";
import { ConversationServiceManager } from "./conversation-service-manager";
import { createFakeTransport } from "../testing/fake-transport";

const at = "2026-11-04T10:00:00Z";
const scope = { userId: "u", workspaceId: "w" };
const summary = { conversation_id: "c", summary_seq: 1, provider: "codex", title: "c", status: "idle", pinned: false, archived: false, updated_at: at };

function snapshot(seq: number, fields: { generation?: string; status?: string; queue?: unknown; title?: string; summarySeq?: number } = {}) {
  return {
    version: 2, store_generation: fields.generation ?? "g", snapshot_seq: seq,
    conversation: { ...summary, title: fields.title ?? "c", summary_seq: fields.summarySeq ?? 1 },
    turns: [{ id: "t", turn_index: 0, request_id: "r", status: fields.status ?? "running", started_at: at, messages: [], events: [] }],
    ...(fields.queue ? { queue: fields.queue } : {}),
  };
}

const completed = (seq: number) => ({
  version: 2, type: "conversation.events", conversation_id: "c", from_seq: seq, to_seq: seq,
  events: [{ seq, turn_id: "t", turn_seq: seq, item_id: null, type: "turn.completed", final: true, created_at: at, payload: { status: "completed" } }],
});
const status = (value: string) => ({ version: 2, type: "subscription.status", conversation_id: "c", status: value });
const settled = () => new Promise((resolve) => setTimeout(resolve, 0));

/** A daemon whose snapshot answers the test releases one at a time, in any order. */
async function opened(first = snapshot(5)) {
  const reads: { resolve(value: unknown): void; reject(error: Error): void }[] = [];
  let immediate: unknown = first;
  const transport = createFakeTransport(scope, (_method, path) => {
    if (!path.includes("/snapshot")) return {};
    if (immediate) { const value = immediate; immediate = null; return value; }
    return new Promise((resolve, reject) => { reads.push({ resolve, reject }); });
  });
  const service = new ConversationServiceManager({ snapshots: () => ({ read: async () => null, write: async () => {} }) }).get(transport, scope);
  service.open("c");
  const model = await service.ensure("c");
  await settled();
  return { transport, service, model, reads };
}

describe("a snapshot of the same store arriving late", () => {
  it("does not undo events applied after it was taken", async () => {
    const { transport, service, model, reads } = await opened();
    const pending = service.refresh("c");
    await settled();
    transport.deliver(completed(6));
    reads[0]!.resolve(snapshot(5));
    await pending;
    expect(model.seq).toBe(6);
    expect(model.turns.get("t")!.status).toBe("completed");
  });

  it("still takes its queue, which is read after the rest and can be newer", async () => {
    const { transport, service, model, reads } = await opened();
    const pending = service.refresh("c");
    await settled();
    transport.deliver(completed(6));
    reads[0]!.resolve(snapshot(5, { queue: { revision: 4, paused: true, items: [{ request_id: "q" }] } }));
    await pending;
    expect(model.queue).toMatchObject({ revision: 4, paused: true, items: [{ request_id: "q" }] });
  });

  it("takes a smaller sequence when the daemon says the cursor is ahead of its store", async () => {
    const { transport, model, reads } = await opened();
    transport.deliver(status("snapshot_required"));
    await settled();
    reads[0]!.resolve(snapshot(3, { status: "completed" }));
    await settled();
    expect(model.seq).toBe(3);
    expect(transport.sent.at(-1)).toMatchObject({ type: "subscribe", after_seq: 3 });
    expect(reads).toHaveLength(1);
  });
});

describe("a read from before the database was replaced", () => {
  /** A is in flight when another read reveals the new database, which starts read B. */
  async function replaced() {
    const fixture = await opened(snapshot(50, { generation: "old" }));
    const a = fixture.service.refresh("c");
    await settled();
    const reveal = fixture.service.snapshot("c");
    await settled();
    fixture.reads[1]!.resolve(snapshot(2, { generation: "new" }));
    await reveal;
    await settled();
    expect(fixture.reads).toHaveLength(3);
    return { ...fixture, a, oldRead: fixture.reads[0]!, newRead: fixture.reads[2]! };
  }

  it.each(["first", "last"])("changes nothing when its answer arrives %s", async (order) => {
    const { service, a, oldRead, newRead, reads } = await replaced();
    const answers = [
      () => oldRead.resolve(snapshot(50, { generation: "old", title: "stale", summarySeq: 9 })),
      () => newRead.resolve(snapshot(2, { generation: "new", title: "current", summarySeq: 2 })),
    ];
    if (order === "last") answers.reverse();
    answers[0]!();
    await settled();
    if (order === "first") expect(service.conversation("c")).toBeUndefined();
    answers[1]!();
    const model = await a;
    await settled();
    expect(model).toBe(service.conversation("c"));
    expect(model).toMatchObject({ storeGeneration: "new", seq: 2 });
    // The stale answer starts no further round of invalidation.
    expect(reads).toHaveLength(3);
    expect(service.list.summary("c")?.title).toBe("current");
  });

  it("does not clear the current read's mark, so nothing starts a third read", async () => {
    const { service, oldRead, reads } = await replaced();
    oldRead.resolve(snapshot(50, { generation: "old" }));
    await settled();
    void service.ensure("c").catch(() => {});
    await settled();
    expect(reads).toHaveLength(3);
  });

  it("does not report its failure as the conversation's error", async () => {
    const { service, oldRead, newRead } = await replaced();
    oldRead.reject(new Error("offline"));
    await settled();
    expect(service.status("c").error).toBeNull();
    newRead.resolve(snapshot(2, { generation: "new" }));
  });
});

describe("refresh during a read", () => {
  it("answers from a read started after the call, sharing it among callers", async () => {
    const { service, model, reads } = await opened();
    const first = service.refresh("c");
    await settled();
    const seen: number[] = [];
    const second = service.refresh("c").then((value) => { seen.push(value.seq); });
    const third = service.refresh("c").then((value) => { seen.push(value.seq); });
    reads[0]!.resolve(snapshot(6));
    await first;
    await settled();
    expect(reads).toHaveLength(2);
    expect(seen).toEqual([]);
    reads[1]!.resolve(snapshot(7));
    await Promise.all([second, third]);
    expect(seen).toEqual([7, 7]);
    expect(model.seq).toBe(7);
  });

  it("still reads again after the read under way fails", async () => {
    const { service, reads } = await opened();
    const first = service.refresh("c").catch(() => "failed");
    await settled();
    const second = service.refresh("c");
    reads[0]!.reject(new Error("offline"));
    expect(await first).toBe("failed");
    await settled();
    reads[1]!.resolve(snapshot(6));
    expect((await second).seq).toBe(6);
  });

  it("lets ensure share the read under way", async () => {
    const { service, reads } = await opened();
    const first = service.refresh("c");
    await settled();
    void service.ensure("c");
    await settled();
    expect(reads).toHaveLength(1);
    reads[0]!.resolve(snapshot(6));
    await first;
  });
});
