import { describe, expect, it } from "vitest";
import type { AgentTask } from "@multica/core/types";
import {
  deriveAvgDurationLast30d,
  matchesCallerFilter,
  resolveAgentCallerId,
  summarizeCallerActivityLast30d,
  taskCallerUserId,
} from "./activity-tab";

const NOW = Date.UTC(2026, 5, 15, 12, 0, 0);
const DAY_MS = 24 * 60 * 60 * 1000;

function task(overrides: Partial<AgentTask> = {}): AgentTask {
  return {
    id: "task-1",
    agent_id: "agent-1",
    runtime_id: "runtime-1",
    status: "completed",
    issue_id: "",
    started_at: new Date(NOW - 10 * 60 * 1000).toISOString(),
    completed_at: new Date(NOW - 5 * 60 * 1000).toISOString(),
    ...overrides,
  } as AgentTask;
}

describe("resolveAgentCallerId", () => {
  it("defaults to the signed-in user when no override was picked", () => {
    expect(resolveAgentCallerId(undefined, "user-1")).toBe("user-1");
    expect(resolveAgentCallerId(undefined, null)).toBeNull();
  });

  it("maps the explicit anyone value to no filter and passes members through", () => {
    expect(resolveAgentCallerId("all", "user-1")).toBeNull();
    expect(resolveAgentCallerId("user-2", "user-1")).toBe("user-2");
  });
});

describe("taskCallerUserId", () => {
  it("prefers the attribution originator over the flat column", () => {
    expect(
      taskCallerUserId(
        task({
          attribution: {
            source: "direct_human",
            precise: true,
            originator: { id: "via-attribution", name: "Ada" },
          },
          originator_user_id: "via-column",
        }),
      ),
    ).toBe("via-attribution");
  });

  it("falls back to the flat originator column", () => {
    expect(taskCallerUserId(task({ originator_user_id: "via-column" }))).toBe(
      "via-column",
    );
  });

  it("is null when neither source names a caller", () => {
    expect(taskCallerUserId(task())).toBeNull();
  });
});

describe("matchesCallerFilter", () => {
  it("keeps every task when no caller is set", () => {
    expect(matchesCallerFilter(task(), null)).toBe(true);
  });

  it("matches only tasks attributed to the caller", () => {
    const t = task({ originator_user_id: "user-1" });
    expect(matchesCallerFilter(t, "user-1")).toBe(true);
    expect(matchesCallerFilter(t, "user-2")).toBe(false);
  });
});

describe("deriveAvgDurationLast30d", () => {
  it("averages wall-clock durations of completions inside the window", () => {
    const inside = (minutesAgo: number, runMinutes: number) =>
      task({
        completed_at: new Date(NOW - minutesAgo * 60 * 1000).toISOString(),
        started_at: new Date(
          NOW - (minutesAgo + runMinutes) * 60 * 1000,
        ).toISOString(),
      });
    // 10m and 20m runs → 15m average.
    expect(
      deriveAvgDurationLast30d([inside(5, 10), inside(60, 20)], NOW),
    ).toBe(15 * 60 * 1000);
  });

  it("ignores completions older than 30 days and broken rows", () => {
    const old = task({
      completed_at: new Date(NOW - 31 * DAY_MS).toISOString(),
      started_at: new Date(NOW - 31 * DAY_MS - 60 * 1000).toISOString(),
    });
    const broken = task({ completed_at: "not-a-date" });
    expect(deriveAvgDurationLast30d([old, broken], NOW)).toBe(0);
  });
});

describe("summarizeCallerActivityLast30d", () => {
  it("rolls up only the caller's terminal non-chat tasks", () => {
    const mine = (status: "completed" | "failed", daysAgo: number) =>
      task({
        status,
        originator_user_id: "user-1",
        completed_at: new Date(NOW - daysAgo * DAY_MS).toISOString(),
      });
    const theirs = task({
      status: "completed",
      originator_user_id: "user-2",
      completed_at: new Date(NOW - DAY_MS).toISOString(),
    });
    const chat = task({
      status: "completed",
      originator_user_id: "user-1",
      chat_session_id: "chat-1",
      completed_at: new Date(NOW - DAY_MS).toISOString(),
    });
    const running = task({
      status: "running",
      originator_user_id: "user-1",
      completed_at: undefined,
    });

    const summary = summarizeCallerActivityLast30d(
      [mine("completed", 1), mine("failed", 2), theirs, chat, running],
      NOW,
      "user-1",
    );

    expect(summary.totalRuns).toBe(2);
    expect(summary.totalCompleted).toBe(1);
    expect(summary.totalFailed).toBe(1);
    // One completed and one failed outcome → 50%.
    expect(summary.successRate).toBe(50);
    expect(summary.windowDays).toBe(30);
    // Exactly one bucket carries the day-1 completion.
    const filled = summary.buckets.filter((b) => b.total > 0);
    expect(filled).toHaveLength(2);
  });

  it("returns zero counts for another caller's window", () => {
    const summary = summarizeCallerActivityLast30d(
      [task({ originator_user_id: "user-1" })],
      NOW,
      "user-2",
    );
    expect(summary.totalRuns).toBe(0);
    expect(summary.successRate).toBeNull();
  });
});
