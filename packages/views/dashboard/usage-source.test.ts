// @vitest-environment node
import { describe, expect, it } from "vitest";
import type { DashboardUsageBySource } from "@multica/core/types";
import { sourceBuckets, sourceTotals, usageByAutopilot } from "./usage-source";

function row(over: Partial<DashboardUsageBySource>): DashboardUsageBySource {
  return {
    date: "2026-11-18",
    agent_id: "a1",
    autopilot_id: null,
    provider: "test",
    model: "unpriced-model",
    input_tokens: 0,
    output_tokens: 0,
    cache_read_tokens: 0,
    cache_write_tokens: 0,
    cost_usd_ticks: 0,
    task_count: 1,
    ...over,
  };
}

const rows = [
  row({ input_tokens: 100, output_tokens: 20 }),
  row({ autopilot_id: "ap1", input_tokens: 300, cost_usd_ticks: 2e10, task_count: 2 }),
  row({ date: "2026-11-23", autopilot_id: "ap2", cache_read_tokens: 50 }),
  row({ date: "2026-11-24", autopilot_id: "ap1", output_tokens: 10 }),
];

describe("usage by source", () => {
  it("splits totals into manual and autopilot", () => {
    expect(sourceTotals(rows)).toMatchObject({ manualTokens: 120, autopilotTokens: 360, autopilotCost: 2 });
  });

  it("buckets by day, and by Monday-first week", () => {
    expect(sourceBuckets(rows, "tokens", false)).toEqual([
      { date: "2026-11-18", manual: 120, autopilot: 300 },
      { date: "2026-11-23", manual: 0, autopilot: 50 },
      { date: "2026-11-24", manual: 0, autopilot: 10 },
    ]);
    // 2026-11-18 is a Wednesday; 11-23 a Monday.
    expect(sourceBuckets(rows, "tokens", true)).toEqual([
      { date: "2026-11-16", manual: 120, autopilot: 300 },
      { date: "2026-11-23", manual: 0, autopilot: 60 },
    ]);
  });

  it("ranks autopilots by the chosen metric", () => {
    expect(usageByAutopilot(rows, "tokens").map((r) => [r.autopilotId, r.tokens])).toEqual([
      ["ap1", 310],
      ["ap2", 50],
    ]);
    expect(usageByAutopilot(rows, "cost")[0]?.autopilotId).toBe("ap1");
  });
});
