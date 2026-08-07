import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import {
  aggregateAgentFailures,
  anonymizeUnresolvedAgentRows,
  UNRESOLVED_AGENTS_ROW_ID,
  aggregateAgentTokens,
  aggregateDailyCost,
  aggregateDailyErrors,
  aggregateDailyTasks,
  aggregateFailureClasses,
  aggregateFailureReasons,
  aggregateWeeklyErrors,
  aggregateWeeklyTasks,
  aggregateWeeklyTime,
  bucketUnknownAgentRows,
  computeDailyTotals,
  computeFailureTotals,
  DELETED_AGENTS_ROW_ID,
  formatDuration,
  hasRateSample,
  isSyntheticAgentRow,
  mergeAgentDashboardRows,
  RESTRICTED_AGENTS_ROW_ID,
  sortAgentFailures,
} from "./utils";

describe("aggregateDailyCost", () => {
  it("collapses multiple rows per day into one stack and sorts by date asc", () => {
    const result = aggregateDailyCost([
      {
        date: "2026-05-10",
        provider: "claude",
        model: "claude-sonnet-4-6",
        input_tokens: 1_000_000,
        output_tokens: 500_000,
        cache_read_tokens: 0,
        cache_write_tokens: 0,
        task_count: 3,
      },
      {
        date: "2026-05-09",
        provider: "claude",
        model: "claude-sonnet-4-6",
        input_tokens: 1_000_000,
        output_tokens: 0,
        cache_read_tokens: 0,
        cache_write_tokens: 0,
        task_count: 1,
      },
    ]);

    // Sort: oldest day first.
    expect(result.map((r) => r.date)).toEqual(["2026-05-09", "2026-05-10"]);
    // claude-sonnet-4-6: input $3/M, output $15/M.
    // 2026-05-09 → 1M input × $3 = $3 input, $0 output, $0 cache.
    expect(result[0]).toMatchObject({ input: 3, output: 0, cacheWrite: 0, total: 3 });
    // 2026-05-10 → $3 input + (0.5M × $15) = $7.5 output. Total $10.5.
    expect(result[1]).toMatchObject({ input: 3, output: 7.5, cacheWrite: 0, total: 10.5 });
  });

  it("treats unmapped models as zero-cost", () => {
    const result = aggregateDailyCost([
      {
        date: "2026-05-10",
        provider: "claude",
        model: "made-up-model",
        input_tokens: 999_999_999,
        output_tokens: 0,
        cache_read_tokens: 0,
        cache_write_tokens: 0,
        task_count: 0,
      },
    ]);
    expect(result[0]?.total).toBe(0);
  });
});

describe("aggregateAgentTokens", () => {
  it("folds per-(agent, model) rows into per-agent totals and sorts by cost desc", () => {
    const rows = aggregateAgentTokens([
      {
        agent_id: "small-spender",
        provider: "claude",
        model: "claude-sonnet-4-6",
        input_tokens: 100_000,
        output_tokens: 0,
        cache_read_tokens: 0,
        cache_write_tokens: 0,
        task_count: 1,
      },
      {
        agent_id: "big-spender",
        provider: "claude",
        model: "claude-sonnet-4-6",
        input_tokens: 5_000_000,
        output_tokens: 0,
        cache_read_tokens: 0,
        cache_write_tokens: 0,
        task_count: 3,
      },
      {
        agent_id: "big-spender",
        provider: "claude",
        model: "claude-haiku-4-5",
        input_tokens: 1_000_000,
        output_tokens: 0,
        cache_read_tokens: 0,
        cache_write_tokens: 0,
        task_count: 2,
      },
    ]);

    expect(rows.map((r) => r.agentId)).toEqual(["big-spender", "small-spender"]);
    expect(rows[0]?.taskCount).toBe(5);
    // big-spender across two models — verify cost > small-spender's.
    expect(rows[0]!.cost).toBeGreaterThan(rows[1]!.cost);
  });
});

describe("computeDailyTotals", () => {
  it("sums tokens across rows and adds estimated cost", () => {
    const totals = computeDailyTotals([
      {
        date: "2026-05-10",
        provider: "claude",
        model: "claude-sonnet-4-6",
        input_tokens: 1_000_000,
        output_tokens: 0,
        cache_read_tokens: 0,
        cache_write_tokens: 0,
        task_count: 2,
      },
      {
        date: "2026-05-09",
        provider: "claude",
        model: "claude-sonnet-4-6",
        input_tokens: 2_000_000,
        output_tokens: 0,
        cache_read_tokens: 0,
        cache_write_tokens: 0,
        task_count: 3,
      },
    ]);
    expect(totals.input).toBe(3_000_000);
    expect(totals.cost).toBe(9); // 3M × $3/M
    expect(totals.taskCount).toBe(5);
  });
});

describe("mergeAgentDashboardRows", () => {
  it("uses run-time rollup's per-agent task count, not the token sum", () => {
    // Token rollup returns two (agent, model) rows for the same task
    // (the agent ran one task that touched two models). The token-side
    // aggregator sums per-row task_count and lands at 2; the run-time
    // rollup correctly reports the underlying distinct count of 1.
    const tokenRows = [
      {
        agentId: "agent-a",
        tokens: 3_000_000,
        cost: 12,
        taskCount: 2, // overcounted because (model-1: 1) + (model-2: 1)
      },
    ];
    const runTimeRows = [
      {
        agent_id: "agent-a",
        total_seconds: 600,
        task_count: 1, // truth: one task touched both models
        failed_count: 0,
        cancelled_count: 0,
      },
    ];
    const merged = mergeAgentDashboardRows(tokenRows, runTimeRows);
    expect(merged).toHaveLength(1);
    expect(merged[0]!.taskCount).toBe(1);
    expect(merged[0]!.seconds).toBe(600);
  });

  it("falls back to token count when no run-time row exists (in-flight task)", () => {
    // Tokens reported mid-run; task hasn't terminated yet so the run-time
    // rollup is silent on this agent. Keep the token-side estimate
    // instead of dropping the agent from the table entirely.
    const merged = mergeAgentDashboardRows(
      [{ agentId: "agent-b", tokens: 100, cost: 0.5, taskCount: 1 }],
      [],
    );
    expect(merged[0]!.taskCount).toBe(1);
    expect(merged[0]!.seconds).toBe(0);
  });

  it("includes agents that have run-time but no tokens", () => {
    // Task errored before reporting any usage — run-time row exists but
    // there's no corresponding token row. Agent must still appear on the
    // list with zeroed-out token columns.
    const merged = mergeAgentDashboardRows(
      [],
      [{ agent_id: "agent-c", total_seconds: 30, task_count: 1, failed_count: 1, cancelled_count: 0 }],
    );
    expect(merged).toHaveLength(1);
    expect(merged[0]!.tokens).toBe(0);
    expect(merged[0]!.cost).toBe(0);
    expect(merged[0]!.taskCount).toBe(1);
  });

  it("sorts by cost desc with run-time as a tiebreaker", () => {
    const merged = mergeAgentDashboardRows(
      [
        { agentId: "low", tokens: 100, cost: 1, taskCount: 1 },
        { agentId: "high", tokens: 100, cost: 9, taskCount: 1 },
        { agentId: "zero-cost-long", tokens: 0, cost: 0, taskCount: 0 },
      ],
      [
        { agent_id: "zero-cost-long", total_seconds: 1000, task_count: 5, failed_count: 0, cancelled_count: 0 },
      ],
    );
    expect(merged.map((r) => r.agentId)).toEqual(["high", "low", "zero-cost-long"]);
  });
});

describe("bucketUnknownAgentRows", () => {
  const live = { agentId: "live", tokens: 100, cost: 1, seconds: 10, taskCount: 1 };
  const archived = {
    agentId: "archived",
    tokens: 80,
    cost: 0.8,
    seconds: 8,
    taskCount: 2,
  };
  const deletedA = {
    agentId: "deleted-a",
    tokens: 50,
    cost: 0.5,
    seconds: 5,
    taskCount: 1,
  };
  const deletedB = {
    agentId: "deleted-b",
    tokens: 30,
    cost: 0.25,
    seconds: 3,
    taskCount: 4,
  };

  it("folds every hard-deleted agent into one aggregated bucket row", () => {
    // "deleted-a" / "deleted-b" are absent from the known set — they'd otherwise
    // render as bare UUIDs. They collapse into a single sentinel row.
    const out = bucketUnknownAgentRows(
      [live, deletedA, deletedB],
      new Set(["live"]),
    );
    expect(out.map((r) => r.agentId)).toEqual(["live", DELETED_AGENTS_ROW_ID]);
    const bucket = out.find((r) => r.agentId === DELETED_AGENTS_ROW_ID)!;
    expect(bucket.tokens).toBe(80);
    expect(bucket.cost).toBeCloseTo(0.75);
    // Time/Tasks never attach to the bucket — the run-time rollup inner-joins
    // `agent`, so deleted agents contribute nothing to those columns.
    expect(bucket.seconds).toBe(0);
    expect(bucket.taskCount).toBe(0);
  });

  it("keeps the bucket total reconciled with the top-line spend", () => {
    // The KPI total counts deleted-agent spend; sum(visible rows) must match it
    // so the breakdown reconciles (MUL-3776).
    const out = bucketUnknownAgentRows(
      [live, deletedA, deletedB],
      new Set(["live"]),
    );
    const visibleCost = out.reduce((s, r) => s + r.cost, 0);
    const kpiCost = [live, deletedA, deletedB].reduce((s, r) => s + r.cost, 0);
    expect(visibleCost).toBeCloseTo(kpiCost);
  });

  it("keeps archived agents as themselves, never in the bucket", () => {
    // The agent list is fetched with archived included, so archived agents are
    // in the known set and stay on the board under their own id.
    const out = bucketUnknownAgentRows(
      [live, archived, deletedA],
      new Set(["live", "archived"]),
    );
    expect(out.map((r) => r.agentId)).toEqual([
      "live",
      "archived",
      DELETED_AGENTS_ROW_ID,
    ]);
  });

  it("adds no bucket row when every agent is known", () => {
    const out = bucketUnknownAgentRows([live, archived], new Set(["live", "archived"]));
    expect(out.map((r) => r.agentId)).toEqual(["live", "archived"]);
  });

  it("keeps every row untouched while the agent list is still loading (null set)", () => {
    const out = bucketUnknownAgentRows([live, deletedA], null);
    expect(out.map((r) => r.agentId)).toEqual(["live", "deleted-a"]);
  });

  // MUL-5409: the server folds agents the viewer may not see onto its own
  // sentinel. That row is not in `knownAgentIds` either, and sweeping it into
  // the "Deleted agents" bucket is exactly the lie the issue was filed for —
  // those agents are alive.
  it("keeps the server's restricted bucket out of the deleted bucket", () => {
    const restricted = {
      agentId: RESTRICTED_AGENTS_ROW_ID,
      tokens: 70,
      cost: 0.7,
      seconds: 42,
      taskCount: 3,
    };
    const out = bucketUnknownAgentRows(
      [live, restricted, deletedA],
      new Set(["live"]),
    );
    expect(out.map((r) => r.agentId)).toEqual([
      "live",
      RESTRICTED_AGENTS_ROW_ID,
      DELETED_AGENTS_ROW_ID,
    ]);
    // It passes through whole: unlike a deleted agent it really ran, so its
    // Time / Tasks columns carry real numbers.
    expect(out.find((r) => r.agentId === RESTRICTED_AGENTS_ROW_ID)).toEqual(
      restricted,
    );
  });

  it("classifies both bucket ids as synthetic and real agents as not", () => {
    expect(isSyntheticAgentRow(DELETED_AGENTS_ROW_ID)).toBe(true);
    expect(isSyntheticAgentRow(RESTRICTED_AGENTS_ROW_ID)).toBe(true);
    expect(isSyntheticAgentRow("live")).toBe(false);
  });
});

describe("formatDuration", () => {
  it("formats seconds-only durations", () => {
    expect(formatDuration(45, "<1m")).toBe("45s");
  });
  it("formats minutes and seconds when under one hour", () => {
    expect(formatDuration(150, "<1m")).toBe("2m 30s");
    expect(formatDuration(60, "<1m")).toBe("1m");
  });
  it("formats hours and minutes when under one day", () => {
    expect(formatDuration(3 * 3600 + 17 * 60, "<1m")).toBe("3h 17m");
    expect(formatDuration(3600, "<1m")).toBe("1h");
  });
  it("formats days and hours when more than 24 hours", () => {
    expect(formatDuration(2 * 86400 + 5 * 3600, "<1m")).toBe("2d 5h");
  });
  it("falls back to the supplied label for sub-second durations", () => {
    expect(formatDuration(0, "<1m")).toBe("<1m");
    expect(formatDuration(0.4, "<1m")).toBe("<1m");
  });
});

// ---------------------------------------------------------------------------
// Weekly run-time / tasks aggregation. Mirrors the runtimes-side
// aggregateByWeek tests: trailing N calendar weeks anchored at today-in-tz,
// pre-zeroed buckets, partial-week metadata, and rows outside the window
// dropped. We assert the same invariants on the workspace dashboard helpers
// so all four metrics behave consistently when the user toggles Weekly.
// ---------------------------------------------------------------------------

describe("aggregateWeeklyTime", () => {
  beforeEach(() => {
    vi.useFakeTimers();
  });
  afterEach(() => {
    vi.useRealTimers();
  });

  it("folds per-day run-time rows into Mon-anchored weekly totals", () => {
    // 2026-05-19 is a Tuesday → current week is Mon=05-18..Sun=05-24.
    vi.setSystemTime(new Date("2026-05-19T12:00:00Z"));
    const rows = [
      { date: "2026-05-11", total_seconds: 100, task_count: 0, failed_count: 0, cancelled_count: 0 },
      { date: "2026-05-17", total_seconds: 50, task_count: 0, failed_count: 0, cancelled_count: 0 },
      { date: "2026-05-18", total_seconds: 25, task_count: 0, failed_count: 0, cancelled_count: 0 },
    ];
    const result = aggregateWeeklyTime(rows, "UTC", 2);
    expect(result).toHaveLength(2);
    expect(result[0]).toMatchObject({
      weekStart: "2026-05-11",
      weekEnd: "2026-05-17",
      totalSeconds: 150,
      partial: false,
      daysCovered: 7,
    });
    expect(result[1]).toMatchObject({
      weekStart: "2026-05-18",
      totalSeconds: 25,
      partial: true,
      daysCovered: 2, // Mon + Tue
    });
  });

  it("drops rows that fall outside the trailing window and keeps empty buckets", () => {
    // Same MUL-2382 sparse-data regression we caught on the runtimes side:
    // an old populated week must not surface when the requested window
    // doesn't include it; in-range empty weeks must remain as zero buckets.
    vi.setSystemTime(new Date("2026-05-19T12:00:00Z"));
    const rows = [
      // 2026-04-13 is a Monday — exactly one week earlier than the oldest
      // in-range week (Mon=04-20) for a 5-week trailing window.
      { date: "2026-04-13", total_seconds: 999, task_count: 0, failed_count: 0, cancelled_count: 0 },
    ];
    const result = aggregateWeeklyTime(rows, "UTC", 5);
    expect(result.map((w) => w.weekStart)).toEqual([
      "2026-04-20",
      "2026-04-27",
      "2026-05-04",
      "2026-05-11",
      "2026-05-18",
    ]);
    for (const w of result) expect(w.totalSeconds).toBe(0);
  });
});

describe("aggregateWeeklyTasks", () => {
  beforeEach(() => {
    vi.useFakeTimers();
  });
  afterEach(() => {
    vi.useRealTimers();
  });

  it("splits completed and failed counts per calendar week", () => {
    vi.setSystemTime(new Date("2026-05-19T12:00:00Z"));
    const rows = [
      { date: "2026-05-12", total_seconds: 0, task_count: 5, failed_count: 1, cancelled_count: 0 },
      { date: "2026-05-18", total_seconds: 0, task_count: 3, failed_count: 0, cancelled_count: 0 },
    ];
    const result = aggregateWeeklyTasks(rows, "UTC", 2);
    expect(result[0]).toMatchObject({
      weekStart: "2026-05-11",
      completed: 4,
      failed: 1,
    });
    expect(result[1]).toMatchObject({
      weekStart: "2026-05-18",
      completed: 3,
      failed: 0,
      partial: true,
    });
  });

  it("keeps cancelled runs out of the completed segment", () => {
    vi.setSystemTime(new Date("2026-05-19T12:00:00Z"));
    const result = aggregateWeeklyTasks(
      [
        {
          date: "2026-05-12",
          total_seconds: 0,
          task_count: 6,
          failed_count: 1,
          cancelled_count: 2,
        },
      ],
      "UTC",
      2,
    );
    expect(result[0]).toMatchObject({
      weekStart: "2026-05-11",
      completed: 3,
      failed: 1,
      cancelled: 2,
    });
  });
});

describe("aggregateDailyTasks", () => {
  // failed_count and cancelled_count are disjoint subsets of task_count, so
  // the succeeded segment is the remainder. Forgetting to subtract cancelled
  // is what renders a run the user stopped as a green "completed" bar.
  it("splits the stack three ways and sorts oldest-first", () => {
    const result = aggregateDailyTasks([
      {
        date: "2026-05-18",
        total_seconds: 0,
        task_count: 4,
        failed_count: 1,
        cancelled_count: 1,
      },
      {
        date: "2026-05-17",
        total_seconds: 0,
        task_count: 3,
        failed_count: 0,
        cancelled_count: 3,
      },
    ]);
    expect(result.map((r) => r.date)).toEqual(["2026-05-17", "2026-05-18"]);
    expect(result[0]).toMatchObject({ completed: 0, failed: 0, cancelled: 3 });
    expect(result[1]).toMatchObject({ completed: 2, failed: 1, cancelled: 1 });
  });

  // An older backend omits cancelled_count; the schema defaults it to 0, and
  // the segment math must degrade to the previous two-way split rather than
  // driving `completed` negative.
  it("clamps completed at zero when the counts overrun task_count", () => {
    const result = aggregateDailyTasks([
      {
        date: "2026-05-18",
        total_seconds: 0,
        task_count: 1,
        failed_count: 1,
        cancelled_count: 1,
      },
    ]);
    expect(result[0]).toMatchObject({ completed: 0, failed: 1, cancelled: 1 });
  });
});

// ---------------------------------------------------------------------------
// Failure aggregations
//
// The rollups ship succeeded rows too, marked by `failure_reason: ""`. Every
// test below leans on that: the succeeded row is what makes an error *rate*
// possible, and mishandling it is the failure mode with the worst blast
// radius — a rate that reads 100% when nothing is wrong.
// ---------------------------------------------------------------------------

describe("aggregateDailyErrors", () => {
  it("stacks failures by class and keeps the succeeded rows as the denominator", () => {
    const result = aggregateDailyErrors([
      { date: "2026-05-10", failure_reason: "", task_count: 8 },
      {
        date: "2026-05-10",
        failure_reason: "agent_error.provider_auth_or_access",
        task_count: 2,
      },
      { date: "2026-05-10", failure_reason: "timeout", task_count: 1 },
      { date: "2026-05-09", failure_reason: "", task_count: 4 },
    ]);

    expect(result.map((r) => r.date)).toEqual(["2026-05-09", "2026-05-10"]);
    expect(result[1]).toMatchObject({
      auth: 2,
      timeout: 1,
      rate_limit: 0,
      failed: 3,
      total: 11,
    });
    // A day with only successes still renders a bar slot, at zero height.
    expect(result[0]).toMatchObject({ failed: 0, total: 4 });
  });

  it("folds a reason this build has never seen into 'other' rather than dropping it", () => {
    const [row] = aggregateDailyErrors([
      { date: "2026-05-10", failure_reason: "agent_error.from_the_future", task_count: 5 },
    ]);
    expect(row).toMatchObject({ other: 5, failed: 5, total: 5 });
  });
});

describe("aggregateWeeklyErrors", () => {
  beforeEach(() => {
    vi.useFakeTimers();
  });
  afterEach(() => {
    vi.useRealTimers();
  });

  it("buckets per calendar week and pre-zeroes weeks with no terminal tasks", () => {
    vi.setSystemTime(new Date("2026-05-19T12:00:00Z"));
    const result = aggregateWeeklyErrors(
      [
        { date: "2026-05-12", failure_reason: "runtime_offline", task_count: 2 },
        { date: "2026-05-12", failure_reason: "", task_count: 6 },
      ],
      "UTC",
      2,
    );

    expect(result[0]).toMatchObject({
      weekStart: "2026-05-11",
      runtime: 2,
      failed: 2,
      total: 8,
    });
    expect(result[1]).toMatchObject({
      weekStart: "2026-05-18",
      failed: 0,
      total: 0,
      partial: true,
    });
  });
});

describe("computeFailureTotals", () => {
  it("excludes the succeeded bucket from the numerator but not the denominator", () => {
    expect(
      computeFailureTotals([
        { failure_reason: "", task_count: 9 },
        { failure_reason: "timeout", task_count: 1 },
      ]),
    ).toEqual({ failed: 1, total: 10, rate: 0.1 });
  });

  it("reports rate 0 rather than dividing by zero on an empty window", () => {
    expect(computeFailureTotals([])).toEqual({ failed: 0, total: 0, rate: 0 });
  });
});

describe("aggregateFailureClasses / aggregateFailureReasons", () => {
  const rows = [
    { failure_reason: "", task_count: 20 },
    { failure_reason: "agent_error.provider_quota_limit", task_count: 3 },
    { failure_reason: "agent_error.provider_capacity_or_rate_limit", task_count: 4 },
    { failure_reason: "timeout", task_count: 5 },
  ];

  it("merges reasons that share a class and ranks by count desc", () => {
    expect(aggregateFailureClasses(rows)).toEqual([
      { failureClass: "rate_limit", count: 7 },
      { failureClass: "timeout", count: 5 },
    ]);
  });

  it("keeps raw reasons separate so an operator can search the exact string", () => {
    expect(aggregateFailureReasons(rows)).toEqual([
      { reason: "timeout", failureClass: "timeout", count: 5 },
      {
        reason: "agent_error.provider_capacity_or_rate_limit",
        failureClass: "rate_limit",
        count: 4,
      },
      {
        reason: "agent_error.provider_quota_limit",
        failureClass: "rate_limit",
        count: 3,
      },
    ]);
  });
});

describe("aggregateAgentFailures", () => {
  it("ranks by failure count, carries the rate, and splits failures by class", () => {
    const result = aggregateAgentFailures([
      { agent_id: "a", failure_reason: "", task_count: 90 },
      { agent_id: "a", failure_reason: "timeout", task_count: 10 },
      { agent_id: "b", failure_reason: "", task_count: 1 },
      { agent_id: "b", failure_reason: "runtime_offline", task_count: 3 },
      { agent_id: "b", failure_reason: "timeout", task_count: 1 },
    ]);

    // `a` fails 10% of the time, `b` fails 80% — but `a` is the bigger
    // absolute problem, so it ranks first and the rate rides along.
    expect(result.map((r) => [r.agentId, r.failed, r.total, r.rate])).toEqual([
      ["a", 10, 100, 0.1],
      ["b", 4, 5, 0.8],
    ]);
    // The whole composition, not just the heaviest class: `b` failing two
    // ways is the thing that decides whether to look at the agent or at the
    // platform, and a single dominant-class label hid it.
    expect(result[1]?.classes).toMatchObject({ runtime: 3, timeout: 1, auth: 0 });
  });

  it("drops agents with no failures — the list is triage, not a census", () => {
    expect(
      aggregateAgentFailures([{ agent_id: "clean", failure_reason: "", task_count: 42 }]),
    ).toEqual([]);
  });
});

describe("sortAgentFailures", () => {
  // `busy` is the workspace's biggest absolute problem; `flaky` is the least
  // healthy per run; `once` is the small-sample trap — a single failed run is
  // a 100% rate and would win the Rate ranking outright.
  const rows = aggregateAgentFailures([
    { agent_id: "busy", failure_reason: "", task_count: 900 },
    { agent_id: "busy", failure_reason: "timeout", task_count: 100 },
    { agent_id: "flaky", failure_reason: "", task_count: 80 },
    { agent_id: "flaky", failure_reason: "runtime_offline", task_count: 20 },
    { agent_id: "once", failure_reason: "timeout", task_count: 1 },
  ]);

  it("ranks by absolute failures by default", () => {
    expect(sortAgentFailures(rows, "failed").map((r) => r.agentId)).toEqual([
      "busy",
      "flaky",
      "once",
    ]);
  });

  it("ranks by rate, with too-small samples demoted rather than dropped", () => {
    // `once` is 100% and `flaky` only 20%, but one run is not evidence. The
    // row still renders — the list has to reconcile with the workspace
    // failure count above it.
    expect(sortAgentFailures(rows, "rate").map((r) => r.agentId)).toEqual([
      "flaky",
      "busy",
      "once",
    ]);
  });

  it("marks which rows have enough runs for their rate to mean anything", () => {
    expect(rows.map((r) => hasRateSample(r))).toEqual([true, true, false]);
  });

  it("leaves the input array untouched", () => {
    const before = rows.map((r) => r.agentId);
    sortAgentFailures(rows, "rate");
    expect(rows.map((r) => r.agentId)).toEqual(before);
  });
});

describe("anonymizeUnresolvedAgentRows", () => {
  // Raw per-(agent, reason) rows, which is the shape this operates on. Two
  // agents the viewer cannot resolve, with deliberately conflicting dominant
  // classes — see the counterexample test below.
  const rows = [
    { agent_id: "visible", failure_reason: "", task_count: 5 },
    { agent_id: "visible", failure_reason: "timeout", task_count: 5 },
    {
      agent_id: "private-a",
      failure_reason: "agent_error.provider_auth_or_access",
      task_count: 6,
    },
    { agent_id: "private-a", failure_reason: "timeout", task_count: 5 },
    { agent_id: "private-b", failure_reason: "timeout", task_count: 10 },
  ];

  it("rewrites unresolvable ids to the sentinel and leaves resolvable ones alone", () => {
    const result = anonymizeUnresolvedAgentRows(rows, new Set(["visible"]));

    expect(result.map((r) => r.agent_id)).toEqual([
      "visible",
      "visible",
      UNRESOLVED_AGENTS_ROW_ID,
      UNRESOLVED_AGENTS_ROW_ID,
      UNRESOLVED_AGENTS_ROW_ID,
    ]);
    // Counts are untouched — only identity is erased.
    expect(result.map((r) => r.task_count)).toEqual([5, 5, 6, 5, 10]);
  });

  it("keeps the bucket's class split honest across merged agents", () => {
    // This is why the rewrite happens on RAW rows. private-a is auth-dominant
    // (6 vs 5) and private-b is timeout-only (10). Merging AFTER aggregation
    // would see only each agent's dominant class and its total failure count —
    // auth 11, timeout 10 — while the true composition is timeout 15 / auth 6.
    const bucket = aggregateAgentFailures(
      anonymizeUnresolvedAgentRows(rows, new Set(["visible"])),
    ).find((r) => r.agentId === UNRESOLVED_AGENTS_ROW_ID);

    expect(bucket).toMatchObject({ failed: 21, total: 21 });
    expect(bucket?.classes).toMatchObject({ timeout: 15, auth: 6 });
  });

  it("anonymizes everything while the agent list is still loading", () => {
    // Deliberately stricter than bucketUnknownAgentRows, which passes rows
    // through on null: a transient flash of raw UUIDs is precisely the leak
    // this function exists to prevent.
    const result = anonymizeUnresolvedAgentRows(rows, null);

    expect(new Set(result.map((r) => r.agent_id))).toEqual(
      new Set([UNRESOLVED_AGENTS_ROW_ID]),
    );
  });

  it("returns the input untouched when every agent resolves", () => {
    const known = new Set(["visible", "private-a", "private-b"]);
    // Same reference, not just equal — nothing needed rewriting.
    expect(anonymizeUnresolvedAgentRows(rows, known)).toBe(rows);
  });
});
