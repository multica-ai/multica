// @vitest-environment node
import { describe, expect, it, beforeEach } from "vitest";
import { createStore, type StoreApi } from "zustand/vanilla";
import { mergeViewStatePersisted, viewStoreSlice, type IssueViewState } from "./view-store";
import { createFilterCondition, createFilterGroup } from "./filter-tree";
import { isIssueViewModified, issueViewSeedDefinition, modifiedFilterDimensions } from "../../issue-views/definition";

function newStore(): StoreApi<IssueViewState> {
  return createStore<IssueViewState>()((set) => viewStoreSlice(set));
}

describe("advanced filter mode", () => {
  let store: StoreApi<IssueViewState>;
  beforeEach(() => {
    store = newStore();
  });

  it("moves the simple filters into a tree and back", () => {
    const s = store.getState();
    s.toggleStatusFilter("done");
    s.setFilterNegated("status", true);
    s.toggleDueDateFilter("overdue");
    s.togglePropertyFilter("prop", "opt");
    store.getState().enterAdvancedFilter();
    const entered = store.getState();
    expect(entered.statusFilters).toEqual([]);
    expect(entered.dueDateFilters).toEqual([]);
    expect(entered.negatedFilters).toEqual([]);
    // Custom properties become conditions like the rest.
    expect(entered.propertyFilters).toEqual({});
    expect(entered.advancedFilter?.children).toHaveLength(3);
    expect(entered.advancedFilter?.children[2]).toMatchObject({ field: "property:prop", values: ["opt"] });

    store.getState().exitAdvancedFilter();
    const back = store.getState();
    expect(back.advancedFilter).toBeNull();
    expect(back.statusFilters).toEqual(["done"]);
    expect(back.negatedFilters).toEqual(["status"]);
    expect(back.dueDateFilters).toEqual(["overdue"]);
    expect(back.propertyFilters).toEqual({ prop: ["opt"] });
  });

  it("stays advanced when simple mode cannot say the tree", () => {
    store.getState().setAdvancedFilter(
      createFilterGroup("any", [
        createFilterCondition("status", ["todo"]),
        createFilterCondition("label", ["l1"]),
      ]),
    );
    store.getState().exitAdvancedFilter();
    expect(store.getState().advancedFilter?.match).toBe("any");
  });

  it("clears the tree with the rest of the filters", () => {
    store.getState().enterAdvancedFilter();
    store.getState().clearFilters();
    expect(store.getState().advancedFilter).toBeNull();
  });

  it("sanitizes persisted trees and due dates", () => {
    const merged = mergeViewStatePersisted(
      {
        dueDateFilters: ["today", "someday", "today"],
        advancedFilter: { type: "group", match: "any", children: [{ type: "condition", field: "status", values: ["todo"] }] },
      },
      store.getState(),
    );
    expect(merged.dueDateFilters).toEqual(["today"]);
    expect(merged.advancedFilter?.match).toBe("any");
    expect(merged.advancedFilter?.children[0]).toMatchObject({ field: "status", values: ["todo"], negated: false });
    expect(mergeViewStatePersisted({ advancedFilter: "junk" }, store.getState()).advancedFilter).toBeNull();
  });

  it("folds property filters persisted beside a tree into it", () => {
    const merged = mergeViewStatePersisted(
      {
        advancedFilter: { type: "group", match: "all", children: [{ type: "condition", field: "status", values: ["todo"] }] },
        propertyFilters: { prop: ["opt"] },
      },
      store.getState(),
    );
    expect(merged.propertyFilters).toEqual({});
    expect(merged.advancedFilter?.children[1]).toMatchObject({ field: "property:prop", values: ["opt"] });
  });
});

describe("saved views in advanced mode", () => {
  const view = {
    query: { statusFilters: ["todo"], labelFilters: ["l1"] },
    display: {},
  };
  function openView(v: { query: Record<string, unknown>; display: Record<string, unknown> }) {
    const store = newStore();
    const seed = issueViewSeedDefinition(v);
    store.setState({ ...seed.query, ...seed.display } as Partial<IssueViewState>);
    return store;
  }

  it("does not count switching modes as a change", () => {
    const store = openView(view);
    store.getState().enterAdvancedFilter();
    expect(isIssueViewModified(view, store.getState()).filters).toBe(false);
    expect(modifiedFilterDimensions(view, store.getState()).size).toBe(0);
  });

  it("does not count property filters moving into the tree as a change", () => {
    const withProperty = { query: { ...view.query, propertyFilters: { prop: ["opt"] } }, display: {} };
    const store = openView(withProperty);
    store.getState().enterAdvancedFilter();
    expect(store.getState().propertyFilters).toEqual({});
    expect(isIssueViewModified(withProperty, store.getState()).filters).toBe(false);
    expect(modifiedFilterDimensions(withProperty, store.getState()).size).toBe(0);
  });

  it("reports a tree simple mode cannot say as an advanced change", () => {
    const store = openView(view);
    store.getState().enterAdvancedFilter();
    const tree = store.getState().advancedFilter!;
    store.getState().setAdvancedFilter({ ...tree, match: "any" });
    expect(isIssueViewModified(view, store.getState()).filters).toBe(true);
    expect(modifiedFilterDimensions(view, store.getState()).has("advanced")).toBe(true);
  });

  it("reopens an advanced view unmodified", () => {
    const advancedView = {
      query: {
        advancedFilter: createFilterGroup("any", [
          createFilterCondition("status", ["todo"]),
          createFilterGroup("all", [createFilterCondition("label", ["l1"])]),
        ]),
      },
      display: {},
    };
    const store = openView(advancedView);
    expect(store.getState().advancedFilter?.match).toBe("any");
    expect(isIssueViewModified(advancedView, store.getState()).filters).toBe(false);
  });
});
