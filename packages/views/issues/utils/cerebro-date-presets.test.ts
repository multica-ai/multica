// CEREBRO-PATCH(my-issues-date-builder): FIR-1812 — engine coverage for the
// reference-aligned date value vocabulary: window math stays dynamic, the
// set-modes and operator negation behave, and absolute on/before/after/range
// round-trip through valueOptionIdForFilter.
import { describe, it, expect } from "vitest";
import {
  addDaysDateOnly,
  todayDateOnly,
} from "@multica/core/issues/date";
import type { Issue } from "@multica/core/types";
import {
  namedWindow,
  buildDatePreset,
  resolveDateWindow,
  buildAbsoluteSingle,
  absoluteSingleDay,
  valueOptionIdForFilter,
  DATE_VALUE_OPTIONS,
  FAR_PAST,
  FAR_FUTURE,
} from "./cerebro-date-presets";
import { filterIssues, type IssueFilters } from "./filter";

const NO_FILTER: IssueFilters = {
  statusFilters: [],
  priorityFilters: [],
  assigneeFilters: [],
  includeNoAssignee: false,
  creatorFilters: [],
  projectFilters: [],
  includeNoProject: false,
  labelFilters: [],
};

function makeIssue(overrides: Partial<Issue> = {}): Issue {
  return {
    id: "i-1",
    workspace_id: "ws-1",
    number: 1,
    identifier: "MUL-1",
    kind: "issue",
    title: "Test",
    description: null,
    status: "todo",
    priority: "medium",
    assignee_type: null,
    assignee_id: null,
    creator_type: "member",
    creator_id: "m-1",
    created_at: todayDateOnly(),
    updated_at: todayDateOnly(),
    due_date: null,
    ...overrides,
  } as Issue;
}

describe("namedWindow", () => {
  it("today / yesterday / tomorrow are single days", () => {
    expect(namedWindow("today")).toEqual({ from: todayDateOnly(), to: todayDateOnly() });
    expect(namedWindow("yesterday")).toEqual({ from: addDaysDateOnly(-1), to: addDaysDateOnly(-1) });
    expect(namedWindow("tomorrow")).toEqual({ from: addDaysDateOnly(1), to: addDaysDateOnly(1) });
  });

  it("this_month spans the first to the last day of the current month and contains today", () => {
    const w = namedWindow("this_month")!;
    expect(w.from.endsWith("-01")).toBe(true);
    expect(w.from <= todayDateOnly() && todayDateOnly() <= w.to).toBe(true);
    // from and to are within the same calendar month; to is its last day.
    expect(w.to.slice(0, 7)).toBe(w.from.slice(0, 7));
    const y = Number(w.to.slice(0, 4));
    const mo = Number(w.to.slice(5, 7)); // 1-based month
    const lastDay = new Date(y, mo, 0).getDate(); // day 0 of next month = last day
    expect(Number(w.to.slice(8, 10))).toBe(lastDay);
  });

  it("this_quarter is a 3-month span starting on a quarter month", () => {
    const w = namedWindow("this_quarter")!;
    const startMonth = Number(w.from.split("-")[1]); // 1-based
    expect([1, 4, 7, 10]).toContain(startMonth);
    expect(w.from <= todayDateOnly() && todayDateOnly() <= w.to).toBe(true);
  });

  it("this_year covers Jan 1 to Dec 31", () => {
    const w = namedWindow("this_year")!;
    expect(w.from.slice(5)).toBe("01-01");
    expect(w.to.slice(5)).toBe("12-31");
  });

  it("overdue is open-ended up to yesterday; later_than_today starts tomorrow", () => {
    expect(namedWindow("overdue")).toEqual({ from: FAR_PAST, to: addDaysDateOnly(-1) });
    expect(namedWindow("today_and_earlier")).toEqual({ from: FAR_PAST, to: todayDateOnly() });
    expect(namedWindow("later_than_today")).toEqual({ from: addDaysDateOnly(1), to: FAR_FUTURE });
  });
});

describe("buildDatePreset set-modes", () => {
  it("any → mode set, none → mode none (due_date)", () => {
    expect(buildDatePreset("created_at", "any").mode).toBe("set");
    const none = buildDatePreset("created_at", "none");
    expect(none.mode).toBe("none");
    expect(none.field).toBe("due_date");
  });
});

describe("resolveDateWindow stays dynamic", () => {
  it("recomputes a named preset from today rather than the stored snapshot", () => {
    const f = buildDatePreset("due_date", "this_week");
    expect(resolveDateWindow({ ...f, from: "2000-01-01", to: "2000-01-02" })).toEqual(namedWindow("this_week"));
  });
});

describe("buildAbsoluteSingle / round-trip", () => {
  it("on/before/after store the picked day and read it back", () => {
    const on = buildAbsoluteSingle("due_date", "on", "2026-06-10");
    expect(on).toMatchObject({ from: "2026-06-10", to: "2026-06-10" });
    expect(absoluteSingleDay(on)).toBe("2026-06-10");

    const before = buildAbsoluteSingle("due_date", "before", "2026-06-10");
    expect(before).toMatchObject({ from: FAR_PAST, to: "2026-06-10" });
    expect(absoluteSingleDay(before)).toBe("2026-06-10");

    const after = buildAbsoluteSingle("due_date", "after", "2026-06-10");
    expect(after).toMatchObject({ from: "2026-06-10", to: FAR_FUTURE });
    expect(absoluteSingleDay(after)).toBe("2026-06-10");
  });

  it("valueOptionIdForFilter classifies each shape", () => {
    expect(valueOptionIdForFilter(buildDatePreset("due_date", "this_month"))).toBe("this_month");
    expect(valueOptionIdForFilter(buildDatePreset("created_at", "any"))).toBe("any");
    expect(valueOptionIdForFilter(buildAbsoluteSingle("due_date", "before", "2026-06-10"))).toBe("before");
    expect(valueOptionIdForFilter(buildAbsoluteSingle("due_date", "after", "2026-06-10"))).toBe("after");
    expect(valueOptionIdForFilter(buildAbsoluteSingle("due_date", "on", "2026-06-10"))).toBe("on");
    expect(valueOptionIdForFilter({ field: "created_at", from: "2026-06-01", to: "2026-06-10", preset: "custom" })).toBe("range");
    expect(valueOptionIdForFilter({ field: "due_date", from: todayDateOnly(), to: todayDateOnly(), preset: "custom", relative: { direction: "past", amount: 7, unit: "day" } })).toBe("last_n");
  });

  it("catalog has the reference values without duplicate ids", () => {
    const ids = DATE_VALUE_OPTIONS.map((o) => o.id);
    expect(new Set(ids).size).toBe(ids.length);
    for (const id of ["today", "this_week", "this_month", "this_quarter", "this_year", "overdue", "last_n", "next_n", "on", "before", "after", "range"]) {
      expect(ids).toContain(id);
    }
  });
});

describe("matcher semantics", () => {
  it("Is set matches issues with the field; Is not set matches issues without", () => {
    const withDue = makeIssue({ due_date: todayDateOnly() });
    const noDue = makeIssue({ due_date: null });
    const isSet: IssueFilters = { ...NO_FILTER, dateFilters: [buildDatePreset("due_date", "any")] };
    const isNotSet: IssueFilters = { ...NO_FILTER, dateFilters: [buildDatePreset("due_date", "none")] };
    expect(filterIssues([withDue, noDue], isSet)).toEqual([withDue]);
    expect(filterIssues([withDue, noDue], isNotSet)).toEqual([noDue]);
  });

  it("negate inverts the window match (Is not)", () => {
    const dueToday = makeIssue({ due_date: todayDateOnly() });
    const dueNextYear = makeIssue({ id: "i-2", due_date: addDaysDateOnly(400) });
    const isToday: IssueFilters = { ...NO_FILTER, dateFilters: [buildDatePreset("due_date", "today")] };
    const isNotToday: IssueFilters = { ...NO_FILTER, dateFilters: [{ ...buildDatePreset("due_date", "today"), negate: true }] };
    expect(filterIssues([dueToday, dueNextYear], isToday)).toEqual([dueToday]);
    expect(filterIssues([dueToday, dueNextYear], isNotToday)).toEqual([dueNextYear]);
  });

  it("before is inclusive of the picked day", () => {
    const onDay = makeIssue({ due_date: "2026-06-10" });
    const dayAfter = makeIssue({ id: "i-2", due_date: "2026-06-11" });
    const before: IssueFilters = { ...NO_FILTER, dateFilters: [buildAbsoluteSingle("due_date", "before", "2026-06-10")] };
    expect(filterIssues([onDay, dayAfter], before)).toEqual([onDay]);
  });
});
