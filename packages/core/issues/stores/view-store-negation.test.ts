// @vitest-environment node
import { describe, expect, it, beforeEach } from "vitest";
import { createStore, type StoreApi } from "zustand/vanilla";
import {
  effectiveNegatedFilters,
  mergeViewStatePersisted,
  viewStoreSlice,
  type IssueViewState,
} from "./view-store";

function newStore(): StoreApi<IssueViewState> {
  return createStore<IssueViewState>()((set) => viewStoreSlice(set));
}

describe("negated filters", () => {
  let store: StoreApi<IssueViewState>;
  beforeEach(() => {
    store = newStore();
  });

  it("flips a dimension to 'is not' and back without touching its values", () => {
    store.getState().toggleStatusFilter("done");
    store.getState().setFilterNegated("status", true);
    store.getState().setFilterNegated("status", true);
    expect(store.getState().negatedFilters).toEqual(["status"]);
    expect(store.getState().statusFilters).toEqual(["done"]);
    store.getState().setFilterNegated("status", false);
    expect(store.getState().negatedFilters).toEqual([]);
    expect(store.getState().statusFilters).toEqual(["done"]);
  });

  it("only counts a negation once the dimension has values", () => {
    store.getState().setFilterNegated("label", true);
    store.getState().setFilterNegated("assignee", true);
    store.getState().toggleNoAssignee();
    expect(effectiveNegatedFilters(store.getState())).toEqual(["assignee"]);
  });

  it("clears with its own dimension and with clearFilters", () => {
    store.getState().toggleStatusFilter("done");
    store.getState().toggleLabelFilter("l-1");
    store.getState().setFilterNegated("status", true);
    store.getState().setFilterNegated("label", true);
    store.getState().clearFilterDimension("status");
    expect(store.getState().negatedFilters).toEqual(["label"]);

    store.getState().clearFilters();
    expect(store.getState().negatedFilters).toEqual([]);
  });
});

describe("relation filters", () => {
  it("toggles relations and clears them as one dimension", () => {
    const store = newStore();
    store.getState().toggleRelationFilter("assigned");
    store.getState().toggleRelationFilter("my_agents");
    expect(store.getState().relationFilters).toEqual(["assigned", "my_agents"]);
    store.getState().toggleRelationFilter("assigned");
    expect(store.getState().relationFilters).toEqual(["my_agents"]);
    store.getState().setFilterNegated("relation", true);
    store.getState().clearFilterDimension("relation");
    expect(store.getState().relationFilters).toEqual([]);
    expect(store.getState().negatedFilters).toEqual([]);
  });
});

describe("mergeViewStatePersisted negation and relations", () => {
  const defaults = () => newStore().getState();

  it("keeps known members and drops the rest", () => {
    const merged = mergeViewStatePersisted(
      {
        relationFilters: ["assigned", "involved", 3],
        negatedFilters: ["status", "status", "date", "property:x", null],
      },
      defaults(),
    );
    expect(merged.relationFilters).toEqual(["assigned"]);
    expect(merged.negatedFilters).toEqual(["status"]);
  });

  it("falls back to no filter for a snapshot saved before the fields", () => {
    const merged = mergeViewStatePersisted({ statusFilters: ["done"] }, defaults());
    expect(merged.relationFilters).toEqual([]);
    expect(merged.negatedFilters).toEqual([]);
  });
});
