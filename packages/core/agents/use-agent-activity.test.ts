import { describe, expect, it } from "vitest";
import type { Agent, AgentActivityBucket } from "../types";
import {
  buildActivityMap,
  deriveAgentActivity,
  summarizeActivityWindow,
} from "./use-agent-activity";

const DAY = 24 * 60 * 60 * 1000;

// Fixed anchor — derivation uses local-time start of "today", a real
// clock would drift. 12:00 also keeps "today" stable across odd timezones.
const NOW = new Date("2026-04-28T12:00:00").getTime();

function bucket(
  agentId: string,
  daysAgo: number,
  taskCount: number,
  failedCount = 0,
): AgentActivityBucket {
  const t = new Date(NOW);
  t.setHours(0, 0, 0, 0);
  return {
    agent_id: agentId,
    bucket_at: new Date(t.getTime() - daysAgo * DAY).toISOString(),
    task_count: taskCount,
    failed_count: failedCount,
  };
}

const fullHistoryAgent: Agent = {
  id: "a1",
  workspace_id: "w",
  runtime_id: "r1",
  name: "Old Agent",
  description: "",
  instructions: "",
  avatar_url: null,
  runtime_mode: "cloud",
  runtime_config: {},
  custom_env: {},
  custom_args: [],
  custom_env_redacted: false,
  visibility: "workspace",
  status: "idle",
  max_concurrent_tasks: 1,
  model: "",
  owner_id: null,
  skills: [],
  // Older than the window so daysSinceCreated saturates at DAYS.
  created_at: new Date(NOW - 100 * DAY).toISOString(),
  updated_at: new Date(NOW).toISOString(),
  archived_at: null,
  archived_by: null,
};

describe("deriveAgentActivity", () => {
  it("places buckets in oldest→newest slots across 30 days", () => {
    const buckets = [
      bucket("a1", 29, 1), // slot 0
      bucket("a1", 0, 5), // slot 29
    ];
    const result = deriveAgentActivity(
      buckets,
      fullHistoryAgent.created_at,
      NOW,
    );
    expect(result.buckets).toHaveLength(30);
    expect(result.buckets[0]).toEqual({ total: 1, failed: 0 });
    expect(result.buckets[29]).toEqual({ total: 5, failed: 0 });
    expect(result.daysSinceCreated).toBe(30);
  });

  it("clamps daysSinceCreated for young agents", () => {
    const created = new Date(NOW - 3 * DAY - 60 * 1000).toISOString();
    const result = deriveAgentActivity([bucket("fresh", 1, 4)], created, NOW);
    expect(result.daysSinceCreated).toBe(3);
  });

  it("treats sub-day-old agents as daysSinceCreated = 0", () => {
    const created = new Date(NOW - 2 * 60 * 60 * 1000).toISOString();
    const result = deriveAgentActivity([bucket("fresh", 0, 1)], created, NOW);
    expect(result.daysSinceCreated).toBe(0);
    // Today's bucket still records — pre-life days simply look like zero
    // days, which is on purpose.
    expect(result.buckets[29]).toEqual({ total: 1, failed: 0 });
  });

  it("ignores buckets older than the 30-day window", () => {
    const result = deriveAgentActivity(
      [bucket("a1", 60, 99)],
      fullHistoryAgent.created_at,
      NOW,
    );
    expect(
      result.buckets.reduce((s, b) => s + b.total, 0),
    ).toBe(0);
  });

  it("zero-fills when the agent has no buckets", () => {
    const result = deriveAgentActivity(
      [],
      fullHistoryAgent.created_at,
      NOW,
    );
    expect(result.buckets).toHaveLength(30);
    expect(result.buckets.every((b) => b.total === 0 && b.failed === 0)).toBe(
      true,
    );
  });
});

describe("summarizeActivityWindow", () => {
  it("rolls up totals across the trailing N buckets", () => {
    // 5 runs total over the 30-day series.
    const result = deriveAgentActivity(
      [
        bucket("a1", 25, 1), // outside 7d, inside 30d
        bucket("a1", 6, 1), // inside 7d
        bucket("a1", 0, 3, 1), // inside 7d
      ],
      fullHistoryAgent.created_at,
      NOW,
    );
    const last7 = summarizeActivityWindow(result, 7);
    expect(last7.totalRuns).toBe(4);
    expect(last7.totalFailed).toBe(1);
    expect(last7.buckets).toHaveLength(7);

    const last30 = summarizeActivityWindow(result, 30);
    expect(last30.totalRuns).toBe(5);
    expect(last30.totalFailed).toBe(1);
    expect(last30.buckets).toHaveLength(30);
  });

  it("returns an empty summary for missing activity", () => {
    const summary = summarizeActivityWindow(undefined, 7);
    expect(summary.buckets).toEqual([]);
    expect(summary.totalRuns).toBe(0);
    expect(summary.totalFailed).toBe(0);
    expect(summary.windowDays).toBe(7);
  });

  it("clamps an oversized window to the available bucket count", () => {
    const result = deriveAgentActivity(
      [bucket("a1", 0, 2)],
      fullHistoryAgent.created_at,
      NOW,
    );
    const summary = summarizeActivityWindow(result, 1000);
    expect(summary.buckets).toHaveLength(30);
    expect(summary.totalRuns).toBe(2);
  });

  it("returns no buckets when window is 0", () => {
    const result = deriveAgentActivity(
      [bucket("a1", 0, 5)],
      fullHistoryAgent.created_at,
      NOW,
    );
    const summary = summarizeActivityWindow(result, 0);
    expect(summary.buckets).toEqual([]);
    expect(summary.totalRuns).toBe(0);
  });
});

describe("buildActivityMap", () => {
  it("groups buckets by agent and yields a derivation per agent", () => {
    const agents: Agent[] = [
      fullHistoryAgent,
      { ...fullHistoryAgent, id: "a2" },
    ];
    const buckets: AgentActivityBucket[] = [
      bucket("a1", 0, 3),
      bucket("a2", 1, 2, 1),
      bucket("a1", 2, 4),
    ];
    const map = buildActivityMap(agents, buckets, NOW);
    expect(map.size).toBe(2);
    expect(summarizeActivityWindow(map.get("a1"), 30).totalRuns).toBe(7);
    expect(summarizeActivityWindow(map.get("a2"), 30).totalRuns).toBe(2);
    expect(summarizeActivityWindow(map.get("a2"), 30).totalFailed).toBe(1);
  });

  it("emits a zero-filled entry for an agent with no buckets", () => {
    const agents: Agent[] = [fullHistoryAgent];
    const map = buildActivityMap(agents, [], NOW);
    const a = map.get("a1");
    expect(a?.buckets).toHaveLength(30);
    expect(summarizeActivityWindow(a, 30).totalRuns).toBe(0);
  });
});
