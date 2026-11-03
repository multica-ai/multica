// @vitest-environment node
import { describe, expect, it } from "vitest";
import { parseLocalTiming, type LocalTimingRecord } from "@multica/tutti-agent-gui-host/local-timing";
import { buildTurnWaterfalls, summarizeFirstOutput } from "./developer-log-waterfall";

// A fake clock: `ms` offsets stand for the record's wall time.
const at = (ms: number) => new Date(Date.UTC(2026, 10, 2, 10, 0, 0, ms)).toISOString();
const row = (ms: number, stage: string, extra: object = {}) => parseLocalTiming({ version: 2, source: "daemon", at: at(ms), stage, event: "mark", elapsedMs: 0, conversationId: "c", requestId: "r", ...extra }) as LocalTimingRecord;
const daemon = { turnId: "turn-r", provider: "codex", origin: "local", clientKind: "desktop", connection: "loopback", turnKind: "user", turnIndex: 2, prepareKind: "cold" };

function slowTurn(): LocalTimingRecord[] {
  return [
    row(0, "client.send", { source: "gui", turnId: undefined }),
    row(0, "client.request", { source: "gui", event: "start" }),
    row(30, "client.request", { source: "gui", event: "end", durationMs: 30, outcome: "completed" }),
    row(10, "daemon.request_received", { ...daemon, turnId: undefined }),
    row(20, "turn.persist", { ...daemon, event: "start" }),
    row(25, "turn.persist", { ...daemon, event: "end", durationMs: 5 }),
    row(100, "thread.prepare", { ...daemon, event: "end", durationMs: 70, reason: "start" }),
    row(5100, "provider.first_reasoning", daemon),
    row(5300, "provider.first_text", daemon),
    row(5310, "batcher.first_output_commit", daemon),
    row(5315, "ws.first_output_write", { ...daemon, clientKind: "mobile", origin: "p2p", reason: "live" }),
    row(5340, "client.first_event_received", { source: "gui", requestId: undefined, turnId: "turn-r" }),
    row(5400, "client.first_event_applied", { source: "gui", requestId: undefined, turnId: "turn-r" }),
    row(9000, "turn.finished", { ...daemon, event: "end", outcome: "completed" }),
  ];
}

describe("turn waterfall", () => {
  it("merges the initiating and executing sides of one turn end to end", () => {
    const { turns, truncated } = buildTurnWaterfalls(slowTurn());
    expect(truncated).toBe(false);
    expect(turns).toHaveLength(1);
    const turn = turns[0]!;
    expect(turn).toMatchObject({ turnId: "turn-r", requestId: "r", conversationId: "c", provider: "codex", origin: "local", clientKind: "desktop", connection: "loopback", prepareKind: "cold", turnKind: "user", turnIndex: 2, outcome: "completed", background: false, startedAt: at(0) });
    expect(turn.totals).toEqual({ clientToFirstEventMs: 5340, daemonToFirstOutputMs: 5090, transportMs: 35 });
    expect(turn.segments.find((segment) => segment.stage === "thread.prepare")).toMatchObject({ startMs: 30, endMs: 100, durationMs: 70, reason: "start" });
    // A start that has its end collapses into one segment.
    expect(turn.segments.filter((segment) => segment.stage === "client.request")).toHaveLength(1);
    expect(turn.segments.find((segment) => segment.stage === "ws.first_output_write")).toMatchObject({ clientKind: "mobile", reason: "live" });
    expect(turn.segments.map((segment) => segment.startMs)).toEqual([...turn.segments.map((segment) => segment.startMs)].sort((a, b) => a - b));
  });

  it("shows a stage that started and never ended", () => {
    const { turns } = buildTurnWaterfalls([row(0, "daemon.request_received", daemon), row(5, "prepare.wait", { ...daemon, event: "start" })]);
    expect(turns[0]!.segments.at(-1)).toMatchObject({ stage: "prepare.wait", unfinished: true });
    expect(turns[0]!.totals).toEqual({});
  });

  it("reads old GUI stages under their current names", () => {
    const { turns } = buildTurnWaterfalls([
      parseLocalTiming({ version: 1, source: "gui", at: at(0), stage: "gui.submit", event: "end", elapsedMs: 9, durationMs: 9, conversationId: "c", requestId: "old" })!,
    ]);
    expect(turns[0]!.segments[0]).toMatchObject({ stage: "client.request", recordedAs: "gui.submit" });
  });

  it("omits a transport total that clock-free math cannot support", () => {
    const { turns } = buildTurnWaterfalls([row(0, "client.send", { source: "gui" }), row(1, "client.first_event_received", { source: "gui" }), row(2, "daemon.request_received"), row(50, "ws.first_output_write")]);
    expect(turns[0]!.totals.transportMs).toBeUndefined();
  });

  it("keeps background and native turns out of first-output percentiles", () => {
    const turn = (id: string, output: number, extra: object) => [row(0, "daemon.request_received", { requestId: id, turnId: id, provider: "codex", prepareKind: "hot", origin: "local", clientKind: "desktop", ...extra }), row(output, "provider.first_text", { requestId: id, turnId: id, provider: "codex", ...extra })];
    const { turns } = buildTurnWaterfalls([
      ...turn("a", 100, { turnKind: "user" }), ...turn("b", 200, { turnKind: "queued" }), ...turn("c", 300, { turnKind: "user" }),
      ...turn("d", 90000, { turnKind: "background", background: true }), ...turn("e", 90000, { turnKind: "native", background: true }),
    ]);
    expect(summarizeFirstOutput(turns)).toEqual([{ provider: "codex", prepareKind: "hot", origin: "local", clientKind: "desktop", samples: 3, p50Ms: 200, p90Ms: 300, maxMs: 300 }]);
  });

  it("bounds the number of turns and keeps the newest", () => {
    const records = Array.from({ length: 205 }, (_, index) => row(index, "turn.persist", { requestId: `r${index}`, turnId: `t${index}` }));
    const { turns, truncated } = buildTurnWaterfalls(records);
    expect(truncated).toBe(true);
    expect(turns).toHaveLength(200);
    expect(turns[0]!.turnId).toBe("t204");
  });
});
