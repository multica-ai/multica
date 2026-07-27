// @vitest-environment jsdom
import { beforeEach, describe, expect, it } from "vitest";
import { getIssueSurfaceViewStore } from "./surface-view-store";
import type { IssueViewState } from "./view-store";

beforeEach(() => {
  // Fresh key per test so initial state is reproducible.
  // (No beforeEach needed — getIssueSurfaceViewStore creates per-key stores.)
});

function freshStore(): {
  getState: () => IssueViewState;
  setState: (
    partial: Partial<IssueViewState> | ((state: IssueViewState) => Partial<IssueViewState>),
  ) => void;
} {
  // Each test uses a unique key to avoid cross-test state leakage.
  const key = `test:reset:${Math.random().toString(36).slice(2)}`;
  return getIssueSurfaceViewStore(key);
}

describe("viewStoreSlice — statusFilters reset semantics", () => {
  // Regression: clicking the "X" next to the filter chip used to set
  // `statusFilters` to a 7-item default seed (all-but-archived), not to an
  // empty list. The visible result happened to be the same (archived was
  // excluded), but the filter UI showed 7 statuses still selected. The model
  // states should match: "no filter applied" means an EMPTY list; the server's
  // `include_archived=false` flag is what hides archived by default.
  it("initial statusFilters is an empty list (no implicit seed)", () => {
    const store = freshStore();
    expect(store.getState().statusFilters).toEqual([]);
  });

  it("clearFilters resets statusFilters to an empty list, not the 7-item seed", () => {
    const store = freshStore();
    // User actively selected archived + todo.
    store.getState().setStatusFilters(["archived", "todo"]);
    expect(store.getState().statusFilters).toEqual(["archived", "todo"]);
    store.getState().clearFilters();
    expect(store.getState().statusFilters).toEqual([]);
  });

  it("toggleStatusFilter on the last selected status leaves an empty list", () => {
    const store = freshStore();
    store.getState().toggleStatusFilter("todo");
    expect(store.getState().statusFilters).toEqual(["todo"]);
    store.getState().toggleStatusFilter("todo");
    expect(store.getState().statusFilters).toEqual([]);
  });

  it("Set View archived chip path surfaces archived rows", () => {
    const store = freshStore();
    // Simulate the View-archived chip click: setStatusFilters(["archived"]).
    store.getState().setStatusFilters(["archived"]);
    expect(store.getState().statusFilters).toEqual(["archived"]);
    expect(store.getState().statusFilters.includes("archived")).toBe(true);
  });
});
