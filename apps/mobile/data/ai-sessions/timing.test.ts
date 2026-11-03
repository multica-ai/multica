// @vitest-environment node
import { describe, expect, it, vi } from "vitest";
import { modelsSchema } from "./schemas";
import { MobileTimingBuffer, diagnosticsSchema, reportAckSchema } from "./timing";

const scope = { deviceId: "peer-a", workspaceId: "workspace-a" };
const endpoint = { url: "http://127.0.0.1:1", token: "t" };

describe("mobile timing buffer", () => {
  it("is bounded and says how much it lost", () => {
    const buffer = new MobileTimingBuffer(2);
    for (let i = 0; i < 5; i++) buffer.write({ stage: "ws.open", count: i });
    expect(buffer.count).toBe(2);
    const batch = buffer.batch(scope)!;
    expect(batch.records.map((record) => record.count)).toEqual([3, 4, 3]);
    expect(batch.records.at(-1)!.stage).toBe("timing.dropped");
    buffer.acknowledge(batch.batch_id);
    expect(buffer.count).toBe(0);
    expect(buffer.batch(scope)).toBeNull();
  });

  it("keeps a batch and its id until it is acknowledged", () => {
    const buffer = new MobileTimingBuffer();
    buffer.write({ stage: "ws.open" });
    const first = buffer.batch(scope)!;
    buffer.write({ stage: "ws.closed" });
    expect(buffer.batch(scope)!.batch_id).toBe(first.batch_id);
    buffer.acknowledge("another");
    expect(buffer.count).toBe(2);
    buffer.acknowledge(first.batch_id);
    expect(buffer.batch(scope)!.records.map((record) => record.stage)).toEqual(["ws.closed"]);
  });

  it("only reports to an executor what is about it", () => {
    const buffer = new MobileTimingBuffer();
    buffer.write({ stage: "ws.open", peer: "peer-a" });
    buffer.write({ stage: "ws.open", peer: "peer-b" });
    buffer.write({ stage: "network.changed" });
    buffer.write({ stage: "client.send", conversationId: "c", peer: "peer-a" }, "workspace-b");
    expect(buffer.batch(scope)!.records.map((record) => [record.stage, record.peer])).toEqual([["ws.open", "peer-a"], ["network.changed", undefined]]);
  });

  it("does not send one workspace's pending batch to another", () => {
    const buffer = new MobileTimingBuffer();
    buffer.write({ stage: "client.send", conversationId: "in-a" }, "workspace-a");
    const first = buffer.batch({ deviceId: "peer-a", workspaceId: "workspace-a" })!;
    buffer.write({ stage: "client.send", conversationId: "in-b" }, "workspace-b");
    const other = buffer.batch({ deviceId: "peer-a", workspaceId: "workspace-b" })!;
    expect(other.records.map((record) => record.conversationId)).toEqual(["in-b"]);
    buffer.acknowledge(other.batch_id);
    // The first workspace's marks wait for it instead of being rejected and cleared.
    const again = buffer.batch({ deviceId: "peer-a", workspaceId: "workspace-a" })!;
    expect(again.records.map((record) => record.conversationId)).toEqual(["in-a"]);
    expect(again.batch_id).not.toBe(first.batch_id);
  });

  it("drops another account's marks", () => {
    const buffer = new MobileTimingBuffer();
    buffer.forUser("a");
    buffer.write({ stage: "ws.open" });
    buffer.forUser("a");
    expect(buffer.count).toBe(1);
    buffer.forUser("b");
    expect(buffer.count).toBe(0);
  });

  it("keeps no free text and no wrong identity", () => {
    const buffer = new MobileTimingBuffer();
    buffer.write({ stage: "ws.closed", reason: "dial tcp 10.0.0.1 refused" as never, peer: "/ip4/10.0.0.1" });
    buffer.write({ stage: "not.a.mark" as never });
    const [record] = buffer.batch(scope)!.records;
    expect(record).toMatchObject({ stage: "ws.closed", source: "mobile", clientKind: "mobile" });
    expect(record!.reason).toBeUndefined();
    expect(record!.peer).toBeUndefined();
    expect(buffer.count).toBe(1);
  });

  it("drains Go marks from the cursor, counts what Go lost and ignores an older link", async () => {
    const buffer = new MobileTimingBuffer();
    const go = { version: 2, source: "mobile", at: "2026-11-02T10:00:00.000Z", stage: "connection.relayed", event: "mark", elapsedMs: 0, peer: "peer-a", addrType: "relay" };
    const fetcher = vi.fn<typeof fetch>()
      .mockResolvedValueOnce(Response.json({ records: [go, { stage: "bogus" }], next: 2, dropped: 3 }))
      .mockResolvedValueOnce(Response.json({ records: [], next: 2, dropped: 3 }));
    await buffer.drain(endpoint, fetcher);
    expect(String(fetcher.mock.calls[0]![0])).toBe("http://127.0.0.1:1/diagnostics?after=0");
    expect(String(fetcher.mock.calls[1]![0])).toBe("http://127.0.0.1:1/diagnostics?after=2");
    expect((fetcher.mock.calls[0]![1]!.headers as Record<string, string>).Authorization).toBe("Bearer t");
    const batch = buffer.batch(scope)!;
    expect(batch.records[0]).toMatchObject({ stage: "connection.relayed", addrType: "relay" });
    expect(batch.records.at(-1)).toMatchObject({ stage: "timing.dropped", count: 4 });

    await new MobileTimingBuffer().drain(endpoint, vi.fn<typeof fetch>(async () => new Response("not found", { status: 404 })));
  });

  describe("acknowledging Go's marks", () => {
    const go = (stage: string) => ({ version: 2, source: "mobile", at: "2026-11-02T10:00:00.000Z", stage, event: "mark", elapsedMs: 0 });
    const acks = (fetcher: ReturnType<typeof vi.fn<typeof fetch>>) => fetcher.mock.calls.filter(([url]) => String(url).endsWith("/diagnostics/ack")).map(([, init]) => JSON.parse(String(init!.body)));
    function link(pages: { records: unknown[]; next: number; dropped?: number }[]) {
      let last = pages[0]!;
      return vi.fn<typeof fetch>(async (url) => {
        if (String(url).endsWith("/diagnostics/ack")) return new Response(null, { status: 204 });
        last = pages.shift() ?? { ...last, records: [] };
        return Response.json(last);
      });
    }

    it("only acknowledges what the executor stored, so a cold start repeats nothing else", async () => {
      const buffer = new MobileTimingBuffer();
      const fetcher = link([{ records: [go("connection.relayed"), go("connection.direct")], next: 2, dropped: 3 }]);
      await buffer.drain(endpoint, fetcher);

      // Drained but unreported: losing the JS buffer must not lose these.
      await buffer.settle(endpoint, fetcher);
      expect(acks(fetcher)).toEqual([]);

      const batch = buffer.batch(scope)!;
      buffer.acknowledge(batch.batch_id);
      await buffer.settle(endpoint, fetcher);
      expect(acks(fetcher).at(-1)).toEqual({ next: 2, dropped: 3 });

      // Nothing new since: no needless write on the phone.
      const before = fetcher.mock.calls.length;
      await buffer.settle(endpoint, fetcher);
      expect(fetcher.mock.calls).toHaveLength(before);
    });

    it("stops at the oldest mark still unreported", async () => {
      const buffer = new MobileTimingBuffer();
      const other = { ...go("connection.relayed"), peer: "other-device" };
      const fetcher = link([{ records: [go("connection.direct"), other, go("connection.lost")], next: 3 }]);
      await buffer.drain(endpoint, fetcher);
      buffer.acknowledge(buffer.batch(scope)!.batch_id);
      await buffer.settle(endpoint, fetcher);
      // The first went out; the second is for another device and holds the position.
      expect(acks(fetcher).at(-1)).toEqual({ next: 1, dropped: 0 });
    });

    it("counts Go's losses from what it still holds after an acknowledgement", async () => {
      const buffer = new MobileTimingBuffer();
      const fetcher = link([{ records: [go("connection.direct")], next: 1, dropped: 3 }]);
      await buffer.drain(endpoint, fetcher);
      buffer.acknowledge(buffer.batch(scope)!.batch_id);
      await buffer.settle(endpoint, fetcher);
      const next = link([{ records: [go("connection.lost")], next: 2, dropped: 1 }]);
      await buffer.drain(endpoint, next);
      expect(buffer.batch(scope)!.records.at(-1)).toMatchObject({ stage: "timing.dropped", count: 1 });
    });

    it("keeps its place when an older link has no such route", async () => {
      const buffer = new MobileTimingBuffer();
      const fetcher = vi.fn<typeof fetch>(async (url) => String(url).endsWith("/diagnostics/ack") ? new Response("not found", { status: 404 }) : Response.json({ records: [go("connection.direct")], next: 1, dropped: 2 }));
      await buffer.drain(endpoint, fetcher);
      buffer.acknowledge(buffer.batch(scope)!.batch_id);
      await buffer.settle(endpoint, fetcher);
      const calls = fetcher.mock.calls.length;
      await buffer.settle(endpoint, fetcher);
      expect(fetcher.mock.calls.length).toBe(calls + 1);
    });
  });

  it("refuses a malformed loopback answer and leaves the cursor where it was", async () => {
    const buffer = new MobileTimingBuffer();
    for (const bad of [{ records: [] }, { records: "x", next: 1 }, { next: -1 }]) {
      await expect(buffer.drain(endpoint, vi.fn<typeof fetch>(async () => Response.json(bad)))).rejects.toThrow("invalid_response");
    }
    const fetcher = vi.fn<typeof fetch>(async () => Response.json({ records: [], next: 0 }));
    await buffer.drain(endpoint, fetcher);
    expect(String(fetcher.mock.calls[0]![0])).toContain("after=0");
  });

  it("parses the executor's answer strictly enough that only success clears a batch", () => {
    expect(reportAckSchema.parse({ accepted: 2 })).toEqual({ accepted: 2, rejected: 0, duplicate: false });
    for (const bad of [{}, { accepted: -1 }, { accepted: "2" }, null]) expect(reportAckSchema.safeParse(bad).success).toBe(false);
    expect(diagnosticsSchema.parse({ next: 0 })).toEqual({ records: [], next: 0, dropped: 0 });
  });
});

describe("model catalog from the executor", () => {
  it("reads the default mark and tolerates a daemon without one", () => {
    expect(modelsSchema.parse({ supported: true, models: [{ id: "a", label: "A", isDefault: true }, { id: "b", label: "B" }] }).models.map((model) => model.isDefault)).toEqual([true, undefined]);
    expect(modelsSchema.safeParse({ models: [{ id: "a", label: "A", isDefault: "yes" }] }).success).toBe(false);
  });
});

describe("MobileTimingBuffer.retain", () => {
  it("copies marks made here and leaves the link's own to Go", () => {
    const buffer = new MobileTimingBuffer();
    const retained: string[] = [];
    buffer.retain = (record) => { retained.push(record.stage); };
    buffer.write({ stage: "ws.open" });
    buffer.write({ stage: "ws.closed" }, undefined, 1);
    expect(retained).toEqual(["ws.open"]);
  });
});
