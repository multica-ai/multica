// @vitest-environment node
import { afterEach, describe, expect, it, vi } from "vitest";
import { createLocalCostProfile, createLocalSendTiming, createLocalTiming, createLocalPresentationTiming, parseLocalTiming, type LocalTimingRecord } from "./local-timing";

afterEach(() => vi.useRealTimers());
describe("local turn timing", () => {
  it("records background cleanup after completion without extending foreground waits", async () => {
    vi.useFakeTimers({ toFake: ["Date", "performance", "setInterval", "clearInterval"] });
    const records: LocalTimingRecord[] = [];
    const timing = createLocalTiming({ source: "main", conversationId: "c" }, (record) => records.push(record));
    timing.heartbeat(); timing.mark("provider.result");
    let release!: () => void;
    const cleanup = timing.background("provider.thread_unload", () => new Promise<void>((resolve) => { release = resolve; }));
    await vi.advanceTimersByTimeAsync(15_000);
    expect(records.at(-1)).toMatchObject({ stage: "provider.result", event: "wait" });
    timing.finish("completed");
    await vi.advanceTimersByTimeAsync(15_000);
    release(); await cleanup;
    expect(records.at(-1)).toMatchObject({ stage: "provider.thread_unload", event: "end", background: true, durationMs: 30_000 });
    expect(records.filter((record) => record.stage === "turn.finished")).toHaveLength(1);
    expect(vi.getTimerCount()).toBe(0);
    const title = createLocalTiming({ source: "main", conversationId: "c", background: true }, (record) => records.push(record));
    await title.measure("title.generate", async () => {}); title.close(); title.mark("provider.exit");
    expect(records.at(-1)).toMatchObject({ stage: "title.generate", background: true, outcome: "completed" });
  });
  it("distinguishes a stalled configuration from a stalled reply and stops heartbeats at termination", async () => {
    vi.useFakeTimers({ toFake: ["Date", "performance", "setInterval", "clearInterval"] });
    const records: LocalTimingRecord[] = [];
    const timing = createLocalTiming({ source: "main", conversationId: "conversation", requestId: "request" }, (record) => records.push(record));
    timing.heartbeat(); timing.heartbeat();
    let release!: () => void;
    const waiting = timing.measure("provider.config", () => new Promise<void>((resolve) => { release = resolve; }));
    await vi.advanceTimersByTimeAsync(15_000);
    expect(records.at(-1)).toMatchObject({ stage: "provider.config", event: "wait", elapsedMs: 15_000 });
    release(); await waiting;
    timing.mark("provider.input_written");
    await vi.advanceTimersByTimeAsync(15_000);
    expect(records.at(-1)).toMatchObject({ stage: "provider.input_written", event: "wait", idleMs: 15_000 });
    timing.once("provider.first_text"); timing.once("provider.first_text");
    timing.finish("cancelled");
    const count = records.length;
    await vi.advanceTimersByTimeAsync(60_000);
    timing.once("provider.exit");
    expect(records).toHaveLength(count);
    expect(records.filter((record) => record.stage === "provider.first_text")).toHaveLength(1);
    expect(records.at(-1)).toMatchObject({ outcome: "cancelled" });
    expect(vi.getTimerCount()).toBe(0);
  });
  it("never logs payloads, credentials or error text, and log failures cannot fail the action", async () => {
    const records: LocalTimingRecord[] = [];
    const timing = createLocalTiming({ source: "main", conversationId: "c" }, (record) => records.push(record));
    await expect(timing.measure("context.credentials", async () => { throw new Error("HTTP 429 secret-token full prompt"); })).rejects.toThrow("HTTP 429");
    expect(records.at(-1)).toMatchObject({ outcome: "failed", failure: "rate_limit" });
    const sanitized = parseLocalTiming({ ...records[0], prompt: "SECRET", env: { TOKEN: "SECRET" } });
    expect(JSON.stringify(sanitized)).not.toContain("SECRET");
    expect(JSON.stringify(records)).not.toMatch(/secret-token|full prompt/);
    expect(parseLocalTiming({ ...records[0], conversationId: "/private/file" })).toBeNull();
    expect(parseLocalTiming({ ...records[0], elapsedMs: Infinity })).toBeNull();
    const broken = createLocalTiming({ source: "main", conversationId: "c" }, () => { throw new Error("disk unavailable"); });
    await expect(broken.measure("turn.persist", async () => "sent")).resolves.toBe("sent");
  });
  it("measures overlapping tools independently without exposing their arguments", async () => {
    vi.useFakeTimers({ toFake: ["Date", "performance"] });
    const records: LocalTimingRecord[] = [];
    const timing = createLocalTiming({ source: "main", conversationId: "c" }, (record) => records.push(record));
    timing.tool("tool_use", "one"); await vi.advanceTimersByTimeAsync(20);
    timing.tool("tool_use", "two"); await vi.advanceTimersByTimeAsync(30);
    timing.tool("tool_result", "two"); timing.tool("tool_result", "one");
    expect(records.filter((record) => record.event === "end").map((record) => record.durationMs)).toEqual([30, 50]);
  });
  it("records one visible output and terminal projection for live turns, ignoring old history", () => {
    const records: LocalTimingRecord[] = [];
    const timing = createLocalPresentationTiming((record) => records.push(record));
    timing.applied("c", "old", true, true);
    timing.applied("c", "live", false, false);
    timing.applied("c", "live", true, false);
    timing.applied("c", "live", true, false);
    timing.applied("c", "live", true, true);
    timing.applied("c", "live", true, true);
    expect(records.map((record) => record.stage)).toEqual(["client.first_event_applied", "client.terminal_applied"]);
  });
  it("records the first model output received once, as a live turn even if it settles at once", () => {
    const records: LocalTimingRecord[] = [];
    const timing = createLocalPresentationTiming((record) => records.push(record));
    timing.received("c", "fast");
    timing.received("c", "fast");
    timing.applied("c", "fast", true, true);
    expect(records.map((record) => record.stage)).toEqual(["client.first_event_received", "client.first_event_applied", "client.terminal_applied"]);
    expect(records[0]).toMatchObject({ version: 2, source: "gui", clientKind: "desktop", conversationId: "c", turnId: "fast" });
  });
  it("marks the pending message on the next frame, and not at all without a window", () => {
    const records: LocalTimingRecord[] = [];
    createLocalSendTiming((record) => records.push(record)).begin("c", "r", Date.now());
    expect(records.map((record) => record.stage)).toEqual(["client.send"]);
    const frames: (() => void)[] = [];
    vi.stubGlobal("requestAnimationFrame", (run: () => void) => frames.push(run));
    try {
      records.length = 0;
      createLocalSendTiming((record) => records.push(record)).begin("c", "r2", Date.now());
      expect(records.map((record) => record.stage)).toEqual(["client.send"]);
      frames[0]!();
      expect(records.map((record) => record.stage)).toEqual(["client.send", "client.pending_shown"]);
    } finally { vi.unstubAllGlobals(); }
  });

  it("dates client.send back to the user's action and counts retries of one request", () => {
    vi.useFakeTimers({ toFake: ["Date", "performance"] });
    vi.setSystemTime(new Date("2026-11-02T10:00:00.000Z"));
    const records: LocalTimingRecord[] = [];
    const sends = createLocalSendTiming((record) => records.push(record));
    const startedAt = Date.now();
    vi.advanceTimersByTime(40);
    sends.begin("c", "r", startedAt).mark("turn.persist");
    sends.begin("c", "r", Date.now());
    expect(records[0]).toMatchObject({ stage: "client.send", at: "2026-11-02T10:00:00.000Z", elapsedMs: 0, attempt: 1, requestId: "r", conversationId: "c" });
    expect(records[1]).toMatchObject({ elapsedMs: 40 });
    expect(records[2]).toMatchObject({ stage: "client.send", attempt: 2 });
  });
});

describe("local conversation cost profile", () => {
  afterEach(() => { vi.useRealTimers(); });
  const setup = () => {
    vi.useFakeTimers();
    const records: LocalTimingRecord[] = [];
    const profile = createLocalCostProfile({ itemCount: () => 12, heapBytes: () => 2048, sink: (record) => records.push(record) });
    return { records, profile };
  };
  const stages = (records: LocalTimingRecord[]) => records.map((record) => record.stage);

  it("writes one summary per segment for a window, not one per run", () => {
    const { records, profile } = setup();
    for (let i = 0; i < 50; i++) {
      profile.measure("projection", "c1", () => { vi.advanceTimersByTime(2); });
      profile.measure("dispatch", "c1", () => { vi.advanceTimersByTime(1); });
    }
    profile.rendered(7);
    expect(records).toEqual([]);
    vi.advanceTimersByTime(5_000);
    expect(stages(records)).toEqual(["client.projection_summary", "client.dispatch_summary", "client.render_summary", "client.heap_sample"]);
    expect(records[0]).toMatchObject({ conversationId: "c1", count: 50, durationMs: 100, maxGapMs: 2, source: "gui" });
    expect(records[2]).toMatchObject({ count: 1, durationMs: 7, maxGapMs: 7 });
    expect(records[3]).toMatchObject({ count: 12, bytes: 2048 });
    vi.advanceTimersByTime(20_000);
    expect(records).toHaveLength(4);
  });

  it("attributes a window to its conversation when another one starts changing", () => {
    const { records, profile } = setup();
    profile.measure("projection", "c1", () => {});
    profile.measure("projection", "c2", () => {});
    expect(records.map((record) => [record.stage, record.conversationId])).toEqual([["client.projection_summary", "c1"], ["client.heap_sample", "c1"]]);
    profile.dispose();
    expect(records.at(-2)).toMatchObject({ stage: "client.projection_summary", conversationId: "c2" });
  });

  it("keeps the result and the failure of what it measures", () => {
    const { profile } = setup();
    expect(profile.measure("dispatch", "c1", () => 3)).toBe(3);
    expect(() => profile.measure("dispatch", "c1", () => { throw new Error("boom"); })).toThrow("boom");
  });

  it("ignores a React commit before any conversation changed, and works without a heap reading", () => {
    vi.useFakeTimers();
    const records: LocalTimingRecord[] = [];
    const profile = createLocalCostProfile({ itemCount: () => 0, heapBytes: () => undefined, sink: (record) => records.push(record) });
    profile.rendered(5);
    vi.advanceTimersByTime(5_000);
    expect(records).toEqual([]);
    profile.measure("projection", "c1", () => {});
    profile.dispose();
    expect(records.at(-1)!.bytes).toBeUndefined();
  });
});
