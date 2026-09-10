// @vitest-environment node
import { describe, expect, it, beforeEach } from "vitest";
import { createStore, type StoreApi } from "zustand/vanilla";
import {
  cardPropertyOptionsForView,
  sortOptionsForView,
  viewStoreSlice,
  type IssueViewState,
} from "./view-store";
import { baselineFromQuery } from "../../issue-views/baseline";

/**
 * Column visibility and the status filter used to be the same field. That was
 * only ever correct while a category held exactly one status; since MUL-6243 a
 * category can hold several, and the two questions have different answers.
 */
describe("column visibility vs status filter", () => {
  let store: StoreApi<IssueViewState>;
  beforeEach(() => {
    store = createStore<IssueViewState>()((set) => viewStoreSlice(set));
  });

  // The regression: hiding one column wrote the OTHER six built-in keys into
  // statusFilters, so the query then excluded every custom status too — hiding
  // Backlog silently dropped a QA card sitting in the In Review column.
  it("hiding a column does not touch the status filter", () => {
    store.getState().hideStatus("backlog");

    expect(store.getState().hiddenStatusCategories).toEqual([
      "cancelled",
      "backlog",
    ]);
    expect(store.getState().statusFilters).toEqual([]);
  });

  it("showing a column restores it without inventing a filter", () => {
    store.getState().hideStatus("backlog");
    store.getState().hideStatus("done");
    store.getState().showStatus("backlog");

    expect(store.getState().hiddenStatusCategories).toEqual([
      "cancelled",
      "done",
    ]);
    expect(store.getState().statusFilters).toEqual([]);
  });

  it("hiding the same column twice is idempotent", () => {
    store.getState().hideStatus("backlog");
    store.getState().hideStatus("backlog");

    expect(store.getState().hiddenStatusCategories).toEqual([
      "cancelled",
      "backlog",
    ]);
  });

  it("a custom status filter survives hiding and showing a column", () => {
    store.getState().toggleStatusFilter("qa");
    store.getState().hideStatus("backlog");
    store.getState().showStatus("backlog");

    expect(store.getState().statusFilters).toEqual(["qa"]);
  });

  it("clearing filters preserves column visibility", () => {
    store.getState().hideStatus("backlog");
    store.getState().showStatus("cancelled");
    store.getState().clearFilters();

    expect(store.getState().hiddenStatusCategories).toEqual(["backlog"]);
  });
});

describe("issue view defaults", () => {
  let store: StoreApi<IssueViewState>;
  beforeEach(() => {
    store = createStore<IssueViewState>()((set) => viewStoreSlice(set));
  });

  it("starts with newest-created issues first and a compact card", () => {
    const state = store.getState();

    expect([state.sortBy, state.sortDirection]).toEqual(["created_at", "desc"]);
    expect(state.cardProperties).toEqual({
      priority: true,
      description: false,
      assignee: true,
      startDate: false,
      dueDate: true,
      project: true,
      childProgress: true,
      labels: false,
    });
  });

  it("uses the semantic default direction when changing sort fields", () => {
    store.getState().setSortBy("updated_at");
    expect(store.getState().sortDirection).toBe("desc");

    store.getState().setSortBy("priority");
    expect(store.getState().sortDirection).toBe("asc");
  });

  it("remembers an explicit direction for each sort field", () => {
    store.getState().setSortBy("priority");
    store.getState().setSortDirection("desc");
    store.getState().setSortBy("created_at");
    store.getState().setSortBy("priority");

    expect(store.getState().sortDirection).toBe("desc");
  });

  it("leaves manual order when the board stops grouping by status", () => {
    store.getState().setSortBy("position");
    store.getState().setGrouping("assignee");

    expect(store.getState().sortBy).toBe("created_at");
    expect(store.getState().sortDirection).toBe("desc");
  });

  it("cannot restore manual order through a list-view round trip", () => {
    store.getState().setGrouping("assignee");
    store.getState().setViewMode("list");
    store.getState().setSortBy("position");
    store.getState().setViewMode("board");

    expect(store.getState().grouping).toBe("assignee");
    expect(store.getState().sortBy).toBe("created_at");
    expect(store.getState().sortDirection).toBe("desc");
  });

  it("only offers controls the active view can apply", () => {
    expect(
      cardPropertyOptionsForView("list").map((option) => option.key),
    ).not.toContain("description");
    expect(cardPropertyOptionsForView("gantt")).toEqual([]);
    expect(
      sortOptionsForView("board", "assignee").map((option) => option.value),
    ).not.toContain("position");
    expect(
      sortOptionsForView("list", "assignee").map((option) => option.value),
    ).not.toContain("position");
  });
});

describe("saved view baseline", () => {
  // The regression: the baseline dropped any status filter that was not one of
  // the 7 built-ins, so reopening a view saved with a custom status filter came
  // back showing MORE than it was saved with.
  it("keeps a custom status filter", () => {
    const baseline = baselineFromQuery({ statusFilters: ["in_review", "qa"] });

    expect(baseline.status.has("qa")).toBe(true);
    expect(baseline.status.has("in_review")).toBe(true);
  });

  it("still drops values it cannot represent", () => {
    const baseline = baselineFromQuery({ statusFilters: ["", "qa"] });

    expect([...baseline.status]).toEqual(["qa"]);
  });
});
