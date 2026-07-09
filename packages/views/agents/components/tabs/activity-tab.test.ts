import { describe, expect, it } from "vitest";
import type { AgentTask } from "@multica/core/types";
import {
  deriveAvgDurationLast30d,
  formatDurationMs,
} from "./activity-tab";

const NOW = new Date("2026-04-28T12:00:00Z").getTime();
const HOUR = 60 * 60 * 1000;
const DAY = 24 * HOUR;

function task(overrides: Partial<AgentTask>): AgentTask {
  return {
    id: "t",
    agent_id: "a",
    runtime_id: "r",
    issue_id: "",
    status: "completed",
    priority: 0,
    dispatched_at: null,
    started_at: null,
    completed_at: null,
    result: null,
    error: null,
    created_at: new Date(NOW - HOUR).toISOString(),
    ...overrides,
  };
}

describe("deriveAvgDurationLast30d", () => {
  it("returns 0 when no tasks have both started_at and completed_at", () => {
    const tasks = [
      task({ started_at: null, completed_at: new Date(NOW).toISOString() }),
      task({
        started_at: new Date(NOW - HOUR).toISOString(),
        completed_at: null,
      }),
    ];
    expect(deriveAvgDurationLast30d(tasks, NOW)).toBe(0);
  });

  it("averages durations across in-window tasks", () => {
    const tasks = [
      task({
        started_at: new Date(NOW - 60_000).toISOString(),
        completed_at: new Date(NOW).toISOString(),
      }), // 60s
      task({
        started_at: new Date(NOW - 180_000).toISOString(),
        completed_at: new Date(NOW).toISOString(),
      }), // 180s
    ];
    expect(deriveAvgDurationLast30d(tasks, NOW)).toBe(120_000);
  });

  it("excludes tasks completed more than 30 days ago", () => {
    const tasks = [
      task({
        started_at: new Date(NOW - 60_000).toISOString(),
        completed_at: new Date(NOW).toISOString(),
      }), // in window, 60s
      task({
        started_at: new Date(NOW - 60 * DAY - 60_000).toISOString(),
        completed_at: new Date(NOW - 60 * DAY).toISOString(),
      }), // 60d ago, ignored
    ];
    expect(deriveAvgDurationLast30d(tasks, NOW)).toBe(60_000);
  });

  it("ignores non-positive durations defensively", () => {
    const tasks = [
      task({
        // Wall-clock anomaly: completed before it started. The aggregation
        // should still produce a sensible average from the well-formed rows
        // rather than poisoning the count with a zero-or-negative entry.
        started_at: new Date(NOW).toISOString(),
        completed_at: new Date(NOW - 1000).toISOString(),
      }),
      task({
        started_at: new Date(NOW - 4000).toISOString(),
        completed_at: new Date(NOW).toISOString(),
      }), // 4s
    ];
    expect(deriveAvgDurationLast30d(tasks, NOW)).toBe(4000);
  });
});

describe("formatDurationMs", () => {
  it("renders sub-minute durations in seconds", () => {
    expect(formatDurationMs(800)).toBe("1s"); // floor avoidance
    expect(formatDurationMs(12_000)).toBe("12s");
    expect(formatDurationMs(59_500)).toBe("60s");
  });

  it("renders sub-hour durations as 'm SS' with padded seconds", () => {
    expect(formatDurationMs(60_000)).toBe("1m 00s");
    expect(formatDurationMs(125_000)).toBe("2m 05s");
    expect(formatDurationMs(605_000)).toBe("10m 05s");
  });

  it("renders multi-hour durations as 'h m'", () => {
    expect(formatDurationMs(3 * 60 * 60_000 + 30 * 60_000)).toBe("3h 30m");
  });

  it("handles zero / negative defensively", () => {
    expect(formatDurationMs(0)).toBe("—");
    expect(formatDurationMs(-100)).toBe("—");
  });
});
