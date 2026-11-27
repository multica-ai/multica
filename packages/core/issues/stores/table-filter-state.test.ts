// @vitest-environment node
import { describe, expect, it } from "vitest";
import type { IssueTableFilters } from "../../types";
import { todayDateOnly } from "../date";
import { stateToTableFilters, tableFiltersToState } from "./table-filter-state";
import { createTransientIssueViewStore } from "./view-store";

describe("table filters ↔ view-store state", () => {
  it("round-trips every flat dimension, negation, properties and the advanced tree", () => {
    const filters: IssueTableFilters = {
      statuses: ["in_review"], priorities: ["high"], assignees: [{ type: "squad", id: "s1" }], include_no_assignee: true,
      creators: [{ type: "agent", id: "a1" }], project_ids: ["p1"], include_no_project: true, project_statuses: ["planned"],
      label_ids: ["l1"], relations: ["assigned"], negate: ["statuses", "label_ids"],
      properties: { unknown: [{ op: "contains", value: "keep" }] },
      advanced: { match: "any", children: [{ field: "priorities", values: ["urgent"] }, { match: "all", children: [{ field: "property:score", negate: true, values: [{ op: "gte", value: "7" }] }] }] },
      idle_only: true, include_sub_issues: false,
    };
    const state = tableFiltersToState(filters);
    expect(state).toMatchObject({ negatedFilters: ["status", "label"], agentIdleFilter: true, showSubIssues: false, unreadFilter: false });
    expect(stateToTableFilters(state)).toEqual(filters);
  });

  it("maps resolved running work to the server-side flag and leaves unread out until its membership is known", () => {
    const state = tableFiltersToState({ working_issue_ids: ["i1"], unread_issue_ids: ["i2"] });
    expect(state).toMatchObject({ agentRunningFilter: true, unreadFilter: true });
    expect(stateToTableFilters(state)).toEqual({ working_only: true });
    expect(stateToTableFilters(state, ["i2", "i2", "i3"])).toEqual({ working_only: true, unread_issue_ids: ["i2", "i3"] });
  });

  it("turns the exclusive date end into an inclusive day and back, ordering a reversed range", () => {
    const start = new Date(2026, 10, 20).toISOString(), end = new Date(2026, 10, 22).toISOString();
    const state = tableFiltersToState({ date: { field: "updated_at", start, end } });
    expect(state.dateFilter).toEqual({ field: "updated_at", from: "2026-11-20", to: "2026-11-21" });
    expect(stateToTableFilters({ ...state, dateFilter: { field: "updated_at", from: "2026-11-21", to: "2026-11-20" } }).date).toEqual({ field: "updated_at", start, end });
  });

  it("sends today only when a due-date bucket is in play, and drops blank advanced rows", () => {
    const base = tableFiltersToState({});
    expect(stateToTableFilters({ ...base, dueDateFilters: ["today"] })).toEqual({ due_dates: ["today"], today: todayDateOnly() });
    expect(stateToTableFilters({ ...base, advancedFilter: { id: "g", type: "group", match: "all", children: [{ id: "c", type: "condition", field: "status", values: [], negated: false }] } })).toEqual({});
  });

  it("creates a store that starts empty and writes nothing to storage", () => {
    const store = createTransientIssueViewStore();
    store.getState().toggleStatusFilter("todo");
    expect(store.getState().statusFilters).toEqual(["todo"]);
    expect(createTransientIssueViewStore().getState().statusFilters).toEqual([]);
  });
});
