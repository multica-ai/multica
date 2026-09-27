// @vitest-environment node
import { describe, expect, it } from "vitest";
import type { AgentTask, TaskUsage } from "@multica/core/types";
import {
  buildRunTimeline,
  groupRunsByDay,
  nearestRunIndex,
  niceTicks,
  timeTicks,
} from "./issue-run-timeline";

function makeTask(overrides: Partial<AgentTask> = {}): AgentTask {
  return {
    id: "task-1",
    agent_id: "agent-1",
    runtime_id: "runtime-1",
    issue_id: "issue-1",
    status: "completed",
    priority: 0,
    dispatched_at: null,
    started_at: "2026-09-24T10:00:00",
    completed_at: "2026-09-24T10:30:00",
    result: null,
    error: null,
    created_at: "2026-09-24T09:59:00",
    ...overrides,
  };
}

// claude-opus-5 at 5 / 25 / 0.50 / 6.25 per million: 1M output = $25.
function usage(outputTokens: number): TaskUsage[] {
  return [
    {
      provider: "anthropic",
      model: "claude-opus-5",
      input_tokens: 0,
      output_tokens: outputTokens,
      cache_read_tokens: 0,
      cache_write_tokens: 0,
    },
  ];
}

const NOW = new Date("2026-09-27T18:00:00").getTime();

describe("buildRunTimeline", () => {
  it("orders runs by start and falls back to dispatch, then creation", () => {
    const timeline = buildRunTimeline(
      [
        makeTask({ id: "late", started_at: "2026-09-25T09:00:00", completed_at: "2026-09-25T09:10:00" }),
        makeTask({
          id: "never-started",
          status: "cancelled",
          started_at: null,
          dispatched_at: null,
          created_at: "2026-09-24T08:00:00",
          completed_at: null,
        }),
        makeTask({ id: "dispatched", started_at: null, dispatched_at: "2026-09-24T09:00:00" }),
      ],
      NOW,
    );

    expect(timeline.runs.map((r) => r.task.id)).toEqual(["never-started", "dispatched", "late"]);
    // A run cancelled before it started still lands on the axis, as a sliver.
    const sliver = timeline.runs[0]!;
    expect(sliver.endMs).toBe(sliver.startMs);
  });

  it("stretches active runs to now and keeps them out of agent time", () => {
    const timeline = buildRunTimeline(
      [
        makeTask({ id: "done" }),
        makeTask({ id: "live", status: "running", started_at: "2026-09-27T17:00:00", completed_at: null }),
      ],
      NOW,
    );

    const live = timeline.runs.find((r) => r.task.id === "live")!;
    expect(live.active).toBe(true);
    expect(live.endMs).toBe(NOW);
    expect(timeline.activeCount).toBe(1);
    expect(timeline.agentMs).toBe(30 * 60 * 1000);
    // Elapsed runs from the first start to now, while a run is still going.
    expect(timeline.elapsedMs).toBe(NOW - new Date("2026-09-24T10:00:00").getTime());
  });

  it("ignores deferred runs, which the execution log does not list", () => {
    const timeline = buildRunTimeline([makeTask({ status: "deferred" })], NOW);
    expect(timeline.runs).toHaveLength(0);
  });

  it("steps the cumulative cost at completion, in completion order", () => {
    // `a` starts first but finishes last; the curve must still only rise.
    const timeline = buildRunTimeline(
      [
        makeTask({ id: "a", started_at: "2026-09-24T10:00:00", completed_at: "2026-09-24T12:00:00", usage: usage(400_000) }),
        makeTask({ id: "b", started_at: "2026-09-24T10:30:00", completed_at: "2026-09-24T11:00:00", usage: usage(200_000) }),
        makeTask({ id: "unpriced", started_at: "2026-09-24T13:00:00", completed_at: "2026-09-24T13:05:00" }),
      ],
      NOW,
    );

    expect(timeline.cumulative.map((s) => s.cost)).toEqual([5, 15]);
    expect(timeline.cumulative.map((s) => s.t)).toEqual([
      new Date("2026-09-24T11:00:00").getTime(),
      new Date("2026-09-24T12:00:00").getTime(),
    ]);
    expect(timeline.totalCost).toBe(15);
    expect(timeline.pricedCount).toBe(2);
    expect(timeline.maxRunCost).toBe(10);
  });

  it("keeps a run without usage at no figure, not $0", () => {
    const timeline = buildRunTimeline([makeTask({ usage: [] })], NOW);
    expect(timeline.runs[0]!.usage).toBeNull();
    expect(timeline.runs[0]!.breakdown).toBeNull();
    expect(timeline.pricedCount).toBe(0);
  });

  it("splits a run's cost by what was billed", () => {
    const timeline = buildRunTimeline([makeTask({ usage: usage(1_000_000) })], NOW);
    expect(timeline.runs[0]!.breakdown).toEqual({ input: 0, output: 25, cacheRead: 0, cacheWrite: 0 });
  });

  it("puts each agent in its own lane, in order of first appearance", () => {
    const timeline = buildRunTimeline(
      [
        makeTask({ id: "b1", agent_id: "agent-b", started_at: "2026-09-24T11:00:00" }),
        makeTask({ id: "a1", agent_id: "agent-a", started_at: "2026-09-24T10:00:00" }),
        makeTask({ id: "a2", agent_id: "agent-a", started_at: "2026-09-24T12:00:00", completed_at: "2026-09-24T12:30:00" }),
      ],
      NOW,
    );

    expect(timeline.lanes.map((l) => [l.agentId, l.runs.map((r) => r.task.id)])).toEqual([
      ["agent-a", ["a1", "a2"]],
      ["agent-b", ["b1"]],
    ]);
  });

  it("names a peak only when one run moved the total enough", () => {
    const dominant = buildRunTimeline(
      [
        makeTask({ id: "big", usage: usage(800_000) }),
        makeTask({ id: "small", started_at: "2026-09-24T11:00:00", completed_at: "2026-09-24T11:10:00", usage: usage(200_000) }),
      ],
      NOW,
    );
    expect(dominant.peak?.task.id).toBe("big");

    // Ten equal runs: the biggest is 10% of the total, which explains nothing.
    const even = buildRunTimeline(
      Array.from({ length: 10 }, (_, i) =>
        makeTask({
          id: `run-${i}`,
          started_at: `2026-09-24T1${i}:00:00`,
          completed_at: `2026-09-24T1${i}:10:00`,
          usage: usage(100_000),
        }),
      ),
      NOW,
    );
    expect(even.peak).toBeNull();
  });

  it("counts failed and cancelled runs", () => {
    const timeline = buildRunTimeline(
      [makeTask({ status: "failed" }), makeTask({ status: "cancelled" }), makeTask({ status: "cancelled" })],
      NOW,
    );
    expect([timeline.failedCount, timeline.cancelledCount]).toEqual([1, 2]);
  });

  it("widens a short span so a single run does not fill the axis", () => {
    const timeline = buildRunTimeline(
      [makeTask({ started_at: "2026-09-24T10:00:00", completed_at: "2026-09-24T10:00:14" })],
      NOW,
    );
    const [d0, d1] = timeline.domain;
    expect(d1 - d0).toBeGreaterThanOrEqual(60 * 60 * 1000);
  });
});

describe("costSoFar", () => {
  it("reads the curve at each run's end, unpriced runs included", () => {
    const timeline = buildRunTimeline(
      [
        makeTask({ id: "a", started_at: "2026-09-24T10:00:00", completed_at: "2026-09-24T10:30:00", usage: usage(400_000) }),
        makeTask({ id: "gap", status: "cancelled", started_at: "2026-09-24T11:00:00", completed_at: "2026-09-24T11:00:05" }),
        makeTask({ id: "b", started_at: "2026-09-24T12:00:00", completed_at: "2026-09-24T12:30:00", usage: usage(200_000) }),
      ],
      NOW,
    );
    expect(timeline.runs.map((r) => [r.task.id, r.costSoFar])).toEqual([
      ["a", 10],
      ["gap", 10],
      ["b", 15],
    ]);
  });
});

describe("nearestRunIndex", () => {
  const { runs } = buildRunTimeline(
    [
      makeTask({ id: "a", started_at: "2026-09-24T10:00:00", completed_at: "2026-09-24T10:30:00" }),
      makeTask({ id: "b", started_at: "2026-09-24T14:00:00", completed_at: "2026-09-24T14:00:20" }),
    ],
    NOW,
  );
  const at = (iso: string) => runs[nearestRunIndex(runs, new Date(iso).getTime())]!.task.id;

  it("picks the run under the pointer", () => {
    expect(at("2026-09-24T10:15:00")).toBe("a");
  });

  it("snaps to the nearest bar, however thin, when between runs", () => {
    expect(at("2026-09-24T11:00:00")).toBe("a");
    expect(at("2026-09-24T13:00:00")).toBe("b");
    expect(at("2026-09-25T00:00:00")).toBe("b");
  });

  it("reaches a run nested inside a longer one", () => {
    // Review repro: A 09:00–12:00 wraps B 10:00–11:00. Over B, B must win.
    const { runs: nested } = buildRunTimeline(
      [
        makeTask({ id: "outer", started_at: "2026-09-24T09:00:00", completed_at: "2026-09-24T12:00:00" }),
        makeTask({ id: "inner", started_at: "2026-09-24T10:00:00", completed_at: "2026-09-24T11:00:00" }),
      ],
      NOW,
    );
    const pick = (iso: string) => nested[nearestRunIndex(nested, new Date(iso).getTime())]!.task.id;
    expect(pick("2026-09-24T10:30:00")).toBe("inner");
    expect(pick("2026-09-24T09:30:00")).toBe("outer");
    expect(pick("2026-09-24T11:30:00")).toBe("outer");
  });

  it("stays in the lane the pointer is over", () => {
    const { runs: lanes } = buildRunTimeline(
      [
        makeTask({ id: "lambda", agent_id: "agent-lambda", started_at: "2026-09-24T10:00:00", completed_at: "2026-09-24T10:20:00" }),
        makeTask({ id: "emacs", agent_id: "agent-emacs", started_at: "2026-09-24T09:00:00", completed_at: "2026-09-24T12:00:00" }),
      ],
      NOW,
    );
    const t = new Date("2026-09-24T10:10:00").getTime();
    expect(lanes[nearestRunIndex(lanes, t)]!.task.id).toBe("lambda");
    expect(lanes[nearestRunIndex(lanes, t, "agent-emacs")]!.task.id).toBe("emacs");
  });

  it("has nothing to point at without runs", () => {
    expect(nearestRunIndex([], 0)).toBe(-1);
  });
});

describe("niceTicks", () => {
  it("steps in clean values and tops out at or above the max", () => {
    expect(niceTicks(166)).toEqual([50, 100, 150, 200]);
    expect(niceTicks(100)).toEqual([25, 50, 75, 100]);
    expect(niceTicks(2)).toEqual([0.5, 1, 1.5, 2]);
    expect(niceTicks(0.37)).toEqual([0.1, 0.2, 0.3, 0.4]);
  });

  it("has nothing to mark without a total", () => {
    expect(niceTicks(0)).toEqual([]);
  });
});

describe("timeTicks", () => {
  it("marks local midnights across a multi-day issue", () => {
    const ticks = timeTicks([
      new Date("2026-09-23T20:00:00").getTime(),
      new Date("2026-09-27T18:00:00").getTime(),
    ]);
    expect(ticks.every((t) => t.kind === "day")).toBe(true);
    expect(ticks.map((t) => new Date(t.t).getDate())).toEqual([24, 25, 26, 27]);
    expect(ticks.every((t) => new Date(t.t).getHours() === 0)).toBe(true);
  });

  it("marks whole hours when the issue fits in a day", () => {
    const ticks = timeTicks([
      new Date("2026-09-24T09:40:00").getTime(),
      new Date("2026-09-24T15:10:00").getTime(),
    ]);
    expect(ticks.every((t) => t.kind === "hour")).toBe(true);
    expect(ticks.map((t) => new Date(t.t).getHours())).toEqual([10, 11, 12, 13, 14, 15]);
  });

  it("thins daily ticks on a long issue", () => {
    const ticks = timeTicks([
      new Date("2026-08-01T00:00:00").getTime(),
      new Date("2026-09-27T00:00:00").getTime(),
    ]);
    expect(ticks.length).toBeLessThanOrEqual(8);
  });
});

describe("groupRunsByDay", () => {
  it("lists newest day first, newest run first, with day totals", () => {
    const { runs } = buildRunTimeline(
      [
        makeTask({ id: "d1-a", started_at: "2026-09-24T10:00:00", completed_at: "2026-09-24T10:30:00", usage: usage(200_000) }),
        makeTask({ id: "d1-b", started_at: "2026-09-24T15:00:00", completed_at: "2026-09-24T15:10:00", usage: usage(400_000) }),
        makeTask({ id: "d2-a", started_at: "2026-09-25T09:00:00", completed_at: "2026-09-25T09:20:00" }),
      ],
      NOW,
    );

    const groups = groupRunsByDay(runs);
    expect(groups.map((g) => g.runs.map((r) => r.task.id))).toEqual([["d2-a"], ["d1-b", "d1-a"]]);
    expect(groups[1]!.cost).toBe(15);
    expect(groups[1]!.agentMs).toBe(40 * 60 * 1000);
  });
});
