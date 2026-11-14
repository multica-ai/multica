import { describe, expect, it } from "vitest";
import type { ScheduleConfig } from "@multica/core/autopilots/schedule";
import { BUILTIN_VIEW_KEYS, builtinViewItemId } from "@multica/core/issue-views/sidebar-views";
import type { Autopilot, Issue, IssueStatusCategory } from "@multica/core/types";
import { stringsFor } from "../strings";
import { autopilotSections } from "./autopilots";
import { formatDay, formatMoment, parseDay, toDay } from "./dates";
import { groupIssuesByStatus, phaseCounts } from "./issue-groups";
import { describeSchedule, scheduleRepeat, withRepeat } from "./schedule";
import { findViewItem, viewRows } from "./views";

const en = stringsFor("en");
const zh = stringsFor("zh-Hans");

const schedule = (patch: Partial<ScheduleConfig>): ScheduleConfig => ({
  time: { kind: "at", time: "09:00" },
  days: { kind: "every" },
  timezone: "Asia/Shanghai",
  raw: null,
  ...patch,
});
const everyTwoHours: ScheduleConfig["time"] = { kind: "every", interval: 2, unit: "hours", window: null, minute: 15 };

describe("scheduleRepeat", () => {
  it("recognises the repeats the phone can edit", () => {
    expect(scheduleRepeat(schedule({}))).toBe("daily");
    expect(scheduleRepeat(schedule({ days: { kind: "weekly", daysOfWeek: [1, 2, 3, 4, 5] } }))).toBe("weekdays");
    expect(scheduleRepeat(schedule({ days: { kind: "weekly", daysOfWeek: [1, 4] } }))).toBe("weekly");
    expect(scheduleRepeat(schedule({ days: { kind: "monthly", dayOfMonth: 15 } }))).toBe("monthly");
    expect(scheduleRepeat(schedule({ time: everyTwoHours }))).toBe("hourly");
  });

  it("leaves raw cron and minute intervals to the web", () => {
    expect(scheduleRepeat(schedule({ raw: "0 9 * * 1#2" }))).toBeNull();
    expect(scheduleRepeat(schedule({ time: { ...everyTwoHours, unit: "minutes" } }))).toBeNull();
    expect(scheduleRepeat(schedule({ time: everyTwoHours, days: { kind: "weekly", daysOfWeek: [1] } }))).toBeNull();
  });
});

describe("withRepeat", () => {
  it("keeps the time of day across repeats", () => {
    const weekly = withRepeat(schedule({ time: { kind: "at", time: "18:30" } }), "weekly");
    expect(weekly.time).toEqual({ kind: "at", time: "18:30" });
    expect(weekly.days).toEqual({ kind: "weekly", daysOfWeek: [1] });
    expect(withRepeat(weekly, "monthly").days).toEqual({ kind: "monthly", dayOfMonth: 1 });
  });

  it("clears a raw expression once a repeat is picked", () => {
    expect(withRepeat(schedule({ raw: "0 9 * * *" }), "daily").raw).toBeNull();
  });
});

describe("describeSchedule", () => {
  it("reads like a sentence", () => {
    expect(describeSchedule(schedule({}), en)).toBe("Every day at 09:00");
    expect(describeSchedule(schedule({ days: { kind: "weekly", daysOfWeek: [1, 2, 3, 4, 5] } }), en)).toBe("On weekdays at 09:00");
    expect(describeSchedule(schedule({ days: { kind: "weekly", daysOfWeek: [1, 4] } }), en)).toBe("On Mon, Thu at 09:00");
    expect(describeSchedule(schedule({ days: { kind: "monthly", dayOfMonth: 15 } }), en)).toBe("On day 15 of the month at 09:00");
    expect(describeSchedule(schedule({ time: everyTwoHours }), en)).toBe("Every 2 hours at :15");
    expect(describeSchedule(schedule({ raw: "0 9 * * 1#2" }), en)).toBe("Custom schedule · 0 9 * * 1#2");
  });

  it("is compact in Chinese", () => {
    expect(describeSchedule(schedule({ days: { kind: "weekly", daysOfWeek: [1, 2, 3, 4, 5] } }), zh)).toBe("工作日 09:00");
    expect(describeSchedule(schedule({ days: { kind: "weekly", daysOfWeek: [1, 4] } }), zh)).toBe("周一、周四 09:00");
  });
});

describe("dates", () => {
  const now = new Date(2026, 10, 14, 12, 0);

  it("round-trips calendar days", () => {
    expect(toDay(parseDay("2026-03-07")!)).toBe("2026-03-07");
    expect(parseDay("soon")).toBeNull();
  });

  it("adds the year only when it isn't this year", () => {
    expect(formatDay("2026-11-30", en, now)).toBe("Nov 30");
    expect(formatDay("2027-01-05", en, now)).toBe("Jan 5, 2027");
  });

  it("names today and tomorrow", () => {
    expect(formatMoment(new Date(2026, 10, 14, 9, 5).toISOString(), en, now)).toBe("Today 09:05");
    expect(formatMoment(new Date(2026, 10, 15, 9, 0).toISOString(), en, now)).toBe("Tomorrow 09:00");
    expect(formatMoment(new Date(2026, 10, 20, 9, 0).toISOString(), en, now)).toBe("Nov 20 09:00");
  });
});

describe("issue groups", () => {
  const categories: Record<string, IssueStatusCategory> = { todo: "unstarted", in_progress: "started", in_review: "started", done: "done" };
  const catalog = {
    statuses: ["todo", "in_progress", "in_review", "done"].map((key) => ({ key })),
    categoryOf: (key: string) => categories[key] ?? "unstarted",
  } as unknown as Parameters<typeof groupIssuesByStatus>[1];
  const issue = (id: string, status: string) => ({ id, identifier: id, title: id, status, assignee_type: null, assignee_id: null }) as unknown as Issue;
  const issues = [issue("A", "done"), issue("B", "todo"), issue("C", "in_review"), issue("D", "in_progress"), issue("E", "todo")];

  it("orders groups by phase, then by the catalog", () => {
    const groups = groupIssuesByStatus(issues, catalog, (key) => ({ key, label: key, color: null, icon: null }), () => ({ kind: "agent", name: "x" }));
    expect(groups.map((group) => [group.key, group.issues.map((row) => row.id), group.finished])).toEqual([
      ["in_progress", ["D"], false],
      ["in_review", ["C"], false],
      ["todo", ["B", "E"], false],
      ["done", ["A"], true],
    ]);
  });

  it("counts the progress bar's phases", () => {
    expect(phaseCounts(issues, catalog.categoryOf)).toEqual({ done: 1, started: 2, unstarted: 2 });
  });
});

describe("viewRows", () => {
  it("lists starred items first and leaves them out of the rest", () => {
    const [first, second] = BUILTIN_VIEW_KEYS;
    const starredItem = { id: builtinViewItemId(second!), kind: "builtin" as const, key: second! };
    const rows = viewRows([starredItem], [], "me", en);
    expect(rows.starred.map((row) => row.id)).toEqual([starredItem.id]);
    expect(rows.others.map((row) => row.id)).toContain(builtinViewItemId(first!));
    expect(rows.others.map((row) => row.id)).not.toContain(starredItem.id);
    expect(rows.others).toHaveLength(BUILTIN_VIEW_KEYS.length - 1);
  });

  it("finds built-ins by id and reports deleted views", () => {
    const key = BUILTIN_VIEW_KEYS[0]!;
    expect(findViewItem(builtinViewItemId(key), [])).toEqual({ id: builtinViewItemId(key), kind: "builtin", key });
    expect(findViewItem("view:gone", [])).toBeNull();
  });
});

describe("autopilotSections", () => {
  const autopilot = (id: string, status: Autopilot["status"], updated_at: string) =>
    ({ id, title: id, status, updated_at, assignee_type: "agent", assignee_id: "a", trigger_kinds: ["schedule"], next_run_at: null, last_run_status: null }) as unknown as Autopilot;

  it("puts active before paused, newest first, and hides archived ones", () => {
    const sections = autopilotSections(
      [autopilot("old", "active", "2026-11-01"), autopilot("off", "paused", "2026-11-02"), autopilot("gone", "archived", "2026-11-03"), autopilot("new", "active", "2026-11-04")],
      () => ({ kind: "agent", name: "Lambda" }),
      en,
    );
    expect(sections.map((section) => [section.key, section.rows.map((row) => row.id)])).toEqual([
      ["active", ["new", "old"]],
      ["paused", ["off"]],
    ]);
    expect(sections[0]!.rows[0]!.triggers).toBe("Schedule");
  });
});
