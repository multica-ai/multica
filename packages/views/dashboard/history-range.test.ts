// @vitest-environment node
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { addDaysIso, aggregateByWeek, todayIso } from "../runtimes/utils";
import { aggregateWeeklyErrors, aggregateWeeklyTasks, aggregateWeeklyTime } from "./utils";
import { dashboardTimeRanges } from "./components/dashboard-shared";
import {
  earliestReadableDay,
  effectiveRange,
  isRangeAllowed,
  planForRange,
} from "./history-range";

const limits = (free: number | null, pro: number | null, business: number | null) => ({
  free: { dashboardHistoryDays: free },
  pro: { dashboardHistoryDays: pro },
  business: { dashboardHistoryDays: business },
});

describe("effectiveRange", () => {
  it("keeps the pick the plan covers and otherwise falls back to the longest that fits", () => {
    expect(effectiveRange(90, null)).toBe(90);
    expect(effectiveRange(365, null)).toBe(365);
    expect(effectiveRange(90, 90)).toBe(90);
    expect(effectiveRange(90, 30)).toBe(30);
    expect(effectiveRange(365, 90)).toBe(90);
    expect(effectiveRange(180, 100)).toBe(90);
    expect(effectiveRange(7, 30)).toBe(7);
    // A limit shorter than every multi-day range still leaves today.
    expect(effectiveRange(30, 3)).toBe(1);
    // Without the year range, a 365 pick reads the longest offered range.
    expect(effectiveRange(365, null, dashboardTimeRanges(false))).toBe(180);
    expect(effectiveRange(365, null, dashboardTimeRanges(true))).toBe(365);
    expect(isRangeAllowed(30, 30)).toBe(true);
    expect(isRangeAllowed(90, 30)).toBe(false);
  });
});

describe("planForRange", () => {
  it("names the cheapest paid plan whose history covers the range", () => {
    const standard = limits(30, 90, 365);
    expect(planForRange(90, standard)).toBe("pro");
    expect(planForRange(180, standard)).toBe("business");
    expect(planForRange(365, standard)).toBe("business");
    // Follows the configuration rather than a fixed table.
    expect(planForRange(180, limits(30, 180, 365))).toBe("pro");
  });

  it("names nothing it cannot back with a configured limit", () => {
    expect(planForRange(90, null)).toBeNull();
    expect(planForRange(90, undefined)).toBeNull();
    expect(planForRange(365, limits(30, 90, 180))).toBeNull();
    expect(planForRange(90, limits(30, null, 365))).toBe("business");
  });
});

describe("earliestReadableDay", () => {
  it("is the first of the last N days, or none", () => {
    expect(earliestReadableDay("2026-11-20", 30, addDaysIso)).toBe("2026-10-22");
    expect(earliestReadableDay("2026-11-20", 1, addDaysIso)).toBe("2026-11-20");
    expect(earliestReadableDay("2026-11-20", null, addDaysIso)).toBeNull();
  });
});

describe("weekly charts under a history limit", () => {
  beforeEach(() => {
    vi.useFakeTimers();
  });
  afterEach(() => {
    vi.useRealTimers();
  });

  // Weekly charts start on Monday; today falls on either end of a week.
  it.each([
    // Wednesday: 30 days back is Tuesday 10-20, one day into the week of Mon 10-19.
    { now: "2026-11-18T12:00:00Z", firstWeek: "2026-10-19", covered: 6, lastCovered: 3 },
    // Sunday: 30 days back is Saturday 10-24, the last day of the week of Mon 10-19.
    { now: "2026-11-22T12:00:00Z", firstWeek: "2026-10-19", covered: 2, lastCovered: 7 },
    // Monday: the five weeks drawn start after 30 days back, so none is cut.
    { now: "2026-11-16T12:00:00Z", firstWeek: "2026-10-19", covered: 7, lastCovered: 1 },
  ])("marks the leftmost week partial when the plan cuts into it ($now)", ({ now, firstWeek, covered, lastCovered }) => {
    vi.setSystemTime(new Date(now));
    const today = todayIso("UTC");
    const earliest = earliestReadableDay(today, 30, addDaysIso);
    const weekCount = Math.ceil(30 / 7);
    const rows = [
      { date: earliest!, total_seconds: 60, task_count: 1, failed_count: 0, cancelled_count: 0 },
    ];

    const time = aggregateWeeklyTime(rows, "UTC", weekCount, earliest);
    expect(time).toHaveLength(5);
    expect(time[0]).toMatchObject({ weekStart: firstWeek, partial: covered < 7, daysCovered: covered });
    expect(time[1]).toMatchObject({ partial: false, daysCovered: 7 });
    expect(time[4]).toMatchObject({ daysCovered: lastCovered, partial: lastCovered < 7 });

    const tasks = aggregateWeeklyTasks(rows, "UTC", weekCount, earliest);
    expect(tasks[0]).toMatchObject({ partial: covered < 7, daysCovered: covered });
    const errors = aggregateWeeklyErrors([], "UTC", weekCount, earliest);
    expect(errors[0]).toMatchObject({ partial: covered < 7, daysCovered: covered });
    const { weeklyTokens, weeklyCostStack } = aggregateByWeek([], "UTC", weekCount, earliest);
    expect(weeklyTokens[0]).toMatchObject({ partial: covered < 7, daysCovered: covered });
    expect(weeklyCostStack[0]).toMatchObject({ partial: covered < 7, daysCovered: covered });

    // Without a limit the same week is whole.
    expect(aggregateWeeklyTime(rows, "UTC", weekCount)[0]).toMatchObject({ partial: false, daysCovered: 7 });
  });

  // A replica still on the old policy answers the 35-day weekly request with
  // the day before the plan's range. That day must not reach the first bucket.
  it("leaves out days before the plan's first day that a stale replica returned", () => {
    vi.setSystemTime(new Date("2026-11-18T12:00:00Z")); // Wednesday
    const today = todayIso("UTC");
    const earliest = earliestReadableDay(today, 30, addDaysIso)!; // Tue 10-20
    const outside = addDaysIso(earliest, -1); // Mon 10-19, day 31
    const weekCount = Math.ceil(30 / 7);

    const runRows = [
      { date: outside, total_seconds: 420, task_count: 7, failed_count: 2, cancelled_count: 1 },
      { date: earliest, total_seconds: 60, task_count: 1, failed_count: 0, cancelled_count: 0 },
    ];
    const time = aggregateWeeklyTime(runRows, "UTC", weekCount, earliest);
    expect(time[0]).toMatchObject({ weekStart: "2026-10-19", daysCovered: 6, totalSeconds: 60 });
    const tasks = aggregateWeeklyTasks(runRows, "UTC", weekCount, earliest);
    expect(tasks[0]).toMatchObject({ completed: 1, failed: 0, cancelled: 0 });

    const errors = aggregateWeeklyErrors(
      [
        { date: outside, failure_reason: "timeout", task_count: 5 },
        { date: earliest, failure_reason: "", task_count: 2 },
      ],
      "UTC",
      weekCount,
      earliest,
    );
    expect(errors[0]).toMatchObject({ failed: 0, total: 2 });

    const usage = (date: string, input: number) => ({
      date,
      provider: "anthropic",
      model: "claude-sonnet-4-6",
      input_tokens: input,
      output_tokens: 0,
      cache_read_tokens: 0,
      cache_write_tokens: 0,
    });
    const { weeklyTokens } = aggregateByWeek(
      [usage(outside, 9_000), usage(earliest, 100)],
      "UTC",
      weekCount,
      earliest,
    );
    expect(weeklyTokens[0]).toMatchObject({ input: 100 });
  });
});
