import { describe, expect, it } from "vitest";
import {
  aggregateAgentTokens,
  bucketUnknownAgentRows,
  computeDailyTotals,
  DELETED_AGENTS_ROW_ID,
  formatDuration,
  mergeAgentDashboardRows,
} from "./usage-display";

describe("computeDailyTotals", () => {
  it("sums tokens, cost, and task count across all rows", () => {
    const totals = computeDailyTotals([
      {
        date: "2026-07-01",
        provider: "anthropic",
        model: "claude-sonnet-4-6",
        input_tokens: 1_000_000,
        output_tokens: 0,
        cache_read_tokens: 0,
        cache_write_tokens: 0,
        task_count: 2,
      },
      {
        date: "2026-07-02",
        provider: "anthropic",
        model: "claude-sonnet-4-6",
        input_tokens: 1_000_000,
        output_tokens: 0,
        cache_read_tokens: 0,
        cache_write_tokens: 0,
        task_count: 3,
      },
    ]);
    expect(totals.input).toBe(2_000_000);
    expect(totals.cost).toBeCloseTo(6, 5); // 2M x $3/1M
    expect(totals.taskCount).toBe(5);
  });
});

describe("aggregateAgentTokens", () => {
  it("folds per-(agent, model) rows into one row per agent, sorted cost desc", () => {
    const rows = aggregateAgentTokens([
      {
        agent_id: "agent-a",
        provider: "anthropic",
        model: "claude-sonnet-4-6",
        input_tokens: 1_000_000,
        output_tokens: 0,
        cache_read_tokens: 0,
        cache_write_tokens: 0,
        task_count: 1,
      },
      {
        agent_id: "agent-b",
        provider: "anthropic",
        model: "claude-opus-4-7",
        input_tokens: 1_000_000,
        output_tokens: 0,
        cache_read_tokens: 0,
        cache_write_tokens: 0,
        task_count: 1,
      },
    ]);
    expect(rows.map((r) => r.agentId)).toEqual(["agent-b", "agent-a"]); // opus ($5) > sonnet ($3)
  });
});

describe("mergeAgentDashboardRows", () => {
  it("prefers the run-time rollup's task count over the token rollup's", () => {
    const merged = mergeAgentDashboardRows(
      [{ agentId: "agent-a", tokens: 1000, cost: 1, taskCount: 5 }],
      [{ agent_id: "agent-a", total_seconds: 120, task_count: 3, failed_count: 0 }],
    );
    expect(merged[0]).toMatchObject({
      agentId: "agent-a",
      tokens: 1000,
      cost: 1,
      seconds: 120,
      taskCount: 3, // from run-time rollup, not the token rollup's 5
    });
  });

  it("includes agents with run-time rows but zero tokens (errored before producing usage)", () => {
    const merged = mergeAgentDashboardRows(
      [],
      [{ agent_id: "agent-a", total_seconds: 5, task_count: 1, failed_count: 1 }],
    );
    expect(merged).toEqual([
      { agentId: "agent-a", tokens: 0, cost: 0, seconds: 5, taskCount: 1 },
    ]);
  });
});

describe("bucketUnknownAgentRows", () => {
  it("folds rows for hard-deleted agents into one synthetic row", () => {
    const rows = [
      { agentId: "known-agent", tokens: 100, cost: 1, seconds: 10, taskCount: 1 },
      { agentId: "deleted-agent", tokens: 50, cost: 0.5, seconds: 5, taskCount: 1 },
    ];
    const bucketed = bucketUnknownAgentRows(rows, new Set(["known-agent"]));
    expect(bucketed).toHaveLength(2);
    expect(bucketed[1]).toEqual({
      agentId: DELETED_AGENTS_ROW_ID,
      tokens: 50,
      cost: 0.5,
      seconds: 0,
      taskCount: 0,
    });
  });

  it("passes rows through untouched while the agent list is still loading (knownAgentIds is null)", () => {
    const rows = [{ agentId: "any-agent", tokens: 1, cost: 1, seconds: 1, taskCount: 1 }];
    expect(bucketUnknownAgentRows(rows, null)).toBe(rows);
  });

  it("returns the rows unchanged when nothing is deleted", () => {
    const rows = [{ agentId: "known-agent", tokens: 1, cost: 1, seconds: 1, taskCount: 1 }];
    expect(bucketUnknownAgentRows(rows, new Set(["known-agent"]))).toEqual(rows);
  });
});

describe("formatDuration", () => {
  it("shows the less-than-a-minute label for sub-second durations", () => {
    expect(formatDuration(0.4, "<1m")).toBe("<1m");
  });

  it("formats sub-minute durations in seconds", () => {
    expect(formatDuration(45, "<1m")).toBe("45s");
  });

  it("formats sub-hour durations as minutes and seconds", () => {
    expect(formatDuration(90, "<1m")).toBe("1m 30s");
  });

  it("drops the seconds segment when it's exactly zero", () => {
    expect(formatDuration(120, "<1m")).toBe("2m");
  });

  it("formats multi-hour durations as hours and minutes", () => {
    expect(formatDuration(3720, "<1m")).toBe("1h 2m");
  });

  it("formats multi-day durations as days and hours", () => {
    expect(formatDuration(90000, "<1m")).toBe("1d 1h");
  });
});
